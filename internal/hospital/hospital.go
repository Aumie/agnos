// Package hospital holds the Hospital entity: shared reference data that
// patient and staff both scope to, and that the his registry routes on via
// Code. It has no service/usecase of its own — hospitals aren't created or
// updated through any API in this scope, they're reference data staff
// and patients are validated against.
//
// Deliberately its own leaf package, depending on nothing else in this
// codebase: `his` already imports `patient` (to implement patient.HISClient/
// HISRegistry), so if Hospital lived in `his`, `patient` would need to
// import `his` too for its HospitalLookup interface — an import cycle. Only
// a dependency-free package can be shared by both.
package hospital

import (
	"time"

	"github.com/google/uuid"
)

type Hospital struct {
	ID        uuid.UUID
	Code      string // stable HIS-adapter routing key, e.g. "hospital_a"
	Name      string // display name, e.g. "Hospital A"
	CreatedAt time.Time
}
