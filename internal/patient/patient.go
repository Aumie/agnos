// Package patient holds the Patient entity and the business logic that
// searches and syncs it. Search, Repository, HISClient, and EventPublisher
// live in service.go, defined at their point of use — see
// docs/project-structure.md.
package patient

import (
	"time"

	"github.com/google/uuid"
)

type Gender string

const (
	GenderMale   Gender = "M"
	GenderFemale Gender = "F"
)

// Patient is a direct, compatible superset of Hospital A's response body
// (FirstNameTH...Gender), plus HospitalID, SyncedAt, and audit timestamps
// that no HIS provides but the middleware needs. See docs/er-diagram.md.
type Patient struct {
	ID         uuid.UUID
	HospitalID uuid.UUID // scoping only — not part of any API response; the scope specifies no such field

	PatientHN string // hospital-internal patient number; NOT NULL, unique per hospital

	// Either or both may be set — Hospital A's contract allows lookup by
	// either national_id or passport_id.
	NationalID *string
	PassportID *string

	FirstNameTH  string
	MiddleNameTH *string
	LastNameTH   string
	FirstNameEN  string
	MiddleNameEN *string
	LastNameEN   string

	DateOfBirth *time.Time // calendar date only — no time-of-day/timezone significance, unlike the timestamptz fields below
	PhoneNumber *string
	Email       *string
	Gender      *Gender

	SyncedAt  time.Time // last upsert from HIS
	CreatedAt time.Time
	UpdatedAt time.Time
}
