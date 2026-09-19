package event

import (
	"context"
	"errors"
	"testing"
)

type testEvent struct {
	value int
}

func TestBus_PublishWithNoSubscribers(t *testing.T) {
	b := NewBus[testEvent]()

	if err := b.Publish(context.Background(), testEvent{value: 1}); err != nil {
		t.Fatalf("Publish with no subscribers should not error, got: %v", err)
	}
}

func TestBus_PublishCallsSubscribersInOrder(t *testing.T) {
	b := NewBus[testEvent]()
	var got []int

	b.Subscribe(func(ctx context.Context, e testEvent) error {
		got = append(got, e.value)
		return nil
	})
	b.Subscribe(func(ctx context.Context, e testEvent) error {
		got = append(got, e.value*10)
		return nil
	})

	if err := b.Publish(context.Background(), testEvent{value: 3}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []int{3, 30}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("got %v, want %v", got, want)
	}
}

func TestBus_PublishStopsAtFirstSubscriberError(t *testing.T) {
	b := NewBus[testEvent]()
	wantErr := errors.New("boom")
	secondCalled := false

	b.Subscribe(func(ctx context.Context, e testEvent) error {
		return wantErr
	})
	b.Subscribe(func(ctx context.Context, e testEvent) error {
		secondCalled = true
		return nil
	})

	err := b.Publish(context.Background(), testEvent{value: 1})
	if !errors.Is(err, wantErr) {
		t.Fatalf("got err %v, want %v", err, wantErr)
	}
	if secondCalled {
		t.Fatal("second subscriber should not run after the first errors")
	}
}

func TestNewBusWithSubscriber_RegistersImmediately(t *testing.T) {
	var got int
	b := NewBusWithSubscriber(func(ctx context.Context, e testEvent) error {
		got = e.value
		return nil
	})

	if err := b.Publish(context.Background(), testEvent{value: 42}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 42 {
		t.Fatalf("got %d, want 42 — subscriber passed to NewBusWithSubscriber should already be registered", got)
	}
}

func TestBus_SatisfiesConsumerOwnedInterface(t *testing.T) {
	// Mirrors how a domain package (e.g. patient) defines its own
	// EventPublisher interface over its own concrete event type, and
	// event.Bus[T] satisfies it structurally with zero linkage.
	type publisher interface {
		Publish(ctx context.Context, e testEvent) error
	}

	var _ publisher = NewBus[testEvent]()
}
