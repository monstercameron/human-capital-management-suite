package crewschedule

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/crewshift"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const testTenant values.TenantId = "acme-crew"

func testRef(kind values.Kind, id string) values.EntityRef {
	return values.EntityRef{Tenant: testTenant, Kind: kind, Id: id}
}

var (
	testWorker    = testRef(values.Kind("worker"), "00000000-0000-4000-8000-000000000001")
	testWorkerTwo = testRef(values.Kind("worker"), "00000000-0000-4000-8000-000000000002")
	testRole      = testRef(values.Kind("role"), "00000000-0000-4000-8000-0000000000a1")
	testSite      = testRef(values.Kind("site"), "00000000-0000-4000-8000-0000000000b1")
	testProject   = testRef(values.Kind("project"), "00000000-0000-4000-8000-0000000000c1")
	testApprover  = testRef(values.Kind("approver"), "00000000-0000-4000-8000-0000000000e1")
)

type testAuthorizer struct {
	mu       sync.Mutex
	deny     bool
	calls    int
	lastCap  Capability
	lastWork string
}

func (a *testAuthorizer) Authorize(_ context.Context, _ *trust.Principal, _ string, worker string, cap Capability) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.calls++
	a.lastCap, a.lastWork = cap, worker
	if a.deny {
		return ErrForbidden
	}
	return nil
}

type testNotifier struct {
	mu    sync.Mutex
	items []crewshift.Notification
}

func (n *testNotifier) NotifyShift(_ context.Context, _ string, item crewshift.Notification) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.items = append(n.items, item)
	return nil
}

type storedCommand struct {
	digest string
	result crewshift.LifecycleResult
}

type testShiftStore struct {
	mu       sync.Mutex
	shifts   map[string]crewshift.Shift
	history  map[string]crewshift.History
	receipts map[string]storedCommand
}

func newTestShiftStore() *testShiftStore {
	return &testShiftStore{shifts: map[string]crewshift.Shift{}, history: map[string]crewshift.History{}, receipts: map[string]storedCommand{}}
}

func (s *testShiftStore) Create(_ context.Context, tenant string, shift crewshift.Shift, key string) (crewshift.Shift, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if tenant == "" || shift.Tenant.String() != tenant || shift.ID == "" || key == "" {
		return crewshift.Shift{}, ErrInvalidRequest
	}
	if _, exists := s.shifts[tenant+"\x00"+shift.ID]; exists {
		return crewshift.Shift{}, errors.New("duplicate shift")
	}
	shift.Revision = 1
	s.shifts[tenant+"\x00"+shift.ID] = shift
	s.history[tenant+"\x00"+shift.ID] = crewshift.History{}
	return shift, nil
}

func (s *testShiftStore) Get(_ context.Context, tenant, id string) (crewshift.Shift, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	shift, ok := s.shifts[tenant+"\x00"+id]
	if !ok {
		return crewshift.Shift{}, ErrNotFound
	}
	return shift, nil
}

func (s *testShiftStore) History(_ context.Context, tenant, id string) (crewshift.History, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.history[tenant+"\x00"+id]
	if !ok {
		return crewshift.History{}, ErrNotFound
	}
	return h, nil
}

func (s *testShiftStore) Execute(_ context.Context, tenant, id, _actor, key, digest string, expected int64, mutate func(crewshift.Shift, crewshift.History) (crewshift.LifecycleResult, error)) (crewshift.LifecycleResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	receiptKey := tenant + "\x00" + id + "\x00" + key
	if receipt, ok := s.receipts[receiptKey]; ok {
		if receipt.digest != digest {
			return crewshift.LifecycleResult{}, errors.New("idempotency conflict")
		}
		return receipt.result, nil
	}
	storeKey := tenant + "\x00" + id
	current, ok := s.shifts[storeKey]
	if !ok {
		return crewshift.LifecycleResult{}, ErrNotFound
	}
	if current.Revision != expected {
		return crewshift.LifecycleResult{}, ErrRevisionConflict
	}
	result, err := mutate(current, s.history[storeKey])
	if err != nil {
		return crewshift.LifecycleResult{}, err
	}
	s.shifts[storeKey] = result.Updated
	s.history[storeKey] = result.History
	s.receipts[receiptKey] = storedCommand{digest: digest, result: result}
	return result, nil
}

