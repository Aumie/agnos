package patient

import (
	"context"
	"errors"
	"log"
	"strings"
	"testing"

	"github.com/google/uuid"

	"hospital-middleware/internal/event"
	"hospital-middleware/internal/hospital"
)

// ---- hand-written fakes ----

type fakeRepo struct {
	patients []Patient
}

func (f *fakeRepo) FindByNationalOrPassportID(ctx context.Context, hospitalID uuid.UUID, id string) (Patient, error) {
	for _, p := range f.patients {
		if p.HospitalID != hospitalID {
			continue
		}
		if p.NationalID != nil && *p.NationalID == id {
			return p, nil
		}
		if p.PassportID != nil && *p.PassportID == id {
			return p, nil
		}
	}
	return Patient{}, ErrNotFound
}

func (f *fakeRepo) Upsert(ctx context.Context, p Patient) (Patient, error) {
	for i, existing := range f.patients {
		if existing.HospitalID == p.HospitalID && existing.PatientHN == p.PatientHN {
			if p.ID == uuid.Nil {
				p.ID = existing.ID
			}
			f.patients[i] = p
			return p, nil
		}
	}
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	f.patients = append(f.patients, p)
	return p, nil
}

func (f *fakeRepo) Search(ctx context.Context, hospitalID uuid.UUID, filter SearchFilter) ([]Patient, int, error) {
	var matches []Patient
	for _, p := range f.patients {
		if p.HospitalID != hospitalID {
			continue
		}
		if filter.NationalID != nil && (p.NationalID == nil || *p.NationalID != *filter.NationalID) {
			continue
		}
		if filter.PassportID != nil && (p.PassportID == nil || *p.PassportID != *filter.PassportID) {
			continue
		}
		if filter.FirstName != nil && !containsFold(p.FirstNameTH, *filter.FirstName) && !containsFold(p.FirstNameEN, *filter.FirstName) {
			continue
		}
		if filter.LastName != nil && !containsFold(p.LastNameTH, *filter.LastName) && !containsFold(p.LastNameEN, *filter.LastName) {
			continue
		}
		matches = append(matches, p)
	}

	total := len(matches)
	start := (filter.Page - 1) * filter.PageSize
	if start > len(matches) {
		start = len(matches)
	}
	end := start + filter.PageSize
	if end > len(matches) {
		end = len(matches)
	}
	return matches[start:end], total, nil
}

func containsFold(s, substr string) bool {
	if s == "" || substr == "" {
		return false
	}
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

type fakeHospitals struct {
	byID map[uuid.UUID]hospital.Hospital
}

func newFakeHospitals(hs ...hospital.Hospital) *fakeHospitals {
	m := map[uuid.UUID]hospital.Hospital{}
	for _, h := range hs {
		m[h.ID] = h
	}
	return &fakeHospitals{byID: m}
}

func (f *fakeHospitals) FindByID(ctx context.Context, id uuid.UUID) (hospital.Hospital, error) {
	h, ok := f.byID[id]
	if !ok {
		return hospital.Hospital{}, ErrHospitalNotFound
	}
	return h, nil
}

type fakeHISClient struct {
	byID  map[string]Patient
	calls int
}

func (f *fakeHISClient) SearchByID(ctx context.Context, id string) (Patient, error) {
	f.calls++
	p, ok := f.byID[id]
	if !ok {
		return Patient{}, ErrHISNoMatch
	}
	return p, nil
}

type fakeHISRegistry struct {
	byCode map[string]HISClient
}

func newFakeHISRegistry() *fakeHISRegistry {
	return &fakeHISRegistry{byCode: map[string]HISClient{}}
}

func (f *fakeHISRegistry) Resolve(code string) (HISClient, bool) {
	c, ok := f.byCode[code]
	return c, ok
}

// ---- fixtures ----

func hospitalA() hospital.Hospital {
	return hospital.Hospital{ID: uuid.New(), Code: "hospital_a", Name: "Hospital A"}
}

func hospitalB() hospital.Hospital {
	return hospital.Hospital{ID: uuid.New(), Code: "hospital_b", Name: "Hospital B"}
}

func strPtr(s string) *string { return &s }

func newService(hs *fakeHospitals, his *fakeHISRegistry, repo *fakeRepo, bus *event.Bus[SyncedEvent]) *Service {
	if repo == nil {
		repo = &fakeRepo{}
	}
	if bus == nil {
		bus = event.NewBus[SyncedEvent]()
	}
	logger := log.New(discardWriter{}, "", 0)
	events := EventPublishers{Synced: bus, SyncFailed: event.NewBus[SyncFailedEvent]()}
	return NewService(repo, hs, his, events, logger)
}

// NewService must fail loudly at construction, not silently at the first
// Search that happens to hit the unwired path — see event.RequireComplete.
func TestNewService_PanicsOnIncompleteEventPublishers(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected NewService to panic with SyncFailed left unset")
		}
	}()

	incomplete := EventPublishers{Synced: event.NewBus[SyncedEvent]()} // SyncFailed unset
	NewService(&fakeRepo{}, &fakeHospitals{}, &fakeHISRegistry{}, incomplete, nil)
}

