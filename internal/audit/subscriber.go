// Package audit is the one subscriber that makes /event real — see
// docs/project-structure.md's "/event + /audit" section for the
// discipline this package exists under: every event needs a real
// subscriber before it counts as done, and a failure here must never
// affect the outcome of the API call that triggered it (already enforced
// on the publishing side, in patient.Service and staff.Service).
//
// Each function returns a subscriber closure matching event.Subscriber[T]
// — main.go registers them on the relevant event.Bus[T] instances.
package audit

import (
	"context"
	"log"

	"hospital-middleware/internal/patient"
	"hospital-middleware/internal/staff"
)

func LogSynced(logger *log.Logger) func(context.Context, patient.SyncedEvent) error {
	return func(ctx context.Context, e patient.SyncedEvent) error {
		logger.Printf("audit: patient synced hospital_id=%s patient_hn=%s at=%s", e.HospitalID, e.PatientHN, e.SyncedAt)
		return nil
	}
}

func LogSyncFailed(logger *log.Logger) func(context.Context, patient.SyncFailedEvent) error {
	return func(ctx context.Context, e patient.SyncFailedEvent) error {
		logger.Printf("audit: patient sync FAILED hospital_id=%s id=%s err=%q at=%s", e.HospitalID, e.ID, e.Err, e.FailedAt)
		return nil
	}
}

func LogStaffCreated(logger *log.Logger) func(context.Context, staff.CreatedEvent) error {
	return func(ctx context.Context, e staff.CreatedEvent) error {
		logger.Printf("audit: staff created hospital_id=%s username=%s at=%s", e.HospitalID, e.Username, e.CreatedAt)
		return nil
	}
}

func LogStaffCreateFailed(logger *log.Logger) func(context.Context, staff.CreatedFailedEvent) error {
	return func(ctx context.Context, e staff.CreatedFailedEvent) error {
		logger.Printf("audit: staff create FAILED hospital=%s username=%s reason=%q at=%s", e.HospitalCode, e.Username, e.Reason, e.AttemptedAt)
		return nil
	}
}

func LogLogin(logger *log.Logger) func(context.Context, staff.LoginEvent) error {
	return func(ctx context.Context, e staff.LoginEvent) error {
		logger.Printf("audit: staff login hospital_id=%s username=%s at=%s", e.HospitalID, e.Username, e.LoggedInAt)
		return nil
	}
}

func LogLoginFailed(logger *log.Logger) func(context.Context, staff.LoginFailedEvent) error {
	return func(ctx context.Context, e staff.LoginFailedEvent) error {
		logger.Printf("audit: staff login FAILED hospital=%s username=%s at=%s", e.HospitalCode, e.Username, e.AttemptedAt)
		return nil
	}
}

func LogRefreshed(logger *log.Logger) func(context.Context, staff.RefreshedEvent) error {
	return func(ctx context.Context, e staff.RefreshedEvent) error {
		logger.Printf("audit: staff token refreshed hospital_id=%s staff_id=%s at=%s", e.HospitalID, e.StaffID, e.RefreshedAt)
		return nil
	}
}

func LogRefreshFailed(logger *log.Logger) func(context.Context, staff.RefreshFailedEvent) error {
	return func(ctx context.Context, e staff.RefreshFailedEvent) error {
		logger.Printf("audit: staff token refresh FAILED at=%s", e.AttemptedAt)
		return nil
	}
}
