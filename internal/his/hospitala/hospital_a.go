// Package hospitala implements patient.HISClient for Hospital A. Named
// distinctly from internal/hospital (the shared Hospital entity package)
// to avoid a package-name collision between the two.
package hospitala

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"hospital-middleware/internal/patient"
)

// hospitalAResponse is the exact JSON shape of Hospital A's response body
// (docs/api-spec.md) — 13 fields, unexported. This wire format never
// leaves this file; toDomain converts it to the shared patient.Patient
// type — keeping the two separate means a HIS's wire shape can never leak
// into the domain type, and a second hospital's differently-shaped
// response just gets its own wire struct instead of reshaping this one.
type hospitalAResponse struct {
	FirstNameTH  string `json:"first_name_th"`
	MiddleNameTH string `json:"middle_name_th"`
	LastNameTH   string `json:"last_name_th"`
	FirstNameEN  string `json:"first_name_en"`
	MiddleNameEN string `json:"middle_name_en"`
	LastNameEN   string `json:"last_name_en"`
	DateOfBirth  string `json:"date_of_birth"`
	PatientHN    string `json:"patient_hn"`
	NationalID   string `json:"national_id"`
	PassportID   string `json:"passport_id"`
	PhoneNumber  string `json:"phone_number"`
	Email        string `json:"email"`
	Gender       string `json:"gender"`
}

// requiredNonEmpty lists the fields patients.<column> declares NOT NULL
// (migrations/000001_init.up.sql) that Hospital A's contract always
// populates. Rejecting a blank one here — instead of letting it reach
// Postgres — turns a HIS data-quality problem into the same handled
// "genuine adapter error" path SearchByID's caller already deals with
// (propagated error, SyncFailedEvent fired, no state corrupted), rather
// than an unhandled NOT NULL constraint violation surfacing as a raw 500.
func (r hospitalAResponse) toDomain() (patient.Patient, error) {
	required := map[string]string{
		"patient_hn":    r.PatientHN,
		"first_name_th": r.FirstNameTH,
		"last_name_th":  r.LastNameTH,
		"first_name_en": r.FirstNameEN,
		"last_name_en":  r.LastNameEN,
	}
	for field, value := range required {
		if value == "" {
			return patient.Patient{}, fmt.Errorf("hospital_a: response missing required field %q", field)
		}
	}

	p := patient.Patient{
		PatientHN:    r.PatientHN,
		NationalID:   nullIfEmpty(r.NationalID),
		PassportID:   nullIfEmpty(r.PassportID),
		FirstNameTH:  r.FirstNameTH,
		MiddleNameTH: nullIfEmpty(r.MiddleNameTH),
		LastNameTH:   r.LastNameTH,
		FirstNameEN:  r.FirstNameEN,
		MiddleNameEN: nullIfEmpty(r.MiddleNameEN),
		LastNameEN:   r.LastNameEN,
		PhoneNumber:  nullIfEmpty(r.PhoneNumber),
		Email:        nullIfEmpty(r.Email),
	}

	if r.DateOfBirth != "" {
		dob, err := time.Parse("2006-01-02", r.DateOfBirth)
		if err != nil {
			return patient.Patient{}, fmt.Errorf("hospital_a: parse date_of_birth %q: %w", r.DateOfBirth, err)
		}
		p.DateOfBirth = &dob
	}

	// The gender column has a CHECK (gender IN ('M','F')) constraint
	// (migrations/000001_init.up.sql) — validated here, not left to
	// surface as a raw constraint-violation 500 on upsert, for the same
	// reason as the required-fields check above.
	if r.Gender != "" {
		if r.Gender != string(patient.GenderMale) && r.Gender != string(patient.GenderFemale) {
			return patient.Patient{}, fmt.Errorf("hospital_a: response has unrecognized gender %q, want %q or %q", r.Gender, patient.GenderMale, patient.GenderFemale)
		}
		g := patient.Gender(r.Gender)
		p.Gender = &g
	}

	return p, nil
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// Client implements patient.HISClient against Hospital A's API:
// GET {baseURL}/patient/search/{id} — docs/api-spec.md.
type Client struct {
	baseURL string
	http    *http.Client
}

func NewClient(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{baseURL: baseURL, http: httpClient}
}

// SearchByID calls Hospital A's API. The scope specifies Hospital
// A's response body but not its status-code contract for a miss, so this
// is an explicit assumption, not an inferred one: 404 means no match
// (the conventional REST default), matched by what /mockserver
// implements — see docs/api-spec.md if this needs revisiting against a
// real Hospital A integration later.
func (c *Client) SearchByID(ctx context.Context, id string) (patient.Patient, error) {
	// PathEscape, not raw concatenation: passport_id (unlike national_id)
	// isn't guaranteed purely numeric, so an unescaped id could contain
	// characters (e.g. "/") that alter the request path instead of being
	// treated as one literal path segment.
	reqURL := c.baseURL + "/patient/search/" + url.PathEscape(id)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return patient.Patient{}, err
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return patient.Patient{}, fmt.Errorf("hospital_a: request failed: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		var body hospitalAResponse
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			return patient.Patient{}, fmt.Errorf("hospital_a: decode response: %w", err)
		}
		return body.toDomain()
	case http.StatusNotFound:
		return patient.Patient{}, patient.ErrHISNoMatch
	default:
		return patient.Patient{}, fmt.Errorf("hospital_a: unexpected status %s", resp.Status)
	}
}
