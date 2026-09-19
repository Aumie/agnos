package httpapi

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"hospital-middleware/internal/auth"
)

const (
	ctxKeyStaffID    = "staff_id"
	ctxKeyHospitalID = "hospital_id"
)

// recoveryMiddleware replaces gin.Recovery(): the default recovery
// handler writes a plain-text 500, which breaks the {"error": {...}}
// envelope every other error response in this API uses. A panic should
// still look like every other internal error to a client.
func recoveryMiddleware() gin.HandlerFunc {
	return gin.CustomRecovery(func(c *gin.Context, recovered any) {
		respondError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
		c.Abort()
	})
}

// authMiddleware enforces "Authorization: Bearer <access_token>" per
// docs/api-spec.md, and stores the token's claims in the Gin context for
// handlers to read — e.g. patient/search's hospital scoping.
func authMiddleware(jwtSecret []byte) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		const prefix = "Bearer "
		if !strings.HasPrefix(header, prefix) || strings.TrimPrefix(header, prefix) == "" {
			respondError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "missing or malformed Authorization header")
			c.Abort()
			return
		}

		claims, err := auth.ParseAccessToken(jwtSecret, strings.TrimPrefix(header, prefix))
		if err != nil {
			respondError(c, http.StatusUnauthorized, "UNAUTHENTICATED", "invalid or expired access token")
			c.Abort()
			return
		}

		c.Set(ctxKeyStaffID, claims.Subject)
		c.Set(ctxKeyHospitalID, claims.HospitalID)
		c.Next()
	}
}
