package staff

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"hospital-middleware/internal/auth"
	"hospital-middleware/internal/event"
	"hospital-middleware/internal/hospital"
)

// ---- hand-written fakes (see docs/project-structure.md: small interfaces,
// fakes over gomock) ----

// fakeRepo is guarded by a mutex — unlike the rest of this package's fakes,
// it's exercised by concurrency tests (TestCreateStaff_ConcurrentDuplicateUsername_OnlyOneWins,
// TestRefreshToken_ConcurrentUseOfSameToken_OnlyOneWins) with real
// goroutines, so it needs to be genuinely safe for concurrent access rather
// than just single-threaded-test-shaped.
type fakeRepo struct {
	mu   sync.Mutex
	byID map[uuid.UUID]Staff
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{byID: map[uuid.UUID]Staff{}}
}

func (f *fakeRepo) Create(ctx context.Context, s Staff) (Staff, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, existing := range f.byID {
		if existing.Username == s.Username && existing.HospitalID == s.HospitalID {
			return Staff{}, ErrUsernameTaken
		}
	}
	f.byID[s.ID] = s
	return s, nil
}

func (f *fakeRepo) FindByUsernameAndHospital(ctx context.Context, username string, hospitalID uuid.UUID) (Staff, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.byID {
		if s.Username == username && s.HospitalID == hospitalID {
			return s, nil
		}
	}
	return Staff{}, ErrNotFound
}

func (f *fakeRepo) FindByRefreshTokenHash(ctx context.Context, hash string) (Staff, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, s := range f.byID {
		if s.RefreshTokenHash != nil && *s.RefreshTokenHash == hash {
			return s, nil
		}
	}
	return Staff{}, ErrNotFound
}

func (f *fakeRepo) UpdateRefreshToken(ctx context.Context, staffID uuid.UUID, hash *string, expiresAt *time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.byID[staffID]
	if !ok {
		return ErrNotFound
	}
	s.RefreshTokenHash = hash
	s.RefreshTokenExpiresAt = expiresAt
	f.byID[staffID] = s
	return nil
}

// RotateRefreshToken mirrors StaffRepo.RotateRefreshToken's compare-and-swap
// semantics: the whole read-compare-write sequence happens under the same
// lock, which is what makes it an atomic CAS rather than two racy steps —
// the same property a single Postgres UPDATE...WHERE gets for free.
func (f *fakeRepo) RotateRefreshToken(ctx context.Context, staffID uuid.UUID, oldHash, newHash string, newExpiresAt time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.byID[staffID]
	if !ok || s.RefreshTokenHash == nil || *s.RefreshTokenHash != oldHash {
		return false, nil
	}
	s.RefreshTokenHash = &newHash
	s.RefreshTokenExpiresAt = &newExpiresAt
	f.byID[staffID] = s
	return true, nil
}

type fakeHospitals struct {
	byCode map[string]hospital.Hospital
}

func newFakeHospitals(hs ...hospital.Hospital) *fakeHospitals {
	m := map[string]hospital.Hospital{}
	for _, h := range hs {
		m[h.Code] = h
	}
	return &fakeHospitals{byCode: m}
}

func (f *fakeHospitals) FindByCode(ctx context.Context, code string) (hospital.Hospital, error) {
	h, ok := f.byCode[code]
	if !ok {
		return hospital.Hospital{}, ErrHospitalNotFound
	}
	return h, nil
}

// ---- test fixtures ----

var testSecret = []byte("test-secret-do-not-use-in-prod")

func hospitalA() hospital.Hospital {
	return hospital.Hospital{ID: uuid.New(), Code: "hospital_a", Name: "Hospital A"}
}

func hospitalB() hospital.Hospital {
	return hospital.Hospital{ID: uuid.New(), Code: "hospital_b", Name: "Hospital B"}
}

// noopEventPublishers builds a real event.Bus[T] per event type — same
// approach as patient's tests — rather than hand-written no-op fakes;
// Bus[T] with zero subscribers already does nothing and returns nil.
func noopEventPublishers() EventPublishers {
	return EventPublishers{
		Created:       event.NewBus[CreatedEvent](),
		CreatedFailed: event.NewBus[CreatedFailedEvent](),
		Login:         event.NewBus[LoginEvent](),
		LoginFailed:   event.NewBus[LoginFailedEvent](),
		Refreshed:     event.NewBus[RefreshedEvent](),
		RefreshFailed: event.NewBus[RefreshFailedEvent](),
	}
}