// discardWriter discards log output so tests don't spam stdout for the
// expected "no adapter registered" log line.
type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

// ---- required test cases from api-spec.md ----

func TestSearch_IDMatch_AdapterRegistered_HISHasMatch(t *testing.T) {
	hA := hospitalA()
	his := newFakeHISRegistry()
	hisClient := &fakeHISClient{byID: map[string]Patient{
		"1234567890123": {
			PatientHN:   "HN001",
			NationalID:  strPtr("1234567890123"),
			FirstNameTH: "สมชาย", LastNameTH: "ใจดี",
			FirstNameEN: "Somchai", LastNameEN: "Jaidee",
		},
	}}
	his.byCode["hospital_a"] = hisClient
	repo := &fakeRepo{}
	svc := newService(newFakeHospitals(hA), his, repo, nil)

	got, total, err := svc.Search(context.Background(), hA.ID, SearchFilter{NationalID: strPtr("1234567890123")})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if hisClient.calls != 1 {
		t.Fatalf("HIS adapter should be called exactly once, got %d calls", hisClient.calls)
	}
	if total != 1 || len(got) != 1 {
		t.Fatalf("got %d results (total=%d), want 1", len(got), total)
	}
	if got[0].PatientHN != "HN001" {
		t.Errorf("PatientHN = %q, want HN001", got[0].PatientHN)
	}
	if len(repo.patients) != 1 {
		t.Fatalf("upsert should have cached the patient, got %d rows", len(repo.patients))
	}
}

func TestSearch_IDMatch_AdapterRegistered_HISNoMatch(t *testing.T) {
	hA := hospitalA()
	his := newFakeHISRegistry()
	his.byCode["hospital_a"] = &fakeHISClient{byID: map[string]Patient{}} // no match for any id
	svc := newService(newFakeHospitals(hA), his, nil, nil)

	got, total, err := svc.Search(context.Background(), hA.ID, SearchFilter{NationalID: strPtr("0000000000000")})
	if err != nil {
		t.Fatalf("Search should not error when HIS has no match, got: %v", err)
	}
	if total != 0 || len(got) != 0 {
		t.Fatalf("got %d results, want 0", len(got))
	}
}

func TestSearch_IDMatch_NoAdapterRegistered(t *testing.T) {
	hA := hospitalA()
	his := newFakeHISRegistry() // nothing registered for hospital_a
	svc := newService(newFakeHospitals(hA), his, nil, nil)

	got, total, err := svc.Search(context.Background(), hA.ID, SearchFilter{NationalID: strPtr("1234567890123")})
	if err != nil {
		t.Fatalf("Search should not error when no adapter is registered, got: %v", err)
	}
	if total != 0 || len(got) != 0 {
		t.Fatalf("got %d results, want 0 (cached-only, nothing cached)", len(got))
	}
}

func TestSearch_NameOnlySearch_NeverInvokesAdapter(t *testing.T) {
	hA := hospitalA()
	his := newFakeHISRegistry()
	hisClient := &fakeHISClient{byID: map[string]Patient{}}
	his.byCode["hospital_a"] = hisClient // registered, but must never be called
	repo := &fakeRepo{patients: []Patient{
		{HospitalID: hA.ID, PatientHN: "HN001", FirstNameEN: "Somchai", LastNameEN: "Jaidee"},
	}}
	svc := newService(newFakeHospitals(hA), his, repo, nil)

	_, _, err := svc.Search(context.Background(), hA.ID, SearchFilter{FirstName: strPtr("Somchai")})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if hisClient.calls != 0 {
		t.Fatalf("HIS adapter should never be called for a name-only search, got %d calls", hisClient.calls)
	}
}

