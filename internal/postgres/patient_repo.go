package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"hospital-middleware/internal/patient"
)

type PatientRepo struct {
	pool *pgxpool.Pool
}

func NewPatientRepo(pool *pgxpool.Pool) *PatientRepo {
	return &PatientRepo{pool: pool}
}

const patientColumns = `id, hospital_id, patient_hn, national_id, passport_id,
	first_name_th, middle_name_th, last_name_th,
	first_name_en, middle_name_en, last_name_en,
	date_of_birth, phone_number, email, gender,
	synced_at, created_at, updated_at`

type patientRow struct {
	ID           uuid.UUID  `db:"id"`
	HospitalID   uuid.UUID  `db:"hospital_id"`
	PatientHN    string     `db:"patient_hn"`
	NationalID   *string    `db:"national_id"`
	PassportID   *string    `db:"passport_id"`
	FirstNameTH  string     `db:"first_name_th"`
	MiddleNameTH *string    `db:"middle_name_th"`
	LastNameTH   string     `db:"last_name_th"`
	FirstNameEN  string     `db:"first_name_en"`
	MiddleNameEN *string    `db:"middle_name_en"`
	LastNameEN   string     `db:"last_name_en"`
	DateOfBirth  *time.Time `db:"date_of_birth"`
	PhoneNumber  *string    `db:"phone_number"`
	Email        *string    `db:"email"`
	Gender       *string    `db:"gender"`
	SyncedAt     time.Time  `db:"synced_at"`
	CreatedAt    time.Time  `db:"created_at"`
	UpdatedAt    time.Time  `db:"updated_at"`
}

func (r patientRow) toDomain() patient.Patient {
	p := patient.Patient{
		ID:           r.ID,
		HospitalID:   r.HospitalID,
		PatientHN:    r.PatientHN,
		NationalID:   r.NationalID,
		PassportID:   r.PassportID,
		FirstNameTH:  r.FirstNameTH,
		MiddleNameTH: r.MiddleNameTH,
		LastNameTH:   r.LastNameTH,
		FirstNameEN:  r.FirstNameEN,
		MiddleNameEN: r.MiddleNameEN,
		LastNameEN:   r.LastNameEN,
		DateOfBirth:  r.DateOfBirth,
		PhoneNumber:  r.PhoneNumber,
		Email:        r.Email,
		SyncedAt:     r.SyncedAt,
		CreatedAt:    r.CreatedAt,
		UpdatedAt:    r.UpdatedAt,
	}
	if r.Gender != nil {
		g := patient.Gender(*r.Gender)
		p.Gender = &g
	}
	return p
}

func genderColumnValue(g *patient.Gender) *string {
	if g == nil {
		return nil
	}
	s := string(*g)
	return &s
}

func (r *PatientRepo) FindByNationalOrPassportID(ctx context.Context, hospitalID uuid.UUID, id string) (patient.Patient, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+patientColumns+`
		FROM patients
		WHERE hospital_id = $1 AND (national_id = $2 OR passport_id = $2)
		LIMIT 1
	`, hospitalID, id)
	if err != nil {
		return patient.Patient{}, fmt.Errorf("postgres: find patient by id: %w", err)
	}
	row, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[patientRow])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return patient.Patient{}, patient.ErrNotFound
		}
		return patient.Patient{}, fmt.Errorf("postgres: find patient by id: %w", err)
	}
	return row.toDomain(), nil
}

// Upsert dedupes on (hospital_id, patient_hn) — the one dedupe key
// guaranteed to be present on every HIS-sourced row (national_id/
// passport_id may legitimately be absent). See docs/er-diagram.md.
func (r *PatientRepo) Upsert(ctx context.Context, p patient.Patient) (patient.Patient, error) {
	rows, err := r.pool.Query(ctx, `
		INSERT INTO patients (
			hospital_id, patient_hn, national_id, passport_id,
			first_name_th, middle_name_th, last_name_th,
			first_name_en, middle_name_en, last_name_en,
			date_of_birth, phone_number, email, gender, synced_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15
		)
		ON CONFLICT ON CONSTRAINT uq_patients_hospital_patient_hn DO UPDATE SET
			national_id    = EXCLUDED.national_id,
			passport_id    = EXCLUDED.passport_id,
			first_name_th  = EXCLUDED.first_name_th,
			middle_name_th = EXCLUDED.middle_name_th,
			last_name_th   = EXCLUDED.last_name_th,
			first_name_en  = EXCLUDED.first_name_en,
			middle_name_en = EXCLUDED.middle_name_en,
			last_name_en   = EXCLUDED.last_name_en,
			date_of_birth  = EXCLUDED.date_of_birth,
			phone_number   = EXCLUDED.phone_number,
			email          = EXCLUDED.email,
			gender         = EXCLUDED.gender,
			synced_at      = EXCLUDED.synced_at,
			updated_at     = now()
		RETURNING `+patientColumns,
		p.HospitalID, p.PatientHN, p.NationalID, p.PassportID,
		p.FirstNameTH, p.MiddleNameTH, p.LastNameTH,
		p.FirstNameEN, p.MiddleNameEN, p.LastNameEN,
		p.DateOfBirth, p.PhoneNumber, p.Email, genderColumnValue(p.Gender), p.SyncedAt,
	)
	if err != nil {
		return patient.Patient{}, fmt.Errorf("postgres: upsert patient: %w", err)
	}
	row, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[patientRow])
	if err != nil {
		return patient.Patient{}, fmt.Errorf("postgres: upsert patient: %w", err)
	}
	return row.toDomain(), nil
}