func newService(hs ...hospital.Hospital) (*Service, *fakeRepo) {
	repo := newFakeRepo()
	svc := NewService(repo, newFakeHospitals(hs...), testSecret, noopEventPublishers(), nil)
	return svc, repo
}

// NewService must fail loudly at construction, not silently at the first
// Login/CreateStaff/RefreshToken call that happens to hit the unwired path
// — see event.RequireComplete.
func TestNewService_PanicsOnIncompleteEventPublishers(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected NewService to panic with LoginFailed left unset")
		}
	}()

	events := noopEventPublishers()
	events.LoginFailed = nil
	NewService(newFakeRepo(), newFakeHospitals(), testSecret, events, nil)
}

// ---- CreateStaff ----

func TestCreateStaff_Success(t *testing.T) {
	hA := hospitalA()
	svc, repo := newService(hA)

	got, err := svc.CreateStaff(context.Background(), "nurse_j", "S3cur3P@ss!", "hospital_a")
	if err != nil {
		t.Fatalf("CreateStaff: %v", err)
	}
	if got.Username != "nurse_j" {
		t.Errorf("Username = %q, want nurse_j", got.Username)
	}
	if got.HospitalID != hA.ID {
		t.Errorf("HospitalID = %v, want %v", got.HospitalID, hA.ID)
	}
	if got.ID == uuid.Nil {
		t.Error("ID should be generated, got uuid.Nil")
	}
	if got.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
	if got.PasswordHash == "" || got.PasswordHash == "S3cur3P@ss!" {
		t.Error("PasswordHash should be a hash, not empty or the plaintext")
	}
	if _, ok := repo.byID[got.ID]; !ok {
		t.Error("staff row should be persisted in the repository")
	}
}

func TestCreateStaff_DuplicateUsernameSameHospital(t *testing.T) {
	hA := hospitalA()
	svc, _ := newService(hA)
	ctx := context.Background()

	if _, err := svc.CreateStaff(ctx, "nurse_j", "S3cur3P@ss!", "hospital_a"); err != nil {
		t.Fatalf("first CreateStaff: %v", err)
	}

	_, err := svc.CreateStaff(ctx, "nurse_j", "AnotherPass1!", "hospital_a")
	if !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("got err %v, want ErrUsernameTaken", err)
	}
}

// TestCreateStaff_DuplicateUsernameSameHospital above proves the
// *sequential* case (create, then create again — rejected). CreateStaff's
// own pre-check (FindByUsernameAndHospital before Create) can't close the
// race between two concurrent creates that both pass that check before
// either writes — only the storage layer's uniqueness guard can (a real
// unique index in Postgres; fakeRepo.Create mirrors that check under its
// own mutex for this test). Exactly one of these must succeed.
func TestCreateStaff_ConcurrentDuplicateUsername_OnlyOneWins(t *testing.T) {
	svc, _ := newService(hospitalA())
	ctx := context.Background()

	const attempts = 20
	var wg sync.WaitGroup
	results := make([]error, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := svc.CreateStaff(ctx, "nurse_j", "S3cur3P@ss!", "hospital_a")
			results[i] = err
		}(i)
	}
	wg.Wait()

	var successes int
	for i, err := range results {
		if err == nil {
			successes++
		} else if !errors.Is(err, ErrUsernameTaken) {
			t.Fatalf("attempt %d: got err %v, want nil or ErrUsernameTaken", i, err)
		}
	}
	if successes != 1 {
		t.Fatalf("exactly 1 of %d concurrent creates of the same username should succeed, got %d", attempts, successes)
	}
}

func TestCreateStaff_SameUsernameDifferentHospital_Succeeds(t *testing.T) {
	hA, hB := hospitalA(), hospitalB()
	svc, _ := newService(hA, hB)
	ctx := context.Background()

	if _, err := svc.CreateStaff(ctx, "nurse_j", "S3cur3P@ss!", "hospital_a"); err != nil {
		t.Fatalf("CreateStaff in hospital_a: %v", err)
	}

	got, err := svc.CreateStaff(ctx, "nurse_j", "AnotherPass1!", "hospital_b")
	if err != nil {
		t.Fatalf("same username in hospital_b should succeed, got: %v", err)
	}
	if got.HospitalID != hB.ID {
		t.Errorf("HospitalID = %v, want %v", got.HospitalID, hB.ID)
	}
}

