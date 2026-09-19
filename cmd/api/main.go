// Command api is the composition root: load config, open the DB pool,
// wire every service by hand (no DI container — see docs/project-structure.md),
// and serve with graceful shutdown.
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"hospital-middleware/internal/audit"
	"hospital-middleware/internal/config"
	"hospital-middleware/internal/event"
	"hospital-middleware/internal/his"
	"hospital-middleware/internal/httpapi"
	"hospital-middleware/internal/patient"
	"hospital-middleware/internal/postgres"
	"hospital-middleware/internal/staff"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("postgres: %v", err)
	}
	defer pool.Close()

	hospitalRepo := postgres.NewHospitalRepo(pool)
	staffRepo := postgres.NewStaffRepo(pool)
	patientRepo := postgres.NewPatientRepo(pool)

	logger := log.Default()

	// Every event gets its own Bus[T] with /audit's logger subscribed —
	// see docs/project-structure.md's "/event + /audit" section: this is
	// the one deliberately "extra" piece in the codebase, and this is the
	// wiring that finally makes it real (previously just built and
	// tested in isolation, with nothing subscribed in the running app).
	//
	// This is the one place in the whole codebase that names event.Bus[T]
	// concretely — staff.EventPublishers/patient.EventPublishers are typed
	// as each package's own consumer-owned interfaces (CreatedEventPublisher
	// etc.), never as *event.Bus[T] itself, and internal/audit only depends
	// on those same interfaces too. That's deliberate: swapping the
	// in-process, synchronous Bus[T] for something with real durability and
	// backpressure (a Postgres outbox table, or a broker via
	// github.com/ThreeDotsLabs/watermill — see event.RequireComplete's
	// neighboring bus.go doc comment for the tradeoffs) means writing one
	// new type per event satisfying the same Publish(ctx, T) error
	// signature and changing only these lines — staff, patient, and audit
	// wouldn't need to change at all.
	patientEvents := patient.EventPublishers{
		Synced:     event.NewBusWithSubscriber(audit.LogSynced(logger)),
		SyncFailed: event.NewBusWithSubscriber(audit.LogSyncFailed(logger)),
	}
	staffEvents := staff.EventPublishers{
		Created:       event.NewBusWithSubscriber(audit.LogStaffCreated(logger)),
		CreatedFailed: event.NewBusWithSubscriber(audit.LogStaffCreateFailed(logger)),
		Login:         event.NewBusWithSubscriber(audit.LogLogin(logger)),
		LoginFailed:   event.NewBusWithSubscriber(audit.LogLoginFailed(logger)),
		Refreshed:     event.NewBusWithSubscriber(audit.LogRefreshed(logger)),
		RefreshFailed: event.NewBusWithSubscriber(audit.LogRefreshFailed(logger)),
	}

	staffSvc := staff.NewService(staffRepo, hospitalRepo, cfg.JWTSecret, staffEvents, logger)

	hisRegistry := his.NewDefaultRegistry(his.LoadConfig())

	patientSvc := patient.NewService(patientRepo, hospitalRepo, hisRegistry, patientEvents, logger)

	router := httpapi.NewRouter(staffSvc, patientSvc, cfg.JWTSecret)

	srv := &http.Server{
		Addr:    ":" + cfg.Port,
		Handler: router,
	}

	go func() {
		log.Printf("listening on :%s", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("shutdown: %v", err)
	}
	log.Println("shutdown complete")
}
