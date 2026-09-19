# Project Structure

Hospital Middleware — single Go service, package-by-feature (idiomatic Go, not layer-by-layer).

## Layout

```
/cmd/api
  main.go               composition root — config, DB pool, services, gin, graceful shutdown

/internal
  /hospital
    hospital.go         Hospital{ID, Code, Name, CreatedAt} — shared reference data.
                         Own leaf package: avoids an import cycle (his imports patient,
                         so patient can't import his back for this)

  /patient
    patient.go          Patient struct
    events.go           SyncedEvent, SyncFailedEvent — each its own small interface
    service.go          Search(...); + Repository/HospitalLookup/HISClient/HISRegistry

  /staff
    staff.go            Staff struct
    service.go          CreateStaff, Login, RefreshToken; + Repository/HospitalLookup

  /his
    registry.go         Registry — resolves a HISClient by hospital code
    factory.go          Factory + BuildRegistry(factories, baseURLs)
    adapters.go          adapters map — explicit per-hospital registration, no init()
    /hospitala
      hospital_a.go            Hospital A's patient.HISClient implementation

  /auth
    password.go         bcrypt hashing
    token.go            JWT access token + opaque refresh token

  /event
    bus.go              Generic Bus[T] — synchronous pub/sub
    validate.go         RequireComplete — panics on a nil publisher field at construction

  /audit
    subscriber.go       One Log* function per event type (8 total)

  /postgres
    postgres.go         pgxpool.Pool setup
    hospital_repo.go    HospitalRepo — satisfies both patient's and staff's HospitalLookup
    patient_repo.go     implements patient.Repository
    staff_repo.go       implements staff.Repository

  /httpapi
    router.go           gin engine, route registration
    response.go         shared error envelope
    patient_handler.go  /patient/search handler
    staff_handler.go    /staff/* handlers
    middleware.go       JWT auth + panic recovery
    docs.go             /docs (Swagger UI) + /docs/openapi.yaml

  /config
    config.go           env loading (PORT, DATABASE_URL, JWT_SECRET)

/migrations             golang-migrate naming
  000001_init                schema (see er-diagram.md)
  000002_seed_hospitals        seeds hospital_a
  000003_seed_hospital_b         seeds hospital_b + one patient (no HIS adapter for it)

/mockserver
  main.go               stub Hospital A server for local dev

/deployments
  docker-compose.yml    nginx + api + postgres (+ mockserver for dev)
  /nginx/nginx.conf
  Dockerfile

/docs                   planning docs (md, docx, openapi.yaml) + embed.go (//go:embed for /docs)

go.mod
Makefile
README.md
```

## Design notes

- **Package by feature, not by layer.** Each domain package (`patient`, `staff`) owns its struct, its business logic, and the interfaces its logic needs — no separate `domain`/`usecase`/`adapter` split. Interfaces are defined at the point of consumption (Go idiom: "accept interfaces, return structs"), not centralized in a contracts layer.
- **`postgres` is one package implementing multiple domain interfaces** (`patient.Repository`, `staff.Repository`) rather than a repository-per-folder tree — avoids the C#/Clean-Architecture folder explosion for what is, structurally, a small schema.
- **No DI container.** Everything is wired by hand in `cmd/api/main.go`. Explicit, greppable, no reflection.
- **`his`** is the adapter boundary for external Hospital Information Systems: one `HISClient` implementation per hospital, selected via a registry keyed by hospital code. Built to support more than Hospital A even though only one is specified, since the scope frames this as "Hospital Information Systems" (plural) and the middleware's whole point is being HIS-agnostic.

## `/event` + `/audit`

Not required by the scope — added to demonstrate how this could evolve toward an event-driven/microservices split. 8 event types, each with its own consumer-owned interface and a real subscriber in `/audit`, wired in `cmd/api/main.go`:

