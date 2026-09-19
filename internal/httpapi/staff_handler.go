package httpapi

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"hospital-middleware/internal/staff"
)

type createStaffRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Hospital string `json:"hospital"`
}

// createStaffResponse's Hospital field echoes the request's code, trimmed
// the same way staff.Service.CreateStaff trims it before lookup/storage —
// see api-spec.md POST /staff/create: the scope specifies no output
// shape, so nothing beyond the (normalized) code and generated
// id/created_at is added.
type createStaffResponse struct {
	ID        string    `json:"id"`
	Username  string    `json:"username"`
	Hospital  string    `json:"hospital"`
	CreatedAt time.Time `json:"created_at"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Hospital string `json:"hospital"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
}

func createStaffHandler(svc *staff.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req createStaffRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "invalid request body")
			return
		}

		created, err := svc.CreateStaff(c.Request.Context(), req.Username, req.Password, req.Hospital)
		if err != nil {
			respondStaffError(c, err)
			return
		}

		c.JSON(http.StatusCreated, createStaffResponse{
			ID:        created.ID.String(),
			Username:  created.Username,
			Hospital:  strings.TrimSpace(req.Hospital),
			CreatedAt: created.CreatedAt,
		})
	}
}

func loginHandler(svc *staff.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req loginRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "invalid request body")
			return
		}

		tokens, err := svc.Login(c.Request.Context(), req.Username, req.Password, req.Hospital)
		if err != nil {
			respondStaffError(c, err)
			return
		}

		c.JSON(http.StatusOK, toTokenResponse(tokens))
	}
}

func refreshHandler(svc *staff.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req refreshRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "invalid request body")
			return
		}

		tokens, err := svc.RefreshToken(c.Request.Context(), req.RefreshToken)
		if err != nil {
			respondStaffError(c, err)
			return
		}

		c.JSON(http.StatusOK, toTokenResponse(tokens))
	}
}

func toTokenResponse(t staff.TokenPair) tokenResponse {
	return tokenResponse{
		AccessToken:  t.AccessToken,
		RefreshToken: t.RefreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    t.ExpiresIn,
	}
}

// respondStaffError maps staff.Service's sentinel errors to the exact
// status/code pairs in docs/api-spec.md's Errors tables.
func respondStaffError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, staff.ErrValidation):
		respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "invalid request")
	case errors.Is(err, staff.ErrUsernameTaken):
		respondError(c, http.StatusConflict, "USERNAME_TAKEN", "username already exists for that hospital")
	case errors.Is(err, staff.ErrHospitalNotFound):
		respondError(c, http.StatusUnprocessableEntity, "UNKNOWN_HOSPITAL", "hospital code not recognized")
	case errors.Is(err, staff.ErrInvalidCredentials):
		respondError(c, http.StatusUnauthorized, "INVALID_CREDENTIALS", "username or password is incorrect")
	case errors.Is(err, staff.ErrInvalidRefreshToken):
		respondError(c, http.StatusUnauthorized, "INVALID_REFRESH_TOKEN", "refresh token is invalid or expired")
	default:
		respondError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
	}
}
