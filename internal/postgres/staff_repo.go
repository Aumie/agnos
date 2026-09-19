package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"hospital-middleware/internal/staff"
)

type StaffRepo struct {
	pool *pgxpool.Pool
}

func NewStaffRepo(pool *pgxpool.Pool) *StaffRepo {
	return &StaffRepo{pool: pool}
}

const staffColumns = `id, hospital_id, username, password_hash, refresh_token_hash, refresh_token_expires_at, created_at, updated_at`

type staffRow struct {
	ID                    uuid.UUID  `db:"id"`
	HospitalID            uuid.UUID  `db:"hospital_id"`
	Username              string     `db:"username"`
	PasswordHash          string     `db:"password_hash"`
	RefreshTokenHash      *string    `db:"refresh_token_hash"`
	RefreshTokenExpiresAt *time.Time `db:"refresh_token_expires_at"`
	CreatedAt             time.Time  `db:"created_at"`
	UpdatedAt             time.Time  `db:"updated_at"`
}

func (r staffRow) toDomain() staff.Staff {
	return staff.Staff{
		ID:                    r.ID,
		HospitalID:            r.HospitalID,
		Username:              r.Username,
		PasswordHash:          r.PasswordHash,
		RefreshTokenHash:      r.RefreshTokenHash,
		RefreshTokenExpiresAt: r.RefreshTokenExpiresAt,
		CreatedAt:             r.CreatedAt,
		UpdatedAt:             r.UpdatedAt,
	}
}

// Create relies on staff.Service having already pre-checked username
// uniqueness (see internal/staff/service.go), but the DB constraint is
// still the authoritative guard against the race window between that
// check and this insert — so a unique-violation here is translated back
// to staff.ErrUsernameTaken rather than surfacing as a raw DB error.
func (r *StaffRepo) Create(ctx context.Context, s staff.Staff) (staff.Staff, error) {
	rows, err := r.pool.Query(ctx, `
		INSERT INTO staff (hospital_id, username, password_hash)
		VALUES ($1, $2, $3)
		RETURNING `+staffColumns, s.HospitalID, s.Username, s.PasswordHash)
	if err != nil {
		return staff.Staff{}, fmt.Errorf("postgres: create staff: %w", err)
	}

	row, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[staffRow])
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return staff.Staff{}, staff.ErrUsernameTaken
		}
		return staff.Staff{}, fmt.Errorf("postgres: create staff: %w", err)
	}
	return row.toDomain(), nil
}

func (r *StaffRepo) FindByUsernameAndHospital(ctx context.Context, username string, hospitalID uuid.UUID) (staff.Staff, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+staffColumns+`
		FROM staff
		WHERE username = $1 AND hospital_id = $2
	`, username, hospitalID)
	if err != nil {
		return staff.Staff{}, fmt.Errorf("postgres: find staff by username: %w", err)
	}
	row, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[staffRow])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return staff.Staff{}, staff.ErrNotFound
		}
		return staff.Staff{}, fmt.Errorf("postgres: find staff by username: %w", err)
	}
	return row.toDomain(), nil
}

func (r *StaffRepo) FindByRefreshTokenHash(ctx context.Context, hash string) (staff.Staff, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+staffColumns+`
		FROM staff
		WHERE refresh_token_hash = $1
	`, hash)
	if err != nil {
		return staff.Staff{}, fmt.Errorf("postgres: find staff by refresh token: %w", err)
	}
	row, err := pgx.CollectExactlyOneRow(rows, pgx.RowToStructByName[staffRow])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return staff.Staff{}, staff.ErrNotFound
		}
		return staff.Staff{}, fmt.Errorf("postgres: find staff by refresh token: %w", err)
	}
	return row.toDomain(), nil
}

func (r *StaffRepo) UpdateRefreshToken(ctx context.Context, staffID uuid.UUID, hash *string, expiresAt *time.Time) error {
	cmd, err := r.pool.Exec(ctx, `
		UPDATE staff
		SET refresh_token_hash = $1, refresh_token_expires_at = $2, updated_at = now()
		WHERE id = $3
	`, hash, expiresAt, staffID)
	if err != nil {
		return fmt.Errorf("postgres: update refresh token: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return staff.ErrNotFound
	}
	return nil
}

// RotateRefreshToken is a compare-and-swap: the WHERE clause requires
// refresh_token_hash to still equal oldHash, so the write only takes effect
// if nobody else has already rotated this exact token out from under us
// since it was read. A single UPDATE statement is atomic in Postgres with
// no extra locking needed — two concurrent calls racing on the same oldHash
// can't both see RowsAffected() == 1, since the first one to commit changes
// the row that the second one's WHERE clause is matching against.
func (r *StaffRepo) RotateRefreshToken(ctx context.Context, staffID uuid.UUID, oldHash, newHash string, newExpiresAt time.Time) (bool, error) {
	cmd, err := r.pool.Exec(ctx, `
		UPDATE staff
		SET refresh_token_hash = $1, refresh_token_expires_at = $2, updated_at = now()
		WHERE id = $3 AND refresh_token_hash = $4
	`, newHash, newExpiresAt, staffID, oldHash)
	if err != nil {
		return false, fmt.Errorf("postgres: rotate refresh token: %w", err)
	}
	return cmd.RowsAffected() == 1, nil
}