func TestSearch_ResultsScopedToHospital(t *testing.T) {
	hA, hB := hospitalA(), hospitalB()
	repo := &fakeRepo{patients: []Patient{
		{HospitalID: hA.ID, PatientHN: "HN001", NationalID: strPtr("1111111111111"), FirstNameEN: "A", LastNameEN: "A"},
		{HospitalID: hB.ID, PatientHN: "HN002", NationalID: strPtr("1111111111111"), FirstNameEN: "B", LastNameEN: "B"},
	}}
	svc := newService(newFakeHospitals(hA, hB), newFakeHISRegistry(), repo, nil)

	got, total, err := svc.Search(context.Background(), hA.ID, SearchFilter{NationalID: strPtr("1111111111111")})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if total != 1 || len(got) != 1 || got[0].HospitalID != hA.ID {
		t.Fatalf("search scoped to hospital A should not return hospital B's patient, got %+v", got)
	}
}

func TestSearch_FirstNameMatchesEitherLanguage(t *testing.T) {
	hA := hospitalA()
	repo := &fakeRepo{patients: []Patient{
		{HospitalID: hA.ID, PatientHN: "HN001", FirstNameTH: "สมชาย", FirstNameEN: "Somchai"},
	}}
	svc := newService(newFakeHospitals(hA), newFakeHISRegistry(), repo, nil)

	// Thai name matches even though the query is Thai and English differs.
	gotTH, _, err := svc.Search(context.Background(), hA.ID, SearchFilter{FirstName: strPtr("สมชาย")})
	if err != nil {
		t.Fatalf("Search (TH): %v", err)
	}
	if len(gotTH) != 1 {
		t.Fatalf("Thai first_name search should match, got %d results", len(gotTH))
	}

	// English name matches too — same patient, other language.
	gotEN, _, err := svc.Search(context.Background(), hA.ID, SearchFilter{FirstName: strPtr("Somchai")})
	if err != nil {
		t.Fatalf("Search (EN): %v", err)
	}
	if len(gotEN) != 1 {
		t.Fatalf("English first_name search should match, got %d results", len(gotEN))
	}
}

func TestSearch_PublishesSyncedEventOnUpsert(t *testing.T) {
	hA := hospitalA()
	his := newFakeHISRegistry()
	his.byCode["hospital_a"] = &fakeHISClient{byID: map[string]Patient{
		"1234567890123": {PatientHN: "HN001", NationalID: strPtr("1234567890123")},
	}}

	bus := event.NewBus[SyncedEvent]()
	var published []SyncedEvent
	bus.Subscribe(func(ctx context.Context, e SyncedEvent) error {
		published = append(published, e)
		return nil
	})

	svc := newService(newFakeHospitals(hA), his, &fakeRepo{}, bus)

	if _, _, err := svc.Search(context.Background(), hA.ID, SearchFilter{NationalID: strPtr("1234567890123")}); err != nil {
		t.Fatalf("Search: %v", err)
	}

	if len(published) != 1 {
		t.Fatalf("expected exactly 1 SyncedEvent published, got %d", len(published))
	}
	if published[0].PatientHN != "HN001" || published[0].HospitalID != hA.ID {
		t.Errorf("unexpected event contents: %+v", published[0])
	}
}

func TestSearch_SucceedsEvenIfEventPublishFails(t *testing.T) {
	hA := hospitalA()
	his := newFakeHISRegistry()
	his.byCode["hospital_a"] = &fakeHISClient{byID: map[string]Patient{
		"1234567890123": {PatientHN: "HN001", NationalID: strPtr("1234567890123")},
	}}

	bus := event.NewBus[SyncedEvent]()
	bus.Subscribe(func(ctx context.Context, e SyncedEvent) error {
		return errors.New("audit subscriber is broken")
	})

	svc := newService(newFakeHospitals(hA), his, &fakeRepo{}, bus)

	got, total, err := svc.Search(context.Background(), hA.ID, SearchFilter{NationalID: strPtr("1234567890123")})
	if err != nil {
		t.Fatalf("Search should succeed even when the event publisher fails, got: %v", err)
	}
	if total != 1 || len(got) != 1 {
		t.Fatalf("the patient should still be returned despite the publish failure, got %d results", len(got))
	}
}

