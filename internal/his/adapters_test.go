package his

import "testing"

func TestNewDefaultRegistry_ResolvesHospitalA(t *testing.T) {
	r := NewDefaultRegistry(LoadConfig())

	client, ok := r.Resolve("hospital_a")
	if !ok {
		t.Fatal("expected hospital_a to resolve from the default registry")
	}
	if client == nil {
		t.Fatal("resolved client should not be nil")
	}
}

func TestNewDefaultRegistry_UnknownHospitalStillUnresolved(t *testing.T) {
	r := NewDefaultRegistry(LoadConfig())

	_, ok := r.Resolve("hospital_z")
	if ok {
		t.Fatal("expected hospital_z to be unresolved")
	}
}

func TestLoadConfig_ReadsBaseURLForEveryHospital(t *testing.T) {
	t.Setenv("BASE_URL_HOSPITAL_A", "http://example.test:1234")

	cfg := LoadConfig()

	if cfg.HospitalBaseURLs["hospital_a"] != "http://example.test:1234" {
		t.Errorf("HospitalBaseURLs[hospital_a] = %q, want http://example.test:1234", cfg.HospitalBaseURLs["hospital_a"])
	}
}

func TestLoadConfig_DefaultsWhenUnset(t *testing.T) {
	cfg := LoadConfig()

	if cfg.HospitalBaseURLs["hospital_a"] == "" {
		t.Error("hospital_a's base URL should have a default when BASE_URL_HOSPITAL_A is unset")
	}
}
