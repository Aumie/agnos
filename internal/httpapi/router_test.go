package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"hospital-middleware/internal/event"
	"hospital-middleware/internal/his"
	"hospital-middleware/internal/hospital"
	"hospital-middleware/internal/patient"
	"hospital-middleware/internal/staff"
)

// ---- fakes (mirroring internal/staff and internal/patient's own test
// fakes, duplicated here since those are unexported test-only types in
// their own packages) ----

type fakeStaffRepo struct {
	byID map[uuid.UUID]staff.Staff
}

func (f *fakeStaffRepo) Create(ctx context.Context, s staff.Staff) (staff.Staff, error) {
	f.byID[s.ID] = s
	return s, nil
}

func (f *fakeStaffRepo) FindByUsernameAndHospital(ctx context.Context, username string, hospitalID uuid.UUID) (staff.Staff, error) {
	for _, s := range f.byID {
		if s.Username == username && s.HospitalID == hospitalID {
			return s, nil
		}
	}
	return staff.Staff{}, staff.ErrNotFound
}

func (f *fakeStaffRepo) FindByRefreshTokenHash(ctx context.Context, hash string) (staff.Staff, error) {
	for _, s := range f.byID {
		if s.RefreshTokenHash != nil && *s.RefreshTokenHash == hash {
			return s, nil
		}
	}
	return staff.Staff{}, staff.ErrNotFound
}

func (f *fakeStaffRepo) UpdateRefreshToken(ctx context.Context, staffID uuid.UUID, hash *string, expiresAt *time.Time) error {
	s, ok := f.byID[staffID]
	if !ok {
		return staff.ErrNotFound
	}
	s.RefreshTokenHash = hash
	s.RefreshTokenExpiresAt = expiresAt
	f.byID[staffID] = s
	return nil
}

// RotateRefreshToken mirrors StaffRepo.RotateRefreshToken's compare-and-swap
// contract (see internal/staff/service.go's Repository interface) — the
// router tests here only exercise sequential HTTP calls, so this doesn't
// need fakeRepo's mutex-guarded version in internal/staff's own tests.
func (f *fakeStaffRepo) RotateRefreshToken(ctx context.Context, staffID uuid.UUID, oldHash, newHash string, newExpiresAt time.Time) (bool, error) {
	s, ok := f.byID[staffID]
	if !ok || s.RefreshTokenHash == nil || *s.RefreshTokenHash != oldHash {
		return false, nil
	}
	s.RefreshTokenHash = &newHash
	s.RefreshTokenExpiresAt = &newExpiresAt
	f.byID[staffID] = s
	return true, nil
}

type fakeHospitalsByCode struct {
	byCode map[string]hospital.Hospital
}

func (f *fakeHospitalsByCode) FindByCode(ctx context.Context, code string) (hospital.Hospital, error) {
	h, ok := f.byCode[code]
	if !ok {
		return hospital.Hospital{}, staff.ErrHospitalNotFound
	}
	return h, nil
}

type fakePatientRepo struct {
	patients []patient.Patient
}

func (f *fakePatientRepo) FindByNationalOrPassportID(ctx context.Context, hospitalID uuid.UUID, id string) (patient.Patient, error) {
	for _, p := range f.patients {
		if p.HospitalID != hospitalID {
			continue
		}
		if p.NationalID != nil && *p.NationalID == id {
			return p, nil
		}
	}
	return patient.Patient{}, patient.ErrNotFound
}

func (f *fakePatientRepo) Upsert(ctx context.Context, p patient.Patient) (patient.Patient, error) {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	f.patients = append(f.patients, p)
	return p, nil
}

func (f *fakePatientRepo) Search(ctx context.Context, hospitalID uuid.UUID, filter patient.SearchFilter) ([]patient.Patient, int, error) {
	var matches []patient.Patient
	for _, p := range f.patients {
		if p.HospitalID == hospitalID {
			matches = append(matches, p)
		}
	}
	return matches, len(matches), nil
}

type fakeHospitalsByID struct {
	byID map[uuid.UUID]hospital.Hospital
}

func (f *fakeHospitalsByID) FindByID(ctx context.Context, id uuid.UUID) (hospital.Hospital, error) {
	h, ok := f.byID[id]
	if !ok {
		return hospital.Hospital{}, patient.ErrHospitalNotFound
	}
	return h, nil
}

// ---- test harness ----

type testEnv struct {
	router      *gin.Engine
	hospitalA   hospital.Hospital
	patientRepo *fakePatientRepo
}