func TestCreateStaff_UnknownHospital(t *testing.T) {
	svc, _ := newService() // no hospitals registered

	_, err := svc.CreateStaff(context.Background(), "nurse_j", "S3cur3P@ss!", "hospital_a")
	if !errors.Is(err, ErrHospitalNotFound) {
		t.Fatalf("got err %v, want ErrHospitalNotFound", err)
	}
}

func TestCreateStaff_Validation(t *testing.T) {
	svc, _ := newService(hospitalA())

	cases := []struct {
		name, username, password, hospitalCode string
	}{
		{"empty username", "", "S3cur3P@ss!", "hospital_a"},
		{"empty password", "nurse_j", "", "hospital_a"},
		{"empty hospital", "nurse_j", "S3cur3P@ss!", ""},
		{"password too short", "nurse_j", "short1!", "hospital_a"},                   // 7 chars, < MinPasswordLength
		{"password too long", "nurse_j", "ThisPasswordIsWayTooLong1!", "hospital_a"}, // 26 chars, > MaxPasswordLength
		{"username too long", strings.Repeat("a", MaxUsernameLength+1), "S3cur3P@ss!", "hospital_a"},
		{"hospital code too long", "nurse_j", "S3cur3P@ss!", strings.Repeat("a", MaxHospitalCodeLength+1)},
		{"whitespace-only username", "   ", "S3cur3P@ss!", "hospital_a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.CreateStaff(context.Background(), tc.username, tc.password, tc.hospitalCode)
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("got err %v, want ErrValidation", err)
			}
		})
	}
}

func TestCreateStaff_TrimsUsernameAndHospitalCodeWhitespace(t *testing.T) {
	svc, _ := newService(hospitalA())

	got, err := svc.CreateStaff(context.Background(), "  nurse_j  ", "S3cur3P@ss!", "  hospital_a  ")
	if err != nil {
		t.Fatalf("CreateStaff: %v", err)
	}
	if got.Username != "nurse_j" {
		t.Errorf("Username = %q, want trimmed %q", got.Username, "nurse_j")
	}
}

// boundaryPassword builds an exactly-n-character password by repeating
// "Aa1!" and slicing — a mechanically exact length beats a hand-counted
// string literal (easy to miscount by one, which would silently defeat the
// very off-by-one test this is used for).
func boundaryPassword(n int) string {
	return strings.Repeat("Aa1!", n/4+1)[:n]
}

// Exact boundary values (MinPasswordLength, MaxPasswordLength) must be
// accepted — TestCreateStaff_Validation only proves values just outside the
// boundary are rejected; this proves the boundary itself is inclusive, not
// off-by-one in either direction.
func TestCreateStaff_PasswordLengthBoundaries_Accepted(t *testing.T) {
	cases := []struct {
		name, username string
		length         int
	}{
		{"exactly min length", "nurse_min", MinPasswordLength},
		{"exactly max length", "nurse_max", MaxPasswordLength},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := newService(hospitalA())
			if _, err := svc.CreateStaff(context.Background(), tc.username, boundaryPassword(tc.length), "hospital_a"); err != nil {
				t.Fatalf("CreateStaff with a %d-char password: %v", tc.length, err)
			}
		})
	}
}

// One character on either side of the boundary — TestCreateStaff_Validation
// already covers this with clearly-short/long values (7 and 26 chars); this
// pins the exact off-by-one edges specifically.
func TestCreateStaff_PasswordLengthBoundaries_RejectedOffByOne(t *testing.T) {
	cases := []struct {
		name   string
		length int
	}{
		{"one below min", MinPasswordLength - 1},
		{"one above max", MaxPasswordLength + 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _ := newService(hospitalA())
			_, err := svc.CreateStaff(context.Background(), "nurse_j", boundaryPassword(tc.length), "hospital_a")
			if !errors.Is(err, ErrValidation) {
				t.Fatalf("password length %d: got err %v, want ErrValidation", tc.length, err)
			}
		})
	}
}

// ---- Login ----

func setupLoggedInStaff(t *testing.T, svc *Service) {
	t.Helper()
	if _, err := svc.CreateStaff(context.Background(), "nurse_j", "S3cur3P@ss!", "hospital_a"); err != nil {
		t.Fatalf("setup CreateStaff: %v", err)
	}
}