// escapeLikePattern escapes a value before it's wrapped in "%...%" for
// ILIKE, so a search term containing a literal "%" or "_" is matched
// literally rather than as a LIKE wildcard — without this, first_name="%"
// matches every patient and first_name="_aidee" matches any single
// character followed by "aidee", neither of which is what a staff member
// typing a literal name expects. Not a SQL-injection concern either way
// (the value is already a bound parameter, never concatenated into the
// query text) — this is about search-result correctness, not safety.
// Backslash is escaped first so a value ending in "\" (which would
// otherwise escape the pattern's own trailing "%") can't do that either.
func escapeLikePattern(s string) string {
	replacer := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return replacer.Replace(s)
}

// Search builds its WHERE clause dynamically from whichever filter fields
// are set. Name fields match both the _th and _en column, OR'd, per
// api-spec.md "Name matching is language-agnostic".
func (r *PatientRepo) Search(ctx context.Context, hospitalID uuid.UUID, filter patient.SearchFilter) ([]patient.Patient, int, error) {
	clauses := []string{"hospital_id = $1"}
	args := []any{hospitalID}

	addEq := func(column string, value any) {
		args = append(args, value)
		clauses = append(clauses, fmt.Sprintf("%s = $%d", column, len(args)))
	}
	addNameILike := func(thColumn, enColumn, value string) {
		args = append(args, "%"+escapeLikePattern(value)+"%")
		n := len(args)
		// Postgres's default LIKE/ILIKE escape character is backslash, so
		// escapeLikePattern's escaping works with no ESCAPE clause needed.
		clauses = append(clauses, fmt.Sprintf("(%s ILIKE $%d OR %s ILIKE $%d)", thColumn, n, enColumn, n))
	}

	if filter.NationalID != nil {
		addEq("national_id", *filter.NationalID)
	}
	if filter.PassportID != nil {
		addEq("passport_id", *filter.PassportID)
	}
	if filter.FirstName != nil {
		addNameILike("first_name_th", "first_name_en", *filter.FirstName)
	}
	if filter.MiddleName != nil {
		addNameILike("middle_name_th", "middle_name_en", *filter.MiddleName)
	}
	if filter.LastName != nil {
		addNameILike("last_name_th", "last_name_en", *filter.LastName)
	}
	if filter.DateOfBirth != nil {
		addEq("date_of_birth", *filter.DateOfBirth)
	}
	if filter.PhoneNumber != nil {
		addEq("phone_number", *filter.PhoneNumber)
	}
	if filter.Email != nil {
		addEq("email", *filter.Email)
	}

	whereSQL := strings.Join(clauses, " AND ")

	var total int
	countSQL := "SELECT count(*) FROM patients WHERE " + whereSQL
	if err := r.pool.QueryRow(ctx, countSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("postgres: count patients: %w", err)
	}

	// A separate args slice for the paginated query, built from a copy of
	// the WHERE-clause args — appending limit/offset here can never
	// affect the count query above, which already ran.
	searchArgs := append(append([]any{}, args...), filter.PageSize, (filter.Page-1)*filter.PageSize)
	searchSQL := fmt.Sprintf(
		"SELECT %s FROM patients WHERE %s ORDER BY created_at DESC LIMIT $%d OFFSET $%d",
		patientColumns, whereSQL, len(args)+1, len(args)+2,
	)
	rows, err := r.pool.Query(ctx, searchSQL, searchArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("postgres: search patients: %w", err)
	}
	patientRows, err := pgx.CollectRows(rows, pgx.RowToStructByName[patientRow])
	if err != nil {
		return nil, 0, fmt.Errorf("postgres: search patients: %w", err)
	}

	results := make([]patient.Patient, len(patientRows))
	for i, pr := range patientRows {
		results[i] = pr.toDomain()
	}
	return results, total, nil
}