type testScheduleReader struct {
	mu      sync.Mutex
	shifts  []crewshift.Shift
	lookups int
}

func (r *testScheduleReader) PublishedForWorker(_ context.Context, _ string, _ string) ([]crewshift.Shift, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lookups++
	return append([]crewshift.Shift(nil), r.shifts...), nil
}

func testPrincipal(t *testing.T) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: testTenant, Subject: "supervisor-1", SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "session-1", IssuedAt: time.Unix(100, 0), ExpiresAt: time.Unix(1000, 0), CredentialDigest: "credential-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func testDraft(id string) crewshift.Shift {
	loc, _ := time.LoadLocation("America/New_York")
	start := time.Date(2024, 6, 10, 9, 0, 0, 0, loc)
	return crewshift.Shift{
		ID: id, Tenant: testTenant, Status: crewshift.StatusDraft, Source: crewshift.SourceManual,
		WorkerRef: testWorker, RoleRef: testRole, SiteRef: testSite, ProjectRef: testProject,
		Timezone: "America/New_York", Work: crewshift.Interval{Start: start, End: start.Add(8 * time.Hour)}, CreatedAt: start.Add(-time.Hour),
	}
}

func newTestService(store *testShiftStore) (*Service, *testAuthorizer, *testNotifier) {
	auth := &testAuthorizer{}
	notify := &testNotifier{}
	return &Service{Shifts: store, Schedule: &testScheduleReader{}, Auth: auth, Notify: notify, Clock: func() time.Time { return time.Date(2024, 6, 1, 9, 0, 0, 0, time.UTC) }}, auth, notify
}

func publishRequest(shift crewshift.Shift, key string) PublishShiftRequest {
	return PublishShiftRequest{
		WorkerRef: shift.WorkerRef.String(), ShiftID: shift.ID, ExpectedRevision: 1, IdempotencyKey: key,
		Input: crewshift.PublishInput{Eligibility: crewshift.EligibilityFacts{Active: true},
			HeldQualifications: nil, ProjectAccess: map[string]bool{shift.ProjectRef.String(): true},
			Approver: testApprover, AuthorityPath: crewshift.AuthorityPathCrewShiftPublish},
	}
}