func TestLogin_Success(t *testing.T) {
	hA := hospitalA()
	svc, _ := newService(hA)
	setupLoggedInStaff(t, svc)

	got, err := svc.Login(context.Background(), "nurse_j", "S3cur3P@ss!", "hospital_a")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if got.AccessToken == "" || got.RefreshToken == "" {
		t.Fatal("Login should return non-empty access and refresh tokens")
	}
	if got.ExpiresIn != int(auth.AccessTokenTTL.Seconds()) {
		t.Errorf("ExpiresIn = %d, want %d", got.ExpiresIn, int(auth.AccessTokenTTL.Seconds()))
	}

	claims, err := auth.ParseAccessToken(testSecret, got.AccessToken)
	if err != nil {
		t.Fatalf("issued access token should parse back: %v", err)
	}
	if claims.HospitalID != hA.ID {
		t.Errorf("access token hospital_id = %v, want %v", claims.HospitalID, hA.ID)
	}
}

func TestLogin_WrongHospital(t *testing.T) {
	hA, hB := hospitalA(), hospitalB()
	svc, _ := newService(hA, hB)
	setupLoggedInStaff(t, svc) // registered under hospital_a only

	_, err := svc.Login(context.Background(), "nurse_j", "S3cur3P@ss!", "hospital_b")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("got err %v, want ErrInvalidCredentials", err)
	}
}

func TestLogin_WrongPassword(t *testing.T) {
	svc, _ := newService(hospitalA())
	setupLoggedInStaff(t, svc)

	_, err := svc.Login(context.Background(), "nurse_j", "WrongPassword1!", "hospital_a")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("got err %v, want ErrInvalidCredentials", err)
	}
}

func TestLogin_UnknownUsername(t *testing.T) {
	svc, _ := newService(hospitalA())

	_, err := svc.Login(context.Background(), "does_not_exist", "S3cur3P@ss!", "hospital_a")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("got err %v, want ErrInvalidCredentials", err)
	}
}

func TestLogin_Validation(t *testing.T) {
	svc, _ := newService(hospitalA())

	cases := []struct{ username, password, hospitalCode string }{
		{"", "S3cur3P@ss!", "hospital_a"},
		{"nurse_j", "", "hospital_a"},
		{"nurse_j", "S3cur3P@ss!", ""},
		{strings.Repeat("a", MaxUsernameLength+1), "S3cur3P@ss!", "hospital_a"},
		{"nurse_j", "S3cur3P@ss!", strings.Repeat("a", MaxHospitalCodeLength+1)},
	}
	for _, tc := range cases {
		_, err := svc.Login(context.Background(), tc.username, tc.password, tc.hospitalCode)
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("got err %v, want ErrValidation", err)
		}
	}
}

// ---- RefreshToken ----

func TestRefreshToken_Success(t *testing.T) {
	svc, _ := newService(hospitalA())
	setupLoggedInStaff(t, svc)
	ctx := context.Background()

	loggedIn, err := svc.Login(ctx, "nurse_j", "S3cur3P@ss!", "hospital_a")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	refreshed, err := svc.RefreshToken(ctx, loggedIn.RefreshToken)
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}
	if refreshed.AccessToken == "" || refreshed.RefreshToken == "" {
		t.Fatal("RefreshToken should return a new non-empty token pair")
	}
	if refreshed.RefreshToken == loggedIn.RefreshToken {
		t.Fatal("refresh should rotate to a new refresh token, not reuse the old one")
	}
}

func TestRefreshToken_Expired(t *testing.T) {
	svc, repo := newService(hospitalA())
	setupLoggedInStaff(t, svc)
	ctx := context.Background()

	loggedIn, err := svc.Login(ctx, "nurse_j", "S3cur3P@ss!", "hospital_a")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	// force the stored expiry into the past
	for id, s := range repo.byID {
		past := time.Now().Add(-time.Minute)
		s.RefreshTokenExpiresAt = &past
		repo.byID[id] = s
	}

	_, err = svc.RefreshToken(ctx, loggedIn.RefreshToken)
	if !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("got err %v, want ErrInvalidRefreshToken", err)
	}
}

func TestRefreshToken_ReuseAfterRotation(t *testing.T) {
	svc, _ := newService(hospitalA())
	setupLoggedInStaff(t, svc)
	ctx := context.Background()

	loggedIn, err := svc.Login(ctx, "nurse_j", "S3cur3P@ss!", "hospital_a")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if _, err := svc.RefreshToken(ctx, loggedIn.RefreshToken); err != nil {
		t.Fatalf("first RefreshToken: %v", err)
	}

	// replay the original (now-rotated-away) refresh token
	_, err = svc.RefreshToken(ctx, loggedIn.RefreshToken)
	if !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("got err %v, want ErrInvalidRefreshToken (replay of a rotated token)", err)
	}
}

