package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testDatabaseURL returns the Postgres connection string these integration
// tests use, defaulting to the docker-compose stack's host-mapped port
// (deployments/docker-compose.yml exposes postgres on 5432) — so `go test
// ./...` exercises this package for real against `make up`, with no extra
// setup. Override with TEST_DATABASE_URL for a different instance.
//
// Deliberately hospital_middleware_test, NOT hospital_middleware (the app's
// own database) — setupTestPool below TRUNCATEs every table it touches,
// including hospitals, before each test. Pointed at the app's database,
// that truncation wipes the hospital_a row the seed migration puts there
// for real API traffic — confirmed happening in practice: /staff/create
// through the live nginx-fronted stack started failing with
// UNKNOWN_HOSPITAL immediately after a `go test ./...` run. See
// deployments/postgres/init/01-create-test-db.sql, which creates this
// database, and the docker-compose.yml "migrate-test" service, which
// migrates it — both alongside, and independent from, the app's own.
func testDatabaseURL() string {
	if v := os.Getenv("TEST_DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://hospital_middleware:hospital_middleware@localhost:5432/hospital_middleware_test?sslmode=disable"
}

// setupTestPool connects to a real Postgres instance and truncates every
// table this package touches, so each test starts from a clean slate. It
// skips (not fails) the test when no Postgres is reachable — this package
// was long flagged in docs/DECISION_LOG.md as needing a real run; unlike the rest
// of the suite, these tests genuinely can't run without one.
func setupTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, testDatabaseURL())
	if err != nil {
		t.Skipf("postgres: skipping integration test, could not create pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("postgres: skipping integration test, no reachable Postgres at %s: %v", testDatabaseURL(), err)
	}

	if _, err := pool.Exec(context.Background(), `TRUNCATE TABLE patients, staff, hospitals CASCADE`); err != nil {
		pool.Close()
		t.Fatalf("postgres: truncate tables before test: %v", err)
	}

	t.Cleanup(pool.Close)
	return pool
}

// seedHospital inserts a hospital row directly — HospitalRepo has no Create
// method (hospitals are reference data seeded by migration, not created
// through any API) — for tests that need one to satisfy staff/patients' FK.
func seedHospital(t *testing.T, pool *pgxpool.Pool, code, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := pool.QueryRow(context.Background(),
		`INSERT INTO hospitals (code, name) VALUES ($1, $2) RETURNING id`, code, name,
	).Scan(&id)
	if err != nil {
		t.Fatalf("seed hospital %q: %v", code, err)
	}
	return id
}
