package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSearchHandler_KnownID(t *testing.T) {
	srv := httptest.NewServer(newMux())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/patient/search/1101234567890")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got patientResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.PatientHN != "HN00123" {
		t.Errorf("PatientHN = %q, want HN00123", got.PatientHN)
	}
	if got.NationalID != "1101234567890" {
		t.Errorf("NationalID = %q", got.NationalID)
	}
}

func TestSearchHandler_KnownPassportID(t *testing.T) {
	srv := httptest.NewServer(newMux())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/patient/search/AB1234567")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got patientResponse
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.PatientHN != "HN00456" {
		t.Errorf("PatientHN = %q, want HN00456", got.PatientHN)
	}
}

func TestSearchHandler_UnknownID(t *testing.T) {
	srv := httptest.NewServer(newMux())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/patient/search/0000000000000")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
}
