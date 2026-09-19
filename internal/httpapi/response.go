// Package httpapi holds Gin route handlers, request/response DTOs, and
// the JWT auth middleware — translating HTTP <-> the patient/staff
// services. See docs/project-structure.md.
package httpapi

import "github.com/gin-gonic/gin"

// errorBody/errorResponse match the common error envelope in
// docs/api-spec.md: {"error": {"code": ..., "message": ...}}.
type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorResponse struct {
	Error errorBody `json:"error"`
}

func respondError(c *gin.Context, status int, code, message string) {
	c.JSON(status, errorResponse{Error: errorBody{Code: code, Message: message}})
}
