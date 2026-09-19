package config

import "testing"

func TestLoad_Success(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("JWT_SECRET", "test-secret")
	t.Setenv("PORT", "9999")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != "9999" {
		t.Errorf("Port = %q, want 9999", cfg.Port)
	}
	if cfg.DatabaseURL != "postgres://localhost/test" {
		t.Errorf("DatabaseURL = %q", cfg.DatabaseURL)
	}
	if string(cfg.JWTSecret) != "test-secret" {
		t.Errorf("JWTSecret = %q", cfg.JWTSecret)
	}
}

func TestLoad_DefaultsPort(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")
	t.Setenv("JWT_SECRET", "test-secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Port != "8080" {
		t.Errorf("Port default = %q, want 8080", cfg.Port)
	}
}

func TestLoad_MissingDatabaseURL(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")

	if _, err := Load(); err == nil {
		t.Fatal("Load should fail without DATABASE_URL")
	}
}

func TestLoad_MissingJWTSecret(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://localhost/test")

	if _, err := Load(); err == nil {
		t.Fatal("Load should fail without JWT_SECRET")
	}
}