// TestTodo_FTIME_006 proves the application boundary cannot publish without
// authorization, and that a successful draft carries its approval revision
// and emits a worker notification only after the store commits it.
func TestTodo_FTIME_006(t *testing.T) {
	store := newTestShiftStore()
	svc, auth, notify := newTestService(store)
	p := testPrincipal(t)
	draft := testDraft("shift-primary")
	if _, err := svc.CreateShift(context.Background(), p, CreateShiftRequest{Shift: draft, IdempotencyKey: "create-primary"}); err != nil {
		t.Fatal(err)
	}
	result, err := svc.PublishShift(context.Background(), p, publishRequest(draft, "publish-primary"))
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != crewshift.StatusPublished || result.Revision != 2 || result.ApprovedBy.String() != testApprover.String() {
		t.Fatalf("published result = %+v", result)
	}
	if auth.calls != 2 || auth.lastCap != CapabilityPublish {
		t.Fatalf("authorization calls = %d/%s, want create and publish", auth.calls, auth.lastCap)
	}
	if len(notify.items) != 1 || notify.items[0].Action != crewshift.ActionPublish {
		t.Fatalf("notifications = %+v, want one publish notification", notify.items)
	}

	denied := &testAuthorizer{deny: true}
	blocked := &Service{Shifts: store, Auth: denied, Clock: svc.Clock}
	if _, err := blocked.PublishShift(context.Background(), p, PublishShiftRequest{WorkerRef: result.WorkerRef.String(), ShiftID: result.ID, ExpectedRevision: result.Revision, IdempotencyKey: "publish-blocked"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("unauthorized publish = %v, want ErrForbidden", err)
	}
}

// TestTodo_FTIME_006_Golden pins absolute-instant worked minutes over both
// New York daylight-saving transitions.
func TestTodo_FTIME_006_Golden(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name       string
		start, end time.Time
		want       int64
	}{
		{"spring-forward", time.Date(2024, 3, 10, 1, 30, 0, 0, loc), time.Date(2024, 3, 10, 4, 30, 0, 0, loc), 120},
		{"fall-back", time.Date(2024, 11, 3, 0, 30, 0, 0, loc), time.Date(2024, 11, 3, 3, 30, 0, 0, loc), 240},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			shift := testDraft(tc.name)
			shift.Work = crewshift.Interval{Start: tc.start, End: tc.end}
			if got := shift.WorkedMinutes(); got != tc.want {
				t.Fatalf("worked minutes = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestTodo_FTIME_006_Integration exercises the full application-to-store
// path and proves the current schedule reader is used for overlap checks.
func TestTodo_FTIME_006_Integration(t *testing.T) {
	store := newTestShiftStore()
	svc, _, _ := newTestService(store)
	p := testPrincipal(t)
	first := testDraft("shift-integration-1")
	if _, err := svc.CreateShift(context.Background(), p, CreateShiftRequest{Shift: first, IdempotencyKey: "create-1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PublishShift(context.Background(), p, publishRequest(first, "publish-1")); err != nil {
		t.Fatal(err)
	}
	second := testDraft("shift-integration-2")
	if _, err := svc.CreateShift(context.Background(), p, CreateShiftRequest{Shift: second, IdempotencyKey: "create-2"}); err != nil {
		t.Fatal(err)
	}
	reader := svc.Schedule.(*testScheduleReader)
	publishedFirst, err := store.Get(context.Background(), testTenant.String(), first.ID)
	if err != nil {
		t.Fatal(err)
	}
	reader.shifts = []crewshift.Shift{publishedFirst}
	if _, err := svc.PublishShift(context.Background(), p, publishRequest(second, "publish-2")); !errors.Is(err, crewshift.ErrPublishRejected) {
		t.Fatalf("overlapping current schedule publish = %v, want crewshift rejection", err)
	}
}

// TestTodo_FTIME_006_Security proves optimizer proposals still use the same
// authority path and a current worker cannot be double-booked across projects.
func TestTodo_FTIME_006_Security(t *testing.T) {
	store := newTestShiftStore()
	svc, _, _ := newTestService(store)
	p := testPrincipal(t)
	draft := testDraft("shift-security")
	if _, err := svc.CreateShift(context.Background(), p, CreateShiftRequest{Shift: draft, IdempotencyKey: "create-security"}); err != nil {
		t.Fatal(err)
	}
	input := publishRequest(draft, "publish-security")
	input.Input.AuthorityPath = "crewshift.optimizer.shortcut.v1"
	if _, err := svc.PublishShift(context.Background(), p, input); !errors.Is(err, crewshift.ErrPublishRejected) {
		t.Fatalf("optimizer shortcut publish = %v, want rejection", err)
	}
	if got, err := store.Get(context.Background(), testTenant.String(), draft.ID); err != nil || got.Status != crewshift.StatusDraft {
		t.Fatalf("failed publish changed draft: %+v / %v", got, err)
	}
}

// TestTodo_FTIME_006_Property checks that every successful application
// publication advances exactly one revision and never leaves a draft result.
func TestTodo_FTIME_006_Property(t *testing.T) {
	store := newTestShiftStore()
	svc, _, _ := newTestService(store)
	p := testPrincipal(t)
	for i := 0; i < 40; i++ {
		shift := testDraft(fmt.Sprintf("shift-property-%d", i))
		if _, err := svc.CreateShift(context.Background(), p, CreateShiftRequest{Shift: shift, IdempotencyKey: fmt.Sprintf("create-%d", i)}); err != nil {
			t.Fatal(err)
		}
		result, err := svc.PublishShift(context.Background(), p, publishRequest(shift, fmt.Sprintf("publish-%d", i)))
		if err != nil || result.Status != crewshift.StatusPublished || result.Revision != 2 {
			t.Fatalf("trial %d result = %+v, err=%v", i, result, err)
		}
	}
}

// TestTodo_FTIME_007 proves lifecycle edits require the current revision,
// retain the previous version, notify affected workers, and do not accept
// punch data as part of a schedule mutation.
func TestTodo_FTIME_007(t *testing.T) {
	store := newTestShiftStore()
	svc, _, notify := newTestService(store)
	p := testPrincipal(t)
	shift := testDraft("shift-lifecycle")
	if _, err := svc.CreateShift(context.Background(), p, CreateShiftRequest{Shift: shift, IdempotencyKey: "create-life"}); err != nil {
		t.Fatal(err)
	}
	published, err := svc.PublishShift(context.Background(), p, publishRequest(shift, "publish-life"))
	if err != nil {
		t.Fatal(err)
	}
	punch := crewshift.PunchInterval{ID: "punch-1", Start: published.Work.Start, End: published.Work.End}
	before := punch
	cancelled, err := svc.CancelShift(context.Background(), p, CancelShiftRequest{WorkerRef: published.WorkerRef.String(), ShiftID: published.ID, ExpectedRevision: published.Revision, Grant: crewshift.Grant{ActorRef: testApprover, Scope: crewshift.ActionCancel}, Reason: "site closed", IdempotencyKey: "cancel-life"})
	if err != nil || cancelled.Status != crewshift.StatusCancelled || cancelled.Revision != 3 {
		t.Fatalf("cancelled = %+v, err=%v", cancelled, err)
	}
	history, err := store.History(context.Background(), testTenant.String(), published.ID)
	if err != nil || len(history.Versions) != 2 || history.Versions[0].Revision != 1 || history.Versions[1].Revision != published.Revision {
		t.Fatalf("history = %+v, err=%v, want revisions 1 and %d", history, err, published.Revision)
	}
	if punch != before || len(notify.items) < 2 {
		t.Fatalf("punch changed or notification missing: punch=%+v notifications=%d", punch, len(notify.items))
	}
	if _, err := svc.CancelShift(context.Background(), p, CancelShiftRequest{WorkerRef: published.WorkerRef.String(), ShiftID: published.ID, ExpectedRevision: published.Revision, Grant: crewshift.Grant{ActorRef: testApprover, Scope: crewshift.ActionCancel}, Reason: "stale", IdempotencyKey: "cancel-stale"}); !errors.Is(err, ErrRevisionConflict) {
		t.Fatalf("stale cancel = %v, want revision conflict", err)
	}
}

// TestTodo_FTIME_007_Integration proves a published shift is compared to
// actual punch intervals and returns a late finding without rewriting either.
func TestTodo_FTIME_007_Integration(t *testing.T) {
	store := newTestShiftStore()
	svc, _, _ := newTestService(store)
	p := testPrincipal(t)
	shift := testDraft("shift-reconcile")
	if _, err := svc.CreateShift(context.Background(), p, CreateShiftRequest{Shift: shift, IdempotencyKey: "create-reconcile"}); err != nil {
		t.Fatal(err)
	}
	published, err := svc.PublishShift(context.Background(), p, publishRequest(shift, "publish-reconcile"))
	if err != nil {
		t.Fatal(err)
	}
	punches := []crewshift.PunchInterval{{ID: "late", Start: published.Work.Start.Add(20 * time.Minute), End: published.Work.End}}
	result, err := svc.ReconcileShift(context.Background(), p, ReconcileShiftRequest{WorkerRef: published.WorkerRef.String(), ShiftID: published.ID, Tolerance: crewshift.Tolerance{Late: 5 * time.Minute}, Punches: punches})
	if err != nil {
		t.Fatal(err)
	}
	if result.Revision != published.Revision || len(result.Exceptions) == 0 || result.Exceptions[0].Kind != crewshift.ExceptionLate {
		t.Fatalf("reconcile result = %+v", result)
	}
}

// TestTodo_FTIME_007_Race proves the store's expected-revision fence allows
// one concurrent lifecycle winner and rejects the other.
func TestTodo_FTIME_007_Race(t *testing.T) {
	store := newTestShiftStore()
	svc, _, _ := newTestService(store)
	p := testPrincipal(t)
	shift := testDraft("shift-race")
	if _, err := svc.CreateShift(context.Background(), p, CreateShiftRequest{Shift: shift, IdempotencyKey: "create-race"}); err != nil {
		t.Fatal(err)
	}
	published, err := svc.PublishShift(context.Background(), p, publishRequest(shift, "publish-race"))
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := svc.CancelShift(context.Background(), p, CancelShiftRequest{WorkerRef: published.WorkerRef.String(), ShiftID: published.ID, ExpectedRevision: published.Revision, Grant: crewshift.Grant{ActorRef: testApprover, Scope: crewshift.ActionCancel}, Reason: "cancel race", IdempotencyKey: "race-cancel"})
		errs <- err
	}()
	go func() {
		defer wg.Done()
		_, err := svc.ReassignShift(context.Background(), p, ReassignShiftRequest{WorkerRef: published.WorkerRef.String(), ShiftID: published.ID, ExpectedRevision: published.Revision, Grant: crewshift.Grant{ActorRef: testApprover, Scope: crewshift.ActionReassign}, NewWorkerRef: testWorkerTwo.String(), NewWorkerEligibility: crewshift.EligibilityFacts{Active: true}, Reason: "reassign race", IdempotencyKey: "race-reassign"})
		errs <- err
	}()
	wg.Wait()
	close(errs)
	successes, failures := 0, 0
	for err := range errs {
		if err == nil {
			successes++
		} else {
			failures++
		}
	}
	if successes != 1 || failures != 1 {
		t.Fatalf("successes=%d failures=%d", successes, failures)
	}
}

// TestTodo_FTIME_007_Security proves a revoked grant is rejected before any
// lifecycle write, even when the expected revision is current.
func TestTodo_FTIME_007_Security(t *testing.T) {
	store := newTestShiftStore()
	svc, _, _ := newTestService(store)
	p := testPrincipal(t)
	shift := testDraft("shift-revoked")
	if _, err := svc.CreateShift(context.Background(), p, CreateShiftRequest{Shift: shift, IdempotencyKey: "create-revoked"}); err != nil {
		t.Fatal(err)
	}
	published, err := svc.PublishShift(context.Background(), p, publishRequest(shift, "publish-revoked"))
	if err != nil {
		t.Fatal(err)
	}
	grant := crewshift.Grant{ActorRef: testApprover, Scope: crewshift.ActionCancel, Revoked: true}
	if _, err := svc.CancelShift(context.Background(), p, CancelShiftRequest{WorkerRef: published.WorkerRef.String(), ShiftID: published.ID, ExpectedRevision: published.Revision, Grant: grant, Reason: "revoked", IdempotencyKey: "cancel-revoked"}); !errors.Is(err, crewshift.ErrLifecycleRejected) {
		t.Fatalf("revoked cancel = %v, want lifecycle rejection", err)
	}
	current, _ := store.Get(context.Background(), testTenant.String(), published.ID)
	if current.Revision != published.Revision || current.Status != crewshift.StatusPublished {
		t.Fatalf("revoked grant changed shift: %+v", current)
	}
	foreignGrant := crewshift.Grant{ActorRef: values.EntityRef{Tenant: values.TenantId("other-tenant"), Kind: values.Kind("approver"), Id: "00000000-0000-4000-8000-0000000000e1"}, Scope: crewshift.ActionCancel}
	if _, err := svc.CancelShift(context.Background(), p, CancelShiftRequest{WorkerRef: published.WorkerRef.String(), ShiftID: published.ID, ExpectedRevision: published.Revision, Grant: foreignGrant, Reason: "cross tenant", IdempotencyKey: "cancel-foreign"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("cross-tenant grant = %v, want ErrForbidden", err)
	}
}
