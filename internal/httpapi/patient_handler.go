package httpapi

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"hospital-middleware/internal/patient"
)

// maxSearchFieldLength bounds each optional filter field — these are
// otherwise-unbounded `text` columns (see migrations/000001_init.up.sql)
// and flow straight into a Postgres query, so an absurdly long value is
// pointless to accept even though pgx's parameterized queries make it safe
// rather than exploitable.
const maxSearchFieldLength = 200

// normalizeSearchField trims whitespace and treats an empty result as
// "not provided" (nil) — without this, a client sending `"national_id": ""`
// would still be treated as an id-based search: syncCandidateID only
// checks for a non-nil pointer, so an empty string would trigger a
// pointless live sync attempt against the HIS with an empty id. Reports
// ok=false if the trimmed value exceeds maxSearchFieldLength.
func normalizeSearchField(s *string) (result *string, ok bool) {
	if s == nil {
		return nil, true
	}
	trimmed := strings.TrimSpace(*s)
	if trimmed == "" {
		return nil, true
	}
	if len(trimmed) > maxSearchFieldLength {
		return nil, false
	}
	return &trimmed, true
}

// patientSearchRequest mirrors api-spec.md's /patient/search request —
// every field optional, no _th/_en split (that only exists in stored
// data — see api-spec.md "Name matching is language-agnostic").
type patientSearchRequest struct {
	NationalID  *string `json:"national_id"`
	PassportID  *string `json:"passport_id"`
	FirstName   *string `json:"first_name"`
	MiddleName  *string `json:"middle_name"`
	LastName    *string `json:"last_name"`
	DateOfBirth *string `json:"date_of_birth"`
	PhoneNumber *string `json:"phone_number"`
	Email       *string `json:"email"`
	Page        int     `json:"page"`
	PageSize    int     `json:"page_size"`
}

// patientResponse is exactly Hospital A's 13 response fields — no
// hospital field, no internal id/synced_at/created_at/updated_at. See
// api-spec.md /patient/search Response.
type patientResponse struct {
	PatientHN    string  `json:"patient_hn"`
	NationalID   *string `json:"national_id"`
	PassportID   *string `json:"passport_id"`
	FirstNameTH  string  `json:"first_name_th"`
	MiddleNameTH *string `json:"middle_name_th"`
	LastNameTH   string  `json:"last_name_th"`
	FirstNameEN  string  `json:"first_name_en"`
	MiddleNameEN *string `json:"middle_name_en"`
	LastNameEN   string  `json:"last_name_en"`
	DateOfBirth  *string `json:"date_of_birth"`
	PhoneNumber  *string `json:"phone_number"`
	Email        *string `json:"email"`
	Gender       *string `json:"gender"`
}

type patientSearchResponse struct {
	Data     []patientResponse `json:"data"`
	Page     int               `json:"page"`
	PageSize int               `json:"page_size"`
	Total    int               `json:"total"`
}

func searchPatientHandler(svc *patient.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req patientSearchRequest
		// Body is entirely optional (api-spec.md: empty body -> all
		// patients in the staff member's hospital). Checking
		// ContentLength > 0 to decide whether to bind is unreliable —
		// it's -1 for some chunked-transfer clients, which would
		// silently skip a body that's actually present. Instead, always
		// attempt the bind and only tolerate io.EOF specifically (Gin's
		// JSON decoder returns exactly that for a genuinely empty body);
		// any other decode error is real malformed JSON.
		if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
			respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "invalid request body")
			return
		}

		var dob *time.Time
		if req.DateOfBirth != nil && *req.DateOfBirth != "" {
			parsed, err := time.Parse("2006-01-02", *req.DateOfBirth)
			if err != nil {
				respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "date_of_birth must be YYYY-MM-DD")
				return
			}
			dob = &parsed
		}

		fields := []**string{&req.NationalID, &req.PassportID, &req.FirstName, &req.MiddleName, &req.LastName, &req.PhoneNumber, &req.Email}
		for _, f := range fields {
			normalized, ok := normalizeSearchField(*f)
			if !ok {
				respondError(c, http.StatusBadRequest, "VALIDATION_ERROR", "a search field exceeds the maximum length")
				return
			}
			*f = normalized
		}

		hospitalID, ok := hospitalIDFromContext(c)
		if !ok {
			// authMiddleware should make this unreachable — a defensive
			// 500 rather than a panic if that invariant is ever broken.
			respondError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
			return
		}

		filter := patient.ApplyPaginationDefaults(patient.SearchFilter{
			NationalID:  req.NationalID,
			PassportID:  req.PassportID,
			FirstName:   req.FirstName,
			MiddleName:  req.MiddleName,
			LastName:    req.LastName,
			DateOfBirth: dob,
			PhoneNumber: req.PhoneNumber,
			Email:       req.Email,
			Page:        req.Page,
			PageSize:    req.PageSize,
		})

		results, total, err := svc.Search(c.Request.Context(), hospitalID, filter)
		if err != nil {
			respondError(c, http.StatusInternalServerError, "INTERNAL_ERROR", "internal server error")
			return
		}

		data := make([]patientResponse, len(results))
		for i, p := range results {
			data[i] = toPatientResponse(p)
		}

		c.JSON(http.StatusOK, patientSearchResponse{
			Data:     data,
			Page:     filter.Page,
			PageSize: filter.PageSize,
			Total:    total,
		})
	}
}

func hospitalIDFromContext(c *gin.Context) (uuid.UUID, bool) {
	v, exists := c.Get(ctxKeyHospitalID)
	if !exists {
		return uuid.Nil, false
	}
	id, ok := v.(uuid.UUID)
	return id, ok
}

func toPatientResponse(p patient.Patient) patientResponse {
	var dob *string
	if p.DateOfBirth != nil {
		s := p.DateOfBirth.Format("2006-01-02")
		dob = &s
	}
	var gender *string
	if p.Gender != nil {
		s := string(*p.Gender)
		gender = &s
	}
	return patientResponse{
		PatientHN:    p.PatientHN,
		NationalID:   p.NationalID,
		PassportID:   p.PassportID,
		FirstNameTH:  p.FirstNameTH,
		MiddleNameTH: p.MiddleNameTH,
		LastNameTH:   p.LastNameTH,
		FirstNameEN:  p.FirstNameEN,
		MiddleNameEN: p.MiddleNameEN,
		LastNameEN:   p.LastNameEN,
		DateOfBirth:  dob,
		PhoneNumber:  p.PhoneNumber,
		Email:        p.Email,
		Gender:       gender,
	}
}
