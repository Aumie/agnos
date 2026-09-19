package event

import (
	"context"
	"strings"
	"testing"
)

type testPublisher interface {
	Publish(ctx context.Context, e testEvent) error
}

type testBundle struct {
	A testPublisher
	B testPublisher
}

func TestRequireComplete_PanicsOnNilField(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("expected a panic for the unset field B, got none")
		}
		msg, ok := r.(string)
		if !ok || !strings.Contains(msg, "testBundle.B") {
			t.Fatalf("panic message = %v, want it to name testBundle.B", r)
		}
	}()

	RequireComplete(testBundle{A: NewBus[testEvent]()}) // B left unset
}

func TestRequireComplete_NoPanicWhenAllFieldsSet(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("unexpected panic with every field wired: %v", r)
		}
	}()

	RequireComplete(testBundle{A: NewBus[testEvent](), B: NewBus[testEvent]()})
}
