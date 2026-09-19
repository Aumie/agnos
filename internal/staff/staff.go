// Package staff holds the Staff entity and the business logic that creates
// and authenticates staff members. CreateStaff, Login, IssueToken, and the
// Repository interface live in service.go, defined at their point of use —
// see docs/project-structure.md.
package staff

import (
	"time"

	"github.com/google/uuid"
)

type Staff struct {
	ID         uuid.UUID
	HospitalID uuid.UUID // FK; /staff/create's response echoes back the submitted hospital code directly, no join needed

	Username     string // unique per HospitalID, not globally — see api-spec.md Assumption 2
	PasswordHash string // bcrypt, never the plaintext

	// RefreshTokenHash is nullable: single active refresh token per staff
	// member, not a sessions table — see api-spec.md Assumption 3.
	// RefreshTokenExpiresAt travels with it (both nil, or both set) — a
	// hash with no expiry would be unenforceable, since api-spec.md
	// commits to a 7-day refresh token lifetime.
	RefreshTokenHash      *string
	RefreshTokenExpiresAt *time.Time

	CreatedAt time.Time
	UpdatedAt time.Time
}
