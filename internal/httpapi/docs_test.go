package httpapi

import (
	"net/http"
	"strings"
	"testing"
)

func TestDocs_SwaggerUIPage(t *testing.T) {
	env := newTestEnv()

	w := env.do(t, http.MethodGet, "/docs", nil, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "swagger-ui") {
		t.Error("expected the Swagger UI page to reference swagger-ui")
	}

	// StandaloneLayout + SwaggerUIStandalonePreset are what actually render
	// the "Authorize" button (where a Bearer token gets entered for
	// /patient/search) — SwaggerUIBundle's default BaseLayout silently
	// omits it even with a valid securitySchemes block in the spec. This
	// was missing once already; pin it so it can't regress silently.
	body := w.Body.String()
	if !strings.Contains(body, "swagger-ui-standalone-preset.js") {
		t.Error("expected the page to load swagger-ui-standalone-preset.js")
	}
	if !strings.Contains(body, "StandaloneLayout") {
		t.Error("expected the page to configure layout: 'StandaloneLayout'")
	}
}

func TestDocs_OpenAPISpec(t *testing.T) {
	env := newTestEnv()

	w := env.do(t, http.MethodGet, "/docs/openapi.yaml", nil, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "openapi:") {
		t.Error("expected the served file to be a valid-looking OpenAPI document")
	}
	if !strings.Contains(w.Body.String(), "/patient/search") {
		t.Error("expected the served spec to document /patient/search")
	}
}