// TestRefreshToken_ReuseAfterRotation above proves the *sequential* replay
// case (use it, rotate, use the old one again — rejected). This proves the
// harder case: two callers presenting the *same not-yet-rotated* token at
// the same instant. Without RotateRefreshToken's compare-and-swap, both
// could pass FindByRefreshTokenHash before either writes, and both would
// mint a valid pair from a token meant to be single-use — exactly one
// caller here must succeed.
func TestRefreshToken_ConcurrentUseOfSameToken_OnlyOneWins(t *testing.T) {
	svc, _ := newService(hospitalA())
	setupLoggedInStaff(t, svc)
	ctx := context.Background()

	loggedIn, err := svc.Login(ctx, "nurse_j", "S3cur3P@ss!", "hospital_a")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}

	const attempts = 20
	var wg sync.WaitGroup
	results := make([]error, attempts)
	successes := make([]TokenPair, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			tokens, err := svc.RefreshToken(ctx, loggedIn.RefreshToken)
			results[i] = err
			successes[i] = tokens
		}(i)
	}
	wg.Wait()

	var winners int
	var winningPair TokenPair
	for i, err := range results {
		if err == nil {
			winners++
			winningPair = successes[i]
		} else if !errors.Is(err, ErrInvalidRefreshToken) {
			t.Fatalf("attempt %d: got err %v, want nil or ErrInvalidRefreshToken", i, err)
		}
	}
	if winners != 1 {
		t.Fatalf("exactly 1 of %d concurrent uses of the same token should succeed, got %d", attempts, winners)
	}

	// The winning refresh token must itself still work — proves the race
	// didn't leave the row in a state where nothing can refresh again.
	if _, err := svc.RefreshToken(ctx, winningPair.RefreshToken); err != nil {
		t.Fatalf("the winning refresh token should still be valid for a subsequent refresh, got: %v", err)
	}
}

func TestRefreshToken_Garbage(t *testing.T) {
	svc, _ := newService(hospitalA())

	_, err := svc.RefreshToken(context.Background(), "not-a-real-token")
	if !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("got err %v, want ErrInvalidRefreshToken", err)
	}
}

func TestRefreshToken_Empty(t *testing.T) {
	svc, _ := newService(hospitalA())

	_, err := svc.RefreshToken(context.Background(), "")
	if !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("got err %v, want ErrInvalidRefreshToken", err)
	}
}

// ---- event publishing ----

func TestCreateStaff_PublishesCreatedEventOnSuccess(t *testing.T) {
	hA := hospitalA()
	events := noopEventPublishers()
	var published []CreatedEvent
	createdBus := event.NewBus[CreatedEvent]()
	createdBus.Subscribe(func(ctx context.Context, e CreatedEvent) error {
		published = append(published, e)
		return nil
	})
	events.Created = createdBus

	svc := NewService(newFakeRepo(), newFakeHospitals(hA), testSecret, events, nil)

	if _, err := svc.CreateStaff(context.Background(), "nurse_j", "S3cur3P@ss!", "hospital_a"); err != nil {
		t.Fatalf("CreateStaff: %v", err)
	}

	if len(published) != 1 {
		t.Fatalf("expected exactly 1 CreatedEvent, got %d", len(published))
	}
	if published[0].Username != "nurse_j" || published[0].HospitalID != hA.ID {
		t.Errorf("unexpected event contents: %+v", published[0])
	}
}

func TestCreateStaff_PublishesCreatedFailedEvent_OnUsernameTaken(t *testing.T) {
	hA := hospitalA()
	events := noopEventPublishers()
	var published []CreatedFailedEvent
	failedBus := event.NewBus[CreatedFailedEvent]()
	failedBus.Subscribe(func(ctx context.Context, e CreatedFailedEvent) error {
		published = append(published, e)
		return nil
	})
	events.CreatedFailed = failedBus

	svc := NewService(newFakeRepo(), newFakeHospitals(hA), testSecret, events, nil)
	ctx := context.Background()

	if _, err := svc.CreateStaff(ctx, "nurse_j", "S3cur3P@ss!", "hospital_a"); err != nil {
		t.Fatalf("first CreateStaff: %v", err)
	}
	if _, err := svc.CreateStaff(ctx, "nurse_j", "AnotherPass1!", "hospital_a"); !errors.Is(err, ErrUsernameTaken) {
		t.Fatalf("got err %v, want ErrUsernameTaken", err)
	}

	if len(published) != 1 {
		t.Fatalf("expected exactly 1 CreatedFailedEvent, got %d", len(published))
	}
	if published[0].Username != "nurse_j" {
		t.Errorf("unexpected event contents: %+v", published[0])
	}
}

