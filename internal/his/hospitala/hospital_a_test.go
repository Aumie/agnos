package hospitala

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"hospital-middleware/internal/patient"
)

func TestHospitalAClient_SearchByID_Success(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"first_name_th": "สมชาย",
			"middle_name_th": "",
			"last_name_th": "ใจดี",
			"first_name_en": "Somchai",
			"middle_name_en": "",
			"last_name_en": "Jaidee",
			"date_of_birth": "1985-04-12",
			"patient_hn": "HN00123",
			"national_id": "1234567890123",
			"passport_id": "",
			"phone_number": "0812345678",
			"email": "somchai@example.com",
			"gender": "M"
		}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, nil)
	got, err := client.SearchByID(context.Background(), "1234567890123")
	if err != nil {
		t.Fatalf("SearchByID: %v", err)
	}

	if gotPath != "/patient/search/1234567890123" {
		t.Errorf("request path = %q, want /patient/search/1234567890123", gotPath)
	}
	if got.PatientHN != "HN00123" {
		t.Errorf("PatientHN = %q, want HN00123", got.PatientHN)
	}
	if got.NationalID == nil || *got.NationalID != "1234567890123" {
		t.Errorf("NationalID = %v, want 1234567890123", got.NationalID)
	}
	if got.PassportID != nil {
		t.Errorf("PassportID should be nil for an empty string field, got %q", *got.PassportID)
	}
	if got.MiddleNameTH != nil {
		t.Errorf("MiddleNameTH should be nil for an empty string field, got %q", *got.MiddleNameTH)
	}
	if got.DateOfBirth == nil || got.DateOfBirth.Format("2006-01-02") != "1985-04-12" {
		t.Errorf("DateOfBirth = %v, want 1985-04-12", got.DateOfBirth)
	}
	if got.Gender == nil || *got.Gender != patient.GenderMale {
		t.Errorf("Gender = %v, want M", got.Gender)
	}
}

func TestHospitalAClient_SearchByID_EscapesSpecialCharactersInID(t *testing.T) {
	var gotPath, gotRawPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotRawPath = r.URL.EscapedPath()
		w.WriteHeader(http.StatusNotFound) // don't care about the body for this test
	}))
	defer srv.Close()

	client := NewClient(srv.URL, nil)
	// A passport_id-shaped id containing characters that would otherwise
	// alter the URL path if concatenated raw.
	weirdID := "AB/12?34#56"
	_, _ = client.SearchByID(context.Background(), weirdID)

	if gotPath != "/patient/search/"+weirdID {
		t.Errorf("server decoded path = %q, want /patient/search/%s (the id should arrive as one literal path segment)", gotPath, weirdID)
	}
	if gotRawPath == "/patient/search/"+weirdID {
		t.Errorf("raw escaped path %q should not equal the unescaped id — expected it to be percent-encoded", gotRawPath)
	}
}

func TestHospitalAClient_SearchByID_NotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, nil)
	_, err := client.SearchByID(context.Background(), "0000000000000")
	if !errors.Is(err, patient.ErrHISNoMatch) {
		t.Fatalf("got err %v, want patient.ErrHISNoMatch", err)
	}
}

func TestHospitalAClient_SearchByID_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := NewClient(srv.URL, nil)
	_, err := client.SearchByID(context.Background(), "1234567890123")
	if err == nil {
		t.Fatal("a 500 from Hospital A should return an error")
	}
	if errors.Is(err, patient.ErrHISNoMatch) {
		t.Fatal("a server error must not be mistaken for ErrHISNoMatch — different failure modes")
	}
}

func TestHospitalAClient_SearchByID_MalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{not valid json`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, nil)
	if _, err := client.SearchByID(context.Background(), "1234567890123"); err == nil {
		t.Fatal("malformed JSON should return an error")
	}
}

func TestHospitalAClient_SearchByID_InvalidDateOfBirth(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Every required field populated except date_of_birth — otherwise
		// the required-field check below would reject this payload first,
		// and the test would pass without ever exercising date parsing.
		w.Write([]byte(`{
			"patient_hn": "HN1", "first_name_th": "สมชาย", "last_name_th": "ใจดี",
			"first_name_en": "Somchai", "last_name_en": "Jaidee",
			"date_of_birth": "not-a-date"
		}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, nil)
	if _, err := client.SearchByID(context.Background(), "1234567890123"); err == nil {
		t.Fatal("an unparseable date_of_birth should return an error")
	}
}

// Required test case beyond api-spec.md's own list: a HIS response missing
// a NOT NULL column (migrations/000001_init.up.sql) must be rejected here,
// not surface as a raw Postgres constraint violation later at Upsert — see
// toDomain's comment.
func TestHospitalAClient_SearchByID_MissingRequiredField(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"missing patient_hn", `{"first_name_th":"สมชาย","last_name_th":"ใจดี","first_name_en":"Somchai","last_name_en":"Jaidee"}`},
		{"missing first_name_th", `{"patient_hn":"HN1","last_name_th":"ใจดี","first_name_en":"Somchai","last_name_en":"Jaidee"}`},
		{"missing last_name_en", `{"patient_hn":"HN1","first_name_th":"สมชาย","last_name_th":"ใจดี","first_name_en":"Somchai"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			client := NewClient(srv.URL, nil)
			if _, err := client.SearchByID(context.Background(), "1234567890123"); err == nil {
				t.Fatal("a response missing a required field should return an error, not reach Upsert with a value that violates a NOT NULL column")
			}
		})
	}
}

// Same rationale as the required-fields test: gender must be validated
// against the patients.gender CHECK (gender IN ('M','F')) constraint here,
// not left to fail as an unhandled constraint violation on upsert.
func TestHospitalAClient_SearchByID_InvalidGender(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
			"patient_hn": "HN1", "first_name_th": "สมชาย", "last_name_th": "ใจดี",
			"first_name_en": "Somchai", "last_name_en": "Jaidee",
			"gender": "O"
		}`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, nil)
	if _, err := client.SearchByID(context.Background(), "1234567890123"); err == nil {
		t.Fatal("an unrecognized gender value should return an error, not reach Upsert and violate the CHECK constraint")
	}
}
