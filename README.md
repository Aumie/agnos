# Hospital Middleware

A middleware that unifies search/display of patient information across multiple Hospital Information Systems (HIS), scoped to each staff member's own hospital.

## Tech stack

Go 1.25 · Gin · Docker · Nginx · Postgres (via `pgx/v5`)

## Planning docs

- [`docs/project-structure.md`](docs/project-structure.md) — package layout and the reasoning behind it
- [`docs/api-spec.md`](docs/api-spec.md) / [`docs/openapi.yaml`](docs/openapi.yaml) — API contract
- [`docs/er-diagram.md`](docs/er-diagram.md) — database schema
- [`docs/DECISION_LOG.md`](docs/DECISION_LOG.md) — running log of non-obvious decisions and why

## Running it

```bash
make up
```

Brings up Postgres, runs migrations, starts the mock Hospital A server, the API, and Nginx (`deployments/docker-compose.yml`). The API is reachable through Nginx on `:80`, or directly on `:8080`.

Works with zero setup — `POSTGRES_PASSWORD`/`JWT_SECRET` fall back to placeholder defaults baked into `docker-compose.yml`, since they only ever protect a Postgres instance local to your own machine's Docker network, never reachable from the internet. To override them, copy `deployments/env.example` to `deployments/.env` (gitignored) and edit that.

```bash
make down     # tear down, including the Postgres volume
make logs     # tail the API service's logs
```

### Running without Docker

```bash
go run ./cmd/api
```

Needs `DATABASE_URL` and `JWT_SECRET` set (see `internal/config`); `PORT` and `BASE_URL_HOSPITAL_A` have defaults.

## Testing

```bash
make test    # go test ./...
make vet     # go vet ./...
make fmt     # gofmt -l . (lists unformatted files, if any)
```

Every package has unit tests except `cmd/api` (the composition root — nothing to unit test beyond wiring already covered elsewhere). `internal/postgres`'s tests are real integration tests against a live Postgres — they `t.Skip()` gracefully if none is reachable, so `go test ./...` still passes with no Docker running, but `make up` first gives full coverage.

`internal/postgres`'s tests run against a **separate `hospital_middleware_test` database**, not the app's own `hospital_middleware` — they `TRUNCATE` tables for per-test isolation, which would otherwise wipe the seed data (`hospital_a`) the running API depends on. `deployments/postgres/init/01-create-test-db.sql` creates it and `docker-compose.yml`'s `migrate-test` service migrates it, both alongside and independent of the app's own database. See `docs/DECISION_LOG.md` for how this was found (running the test suite against a live `make up` stack broke `/staff/create` with `UNKNOWN_HOSPITAL` until this was fixed).

## API

Four endpoints:

| Method | Path | Auth |
|---|---|---|
| POST | `/staff/create` | none |
| POST | `/staff/login` | none |
| POST | `/staff/refresh` | none |
| POST | `/patient/search` | Bearer access token |

Full request/response shapes, error codes, and behavior notes: [`docs/api-spec.md`](docs/api-spec.md). Machine-readable: [`docs/openapi.yaml`](docs/openapi.yaml) — importable into Postman, or browsable via the live `/docs` Swagger UI route once the server is running.

## Adding a second hospital

Two separate things, and `hospital_b` already exists as a live example of doing the first without the second:

1. **The hospital row itself** — a migration (see `migrations/000003_seed_hospital_b.up.sql`), following the `000002_seed_hospitals` pattern; there's no admin API for this (see `docs/api-spec.md`'s Assumption 7).
2. **The HIS adapter** — write `internal/his/hospitalb/` following the `internal/his/hospitala` pattern (its own wire struct + `toDomain()` mapping — every HIS has a different response shape), then add one line to the `adapters` map in `internal/his/adapters.go`. See that file's comments and `docs/DECISION_LOG.md` for why registration is an explicit map rather than `init()`-based self-registration.

`hospital_b` has (1) but not (2) on purpose — its migration seeds the hospital row *and* one patient directly (bypassing HIS entirely, since there's no adapter to sync from), so `/patient/search` has something to find for it locally, while `/patient/search` by id still demonstrates the graceful-degrade path (`his.Registry` has no adapter for `hospital_b` → sync is skipped and logged, never a request error) rather than erroring.

## Deploying it live

[`docs/deploy-gcp.md`](docs/deploy-gcp.md) — Cloud Run (api + mockserver) backed by Neon Postgres, both on perpetual free tiers. `docker-compose.yml` stays the primary local-dev path; this is a separate, optional deploy path for sharing a live demo link.

**Live demo:**

| | |
|---|---|
| API | https://api-kumzoflgva-as.a.run.app |
| Mockserver (stub Hospital A) | https://mockserver-kumzoflgva-as.a.run.app |
| Swagger UI | https://api-kumzoflgva-as.a.run.app/docs |

Both services scale to zero when idle — the first request after a quiet period has a cold start of a few seconds. `/staff/create`/`/staff/login` are reachable by anyone with the URL (the app itself leaves them unauthenticated, per `docs/api-spec.md`'s Assumption 1); `/patient/search` still requires a valid Bearer token regardless.
