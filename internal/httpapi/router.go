package httpapi

import (
	"github.com/gin-gonic/gin"

	"hospital-middleware/internal/patient"
	"hospital-middleware/internal/staff"
)

// NewRouter wires the 4 required endpoints (docs/api-spec.md) — no
// version prefix, routes match the scope's literal paths exactly
// (see openapi.yaml's servers block for why that matters).
func NewRouter(staffSvc *staff.Service, patientSvc *patient.Service, jwtSecret []byte) *gin.Engine {
	// Must be set before gin.New()/route registration — Gin's debug mode
	// (the default when nothing sets this) prints a [GIN-debug] line per
	// registered route plus a "switch to release mode in production"
	// warning at startup, in every environment including the real Cloud
	// Run deployment, since nothing was ever setting this before. Fixed
	// here rather than via the GIN_MODE env var so it's correct
	// unconditionally, in every environment this binary runs in, without
	// relying on docker-compose.yml/deploy-gcp.sh each remembering to set
	// it.
	gin.SetMode(gin.ReleaseMode)

	r := gin.New()
	r.Use(recoveryMiddleware())

	registerDocsRoutes(r)

	r.POST("/staff/create", createStaffHandler(staffSvc))
	r.POST("/staff/login", loginHandler(staffSvc))
	r.POST("/staff/refresh", refreshHandler(staffSvc))

	authorized := r.Group("/")
	authorized.Use(authMiddleware(jwtSecret))
	authorized.POST("/patient/search", searchPatientHandler(patientSvc))

	return r
}