func newTestEnv() *testEnv {
	// No gin.SetMode call needed here — NewRouter itself sets
	// gin.ReleaseMode unconditionally now, which also suppresses the
	// debug-mode route-registration logging this used to silence via
	// gin.TestMode.
	jwtSecret := []byte("test-secret-do-not-use-in-prod")

	hA := hospital.Hospital{ID: uuid.New(), Code: "hospital_a", Name: "Hospital A"}

	logger := log.New(io.Discard, "", 0)

	staffRepo := &fakeStaffRepo{byID: map[uuid.UUID]staff.Staff{}}
	staffHospitals := &fakeHospitalsByCode{byCode: map[string]hospital.Hospital{"hospital_a": hA}}
	staffEvents := staff.EventPublishers{
		Created:       event.NewBus[staff.CreatedEvent](),
		CreatedFailed: event.NewBus[staff.CreatedFailedEvent](),
		Login:         event.NewBus[staff.LoginEvent](),
		LoginFailed:   event.NewBus[staff.LoginFailedEvent](),
		Refreshed:     event.NewBus[staff.RefreshedEvent](),
		RefreshFailed: event.NewBus[staff.RefreshFailedEvent](),
	}
	staffSvc := staff.NewService(staffRepo, staffHospitals, jwtSecret, staffEvents, logger)

	patientRepo := &fakePatientRepo{}
	patientHospitals := &fakeHospitalsByID{byID: map[uuid.UUID]hospital.Hospital{hA.ID: hA}}
	hisRegistry := his.NewRegistry() // no adapters registered — fine, tests here don't exercise live sync
	patientEvents := patient.EventPublishers{
		Synced:     event.NewBus[patient.SyncedEvent](),
		SyncFailed: event.NewBus[patient.SyncFailedEvent](),
	}
	patientSvc := patient.NewService(patientRepo, patientHospitals, hisRegistry, patientEvents, logger)

	return &testEnv{
		router:      NewRouter(staffSvc, patientSvc, jwtSecret),
		hospitalA:   hA,
		patientRepo: patientRepo,
	}
}

func (e *testEnv) do(t *testing.T, method, path string, body any, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal request body: %v", err)
		}
		reader = bytes.NewReader(b)
	} else {
		reader = bytes.NewReader(nil)
	}

	req := httptest.NewRequest(method, path, reader)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	return w
}

func decodeBody[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode response body %q: %v", w.Body.String(), err)
	}
	return v
}

// ---- /staff/create ----

func TestCreateStaff_HTTP_Success(t *testing.T) {
	env := newTestEnv()

	w := env.do(t, http.MethodPost, "/staff/create", createStaffRequest{
		Username: "nurse_j", Password: "S3cur3P@ss!", Hospital: "hospital_a",
	}, nil)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body: %s", w.Code, w.Body.String())
	}
	resp := decodeBody[createStaffResponse](t, w)
	if resp.Username != "nurse_j" || resp.Hospital != "hospital_a" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

// Gin's ShouldBindJSON (used by every handler here) decodes the body as
// JSON unconditionally — it never inspects Content-Type. This pins that
// behavior down explicitly: a client that sends valid JSON with no
// Content-Type header (or the wrong one) must still be accepted, not
// rejected on a technicality the API has never actually enforced.
func TestCreateStaff_HTTP_SucceedsWithoutContentTypeHeader(t *testing.T) {
	env := newTestEnv()

	body, err := json.Marshal(createStaffRequest{Username: "nurse_j", Password: "S3cur3P@ss!", Hospital: "hospital_a"})
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/staff/create", bytes.NewReader(body))
	// Deliberately no Content-Type header set at all.
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body: %s", w.Code, w.Body.String())
	}
}

func TestCreateStaff_HTTP_SucceedsWithWrongContentTypeHeader(t *testing.T) {
	env := newTestEnv()

	body, err := json.Marshal(createStaffRequest{Username: "nurse_j", Password: "S3cur3P@ss!", Hospital: "hospital_a"})
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/staff/create", bytes.NewReader(body))
	req.Header.Set("Content-Type", "text/plain") // wrong, but the body is still valid JSON
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body: %s", w.Code, w.Body.String())
	}
}

