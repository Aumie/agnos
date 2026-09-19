package staff

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"

	"hospital-middleware/internal/auth"
	"hospital-middleware/internal/event"
	"hospital-middleware/internal/hospital"
)

const (
	MinPasswordLength = 10 // api-spec.md /staff/create field notes
	// MaxPasswordLength also keeps every password well under bcrypt's
	// 72-byte input limit (bcrypt silently truncates anything longer,
	// which would let two different passwords beyond byte 72 hash
	// identically and both authenticate).
	MaxPasswordLength = 20

	// MaxUsernameLength/MaxHospitalCodeLength bound otherwise-unbounded
	// `text` columns (see migrations/000001_init.up.sql) — without a cap,
	// a client could submit an arbitrarily large username/hospital code
	// that's accepted by Postgres but pointless and wasteful to store and
	// index. Password has no separate min/max content check beyond length
	// since bcrypt hashes it, but username/hospital feed straight into
	// indexed equality lookups.
	MaxUsernameLength     = 50
	MaxHospitalCodeLength = 50
)

var (
	// ErrNotFound is returned by Repository lookups that find no row —
	// the service translates it into the appropriate public error
	// (ErrUsernameTaken's absence, ErrInvalidCredentials, etc).
	ErrNotFound = errors.New("staff: not found")

	ErrUsernameTaken       = errors.New("staff: username already taken for this hospital")
	ErrHospitalNotFound    = errors.New("staff: hospital not found")
	ErrValidation          = errors.New("staff: validation failed")
	ErrInvalidCredentials  = errors.New("staff: invalid credentials")
	ErrInvalidRefreshToken = errors.New("staff: invalid refresh token")
)

// Repository is defined here, at the point of use, per this codebase's
// convention (see docs/project-structure.md) — not in a separate ports
// package.
type Repository interface {
	Create(ctx context.Context, s Staff) (Staff, error)
	// FindByUsernameAndHospital returns ErrNotFound if no staff member
	// matches.
	FindByUsernameAndHospital(ctx context.Context, username string, hospitalID uuid.UUID) (Staff, error)
	// FindByRefreshTokenHash returns ErrNotFound if hash matches no
	// staff member's current refresh token.
	FindByRefreshTokenHash(ctx context.Context, hash string) (Staff, error)
	// UpdateRefreshToken sets (or clears, if hash is nil) the staff
	// member's stored refresh token hash and expiry. Used for issuing a
	// token pair with nothing to compare-and-swap against (Login) — see
	// RotateRefreshToken for the case that needs one (RefreshToken).
	UpdateRefreshToken(ctx context.Context, staffID uuid.UUID, hash *string, expiresAt *time.Time) error
	// RotateRefreshToken atomically replaces oldHash with newHash/
	// newExpiresAt, but only if the row's current refresh_token_hash still
	// equals oldHash — a compare-and-swap. Reports ok=false (not an error)
	// if it no longer matches, meaning something else already consumed
	// oldHash first. This closes a TOCTOU race RefreshToken would
	// otherwise have: without it, two concurrent refreshes presenting the
	// same not-yet-rotated token could both pass FindByRefreshTokenHash
	// before either writes, and both would mint a valid pair from a token
	// that's supposed to be single-use.
	RotateRefreshToken(ctx context.Context, staffID uuid.UUID, oldHash, newHash string, newExpiresAt time.Time) (ok bool, err error)
}

// HospitalLookup is defined here for the same reason — staff.Service is
// the consumer, so it owns the interface it needs, even though hospital
// data is owned by the hospital package.
type HospitalLookup interface {
	// FindByCode returns ErrHospitalNotFound if no hospital matches code.
	FindByCode(ctx context.Context, code string) (hospital.Hospital, error)
}

type TokenPair struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int // access token lifetime in seconds
}

type Service struct {
	repo      Repository
	hospitals HospitalLookup
	jwtSecret []byte
	events    EventPublishers
	logger    *log.Logger
}

func NewService(repo Repository, hospitals HospitalLookup, jwtSecret []byte, events EventPublishers, logger *log.Logger) *Service {
	event.RequireComplete(events)
	if logger == nil {
		logger = log.Default()
	}
	return &Service{repo: repo, hospitals: hospitals, jwtSecret: jwtSecret, events: events, logger: logger}
}

