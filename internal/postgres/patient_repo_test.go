package postgres

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"hospital-middleware/internal/patient"
)

func strPtr(s string) *string { return &s }

// seedPatient is a small helper for tests that don't need every field —
// nationalID may be "" to leave it unset (national_id has a per-hospital
// partial unique index, so many seeded rows can't all share one non-null
// value the way patient_hn's plain unique constraint requires a distinct hn
// per row instead).
func seedPatient(t *testing.T, repo *PatientRepo, hospitalID uuid.UUID, hn, nationalID, firstTH, lastTH, firstEN, lastEN string) patient.Patient {
	t.Helper()
	p := patient.Patient{
		HospitalID:  hospitalID,
		PatientHN:   hn,
		FirstNameTH: firstTH, LastNameTH: lastTH,
		FirstNameEN: firstEN, LastNameEN: lastEN,
		SyncedAt: time.Now(),
	}
	if nationalID != "" {
		p.NationalID = strPtr(nationalID)
	}
	got, err := repo.Upsert(context.Background(), p)
	if err != nil {
		t.Fatalf("seedPatient Upsert: %v", err)
	}
	return got
}

func TestPatientRepo_Upsert_InsertsNewRow(t *testing.T) {
	pool := setupTestPool(t)
	repo := NewPatientRepo(pool)
	hospitalID := seedHospital(t, pool, "hospital_a", "Hospital A")

	got, err := repo.Upsert(context.Background(), patient.Patient{
		HospitalID: hospitalID, PatientHN: "HN001",
		NationalID:  strPtr("1101234567890"),
		FirstNameTH: "สมชาย", LastNameTH: "ใจดี",
		FirstNameEN: "Somchai", LastNameEN: "Jaidee",
		SyncedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if got.ID == uuid.Nil {
		t.Fatal("expected a generated ID")
	}
	if got.PatientHN != "HN001" || got.FirstNameEN != "Somchai" {
		t.Errorf("unexpected row: %+v", got)
	}
}

// The core of the sync flow (patient.Service.syncIfMissing): a second
// Upsert on the same (hospital_id, patient_hn) must update the existing row
// in place, not create a duplicate — this is what makes the ON CONFLICT
// clause's dedupe key correct.
func TestPatientRepo_Upsert_UpdatesOnConflict(t *testing.T) {
	pool := setupTestPool(t)
	repo := NewPatientRepo(pool)
	hospitalID := seedHospital(t, pool, "hospital_a", "Hospital A")
	ctx := context.Background()

	first, err := repo.Upsert(ctx, patient.Patient{
		HospitalID: hospitalID, PatientHN: "HN001",
		NationalID:  strPtr("1101234567890"),
		FirstNameTH: "สมชาย", LastNameTH: "ใจดี",
		FirstNameEN: "Somchai", LastNameEN: "Jaidee",
		PhoneNumber: strPtr("0811111111"),
		SyncedAt:    time.Now(),
	})
	if err != nil {
		t.Fatalf("first Upsert: %v", err)
	}

	updated, err := repo.Upsert(ctx, patient.Patient{
		HospitalID: hospitalID, PatientHN: "HN001", // same dedupe key
		NationalID:  strPtr("1101234567890"),
		FirstNameTH: "สมชาย", LastNameTH: "ใจดี",
		FirstNameEN: "Somchai", LastNameEN: "Jaidee",
		PhoneNumber: strPtr("0899999999"), // changed field
		SyncedAt:    time.Now(),
	})
	if err != nil {
		t.Fatalf("second Upsert: %v", err)
	}

	if updated.ID != first.ID {
		t.Fatalf("upsert on the same (hospital_id, patient_hn) should update the same row, got new ID %v (was %v)", updated.ID, first.ID)
	}
	if updated.PhoneNumber == nil || *updated.PhoneNumber != "0899999999" {
		t.Errorf("PhoneNumber = %v, want updated to 0899999999", updated.PhoneNumber)
	}

	_, total, err := repo.Search(ctx, hospitalID, patient.SearchFilter{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if total != 1 {
		t.Fatalf("total = %d, want 1 row after upserting the same patient twice, not 2", total)
	}
}

// Mirrors the real scenario in patient.Service.syncIfMissing: two concurrent
// /patient/search requests for the same not-yet-cached id can both pass
// FindByNationalOrPassportID (ErrNotFound) before either upserts, and both
// then call HIS + Upsert for the same patient_hn at nearly the same
// instant. The ON CONFLICT clause needs to make that safe — one INSERT,
// one UPDATE (or two concurrent conflict-resolutions), never a duplicate
// row and never an unhandled unique-violation error surfacing to either
// caller.
func TestPatientRepo_Upsert_ConcurrentSamePatientHN_NoDuplicateNoError(t *testing.T) {
	pool := setupTestPool(t)
	repo := NewPatientRepo(pool)
	hospitalID := seedHospital(t, pool, "hospital_a", "Hospital A")

	const attempts = 20
	var wg sync.WaitGroup
	errs := make([]error, attempts)
	ids := make([]uuid.UUID, attempts)
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got, err := repo.Upsert(context.Background(), patient.Patient{
				HospitalID: hospitalID, PatientHN: "HN001", // same dedupe key from every goroutine
				NationalID:  strPtr("1101234567890"),
				FirstNameTH: "สมชาย", LastNameTH: "ใจดี",
				FirstNameEN: "Somchai", LastNameEN: "Jaidee",
				SyncedAt: time.Now(),
			})
			errs[i] = err
			ids[i] = got.ID
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("attempt %d: Upsert: %v (concurrent upserts on the same patient_hn must never surface a raw conflict error)", i, err)
		}
	}
	for i, id := range ids {
		if id != ids[0] {
			t.Fatalf("attempt %d returned a different row ID (%v) than attempt 0 (%v) — every concurrent upsert on the same dedupe key must resolve to the same row", i, id, ids[0])
		}
	}

	_, total, err := repo.Search(context.Background(), hospitalID, patient.SearchFilter{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if total != 1 {
		t.Fatalf("total = %d, want exactly 1 row after %d concurrent upserts on the same patient_hn", total, attempts)
	}
}

func TestPatientRepo_FindByNationalOrPassportID(t *testing.T) {
	pool := setupTestPool(t)
	repo := NewPatientRepo(pool)
	hospitalID := seedHospital(t, pool, "hospital_a", "Hospital A")
	ctx := context.Background()

	seedPatient(t, repo, hospitalID, "HN001", "1101234567890", "สมชาย", "ใจดี", "Somchai", "Jaidee")
	if _, err := repo.Upsert(ctx, patient.Patient{
		HospitalID: hospitalID, PatientHN: "HN002",
		PassportID:  strPtr("AB1234567"),
		FirstNameTH: "สมหญิง", LastNameTH: "ดีใจ",
		FirstNameEN: "Somying", LastNameEN: "Deejai",
		SyncedAt: time.Now(),
	}); err != nil {
		t.Fatalf("seed by passport_id: %v", err)
	}

	byNational, err := repo.FindByNationalOrPassportID(ctx, hospitalID, "1101234567890")
	if err != nil {
		t.Fatalf("find by national_id: %v", err)
	}
	if byNational.PatientHN != "HN001" {
		t.Errorf("PatientHN = %q, want HN001", byNational.PatientHN)
	}

	byPassport, err := repo.FindByNationalOrPassportID(ctx, hospitalID, "AB1234567")
	if err != nil {
		t.Fatalf("find by passport_id: %v", err)
	}
	if byPassport.PatientHN != "HN002" {
		t.Errorf("PatientHN = %q, want HN002", byPassport.PatientHN)
	}
}

func TestPatientRepo_FindByNationalOrPassportID_NotFound(t *testing.T) {
	pool := setupTestPool(t)
	repo := NewPatientRepo(pool)
	hospitalID := seedHospital(t, pool, "hospital_a", "Hospital A")

	_, err := repo.FindByNationalOrPassportID(context.Background(), hospitalID, "does-not-exist")
	if !errors.Is(err, patient.ErrNotFound) {
		t.Fatalf("got err %v, want patient.ErrNotFound", err)
	}
}

// Required test case in api-spec.md: "search results never include another
// hospital's patients, even when filters would otherwise match."
func TestPatientRepo_Search_ScopedToHospital(t *testing.T) {
	pool := setupTestPool(t)
	repo := NewPatientRepo(pool)
	hA := seedHospital(t, pool, "hospital_a", "Hospital A")
	hB := seedHospital(t, pool, "hospital_b", "Hospital B")

	seedPatient(t, repo, hA, "HN001", "1111111111111", "สมชาย", "ใจดี", "Somchai", "Jaidee")
	seedPatient(t, repo, hB, "HN002", "1111111111111", "สมชาย", "ใจดี", "Somchai", "Jaidee")

	got, total, err := repo.Search(context.Background(), hA, patient.SearchFilter{NationalID: strPtr("1111111111111"), Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if total != 1 || len(got) != 1 || got[0].HospitalID != hA {
		t.Fatalf("search scoped to hospital A leaked hospital B's patient, got %+v (total=%d)", got, total)
	}
}

// Required test case: "a first_name filter matches a patient whose Thai
// name matches, even if their English name doesn't (and vice versa)" — this
// is the actual SQL OR-clause (first_name_th ILIKE $1 OR first_name_en ILIKE
// $1), not the fake repo used in internal/patient's own service tests.
func TestPatientRepo_Search_NameMatchesEitherLanguage(t *testing.T) {
	pool := setupTestPool(t)
	repo := NewPatientRepo(pool)
	hospitalID := seedHospital(t, pool, "hospital_a", "Hospital A")
	ctx := context.Background()
	seedPatient(t, repo, hospitalID, "HN001", "1101234567890", "สมชาย", "ใจดี", "Somchai", "Jaidee")

	th, _, err := repo.Search(ctx, hospitalID, patient.SearchFilter{FirstName: strPtr("สมชาย"), Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("Search (TH): %v", err)
	}
	if len(th) != 1 {
		t.Fatalf("Thai first_name search should match, got %d results", len(th))
	}

	en, _, err := repo.Search(ctx, hospitalID, patient.SearchFilter{FirstName: strPtr("Somchai"), Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("Search (EN): %v", err)
	}
	if len(en) != 1 {
		t.Fatalf("English first_name search should match, got %d results", len(en))
	}

	none, _, err := repo.Search(ctx, hospitalID, patient.SearchFilter{FirstName: strPtr("Nobody"), Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("Search (no match): %v", err)
	}
	if len(none) != 0 {
		t.Fatalf("non-matching first_name should return 0 results, got %d", len(none))
	}
}

// Combines partial-match and hospital-scoping — each is covered separately
// by NamePartialMatch and ScopedToHospital above, but not together. Two
// different hospitals each have a patient whose first name contains "som"
// (Somchai/Somying, matching the actual seed data in migrations/
// 000003_seed_hospital_b.up.sql and mockserver/main.go), and a search
// scoped to one hospital must find only that hospital's match(es), not the
// other's, even though the substring alone matches across both.
func TestPatientRepo_Search_PartialNameMatchStaysScopedToHospital(t *testing.T) {
	pool := setupTestPool(t)
	repo := NewPatientRepo(pool)
	hA := seedHospital(t, pool, "hospital_a", "Hospital A")
	hB := seedHospital(t, pool, "hospital_b", "Hospital B")
	ctx := context.Background()

	seedPatient(t, repo, hA, "HN001", "1111111111111", "สมชาย", "ใจดี", "Somchai", "Jaidee")
	seedPatient(t, repo, hB, "HN002", "2222222222222", "สมชาย", "สุขใจ", "Somchai", "Sukjai")

	got, total, err := repo.Search(ctx, hB, patient.SearchFilter{FirstName: strPtr("som"), Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if total != 1 || len(got) != 1 {
		t.Fatalf("expected exactly 1 match scoped to hospital B, got %d (total=%d): %+v", len(got), total, got)
	}
	if got[0].HospitalID != hB || got[0].LastNameEN != "Sukjai" {
		t.Fatalf("search leaked hospital A's same-first-name patient, got %+v", got[0])
	}
}

// Partial-match behavior of ILIKE ('%value%') — the scope's filters
// read as exact-ish search fields, but a substring match is what the "%..%"
// wrapping in patient_repo.go actually implements; worth pinning down.
func TestPatientRepo_Search_NamePartialMatch(t *testing.T) {
	pool := setupTestPool(t)
	repo := NewPatientRepo(pool)
	hospitalID := seedHospital(t, pool, "hospital_a", "Hospital A")
	ctx := context.Background()
	seedPatient(t, repo, hospitalID, "HN001", "1101234567890", "สมชาย", "ใจดี", "Somchai", "Jaidee")

	got, _, err := repo.Search(ctx, hospitalID, patient.SearchFilter{LastName: strPtr("aide"), Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("substring last_name search should match Jaidee via 'aide', got %d results", len(got))
	}
}

// Without escaping, "%" and "_" in a search term are live LIKE wildcards —
// "%" would match every patient regardless of name, and "_" would match
// any single character. Neither is what a staff member typing a literal
// name (that happens to contain one of these characters) expects.
func TestPatientRepo_Search_LiteralPercentAndUnderscoreNotTreatedAsWildcards(t *testing.T) {
	pool := setupTestPool(t)
	repo := NewPatientRepo(pool)
	hospitalID := seedHospital(t, pool, "hospital_a", "Hospital A")
	ctx := context.Background()
	seedPatient(t, repo, hospitalID, "HN001", "1101234567890", "สมชาย", "ใจดี", "Somchai", "Jaidee")

	percent, _, err := repo.Search(ctx, hospitalID, patient.SearchFilter{FirstName: strPtr("%"), Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("Search (literal %%): %v", err)
	}
	if len(percent) != 0 {
		t.Fatalf("a bare \"%%\" should not match every patient as a wildcard, got %d results", len(percent))
	}

	underscore, _, err := repo.Search(ctx, hospitalID, patient.SearchFilter{LastName: strPtr("_aidee"), Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("Search (literal _): %v", err)
	}
	if len(underscore) != 0 {
		t.Fatalf("\"_aidee\" should not match \"Jaidee\" via _ as a single-character wildcard, got %d results", len(underscore))
	}

	// A name containing the literal character should still match on it.
	seedPatient(t, repo, hospitalID, "HN002", "", "100%", "ดี", "100%Sure", "Sure")
	literalMatch, _, err := repo.Search(ctx, hospitalID, patient.SearchFilter{FirstName: strPtr("100%"), Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("Search (literal 100%%): %v", err)
	}
	if len(literalMatch) != 1 {
		t.Fatalf("searching for the literal substring \"100%%\" should match the patient named it, got %d results", len(literalMatch))
	}
}

func TestPatientRepo_Search_Pagination(t *testing.T) {
	pool := setupTestPool(t)
	repo := NewPatientRepo(pool)
	hospitalID := seedHospital(t, pool, "hospital_a", "Hospital A")

	for i := 0; i < 25; i++ {
		seedPatient(t, repo, hospitalID, uuid.NewString(), "", "สมชาย", "ใจดี", "Somchai", "Jaidee")
	}

	page1, total, err := repo.Search(context.Background(), hospitalID, patient.SearchFilter{FirstName: strPtr("Somchai"), Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("Search page 1: %v", err)
	}
	if total != 25 {
		t.Fatalf("total = %d, want 25 (pagination must not affect the count)", total)
	}
	if len(page1) != 20 {
		t.Fatalf("page 1 = %d results, want 20", len(page1))
	}

	page2, _, err := repo.Search(context.Background(), hospitalID, patient.SearchFilter{FirstName: strPtr("Somchai"), Page: 2, PageSize: 20})
	if err != nil {
		t.Fatalf("Search page 2: %v", err)
	}
	if len(page2) != 5 {
		t.Fatalf("page 2 = %d results, want the remaining 5", len(page2))
	}
}

// A page number past the last page of actual results (OFFSET larger than
// the matching row count) must come back as an empty, non-error result with
// the correct total — not an out-of-range error. The SQL LIMIT/OFFSET
// handles this by construction, but it was never pinned down by a test.
func TestPatientRepo_Search_PageBeyondTotalResults(t *testing.T) {
	pool := setupTestPool(t)
	repo := NewPatientRepo(pool)
	hospitalID := seedHospital(t, pool, "hospital_a", "Hospital A")

	for i := 0; i < 3; i++ {
		seedPatient(t, repo, hospitalID, uuid.NewString(), "", "สมชาย", "ใจดี", "Somchai", "Jaidee")
	}

	got, total, err := repo.Search(context.Background(), hospitalID, patient.SearchFilter{FirstName: strPtr("Somchai"), Page: 999, PageSize: 20})
	if err != nil {
		t.Fatalf("Search far beyond the last page: %v", err)
	}
	if total != 3 {
		t.Fatalf("total = %d, want 3 (a page beyond the last must not change the total count)", total)
	}
	if len(got) != 0 {
		t.Fatalf("got %d results on a page beyond the last, want 0", len(got))
	}
}
