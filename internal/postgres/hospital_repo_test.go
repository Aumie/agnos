package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"hospital-middleware/internal/patient"
	"hospital-middleware/internal/staff"
)

func TestHospitalRepo_FindByCode_Found(t *testing.T) {
	pool := setupTestPool(t)
	repo := NewHospitalRepo(pool)
	id := seedHospital(t, pool, "hospital_a", "Hospital A")

	h, err := repo.FindByCode(context.Background(), "hospital_a")
	if err != nil {
		t.Fatalf("FindByCode: %v", err)
	}
	if h.ID != id || h.Code != "hospital_a" || h.Name != "Hospital A" {
		t.Errorf("unexpected hospital: %+v", h)
	}
}

// FindByCode is staff.Service's lookup path, so a miss must return
// staff.ErrHospitalNotFound specifically — not patient's sentinel, and not a
// bare pgx.ErrNoRows leaking out of this package.
func TestHospitalRepo_FindByCode_NotFound(t *testing.T) {
	pool := setupTestPool(t)
	repo := NewHospitalRepo(pool)

	_, err := repo.FindByCode(context.Background(), "does_not_exist")
	if !errors.Is(err, staff.ErrHospitalNotFound) {
		t.Fatalf("got err %v, want staff.ErrHospitalNotFound", err)
	}
}

func TestHospitalRepo_FindByID_Found(t *testing.T) {
	pool := setupTestPool(t)
	repo := NewHospitalRepo(pool)
	id := seedHospital(t, pool, "hospital_a", "Hospital A")

	h, err := repo.FindByID(context.Background(), id)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if h.ID != id {
		t.Errorf("ID = %v, want %v", h.ID, id)
	}
}

// FindByID is patient.Service's lookup path (from a JWT's hospital_id claim),
// so a miss must return patient.ErrHospitalNotFound — the other sentinel.
func TestHospitalRepo_FindByID_NotFound(t *testing.T) {
	pool := setupTestPool(t)
	repo := NewHospitalRepo(pool)

	_, err := repo.FindByID(context.Background(), uuid.New())
	if !errors.Is(err, patient.ErrHospitalNotFound) {
		t.Fatalf("got err %v, want patient.ErrHospitalNotFound", err)
	}
}
