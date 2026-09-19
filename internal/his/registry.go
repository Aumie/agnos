package his

import "hospital-middleware/internal/patient"

// Registry implements patient.HISRegistry: a static, source-defined set
// of adapters keyed by hospital code. Not built from the hospitals table
// at runtime — adding a new hospital's HIS integration means writing its
// client and registering it here in code, not a data-only change (see
// docs/api-spec.md's graceful-degrade behavior note under /patient/search
// for what happens when a hospital exists in the DB before that).
type Registry struct {
	clients map[string]patient.HISClient
}

func NewRegistry() *Registry {
	return &Registry{clients: map[string]patient.HISClient{}}
}

func (r *Registry) Register(code string, client patient.HISClient) {
	r.clients[code] = client
}

func (r *Registry) Resolve(code string) (patient.HISClient, bool) {
	c, ok := r.clients[code]
	return c, ok
}
