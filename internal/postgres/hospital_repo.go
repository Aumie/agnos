package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"hospital-middleware/internal/hospital"
	"hospital-middleware/internal/patient"
	"hospital-middleware/internal/staff"
)

// HospitalRepo satisfies both patient.HospitalLookup (FindByID) and
// staff.HospitalLookup (FindByCode) — one type structurally implementing
// two consumer-owned interfaces, since both just need read access to the
// same hospitals table. Each method returns the sentinel its own
// interface's caller expects (patient.ErrHospitalNotFound vs
// staff.ErrHospitalNotFound) — they're deliberately separate sentinels
// per package, not a shared one, so this type has to know both.
type HospitalRepo struct {
	pool *pgxpool.Pool
}

func NewHospitalRepo(pool *pgxpool.Pool) *HospitalRepo {
	return &HospitalRepo{pool: pool}
}

type hospitalRow struct {
	ID        uuid.UUID `db:"id"`
	Code      string    `db:"code"`
	Name      string    `db:"name"`
	CreatedAt time.Time `db:"created_at"`
}

func (r hospitalRow) toDomain() hospital.Hospital {
	return hospital.Hospital{ID: r.ID, Code: r.Code, Name: r.Name, CreatedAt: r.CreatedAt}
}

func (r *HospitalRepo) FindByCode(ctx context.Context, code string) (hospital.Hospital, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, code, name, created_at FROM hospitals WHERE code = $1`, code)
	if err != nil {
		return hospital.Hospital{}, fmt.Errorf("postgres: find hospital by code: %w", err)
	}
	row, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[hospitalRow])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return hospital.Hospital{}, staff.ErrHospitalNotFound
		}
		return hospital.Hospital{}, fmt.Errorf("postgres: find hospital by code: %w", err)
	}
	return row.toDomain(), nil
}

func (r *HospitalRepo) FindByID(ctx context.Context, id uuid.UUID) (hospital.Hospital, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, code, name, created_at FROM hospitals WHERE id = $1`, id)
	if err != nil {
		return hospital.Hospital{}, fmt.Errorf("postgres: find hospital by id: %w", err)
	}
	row, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[hospitalRow])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return hospital.Hospital{}, patient.ErrHospitalNotFound
		}
		return hospital.Hospital{}, fmt.Errorf("postgres: find hospital by id: %w", err)
	}
	return row.toDomain(), nil
}