| Event | Publisher | Fires from |
|---|---|---|
| `patient.SyncedEvent` | `patient.EventPublisher` | `patient.Service` — successful HIS sync |
| `patient.SyncFailedEvent` | `patient.SyncFailedEventPublisher` | `patient.Service` — genuine HIS/adapter error (not `ErrHISNoMatch`, not `ErrNoAdapterRegistered` — those are graceful no-ops, not failures) |
| `staff.CreatedEvent` | `staff.CreatedEventPublisher` | `staff.Service.CreateStaff` — success |
| `staff.CreatedFailedEvent` | `staff.CreatedFailedEventPublisher` | `staff.Service.CreateStaff` — validation, unknown hospital, or duplicate username |
| `staff.LoginEvent` | `staff.LoginEventPublisher` | `staff.Service.Login` — success |
| `staff.LoginFailedEvent` | `staff.LoginFailedEventPublisher` | `staff.Service.Login` — any failure (published *after* the constant-time bcrypt comparison, so it can't reopen the timing side-channel that comparison closes) |
| `staff.RefreshedEvent` | `staff.RefreshedEventPublisher` | `staff.Service.RefreshToken` — success |
| `staff.RefreshFailedEvent` | `staff.RefreshFailedEventPublisher` | `staff.Service.RefreshToken` — any failure |

- These 8, and no more — no "patient viewed" event, no generic infra-failure event.
- Every event has a real subscriber: `/audit`'s 8 `Log*` functions, one per event type, each logging via an injected `*log.Logger`. Exercised by `internal/audit/subscriber_test.go` and the publish-site tests in `staff`/`patient`.
- `patient`/`staff` bundle their publishers into `EventPublishers` structs (`patient.EventPublishers{Synced, SyncFailed}`, `staff.EventPublishers{Created, CreatedFailed, Login, LoginFailed, Refreshed, RefreshFailed}`) so `NewService` doesn't grow one parameter per event — each field is still its own small, consumer-owned interface.
- Synchronous, no goroutines, no retry/backpressure. A production version with independently-deployed services would need that; this in-process version doesn't.
- A failure in a subscriber never breaks the caller — `Publish` errors are logged and swallowed across all 8 publish sites in `patient.Service`/`staff.Service`.
- Each event's publisher is a one-method interface defined in the package that needs it, same as `Repository`/`HISClient`. If `patient`/`staff` became separately deployed services, only the implementation behind that interface changes (in-process `event.Bus` -> a Kafka/NATS producer) — the usecase code that publishes it doesn't.

Built after the 4 required APIs and their tests, once nothing else required was left unfinished — every event fires from a code path that already worked without it.

## Testing strategy

- **Service layer** (`patient`, `staff`): TDD, red-green-refactor, against the interfaces each package defines — hand-written fakes, not `gomock`/`mockery`. Every interface here is 1-4 methods; a fake needs no codegen step and behaves statefully (create-then-find round-trips actually work), which is closer to real DB semantics than a mocking framework buys for interfaces this small. Tests colocated as `service_test.go` next to `service.go` (standard Go convention — not a separate `/test` tree for unit tests).
- **`postgres`**: integration tests (`hospital_repo_test.go`, `patient_repo_test.go`, `staff_repo_test.go`) run against a real Postgres — the docker-compose stack's database, via a plain `pgxpool.Pool` (no `testcontainers-go`; the project already has a docker-compose Postgres to point at). Each test truncates and re-seeds its own tables for isolation, against a dedicated `hospital_middleware_test` database (see `testdb_test.go`) so this never touches the app's own seeded data. They skip, not fail, when no Postgres is reachable, so `go test ./...` still passes with no Docker running — `make up` first gives full coverage.
- **`httpapi`**: real HTTP round-trips via `httptest`, against a router wired with real services backed by fakes, covering every required test case in `api-spec.md`.
- **`his`**: `httptest`-backed tests against `hospitala.Client` directly (no need for the standalone `/mockserver` binary in unit tests — that's for local `docker-compose` dev and any later end-to-end pass).
- **`event`**: `Bus[T]` tested in isolation (ordering, first-error-stops-the-chain, structural interface satisfaction, plus `NewBusWithSubscriber` registering immediately); `RequireComplete` tested for both the panic-on-nil-field and no-panic-when-complete cases.
- **`audit`**: one test per `Log*` subscriber (8 total) — each asserts a nil error and that the logged line contains the event's identifying detail (patient_hn, username, failure reason, etc.).
- **`patient`/`staff` event publishing**: tests asserting each service calls the right publisher on success and failure paths (e.g. `TestCreateStaff_PublishesCreatedEventOnSuccess`, `TestLogin_PublishesLoginFailedEvent_OnWrongPassword`, `TestSearch_PublishesSyncFailedEvent_OnGenuineHISError`).
- **`httpapi/docs`**: `TestDocs_SwaggerUIPage` and `TestDocs_OpenAPISpec` confirm `/docs` serves the Swagger UI page and `/docs/openapi.yaml` serves the embedded spec.
