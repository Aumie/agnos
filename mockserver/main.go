// mockserver stubs Hospital A's API (docs/api-spec.md) for local
// docker-compose dev and adapter integration tests — hospital-a.api.co.th
// isn't a real, reachable domain. No framework: one route, Go 1.22+'s
// stdlib ServeMux routing is enough.
package main

import (
	"encoding/json"
	"log"
	"net/http"
)

// patientResponse is Hospital A's exact 13-field response body.
type patientResponse struct {
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

// seedPatients is a small, fixed set keyed by the id a client looks up
// (national_id or passport_id) — enough to exercise the full sync flow
// against docker-compose locally.
var seedPatients = map[string]patientResponse{
	"1101234567890": {
		FirstNameTH: "สมชาย", LastNameTH: "ใจดี",
		FirstNameEN: "Somchai", LastNameEN: "Jaidee",
		DateOfBirth: "1985-04-12", PatientHN: "HN00123",
		NationalID:  "1101234567890",
		PhoneNumber: "0812345678", Email: "somchai@example.com", Gender: "M",
	},
	"AB1234567": {
		FirstNameTH: "สมหญิง", LastNameTH: "ดีใจ",
		FirstNameEN: "Somying", LastNameEN: "Deejai",
		DateOfBirth: "1990-11-03", PatientHN: "HN00456",
		PassportID:  "AB1234567",
		PhoneNumber: "0898765432", Email: "somying@example.com", Gender: "F",
	},
}

func searchHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, ok := seedPatients[id]
	if !ok {
		// See internal/his/hospitala/hospital_a.go: 404 is the assumed "no match"
		// contract, since the scope doesn't specify one — this must
		// match that assumption exactly.
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(p); err != nil {
		log.Printf("mockserver: encode response: %v", err)
	}
}

func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /patient/search/{id}", searchHandler)
	return mux
}

func main() {
	const addr = ":9000"
	log.Printf("mockserver: listening on %s", addr)
	if err := http.ListenAndServe(addr, newMux()); err != nil {
		log.Fatal(err)
	}
}