// Regression test: the response's Hospital field must reflect the same
// trimmed value staff.Service actually validated and stored against — not
// the raw request string. Before this was fixed, Username in the response
// came from the service's (trimmed) return value while Hospital was echoed
// straight from the untrimmed request, so a padded hospital code like
// "  hospital_a  " came back with the whitespace still in it.
func TestCreateStaff_HTTP_EchoesTrimmedHospitalCode(t *testing.T) {
	env := newTestEnv()

	w := env.do(t, http.MethodPost, "/staff/create", createStaffRequest{
		Username: "nurse_j", Password: "S3cur3P@ss!", Hospital: "  hospital_a  ",
	}, nil)

	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201, body: %s", w.Code, w.Body.String())
	}
	resp := decodeBody[createStaffResponse](t, w)
	if resp.Hospital != "hospital_a" {
		t.Errorf("Hospital = %q, want trimmed %q", resp.Hospital, "hospital_a")
	}
}

func TestCreateStaff_HTTP_ValidationError(t *testing.T) {
	env := newTestEnv()

	w := env.do(t, http.MethodPost, "/staff/create", createStaffRequest{
		Username: "", Password: "S3cur3P@ss!", Hospital: "hospital_a",
	}, nil)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", w.Code, w.Body.String())
	}
	resp := decodeBody[errorResponse](t, w)
	if resp.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("error code = %q, want VALIDATION_ERROR", resp.Error.Code)
	}
}

func TestCreateStaff_HTTP_UnknownHospital(t *testing.T) {
	env := newTestEnv()

	w := env.do(t, http.MethodPost, "/staff/create", createStaffRequest{
		Username: "nurse_j", Password: "S3cur3P@ss!", Hospital: "hospital_z",
	}, nil)

	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422, body: %s", w.Code, w.Body.String())
	}
	resp := decodeBody[errorResponse](t, w)
	if resp.Error.Code != "UNKNOWN_HOSPITAL" {
		t.Errorf("error code = %q, want UNKNOWN_HOSPITAL", resp.Error.Code)
	}
}

// ---- /staff/login ----

func TestLogin_HTTP_Success(t *testing.T) {
	env := newTestEnv()
	env.do(t, http.MethodPost, "/staff/create", createStaffRequest{
		Username: "nurse_j", Password: "S3cur3P@ss!", Hospital: "hospital_a",
	}, nil)

	w := env.do(t, http.MethodPost, "/staff/login", loginRequest{
		Username: "nurse_j", Password: "S3cur3P@ss!", Hospital: "hospital_a",
	}, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	resp := decodeBody[tokenResponse](t, w)
	if resp.AccessToken == "" || resp.RefreshToken == "" {
		t.Fatal("expected non-empty tokens")
	}
	if resp.TokenType != "Bearer" {
		t.Errorf("token_type = %q, want Bearer", resp.TokenType)
	}
}

func TestLogin_HTTP_WrongPassword(t *testing.T) {
	env := newTestEnv()
	env.do(t, http.MethodPost, "/staff/create", createStaffRequest{
		Username: "nurse_j", Password: "S3cur3P@ss!", Hospital: "hospital_a",
	}, nil)

	w := env.do(t, http.MethodPost, "/staff/login", loginRequest{
		Username: "nurse_j", Password: "WrongPassword1!", Hospital: "hospital_a",
	}, nil)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body: %s", w.Code, w.Body.String())
	}
	resp := decodeBody[errorResponse](t, w)
	if resp.Error.Code != "INVALID_CREDENTIALS" {
		t.Errorf("error code = %q, want INVALID_CREDENTIALS", resp.Error.Code)
	}
}

// ---- /staff/refresh ----

func TestRefresh_HTTP_Success(t *testing.T) {
	env := newTestEnv()
	env.do(t, http.MethodPost, "/staff/create", createStaffRequest{
		Username: "nurse_j", Password: "S3cur3P@ss!", Hospital: "hospital_a",
	}, nil)
	loginResp := decodeBody[tokenResponse](t, env.do(t, http.MethodPost, "/staff/login", loginRequest{
		Username: "nurse_j", Password: "S3cur3P@ss!", Hospital: "hospital_a",
	}, nil))

	w := env.do(t, http.MethodPost, "/staff/refresh", refreshRequest{RefreshToken: loginResp.RefreshToken}, nil)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	resp := decodeBody[tokenResponse](t, w)
	if resp.AccessToken == "" || resp.RefreshToken == "" {
		t.Fatal("expected non-empty tokens")
	}
	if resp.RefreshToken == loginResp.RefreshToken {
		t.Fatal("refresh should rotate to a new refresh token, not echo the old one")
	}
}