func TestLogin_PublishesLoginEventOnSuccess(t *testing.T) {
	hA := hospitalA()
	events := noopEventPublishers()
	var published []LoginEvent
	loginBus := event.NewBus[LoginEvent]()
	loginBus.Subscribe(func(ctx context.Context, e LoginEvent) error {
		published = append(published, e)
		return nil
	})
	events.Login = loginBus

	svc := NewService(newFakeRepo(), newFakeHospitals(hA), testSecret, events, nil)
	ctx := context.Background()
	setupLoggedInStaff(t, svc)

	if _, err := svc.Login(ctx, "nurse_j", "S3cur3P@ss!", "hospital_a"); err != nil {
		t.Fatalf("Login: %v", err)
	}

	if len(published) != 1 {
		t.Fatalf("expected exactly 1 LoginEvent, got %d", len(published))
	}
	if published[0].Username != "nurse_j" {
		t.Errorf("unexpected event contents: %+v", published[0])
	}
}

func TestLogin_PublishesLoginFailedEvent_OnWrongPassword(t *testing.T) {
	hA := hospitalA()
	events := noopEventPublishers()
	var published []LoginFailedEvent
	failedBus := event.NewBus[LoginFailedEvent]()
	failedBus.Subscribe(func(ctx context.Context, e LoginFailedEvent) error {
		published = append(published, e)
		return nil
	})
	events.LoginFailed = failedBus

	svc := NewService(newFakeRepo(), newFakeHospitals(hA), testSecret, events, nil)
	ctx := context.Background()
	setupLoggedInStaff(t, svc)

	if _, err := svc.Login(ctx, "nurse_j", "WrongPassword1!", "hospital_a"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("got err %v, want ErrInvalidCredentials", err)
	}

	if len(published) != 1 {
		t.Fatalf("expected exactly 1 LoginFailedEvent, got %d", len(published))
	}
	if published[0].Username != "nurse_j" {
		t.Errorf("unexpected event contents: %+v", published[0])
	}
}

func TestRefreshToken_PublishesRefreshedEventOnSuccess(t *testing.T) {
	hA := hospitalA()
	events := noopEventPublishers()
	var published []RefreshedEvent
	refreshedBus := event.NewBus[RefreshedEvent]()
	refreshedBus.Subscribe(func(ctx context.Context, e RefreshedEvent) error {
		published = append(published, e)
		return nil
	})
	events.Refreshed = refreshedBus

	svc := NewService(newFakeRepo(), newFakeHospitals(hA), testSecret, events, nil)
	ctx := context.Background()
	setupLoggedInStaff(t, svc)

	loggedIn, err := svc.Login(ctx, "nurse_j", "S3cur3P@ss!", "hospital_a")
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if _, err := svc.RefreshToken(ctx, loggedIn.RefreshToken); err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}

	if len(published) != 1 {
		t.Fatalf("expected exactly 1 RefreshedEvent, got %d", len(published))
	}
}

func TestRefreshToken_PublishesRefreshFailedEvent_OnInvalidToken(t *testing.T) {
	hA := hospitalA()
	events := noopEventPublishers()
	var published []RefreshFailedEvent
	failedBus := event.NewBus[RefreshFailedEvent]()
	failedBus.Subscribe(func(ctx context.Context, e RefreshFailedEvent) error {
		published = append(published, e)
		return nil
	})
	events.RefreshFailed = failedBus

	svc := NewService(newFakeRepo(), newFakeHospitals(hA), testSecret, events, nil)

	if _, err := svc.RefreshToken(context.Background(), "garbage-token"); !errors.Is(err, ErrInvalidRefreshToken) {
		t.Fatalf("got err %v, want ErrInvalidRefreshToken", err)
	}

	if len(published) != 1 {
		t.Fatalf("expected exactly 1 RefreshFailedEvent, got %d", len(published))
	}
}
