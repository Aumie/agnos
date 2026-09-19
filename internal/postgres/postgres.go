// Package postgres implements patient.Repository, staff.Repository, and
// both packages' HospitalLookup interfaces against a real Postgres
// database via pgx/v5 — one package implementing multiple domain
// interfaces, not a repository-per-folder tree. See docs/project-structure.md.
//
// Verified against a real Postgres by *_test.go's integration tests, which
// run against the docker-compose stack's database (a separate
// hospital_middleware_test one — see testdb_test.go) and skip gracefully
// when no Postgres is reachable.
package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

func NewPool(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	return pgxpool.New(ctx, databaseURL)
}
