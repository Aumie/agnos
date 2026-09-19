package audit

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"hospital-middleware/internal/patient"
	"hospital-middleware/internal/staff"
)

func newTestLogger() (*log.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return log.New(&buf, "", 0), &buf
}

func TestLogSynced(t *testing.T) {
	logger, buf := newTestLogger()
	err := LogSynced(logger)(context.Background(), patient.SyncedEvent{
		PatientID: uuid.New(), HospitalID: uuid.New(), PatientHN: "HN001", SyncedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "HN001") {
		t.Errorf("expected log to mention patient_hn, got: %s", buf.String())
	}
}

func TestLogSyncFailed(t *testing.T) {
	logger, buf := newTestLogger()
	err := LogSyncFailed(logger)(context.Background(), patient.SyncFailedEvent{
		HospitalID: uuid.New(), ID: "1234567890123", Err: "connection refused", FailedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "connection refused") {
		t.Errorf("expected log to mention the failure reason, got: %s", buf.String())
	}
}

func TestLogStaffCreated(t *testing.T) {
	logger, buf := newTestLogger()
	err := LogStaffCreated(logger)(context.Background(), staff.CreatedEvent{
		StaffID: uuid.New(), HospitalID: uuid.New(), Username: "nurse_j", CreatedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "nurse_j") {
		t.Errorf("expected log to mention username, got: %s", buf.String())
	}
}

func TestLogStaffCreateFailed(t *testing.T) {
	logger, buf := newTestLogger()
	err := LogStaffCreateFailed(logger)(context.Background(), staff.CreatedFailedEvent{
		HospitalCode: "hospital_a", Username: "nurse_j", Reason: "username already taken", AttemptedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "username already taken") {
		t.Errorf("expected log to mention the failure reason, got: %s", buf.String())
	}
}

func TestLogLogin(t *testing.T) {
	logger, buf := newTestLogger()
	err := LogLogin(logger)(context.Background(), staff.LoginEvent{
		StaffID: uuid.New(), HospitalID: uuid.New(), Username: "nurse_j", LoggedInAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "nurse_j") {
		t.Errorf("expected log to mention username, got: %s", buf.String())
	}
}

func TestLogLoginFailed(t *testing.T) {
	logger, buf := newTestLogger()
	err := LogLoginFailed(logger)(context.Background(), staff.LoginFailedEvent{
		HospitalCode: "hospital_a", Username: "nurse_j", AttemptedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "FAILED") {
		t.Errorf("expected log to flag this as a failure, got: %s", buf.String())
	}
}

func TestLogRefreshed(t *testing.T) {
	logger, buf := newTestLogger()
	err := LogRefreshed(logger)(context.Background(), staff.RefreshedEvent{
		StaffID: uuid.New(), HospitalID: uuid.New(), RefreshedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "refreshed") {
		t.Errorf("expected log to mention the refresh, got: %s", buf.String())
	}
}

func TestLogRefreshFailed(t *testing.T) {
	logger, buf := newTestLogger()
	err := LogRefreshFailed(logger)(context.Background(), staff.RefreshFailedEvent{
		AttemptedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(buf.String(), "FAILED") {
		t.Errorf("expected log to flag this as a failure, got: %s", buf.String())
	}
}
