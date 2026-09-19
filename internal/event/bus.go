// Package event provides generic in-process, synchronous publish/subscribe
// plumbing. It has no knowledge of any specific event type — each domain
// package defines its own event structs and its own EventPublisher
// interface (consumer-owned, per docs/project-structure.md), then
// instantiates its own Bus[T] to satisfy that interface structurally. No
// shared marker interface is needed even with multiple event types across
// multiple packages.
//
// Deliberately synchronous, no goroutines, no retry/backpressure handling
// — see docs/project-structure.md for why that's an intentional
// simplification, not an oversight.
//
// Today every event (patient's Synced/SyncFailed, staff's
// Created/CreatedFailed/Login/LoginFailed/Refreshed/RefreshFailed) has
// exactly one subscriber: internal/audit's logger. That's a deliberately
// minimal starting point, not a ceiling — Bus[T] already supports more:
// Subscribe can be called repeatedly, and Publish runs every registered
// subscriber in order. Realistic next subscribers, and why they'd attach
// here rather than get bolted directly onto staff/patient's own service
// methods:
//
//   - A real audit trail. internal/audit currently just log.Printf's each
//     event; a persisted `audit_log` table row per event (who searched
//     which patient, when, from which hospital) is often a compliance
//     requirement for a real hospital system, not just log lines.
//   - Search-index projection. patient.SyncedEvent reindexing into
//     Elasticsearch, or refreshing a pg_trgm index — decouples the write
//     path (upsert from HIS) from the search path. See docs/DECISION_LOG.md's
//     noted future upgrade for fuzzy name search.
//   - Anti-abuse. staff.LoginFailedEvent counted per username+hospital in
//     a sliding window, to trigger a lockout or alert on suspected
//     brute-forcing. Unlike audit logging, this one would be load-bearing
//     — a dropped publish means a real security signal got missed, not
//     just an absent log line — so it can't share today's
//     log-and-swallow-every-publish-error convention (see staff.Service's
//     publishLoginFailed and friends) without that convention being
//     rethought first.
//   - Cache invalidation. staff.RefreshedEvent/LoginEvent invalidating a
//     cached session/staff lookup, if a read-through cache ever sat in
//     front of StaffRepo.
//   - The actual inter-service contract, if this ever became real
//     microservices. Swap Bus[T] for a real broker (Kafka/NATS/SQS) and
//     these same event structs become messages a separate audit-service /
//     search-service / notification-service each consume independently —
//     nothing about the event shapes would need to change, only the
//     transport.
package event

import "context"

// Subscriber reacts to an event of type T.
type Subscriber[T any] func(ctx context.Context, event T) error

// Bus is a minimal in-process publisher for one event type. Each domain
// package instantiates its own, e.g. event.NewBus[patient.SyncedEvent]() —
// the instantiation itself is what satisfies that package's own
// EventPublisher interface, with zero explicit linkage.
type Bus[T any] struct {
	subscribers []Subscriber[T]
}

func NewBus[T any]() *Bus[T] {
	return &Bus[T]{}
}

// NewBusWithSubscriber is a convenience for the common case of a bus with
// exactly one subscriber known at construction time — collapses
// "NewBus[T](); Subscribe(fn)" into one line at each wiring call site in
// main.go, where this pattern repeats once per event type.
func NewBusWithSubscriber[T any](fn Subscriber[T]) *Bus[T] {
	b := NewBus[T]()
	b.Subscribe(fn)
	return b
}

// Subscribe registers fn to run on every future Publish call, in
// registration order. Not concurrency-safe — Subscribe is wiring-time
// setup (called from main.go), not a runtime operation.
func (b *Bus[T]) Subscribe(fn Subscriber[T]) {
	b.subscribers = append(b.subscribers, fn)
}

// Publish calls every subscriber in registration order, synchronously. A
// subscriber's error stops the chain and is returned as-is — a failure is
// surfaced to the publishing caller, never silently swallowed.
func (b *Bus[T]) Publish(ctx context.Context, e T) error {
	for _, sub := range b.subscribers {
		if err := sub(ctx, e); err != nil {
			return err
		}
	}
	return nil
}