// Required test case in api-spec.md: replaying an already-rotated refresh
// token must be rejected, not silently accepted a second time.
func TestRefresh_HTTP_ReuseAfterRotation(t *testing.T) {
	env := newTestEnv()
	env.do(t, http.MethodPost, "/staff/create", createStaffRequest{
		Username: "nurse_j", Password: "S3cur3P@ss!", Hospital: "hospital_a",
	}, nil)
	loginResp := decodeBody[tokenResponse](t, env.do(t, http.MethodPost, "/staff/login", loginRequest{
		Username: "nurse_j", Password: "S3cur3P@ss!", Hospital: "hospital_a",
	}, nil))

	first := env.do(t, http.MethodPost, "/staff/refresh", refreshRequest{RefreshToken: loginResp.RefreshToken}, nil)
	if first.Code != http.StatusOK {
		t.Fatalf("first refresh status = %d, want 200, body: %s", first.Code, first.Body.String())
	}

	replay := env.do(t, http.MethodPost, "/staff/refresh", refreshRequest{RefreshToken: loginResp.RefreshToken}, nil)
	if replay.Code != http.StatusUnauthorized {
		t.Fatalf("replay status = %d, want 401, body: %s", replay.Code, replay.Body.String())
	}
	resp := decodeBody[errorResponse](t, replay)
	if resp.Error.Code != "INVALID_REFRESH_TOKEN" {
		t.Errorf("error code = %q, want INVALID_REFRESH_TOKEN", resp.Error.Code)
	}
}

func TestRefresh_HTTP_GarbageToken(t *testing.T) {
	env := newTestEnv()

	w := env.do(t, http.MethodPost, "/staff/refresh", refreshRequest{RefreshToken: "not-a-real-token"}, nil)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body: %s", w.Code, w.Body.String())
	}
	resp := decodeBody[errorResponse](t, w)
	if resp.Error.Code != "INVALID_REFRESH_TOKEN" {
		t.Errorf("error code = %q, want INVALID_REFRESH_TOKEN", resp.Error.Code)
	}
}

// ---- /patient/search ----

func TestPatientSearch_HTTP_RequiresAuth(t *testing.T) {
	env := newTestEnv()

	w := env.do(t, http.MethodPost, "/patient/search", patientSearchRequest{}, nil)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body: %s", w.Code, w.Body.String())
	}
	resp := decodeBody[errorResponse](t, w)
	if resp.Error.Code != "UNAUTHENTICATED" {
		t.Errorf("error code = %q, want UNAUTHENTICATED", resp.Error.Code)
	}
}

func TestPatientSearch_HTTP_WithValidToken(t *testing.T) {
	env := newTestEnv()
	env.patientRepo.patients = append(env.patientRepo.patients, patient.Patient{
		ID: uuid.New(), HospitalID: env.hospitalA.ID,
		PatientHN: "HN001", FirstNameEN: "Somchai", LastNameEN: "Jaidee",
	})

	env.do(t, http.MethodPost, "/staff/create", createStaffRequest{
		Username: "nurse_j", Password: "S3cur3P@ss!", Hospital: "hospital_a",
	}, nil)
	loginResp := decodeBody[tokenResponse](t, env.do(t, http.MethodPost, "/staff/login", loginRequest{
		Username: "nurse_j", Password: "S3cur3P@ss!", Hospital: "hospital_a",
	}, nil))

	w := env.do(t, http.MethodPost, "/patient/search", patientSearchRequest{}, map[string]string{
		"Authorization": "Bearer " + loginResp.AccessToken,
	})

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
	resp := decodeBody[patientSearchResponse](t, w)
	if resp.Total != 1 || len(resp.Data) != 1 {
		t.Fatalf("got %d results (total=%d), want 1", len(resp.Data), resp.Total)
	}
	if resp.Data[0].PatientHN != "HN001" {
		t.Errorf("PatientHN = %q, want HN001", resp.Data[0].PatientHN)
	}
	if resp.Page != 1 || resp.PageSize != patient.DefaultPageSize {
		t.Errorf("page=%d page_size=%d, want defaults 1/%d", resp.Page, resp.PageSize, patient.DefaultPageSize)
	}
}

