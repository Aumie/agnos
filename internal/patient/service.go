package patient

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/google/uuid"

	"hospital-middleware/internal/event"
	"hospital-middleware/internal/hospital"
)

const (
	DefaultPageSize = 20
	MaxPageSize     = 100
)

var (
	// ErrNotFound is returned by Repository lookups that find no row.
	ErrNotFound = errors.New("patient: not found")

	// ErrHISNoMatch is returned by a HISClient when the upstream HIS has
	// no record for the given id — distinct from a transport/adapter
	// error, and distinct from ErrNoAdapterRegistered below (required
	// test cases in api-spec.md treat these as different code paths).
	ErrHISNoMatch = errors.New("patient: HIS has no match for id")

	// ErrNoAdapterRegistered means hospitals.code exists in the DB but
	// his.Registry has no HISClient for it (a hospital onboarded
	// administratively before its HIS integration is coded) — see
	// api-spec.md's graceful-degrade behavior note.
	ErrNoAdapterRegistered = errors.New("patient: no HIS adapter registered for hospital")

	ErrHospitalNotFound = errors.New("patient: hospital not found")
)

// SearchFilter mirrors api-spec.md's /patient/search request — every
// field optional except pagination, which always has a value after
// ApplyPaginationDefaults.
type SearchFilter struct {
	NationalID  *string
	PassportID  *string
	FirstName   *string
	MiddleName  *string
	LastName    *string
	DateOfBirth *time.Time
	PhoneNumber *string
	Email       *string
	Page        int
	PageSize    int
}

// Repository is defined here, at the point of use — see
// docs/project-structure.md.
type Repository interface {
	// Search returns patients in hospitalID matching filter (paginated),
	// plus the total match count ignoring pagination. Name fields match
	// against both the _th and _en column, OR'd — see api-spec.md
	// "Name matching is language-agnostic."
	Search(ctx context.Context, hospitalID uuid.UUID, filter SearchFilter) (patients []Patient, total int, err error)
	// FindByNationalOrPassportID returns ErrNotFound if no patient in
	// hospitalID has national_id or passport_id equal to id.
	FindByNationalOrPassportID(ctx context.Context, hospitalID uuid.UUID, id string) (Patient, error)
	// Upsert inserts or updates by the dedupe keys in docs/er-diagram.md
	// (patient_hn, national_id, passport_id — all scoped to hospital_id).
	Upsert(ctx context.Context, p Patient) (Patient, error)
}

// HospitalLookup is defined here for the same reason. Distinct from
// staff.HospitalLookup (which looks up by code) — this package only ever
// has a hospital_id (from JWT claims) and needs the hospital's code to
// resolve a HIS adapter via HISRegistry below.
type HospitalLookup interface {
	// FindByID returns ErrHospitalNotFound if no hospital matches id.
	FindByID(ctx context.Context, id uuid.UUID) (hospital.Hospital, error)
}

// HISClient is one hospital's adapter to its external HIS. Implementations
// (e.g. a Hospital A client) live in the his package.
type HISClient interface {
	// SearchByID returns ErrHISNoMatch if the HIS has no record for id.
	SearchByID(ctx context.Context, id string) (Patient, error)
}

// HISRegistry resolves the HISClient for a hospital by code — not by
// hospital_id, since the registry is a static, source-code-defined set
// (hospital_a -> hospitala.Client) and a UUID generated at seed time isn't
// something a developer would hardcode as a map key.
type HISRegistry interface {
	// Resolve reports ok=false if no adapter is registered for code —
	// the graceful-degrade case, not an error.
	Resolve(code string) (HISClient, bool)
}

type Service struct {
	repo      Repository
	hospitals HospitalLookup
	his       HISRegistry
	events    EventPublishers
	logger    *log.Logger
}

func NewService(repo Repository, hospitals HospitalLookup, his HISRegistry, events EventPublishers, logger *log.Logger) *Service {
	event.RequireComplete(events)
	if logger == nil {
		logger = log.Default()
	}
	return &Service{repo: repo, hospitals: hospitals, his: his, events: events, logger: logger}
}

