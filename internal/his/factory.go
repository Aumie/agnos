package his

import "hospital-middleware/internal/patient"

// Factory constructs a HISClient for one hospital, given its base URL.
type Factory func(baseURL string) patient.HISClient

// BuildRegistry constructs a Registry from a set of hospital factories and
// their base URLs — the mechanism NewDefaultRegistry uses against the
// real `adapters` map (see adapters.go). Exposed separately so it's
// testable against a synthetic factories map without needing real
// hospital packages — see factory_test.go, which proves this scales to
// 100 registrations without needing 100 real hospital integrations.
func BuildRegistry(factories map[string]Factory, baseURLs map[string]string) *Registry {
	r := NewRegistry()
	for code, factory := range factories {
		r.Register(code, factory(baseURLs[code]))
	}
	return r
}