func TestPatientSearch_HTTP_InvalidDateFormat(t *testing.T) {
	env := newTestEnv()
	env.do(t, http.MethodPost, "/staff/create", createStaffRequest{
		Username: "nurse_j", Password: "S3cur3P@ss!", Hospital: "hospital_a",
	}, nil)
	loginResp := decodeBody[tokenResponse](t, env.do(t, http.MethodPost, "/staff/login", loginRequest{
		Username: "nurse_j", Password: "S3cur3P@ss!", Hospital: "hospital_a",
	}, nil))

	badDate := "not-a-date"
	w := env.do(t, http.MethodPost, "/patient/search", patientSearchRequest{DateOfBirth: &badDate}, map[string]string{
		"Authorization": "Bearer " + loginResp.AccessToken,
	})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", w.Code, w.Body.String())
	}
}

func TestPatientSearch_HTTP_FieldTooLong(t *testing.T) {
	env := newTestEnv()
	env.do(t, http.MethodPost, "/staff/create", createStaffRequest{
		Username: "nurse_j", Password: "S3cur3P@ss!", Hospital: "hospital_a",
	}, nil)
	loginResp := decodeBody[tokenResponse](t, env.do(t, http.MethodPost, "/staff/login", loginRequest{
		Username: "nurse_j", Password: "S3cur3P@ss!", Hospital: "hospital_a",
	}, nil))

	tooLong := strings.Repeat("a", maxSearchFieldLength+1)
	w := env.do(t, http.MethodPost, "/patient/search", patientSearchRequest{FirstName: &tooLong}, map[string]string{
		"Authorization": "Bearer " + loginResp.AccessToken,
	})

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", w.Code, w.Body.String())
	}
	resp := decodeBody[errorResponse](t, w)
	if resp.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("error code = %q, want VALIDATION_ERROR", resp.Error.Code)
	}
}

func TestPatientSearch_HTTP_InvalidToken(t *testing.T) {
	env := newTestEnv()

	w := env.do(t, http.MethodPost, "/patient/search", patientSearchRequest{}, map[string]string{
		"Authorization": "Bearer not-a-real-token",
	})

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401, body: %s", w.Code, w.Body.String())
	}
}

func TestPatientSearch_HTTP_UnknownContentLengthEmptyBody(t *testing.T) {
	env := newTestEnv()
	env.do(t, http.MethodPost, "/staff/create", createStaffRequest{
		Username: "nurse_j", Password: "S3cur3P@ss!", Hospital: "hospital_a",
	}, nil)
	loginResp := decodeBody[tokenResponse](t, env.do(t, http.MethodPost, "/staff/login", loginRequest{
		Username: "nurse_j", Password: "S3cur3P@ss!", Hospital: "hospital_a",
	}, nil))

	// Simulate a client that doesn't report Content-Length (e.g. chunked
	// transfer) with a genuinely empty body — this must still succeed as
	// an empty search, not be rejected as invalid JSON.
	req := httptest.NewRequest(http.MethodPost, "/patient/search", bytes.NewReader(nil))
	req.ContentLength = -1
	req.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body: %s", w.Code, w.Body.String())
	}
}

func TestPatientSearch_HTTP_MalformedJSON(t *testing.T) {
	env := newTestEnv()
	env.do(t, http.MethodPost, "/staff/create", createStaffRequest{
		Username: "nurse_j", Password: "S3cur3P@ss!", Hospital: "hospital_a",
	}, nil)
	loginResp := decodeBody[tokenResponse](t, env.do(t, http.MethodPost, "/staff/login", loginRequest{
		Username: "nurse_j", Password: "S3cur3P@ss!", Hospital: "hospital_a",
	}, nil))

	req := httptest.NewRequest(http.MethodPost, "/patient/search", bytes.NewReader([]byte(`{not valid json`)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+loginResp.AccessToken)
	w := httptest.NewRecorder()
	env.router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body: %s", w.Code, w.Body.String())
	}
	resp := decodeBody[errorResponse](t, w)
	if resp.Error.Code != "VALIDATION_ERROR" {
		t.Errorf("error code = %q, want VALIDATION_ERROR", resp.Error.Code)
	}
}

func TestRecoveryMiddleware_ReturnsErrorEnvelope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(recoveryMiddleware())
	r.GET("/boom", func(c *gin.Context) {
		panic("something went wrong")
	})

	req := httptest.NewRequest(http.MethodGet, "/boom", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500, body: %s", w.Code, w.Body.String())
	}
	resp := decodeBody[errorResponse](t, w)
	if resp.Error.Code != "INTERNAL_ERROR" {
		t.Errorf("a panic should still produce the standard error envelope, got body: %s", w.Body.String())
	}
}