// CreateStaff creates a new staff member with login credentials, scoped to
// hospitalCode. See api-spec.md POST /staff/create.
func (s *Service) CreateStaff(ctx context.Context, username, password, hospitalCode string) (Staff, error) {
	username = strings.TrimSpace(username)
	hospitalCode = strings.TrimSpace(hospitalCode)

	if username == "" || password == "" || hospitalCode == "" {
		s.publishCreatedFailed(ctx, hospitalCode, username, "validation failed: missing field")
		return Staff{}, ErrValidation
	}
	if len(password) < MinPasswordLength || len(password) > MaxPasswordLength {
		s.publishCreatedFailed(ctx, hospitalCode, username, "validation failed: password length")
		return Staff{}, ErrValidation
	}
	if len(username) > MaxUsernameLength || len(hospitalCode) > MaxHospitalCodeLength {
		s.publishCreatedFailed(ctx, hospitalCode, username, "validation failed: field too long")
		return Staff{}, ErrValidation
	}

	h, err := s.hospitals.FindByCode(ctx, hospitalCode)
	if err != nil {
		if errors.Is(err, ErrHospitalNotFound) {
			s.publishCreatedFailed(ctx, hospitalCode, username, "unknown hospital")
			return Staff{}, ErrHospitalNotFound
		}
		return Staff{}, err
	}

	_, err = s.repo.FindByUsernameAndHospital(ctx, username, h.ID)
	if err == nil {
		s.publishCreatedFailed(ctx, hospitalCode, username, "username already taken")
		return Staff{}, ErrUsernameTaken
	}
	if !errors.Is(err, ErrNotFound) {
		return Staff{}, err
	}

	passwordHash, err := auth.HashPassword(password)
	if err != nil {
		return Staff{}, err
	}

	now := time.Now()
	created, err := s.repo.Create(ctx, Staff{
		ID:           uuid.New(),
		HospitalID:   h.ID,
		Username:     username,
		PasswordHash: passwordHash,
		CreatedAt:    now,
		UpdatedAt:    now,
	})
	if err != nil {
		return Staff{}, err
	}

	if pubErr := s.events.Created.Publish(ctx, CreatedEvent{
		StaffID:    created.ID,
		HospitalID: created.HospitalID,
		Username:   created.Username,
		CreatedAt:  created.CreatedAt,
	}); pubErr != nil {
		s.logger.Printf("staff: failed to publish CreatedEvent for staff %s: %v", created.ID, pubErr)
	}

	return created, nil
}

// dummyPasswordHash is compared against when no real staff record was
// found (unknown hospital or unknown username), so Login always performs
// exactly one bcrypt comparison regardless of which lookup failed.
// Without this, a request for a nonexistent username returns faster than
// one for a real username with a wrong password — bcrypt is deliberately
// slow, so that latency gap is a timing side-channel an attacker can use
// to enumerate valid usernames (or hospital codes) even though the error
// message never reveals it. Computed once at package init, not per
// request.
var dummyPasswordHash = mustHashDummyPassword()

func mustHashDummyPassword() string {
	hash, err := auth.HashPassword("dummy-password-for-constant-time-login-checks")
	if err != nil {
		panic("staff: failed to precompute dummy password hash: " + err.Error())
	}
	return hash
}

// Login authenticates a staff member and issues a token pair. See
// api-spec.md POST /staff/login.
func (s *Service) Login(ctx context.Context, username, password, hospitalCode string) (TokenPair, error) {
	username = strings.TrimSpace(username)
	hospitalCode = strings.TrimSpace(hospitalCode)

	if username == "" || password == "" || hospitalCode == "" {
		return TokenPair{}, ErrValidation
	}
	if len(username) > MaxUsernameLength || len(hospitalCode) > MaxHospitalCodeLength {
		// No dummy-hash bcrypt comparison here deliberately: an
		// over-length username/hospital code can never match a real
		// row (both are bounded at write time by CreateStaff), so this
		// carries no username-enumeration signal beyond what an unknown
		// hospital/username already leaks via the same ErrValidation vs
		// ErrInvalidCredentials distinction the API already exposes.
		return TokenPair{}, ErrValidation
	}

	var st Staff
	var found bool

	h, err := s.hospitals.FindByCode(ctx, hospitalCode)
	switch {
	case err == nil:
		st, err = s.repo.FindByUsernameAndHospital(ctx, username, h.ID)
		switch {
		case err == nil:
			found = true
		case errors.Is(err, ErrNotFound):
			// fall through with found=false
		default:
			return TokenPair{}, err
		}
	case errors.Is(err, ErrHospitalNotFound):
		// fall through with found=false — unknown hospital must look
		// identical to wrong credentials, never reveal which part of
		// the request was wrong.
	default:
		return TokenPair{}, err
	}

	hash := dummyPasswordHash
	if found {
		hash = st.PasswordHash
	}

	// Always exactly one bcrypt comparison, whether or not a real staff
	// record was found — see dummyPasswordHash above. Everything after
	// this point (which event gets published) runs after the
	// constant-time-sensitive step, so it doesn't reopen the timing
	// side-channel this was built to close.
	pwErr := auth.ComparePassword(hash, password)
	if !found || pwErr != nil {
		s.publishLoginFailed(ctx, hospitalCode, username)
		return TokenPair{}, ErrInvalidCredentials
	}

	tokens, err := s.issueTokens(ctx, st)
	if err != nil {
		return TokenPair{}, err
	}

	if pubErr := s.events.Login.Publish(ctx, LoginEvent{
		StaffID:    st.ID,
		HospitalID: st.HospitalID,
		Username:   st.Username,
		LoggedInAt: time.Now(),
	}); pubErr != nil {
		s.logger.Printf("staff: failed to publish LoginEvent for staff %s: %v", st.ID, pubErr)
	}

	return tokens, nil
}

