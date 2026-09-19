package his

import (
	"os"
	"strings"

	"hospital-middleware/internal/his/hospitala"
	"hospital-middleware/internal/patient"
	// "hospital-middleware/internal/his/hospitalb" // hospital B, when it exists
)

// Config holds every hospital's connection details, keyed by code. Kept
// as an explicit value (rather than reading the environment directly in
// NewDefaultRegistry) so tests can construct one without touching the OS
// environment — see LoadConfig for the real, env-backed source of one.
type Config struct {
	HospitalBaseURLs map[string]string
}

// adapters is the explicit, single source of truth for every hospital
// this codebase supports — one entry per hospital, in one visible place.
// Adding a hospital means writing its package (internal/his/hospitalb,
// following the hospitala pattern) and adding one line here — nothing
// hidden in an init() function, no blank import required for it to take
// effect, nothing to forget that fails silently. A duplicate key here is
// a compile-time error (go vet catches duplicate map-literal keys), not a
// runtime one. See docs/DECISION_LOG.md for why this replaced an earlier
// init()-based self-registration attempt.
var adapters = map[string]Factory{
	"hospital_a": func(baseURL string) patient.HISClient { return hospitala.NewClient(baseURL, nil) },
	// "hospital_b": func(baseURL string) patient.HISClient { return hospitalb.NewClient(baseURL, nil) },
}

// LoadConfig reads every hospital's base URL from the environment, as
// BASE_URL_<CODE> (e.g. hospital_a -> BASE_URL_HOSPITAL_A). This is the
// one place — and the only place in the whole codebase — that knows that
// naming convention. internal/config does not: it stays generic/app-wide
// (PORT, DATABASE_URL, JWT_SECRET), same as `his` doesn't depend on it.
// Which hospitals to read comes from the adapters map above, not a
// separate hardcoded list.
func LoadConfig() Config {
	urls := map[string]string{}
	for code := range adapters {
		envVar := "BASE_URL_" + strings.ToUpper(code)
		urls[code] = getEnv(envVar, "http://mockserver:9000")
	}
	return Config{HospitalBaseURLs: urls}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// NewDefaultRegistry builds a Registry from every hospital listed in
// adapters above.
func NewDefaultRegistry(cfg Config) *Registry {
	return BuildRegistry(adapters, cfg.HospitalBaseURLs)
}