func TestSearch_NoSyncedEvent_WhenNothingUpserted(t *testing.T) {
	hA := hospitalA()
	his := newFakeHISRegistry() // no adapter -> nothing to sync

	bus := event.NewBus[SyncedEvent]()
	called := false
	bus.Subscribe(func(ctx context.Context, e SyncedEvent) error {
		called = true
		return nil
	})

	svc := newService(newFakeHospitals(hA), his, &fakeRepo{}, bus)

	if _, _, err := svc.Search(context.Background(), hA.ID, SearchFilter{NationalID: strPtr("1234567890123")}); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if called {
		t.Fatal("SyncedEvent should not publish when nothing was upserted")
	}
}

func TestSearch_PaginationDefaults(t *testing.T) {
	hA := hospitalA()
	var patients []Patient
	for i := 0; i < 25; i++ {
		patients = append(patients, Patient{HospitalID: hA.ID, PatientHN: uuid.NewString(), FirstNameEN: "Somchai"})
	}
	repo := &fakeRepo{patients: patients}
	svc := newService(newFakeHospitals(hA), newFakeHISRegistry(), repo, nil)

	got, total, err := svc.Search(context.Background(), hA.ID, SearchFilter{FirstName: strPtr("Somchai")})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if total != 25 {
		t.Fatalf("total = %d, want 25", total)
	}
	if len(got) != DefaultPageSize {
		t.Fatalf("got %d results with no page_size set, want default %d", len(got), DefaultPageSize)
	}
}

func TestSearch_PageSizeClampedToMax(t *testing.T) {
	hA := hospitalA()
	svc := newService(newFakeHospitals(hA), newFakeHISRegistry(), &fakeRepo{}, nil)

	// Indirectly verify the clamp via ApplyPaginationDefaults, since the
	// fake repo would otherwise silently accept any page size.
	f := ApplyPaginationDefaults(SearchFilter{PageSize: 9999})
	if f.PageSize != MaxPageSize {
		t.Fatalf("PageSize = %d, want clamped to %d", f.PageSize, MaxPageSize)
	}
	_ = svc
}

func TestSearch_PropagatesGenuineHISError(t *testing.T) {
	hA := hospitalA()
	his := newFakeHISRegistry()
	wantErr := errors.New("boom: upstream timeout")
	his.byCode["hospital_a"] = failingHISClient{err: wantErr}
	svc := newService(newFakeHospitals(hA), his, &fakeRepo{}, nil)

	_, _, err := svc.Search(context.Background(), hA.ID, SearchFilter{NationalID: strPtr("1234567890123")})
	if !errors.Is(err, wantErr) {
		t.Fatalf("a genuine HIS/transport error should propagate, got: %v", err)
	}
}

type failingHISClient struct{ err error }

func (f failingHISClient) SearchByID(ctx context.Context, id string) (Patient, error) {
	return Patient{}, f.err
}

func TestSearch_PublishesSyncFailedEvent_OnGenuineHISError(t *testing.T) {
	hA := hospitalA()
	his := newFakeHISRegistry()
	wantErr := errors.New("boom: upstream timeout")
	his.byCode["hospital_a"] = failingHISClient{err: wantErr}

	var published []SyncFailedEvent
	syncFailedBus := event.NewBus[SyncFailedEvent]()
	syncFailedBus.Subscribe(func(ctx context.Context, e SyncFailedEvent) error {
		published = append(published, e)
		return nil
	})
	events := EventPublishers{Synced: event.NewBus[SyncedEvent](), SyncFailed: syncFailedBus}
	svc := NewService(&fakeRepo{}, newFakeHospitals(hA), his, events, log.New(discardWriter{}, "", 0))

	_, _, err := svc.Search(context.Background(), hA.ID, SearchFilter{NationalID: strPtr("1234567890123")})
	if !errors.Is(err, wantErr) {
		t.Fatalf("expected the original error to still propagate, got: %v", err)
	}

	if len(published) != 1 {
		t.Fatalf("expected exactly 1 SyncFailedEvent, got %d", len(published))
	}
	if published[0].HospitalID != hA.ID || published[0].ID != "1234567890123" || published[0].Err != wantErr.Error() {
		t.Errorf("unexpected event contents: %+v", published[0])
	}
}