// RefreshToken exchanges a valid, unexpired refresh token for a new,
// rotated token pair. See api-spec.md POST /staff/refresh.
func (s *Service) RefreshToken(ctx context.Context, refreshToken string) (TokenPair, error) {
	if refreshToken == "" {
		s.publishRefreshFailed(ctx)
		return TokenPair{}, ErrInvalidRefreshToken
	}

	oldHash := auth.HashToken(refreshToken)
	st, err := s.repo.FindByRefreshTokenHash(ctx, oldHash)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			s.publishRefreshFailed(ctx)
			return TokenPair{}, ErrInvalidRefreshToken
		}
		return TokenPair{}, err
	}

	if st.RefreshTokenExpiresAt == nil || time.Now().After(*st.RefreshTokenExpiresAt) {
		s.publishRefreshFailed(ctx)
		return TokenPair{}, ErrInvalidRefreshToken
	}

	access, refresh, newHash, newExpiresAt, err := s.generateTokenPair(st)
	if err != nil {
		return TokenPair{}, err
	}

	// Compare-and-swap on oldHash, not a plain unconditional write — see
	// RotateRefreshToken's doc comment on the interface for the race this
	// closes. A lost race is indistinguishable from a sequential replay,
	// both from the caller's perspective and in what gets published.
	ok, err := s.repo.RotateRefreshToken(ctx, st.ID, oldHash, newHash, newExpiresAt)
	if err != nil {
		return TokenPair{}, err
	}
	if !ok {
		s.publishRefreshFailed(ctx)
		return TokenPair{}, ErrInvalidRefreshToken
	}

	tokens := TokenPair{AccessToken: access, RefreshToken: refresh, ExpiresIn: int(auth.AccessTokenTTL.Seconds())}

	if pubErr := s.events.Refreshed.Publish(ctx, RefreshedEvent{
		StaffID:     st.ID,
		HospitalID:  st.HospitalID,
		RefreshedAt: time.Now(),
	}); pubErr != nil {
		s.logger.Printf("staff: failed to publish RefreshedEvent for staff %s: %v", st.ID, pubErr)
	}

	return tokens, nil
}

// generateTokenPair signs a fresh access/refresh pair without persisting
// anything — issueTokens (Login) and RefreshToken each persist it
// differently (an unconditional write vs. a compare-and-swap), so the
// signing step is shared but the write isn't.
func (s *Service) generateTokenPair(st Staff) (access, refresh, refreshHash string, expiresAt time.Time, err error) {
	access, err = auth.IssueAccessToken(s.jwtSecret, st.ID, st.HospitalID)
	if err != nil {
		return "", "", "", time.Time{}, err
	}
	refresh, err = auth.GenerateRefreshToken()
	if err != nil {
		return "", "", "", time.Time{}, err
	}
	refreshHash = auth.HashToken(refresh)
	expiresAt = time.Now().Add(auth.RefreshTokenTTL)
	return access, refresh, refreshHash, expiresAt, nil
}

// issueTokens signs a fresh access/refresh pair and unconditionally sets it
// as the staff member's current refresh token — used by Login, which is
// establishing a fresh session rather than consuming an existing token, so
// there's nothing to compare-and-swap against (see RefreshToken for the
// case that needs one).
func (s *Service) issueTokens(ctx context.Context, st Staff) (TokenPair, error) {
	access, refresh, hash, expiresAt, err := s.generateTokenPair(st)
	if err != nil {
		return TokenPair{}, err
	}
	if err := s.repo.UpdateRefreshToken(ctx, st.ID, &hash, &expiresAt); err != nil {
		return TokenPair{}, err
	}
	return TokenPair{
		AccessToken:  access,
		RefreshToken: refresh,
		ExpiresIn:    int(auth.AccessTokenTTL.Seconds()),
	}, nil
}

// publishCreatedFailed, publishLoginFailed, and publishRefreshFailed all
// follow the same failure-isolation rule as patient.Service's event
// publishing: a publish error is logged and swallowed, never propagated
// — a bug in /audit must never affect the outcome of a real API call.

func (s *Service) publishCreatedFailed(ctx context.Context, hospitalCode, username, reason string) {
	if err := s.events.CreatedFailed.Publish(ctx, CreatedFailedEvent{
		HospitalCode: hospitalCode,
		Username:     username,
		Reason:       reason,
		AttemptedAt:  time.Now(),
	}); err != nil {
		s.logger.Printf("staff: failed to publish CreatedFailedEvent for username %s: %v", username, err)
	}
}

func (s *Service) publishLoginFailed(ctx context.Context, hospitalCode, username string) {
	if err := s.events.LoginFailed.Publish(ctx, LoginFailedEvent{
		HospitalCode: hospitalCode,
		Username:     username,
		AttemptedAt:  time.Now(),
	}); err != nil {
		s.logger.Printf("staff: failed to publish LoginFailedEvent for username %s: %v", username, err)
	}
}

func (s *Service) publishRefreshFailed(ctx context.Context) {
	if err := s.events.RefreshFailed.Publish(ctx, RefreshFailedEvent{
		AttemptedAt: time.Now(),
	}); err != nil {
		s.logger.Printf("staff: failed to publish RefreshFailedEvent: %v", err)
	}
}
