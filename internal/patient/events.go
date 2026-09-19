package patient

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// SyncedEvent fires after a HIS-sourced upsert. It's the one deliberately
// "extra" piece in this codebase — see docs/project-structure.md for why
// it exists and why it stays this small.
type SyncedEvent struct {
	PatientID  uuid.UUID
	HospitalID uuid.UUID
	PatientHN  string
	SyncedAt   time.Time
}

// EventPublisher is defined here, in the package that needs it, not in
// event — same rule as Repository and HISClient. It takes the concrete
// SyncedEvent type rather than a generic Event marker, since a generic
// marker is only needed when one interface must cover several event
// types at once — see SyncFailedEvent below for how a second event type
// gets its own interface instead of widening this one.
type EventPublisher interface {
	Publish(ctx context.Context, event SyncedEvent) error
}

// SyncFailedEvent fires when a live HIS sync genuinely fails — a
// transport/adapter error, not ErrHISNoMatch (a valid negative result)
// and not ErrNoAdapterRegistered (a config gap, already logged
// separately). This is the one case with real operational value: it's
// what "the HIS integration is broken" looks like, distinct from
// "nothing to find."
type SyncFailedEvent struct {
	HospitalID uuid.UUID
	ID         string // the national_id/passport_id that was being synced
	Err        string
	FailedAt   time.Time
}

type SyncFailedEventPublisher interface {
	Publish(ctx context.Context, event SyncFailedEvent) error
}

// EventPublishers bundles every event Service publishes, so NewService
// doesn't grow one parameter per event type. Each field is still its own
// small, consumer-owned interface — this only groups them for wiring
// convenience, it isn't a generic Event abstraction.
type EventPublishers struct {
	Synced     EventPublisher
	SyncFailed SyncFailedEventPublisher
}
