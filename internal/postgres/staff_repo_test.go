package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"hospital-middleware/internal/staff"
)

func TestStaffRepo_CreateAndFindByUsernameAndHospital(t *testing.T) {
	pool := setupTestPool(t)
	repo := NewStaffRepo(pool)
	hospitalID := seedHospital(t, pool, "hospital_a", "Hospital A")

	created, err := repo.Create(context.Background(), staff.Staff{
		ID: uuid.New(), HospitalID: hospitalID,
		Username: "nurse_j", PasswordHash: "bcrypt-hash",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ID == uuid.Nil {
		t.Fatal("expected a non-nil ID on the returned row")
	}
	if created.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set by the DB default")
	}

	found, err := repo.FindByUsernameAndHospital(context.Background(), "nurse_j", hospitalID)
	if err != nil {
		t.Fatalf("FindByUsernameAndHospital: %v", err)
	}
	if found.ID != created.ID {
		t.Errorf("ID = %v, want %v", found.ID, created.ID)
	}
}

// Exercises the real DB constraint (uq_staff_hospital_username) and its
// translation back to staff.ErrUsernameTaken — see StaffRepo.Create's
// comment on why the service-level pre-check alone isn't the authoritative
// guard.
func TestStaffRepo_Create_DuplicateUsernameSameHospital(t *testing.T) {
	pool := setupTestPool(t)
	repo := NewStaffRepo(pool)
	hospitalID := seedHospital(t, pool, "hospital_a", "Hospital A")
	ctx := context.Background()

	if _, err := repo.Create(ctx, staff.Staff{ID: uuid.New(), HospitalID: hospitalID, Username: "nurse_j", PasswordHash: "hash1"}); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	_, err := repo.Create(ctx, staff.Staff{ID: uuid.New(), HospitalID: hospitalID, Username: "nurse_j", PasswordHash: "hash2"})
	if !errors.Is(err, staff.ErrUsernameTaken) {
		t.Fatalf("got err %v, want staff.ErrUsernameTaken (23505 translation)", err)
	}
}

func TestStaffRepo_Create_SameUsernameDifferentHospital_Succeeds(t *testing.T) {
	pool := setupTestPool(t)
	repo := NewStaffRepo(pool)
	hA := seedHospital(t, pool, "hospital_a", "Hospital A")
	hB := seedHospital(t, pool, "hospital_b", "Hospital B")
	ctx := context.Background()

	if _, err := repo.Create(ctx, staff.Staff{ID: uuid.New(), HospitalID: hA, Username: "nurse_j", PasswordHash: "hash"}); err != nil {
		t.Fatalf("Create in hospital_a: %v", err)
	}
	if _, err := repo.Create(ctx, staff.Staff{ID: uuid.New(), HospitalID: hB, Username: "nurse_j", PasswordHash: "hash"}); err != nil {
		t.Fatalf("same username in hospital_b should succeed (per-hospital uniqueness), got: %v", err)
	}
}

func TestStaffRepo_FindByUsernameAndHospital_NotFound(t *testing.T) {
	pool := setupTestPool(t)
	repo := NewStaffRepo(pool)
	hospitalID := seedHospital(t, pool, "hospital_a", "Hospital A")

	_, err := repo.FindByUsernameAndHospital(context.Background(), "ghost", hospitalID)
	if !errors.Is(err, staff.ErrNotFound) {
		t.Fatalf("got err %v, want staff.ErrNotFound", err)
	}
}

func TestStaffRepo_UpdateRefreshToken_ThenFindByHash(t *testing.T) {
	pool := setupTestPool(t)
	repo := NewStaffRepo(pool)
	hospitalID := seedHospital(t, pool, "hospital_a", "Hospital A")
	ctx := context.Background()

	created, err := repo.Create(ctx, staff.Staff{ID: uuid.New(), HospitalID: hospitalID, Username: "nurse_j", PasswordHash: "hash"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	hash := "deadbeefcafe"
	expiresAt := time.Now().Add(7 * 24 * time.Hour)
	if err := repo.UpdateRefreshToken(ctx, created.ID, &hash, &expiresAt); err != nil {
		t.Fatalf("UpdateRefreshToken: %v", err)
	}

	found, err := repo.FindByRefreshTokenHash(ctx, hash)
	if err != nil {
		t.Fatalf("FindByRefreshTokenHash: %v", err)
	}
	if found.ID != created.ID {
		t.Errorf("ID = %v, want %v", found.ID, created.ID)
	}
	if found.RefreshTokenExpiresAt == nil {
		t.Fatal("RefreshTokenExpiresAt should round-trip, got nil")
	}
}

// Rotation clears the old hash by writing nil — a stale hash must stop
// resolving immediately, otherwise a rotated-away token could still be
// looked up (the replay path api-spec.md's required test cases call out).
func TestStaffRepo_UpdateRefreshToken_ClearWithNil(t *testing.T) {
	pool := setupTestPool(t)
	repo := NewStaffRepo(pool)
	hospitalID := seedHospital(t, pool, "hospital_a", "Hospital A")
	ctx := context.Background()

	created, err := repo.Create(ctx, staff.Staff{ID: uuid.New(), HospitalID: hospitalID, Username: "nurse_j", PasswordHash: "hash"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	hash := "deadbeefcafe"
	expiresAt := time.Now().Add(time.Hour)
	if err := repo.UpdateRefreshToken(ctx, created.ID, &hash, &expiresAt); err != nil {
		t.Fatalf("UpdateRefreshToken (set): %v", err)
	}
	if err := repo.UpdateRefreshToken(ctx, created.ID, nil, nil); err != nil {
		t.Fatalf("UpdateRefreshToken (clear): %v", err)
	}

	_, err = repo.FindByRefreshTokenHash(ctx, hash)
	if !errors.Is(err, staff.ErrNotFound) {
		t.Fatalf("cleared refresh token hash should no longer resolve, got err %v", err)
	}
}

func TestStaffRepo_UpdateRefreshToken_UnknownStaff(t *testing.T) {
	pool := setupTestPool(t)
	repo := NewStaffRepo(pool)

	hash := "deadbeefcafe"
	expiresAt := time.Now().Add(time.Hour)
	err := repo.UpdateRefreshToken(context.Background(), uuid.New(), &hash, &expiresAt)
	if !errors.Is(err, staff.ErrNotFound) {
		t.Fatalf("got err %v, want staff.ErrNotFound", err)
	}
}

func TestStaffRepo_RotateRefreshToken_SucceedsWhenHashMatches(t *testing.T) {
	pool := setupTestPool(t)
	repo := NewStaffRepo(pool)
	hospitalID := seedHospital(t, pool, "hospital_a", "Hospital A")
	ctx := context.Background()

	created, err := repo.Create(ctx, staff.Staff{ID: uuid.New(), HospitalID: hospitalID, Username: "nurse_j", PasswordHash: "hash"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	oldHash := "old-hash"
	if err := repo.UpdateRefreshToken(ctx, created.ID, &oldHash, ptrTime(time.Now().Add(time.Hour))); err != nil {
		t.Fatalf("seed UpdateRefreshToken: %v", err)
	}

	newExpiresAt := time.Now().Add(7 * 24 * time.Hour)
	ok, err := repo.RotateRefreshToken(ctx, created.ID, oldHash, "new-hash", newExpiresAt)
	if err != nil {
		t.Fatalf("RotateRefreshToken: %v", err)
	}
	if !ok {
		t.Fatal("RotateRefreshToken should succeed when oldHash matches the stored hash")
	}

	found, err := repo.FindByRefreshTokenHash(ctx, "new-hash")
	if err != nil {
		t.Fatalf("FindByRefreshTokenHash after rotation: %v", err)
	}
	if found.ID != created.ID {
		t.Errorf("ID = %v, want %v", found.ID, created.ID)
	}
}

// The compare-and-swap contract: a stale oldHash (already rotated away, or
// never correct) must not update the row, and must report ok=false rather
// than an error — RefreshToken's caller treats that identically to a
// sequential replay of an already-used token.
func TestStaffRepo_RotateRefreshToken_FailsWhenHashStale(t *testing.T) {
	pool := setupTestPool(t)
	repo := NewStaffRepo(pool)
	hospitalID := seedHospital(t, pool, "hospital_a", "Hospital A")
	ctx := context.Background()

	created, err := repo.Create(ctx, staff.Staff{ID: uuid.New(), HospitalID: hospitalID, Username: "nurse_j", PasswordHash: "hash"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	currentHash := "current-hash"
	if err := repo.UpdateRefreshToken(ctx, created.ID, &currentHash, ptrTime(time.Now().Add(time.Hour))); err != nil {
		t.Fatalf("seed UpdateRefreshToken: %v", err)
	}

	ok, err := repo.RotateRefreshToken(ctx, created.ID, "wrong-old-hash", "new-hash", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("RotateRefreshToken: %v", err)
	}
	if ok {
		t.Fatal("RotateRefreshToken should fail (ok=false) when oldHash doesn't match the stored hash")
	}

	// The row must be untouched — still findable by the original hash.
	if _, err := repo.FindByRefreshTokenHash(ctx, currentHash); err != nil {
		t.Fatalf("original hash should still resolve after a failed rotation attempt, got: %v", err)
	}
}

// The real proof this closes the race: fire many goroutines at the exact
// same not-yet-rotated hash, against a real Postgres connection pool (not
// the in-memory fake) — a single UPDATE...WHERE is atomic per-row in
// Postgres, so exactly one of these concurrent statements can see
// RowsAffected() == 1, regardless of how many happen to overlap in time.
func TestStaffRepo_RotateRefreshToken_ConcurrentSameOldHash_OnlyOneWins(t *testing.T) {
	pool := setupTestPool(t)
	repo := NewStaffRepo(pool)
	hospitalID := seedHospital(t, pool, "hospital_a", "Hospital A")
	ctx := context.Background()

	created, err := repo.Create(ctx, staff.Staff{ID: uuid.New(), HospitalID: hospitalID, Username: "nurse_j", PasswordHash: "hash"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	oldHash := "shared-old-hash"
	if err := repo.UpdateRefreshToken(ctx, created.ID, &oldHash, ptrTime(time.Now().Add(time.Hour))); err != nil {
		t.Fatalf("seed UpdateRefreshToken: %v", err)
	}

	const attempts = 20
	var wg sync.WaitGroup
	oks := make([]bool, attempts)
	errs := make([]error, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Each goroutine proposes a distinct new hash so a successful
			// rotation is unambiguously attributable to exactly one of them.
			ok, err := repo.RotateRefreshToken(ctx, created.ID, oldHash, uuid.NewString(), time.Now().Add(time.Hour))
			oks[i] = ok
			errs[i] = err
		}(i)
	}
	wg.Wait()

	var winners int
	for i, err := range errs {
		if err != nil {
			t.Fatalf("attempt %d: RotateRefreshToken: %v", i, err)
		}
		if oks[i] {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("exactly 1 of %d concurrent rotations of the same oldHash should succeed, got %d", attempts, winners)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
