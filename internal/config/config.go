// Package config loads generic, app-wide runtime configuration from the
// environment — PORT, DATABASE_URL, JWT_SECRET. It deliberately does not
// know about any specific hospital's connection details; that's
// internal/his.LoadConfig's job, same reasoning as his.Config not
// depending on this package — each piece of config lives with the code
// that actually needs it.
package config

import (
	"fmt"
	"os"
)

type Config struct {
	Port        string
	DatabaseURL string
	JWTSecret   []byte
}

func Load() (Config, error) {
	cfg := Config{
		Port:        getEnv("PORT", "8080"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
	}

	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("config: DATABASE_URL is required")
	}

	secret := os.Getenv("JWT_SECRET")
	if secret == "" {
		return Config{}, fmt.Errorf("config: JWT_SECRET is required")
	}
	cfg.JWTSecret = []byte(secret)

	return cfg, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
