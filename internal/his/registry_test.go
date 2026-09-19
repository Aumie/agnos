package his

import (
	"context"
	"testing"

	"hospital-middleware/internal/patient"
)

type stubHISClient struct{}

func (stubHISClient) SearchByID(ctx context.Context, id string) (patient.Patient, error) {
	return patient.Patient{PatientHN: "stub"}, nil
}

func TestRegistry_ResolveRegistered(t *testing.T) {
	r := NewRegistry()
	r.Register("hospital_a", stubHISClient{})

	client, ok := r.Resolve("hospital_a")
	if !ok {
		t.Fatal("expected hospital_a to resolve")
	}
	if client == nil {
		t.Fatal("resolved client should not be nil")
	}
}

func TestRegistry_ResolveUnregistered(t *testing.T) {
	r := NewRegistry()

	_, ok := r.Resolve("hospital_z")
	if ok {
		t.Fatal("expected hospital_z to be unresolved (ok=false), not an error")
	}
}
