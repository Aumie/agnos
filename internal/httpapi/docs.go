package httpapi

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"hospital-middleware/docs"
)

// swaggerUIHTML loads Swagger UI from a CDN rather than vendoring it —
// this route is a nice-to-have for browsing the API, not something worth
// a bundled dependency for.
const swaggerUIHTML = `<!DOCTYPE html>
<html>
<head>
  <title>Hospital Middleware API</title>
  <link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui.css" />
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-standalone-preset.js"></script>
  <script>
    window.onload = () => {
      // presets/layout matter here, not just cosmetics: the "Authorize"
      // button (where a Bearer token gets entered for /patient/search,
      // per openapi.yaml's bearerAuth securityScheme) is rendered by
      // SwaggerUIStandalonePreset's StandaloneLayout — SwaggerUIBundle's
      // default BaseLayout never renders it, even though the spec's
      // security schemes are otherwise read and applied correctly.
      window.ui = SwaggerUIBundle({
        url: '/docs/openapi.yaml',
        dom_id: '#swagger-ui',
        presets: [
          SwaggerUIBundle.presets.apis,
          SwaggerUIStandalonePreset,
        ],
        plugins: [
          SwaggerUIBundle.plugins.DownloadUrl,
        ],
        layout: 'StandaloneLayout',
      });
    };
  </script>
</body>
</html>`

// registerDocsRoutes serves a live Swagger UI page and the OpenAPI spec
// it reads from docs.OpenAPISpec (embedded at build time — see
// docs/embed.go), so docker-compose up alone gives a browsable API
// reference with no separate file to ship or keep in sync.
func registerDocsRoutes(r *gin.Engine) {
	r.GET("/docs", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(swaggerUIHTML))
	})
	r.GET("/docs/openapi.yaml", func(c *gin.Context) {
		c.Data(http.StatusOK, "application/yaml", docs.OpenAPISpec)
	})
}
