package his

import (
	"context"
	"fmt"
	"testing"

	"hospital-middleware/internal/patient"
)

// recordingHISClient is a minimal patient.HISClient for testing
// BuildRegistry in isolation — it never calls a real hospital, it just
// records which base URL it was built with, so tests can confirm each
// synthetic hospital below gets wired to its own URL, not a neighbor's.
type recordingHISClient struct {
	baseURL string
}

func (s recordingHISClient) SearchByID(ctx context.Context, id string) (patient.Patient, error) {
	return patient.Patient{}, patient.ErrHISNoMatch
}

// TestBuildRegistry_ScalesTo100Hospitals proves the registration
// mechanism itself — not this codebase's real hospital count. Only
// hospital_a is a real integration (see the `adapters` map in
// adapters.go); this uses 100 synthetic factories against a fresh map, so
// it can't collide with or affect the real `adapters`.
func TestBuildRegistry_ScalesTo100Hospitals(t *testing.T) {
	factories := map[string]Factory{}
	baseURLs := map[string]string{}

	for i := 1; i <= 100; i++ {
		code := fmt.Sprintf("hospital_%03d", i)
		baseURLs[code] = fmt.Sprintf("http://hospital-%03d.example.test", i)
		factories[code] = func(baseURL string) patient.HISClient {
			return recordingHISClient{baseURL: baseURL}
		}
	}

	registry := BuildRegistry(factories, baseURLs)

	for i := 1; i <= 100; i++ {
		code := fmt.Sprintf("hospital_%03d", i)

		client, ok := registry.Resolve(code)
		if !ok {
			t.Fatalf("hospital %s should resolve after BuildRegistry", code)
		}

		stub, ok := client.(recordingHISClient)
		if !ok {
			t.Fatalf("hospital %s resolved to unexpected client type %T", code, client)
		}

		wantURL := baseURLs[code]
		if stub.baseURL != wantURL {
			t.Fatalf("hospital %s got base URL %q, want %q (its own, not a neighbor's)", code, stub.baseURL, wantURL)
		}
	}
}

func TestBuildRegistry_MissingBaseURL(t *testing.T) {
	// A code with no entry in baseURLs still builds — the factory just
	// receives an empty string. Whether that's valid is that specific
	// HISClient's problem, not BuildRegistry's.
	factories := map[string]Factory{
		"hospital_a": func(baseURL string) patient.HISClient {
			return recordingHISClient{baseURL: baseURL}
		},
	}

	registry := BuildRegistry(factories, map[string]string{})

	client, ok := registry.Resolve("hospital_a")
	if !ok {
		t.Fatal("hospital_a should still resolve even with no base URL configured")
	}
	if client.(recordingHISClient).baseURL != "" {
		t.Errorf("expected empty base URL, got %q", client.(recordingHISClient).baseURL)
	}
}
