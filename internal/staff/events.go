package staff

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// CreatedEvent fires after a staff member is successfully created.
type CreatedEvent struct {
	StaffID    uuid.UUID
	HospitalID uuid.UUID
	Username   string
	CreatedAt  time.Time
}

type CreatedEventPublisher interface {
	Publish(ctx context.Context, event CreatedEvent) error
}

// CreatedFailedEvent fires when staff creation fails validation, hits an
// unknown hospital, or a duplicate username — audit-log value (who tried
// what, and why it didn't work), not client-facing; carries more detail
// than the API response does on purpose, same as LoginFailedEvent below.
type CreatedFailedEvent struct {
	HospitalCode string
	Username     string
	Reason       string
	AttemptedAt  time.Time
}

type CreatedFailedEventPublisher interface {
	Publish(ctx context.Context, event CreatedFailedEvent) error
}

// LoginEvent fires after a successful login.
type LoginEvent struct {
	StaffID    uuid.UUID
	HospitalID uuid.UUID
	Username   string
	LoggedInAt time.Time
}

type LoginEventPublisher interface {
	Publish(ctx context.Context, event LoginEvent) error
}

// LoginFailedEvent fires on any failed login attempt (unknown hospital,
// unknown username, or wrong password — Login's timing-attack mitigation
// deliberately makes these indistinguishable to the *caller*, but this
// event is server-side only, so it can safely carry what was actually
// attempted, for security monitoring — e.g. brute-force detection).
type LoginFailedEvent struct {
	HospitalCode string
	Username     string
	AttemptedAt  time.Time
}

type LoginFailedEventPublisher interface {
	Publish(ctx context.Context, event LoginFailedEvent) error
}

// RefreshedEvent fires after a successful token refresh.
type RefreshedEvent struct {
	StaffID     uuid.UUID
	HospitalID  uuid.UUID
	RefreshedAt time.Time
}

type RefreshedEventPublisher interface {
	Publish(ctx context.Context, event RefreshedEvent) error
}

// RefreshFailedEvent fires on any failed refresh attempt. No staff
// identity is available here by design — an invalid/expired/garbage
// token doesn't resolve to a staff member, that's the whole point of it
// being invalid — so this only records that an attempt happened.
type RefreshFailedEvent struct {
	AttemptedAt time.Time
}

type RefreshFailedEventPublisher interface {
	Publish(ctx context.Context, event RefreshFailedEvent) error
}

// EventPublishers bundles every event Service publishes, so NewService
// doesn't grow one parameter per event type. Each field is still its own
// small, consumer-owned interface — this only groups them for wiring
// convenience, it isn't a generic Event abstraction. Mirrors
// patient.EventPublishers.
type EventPublishers struct {
	Created       CreatedEventPublisher
	CreatedFailed CreatedFailedEventPublisher
	Login         LoginEventPublisher
	LoginFailed   LoginFailedEventPublisher
	Refreshed     RefreshedEventPublisher
	RefreshFailed RefreshFailedEventPublisher
}