// Search implements api-spec.md POST /patient/search's behavior: an
// id-based filter (national_id/passport_id) triggers a live HIS sync if
// not already cached, then every filter is applied against Postgres —
// which is the only thing capable of the name/DOB/phone/email filters at
// all, since the upstream HIS only supports lookup-by-id.
func (s *Service) Search(ctx context.Context, hospitalID uuid.UUID, filter SearchFilter) ([]Patient, int, error) {
	filter = ApplyPaginationDefaults(filter)

	if id := syncCandidateID(filter); id != nil {
		if err := s.syncIfMissing(ctx, hospitalID, *id); err != nil {
			switch {
			case errors.Is(err, ErrHISNoMatch), errors.Is(err, ErrNoAdapterRegistered):
				// Both are graceful no-ops from the caller's perspective
				// — fall through to the plain DB search below, which
				// will simply find nothing new for this id.
			default:
				return nil, 0, err
			}
		}
	}

	return s.repo.Search(ctx, hospitalID, filter)
}

// syncCandidateID picks the id to sync against when both national_id and
// passport_id happen to be given — national_id takes precedence,
// arbitrarily but deterministically; Hospital A's contract only ever
// takes one id regardless.
func syncCandidateID(f SearchFilter) *string {
	if f.NationalID != nil {
		return f.NationalID
	}
	return f.PassportID
}

func (s *Service) syncIfMissing(ctx context.Context, hospitalID uuid.UUID, id string) error {
	_, err := s.repo.FindByNationalOrPassportID(ctx, hospitalID, id)
	if err == nil {
		return nil // already cached, nothing to sync
	}
	if !errors.Is(err, ErrNotFound) {
		return err
	}

	h, err := s.hospitals.FindByID(ctx, hospitalID)
	if err != nil {
		return err
	}

	client, ok := s.his.Resolve(h.Code)
	if !ok {
		s.logger.Printf("patient: no HIS adapter registered for hospital %s, skipping live sync for id %s", h.Code, id)
		return ErrNoAdapterRegistered
	}

	found, err := client.SearchByID(ctx, id)
	if errors.Is(err, ErrHISNoMatch) {
		return ErrHISNoMatch
	}
	if err != nil {
		// A genuine adapter/transport failure — the HIS integration is
		// flaky, not just "no match." Same failure-isolation rule as
		// SyncedEvent below: a publish error must never mask the
		// original sync error. Unlike SyncedEvent's success case, the
		// original error still propagates here — this is a real
		// failure the caller needs to see, unlike ErrHISNoMatch/
		// ErrNoAdapterRegistered's graceful no-op.
		if pubErr := s.events.SyncFailed.Publish(ctx, SyncFailedEvent{
			HospitalID: hospitalID,
			ID:         id,
			Err:        err.Error(),
			FailedAt:   time.Now(),
		}); pubErr != nil {
			s.logger.Printf("patient: failed to publish SyncFailedEvent for id %s: %v", id, pubErr)
		}
		return err
	}

	found.HospitalID = hospitalID
	found.SyncedAt = time.Now()
	upserted, err := s.repo.Upsert(ctx, found)
	if err != nil {
		return err
	}

	// A failure publishing this (optional, demo-purposed — see
	// docs/project-structure.md) event must never fail the search
	// request itself: the patient was already successfully synced and
	// upserted by this point. Log and swallow, don't propagate.
	if err := s.events.Synced.Publish(ctx, SyncedEvent{
		PatientID:  upserted.ID,
		HospitalID: hospitalID,
		PatientHN:  upserted.PatientHN,
		SyncedAt:   upserted.SyncedAt,
	}); err != nil {
		s.logger.Printf("patient: failed to publish SyncedEvent for patient %s: %v", upserted.ID, err)
	}

	return nil
}

func ApplyPaginationDefaults(f SearchFilter) SearchFilter {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.PageSize < 1 {
		f.PageSize = DefaultPageSize
	}
	if f.PageSize > MaxPageSize {
		f.PageSize = MaxPageSize
	}
	return f
}
