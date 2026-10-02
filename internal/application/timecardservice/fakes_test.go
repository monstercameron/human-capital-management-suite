package timecardservice

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/crewshift"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timecard"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timeprofile"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timesession"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// -- shared test helpers -----------------------------------------------

func testPrincipal(t *testing.T, tenant, subject string) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId(tenant), Subject: subject, SubjectKind: trust.SubjectKindHuman,
		OrganizationScopeID: "org-1", Purposes: []string{"time"},
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "session-" + subject, IssuedAt: time.Unix(100, 0), ExpiresAt: time.Unix(1e9, 0),
		CredentialDigest: "digest-" + subject,
	})
	if err != nil {
		t.Fatalf("NewPrincipal: %v", err)
	}
	return p
}

// -- Authorizer fake -----------------------------------------------------

type fakeAuthorizer struct {
	mu     sync.Mutex
	denied map[string]bool // key: tenant|worker|cap
}

func newFakeAuthorizer() *fakeAuthorizer { return &fakeAuthorizer{denied: map[string]bool{}} }

func (f *fakeAuthorizer) deny(tenant, worker string, cap Capability) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.denied[tenant+"|"+worker+"|"+string(cap)] = true
}

func (f *fakeAuthorizer) Authorize(ctx context.Context, actor *trust.Principal, tenant, workerRef string, cap Capability) error {
	if actor == nil || string(actor.Tenant()) != tenant {
		return ErrForbidden
	}
	f.mu.Lock()
	denied := f.denied[tenant+"|"+workerRef+"|"+string(cap)]
	f.mu.Unlock()
	if denied {
		return ErrForbidden
	}
	return nil
}

// -- WorkerDirectory fake -------------------------------------------------

type fakeWorkers struct {
	mu         sync.Mutex
	exists     map[string]bool            // tenant|worker
	supervises map[string]map[string]bool // tenant|supervisor -> worker set
}

func newFakeWorkers() *fakeWorkers {
	return &fakeWorkers{exists: map[string]bool{}, supervises: map[string]map[string]bool{}}
}

func (f *fakeWorkers) addWorker(tenant, worker string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.exists[tenant+"|"+worker] = true
}

func (f *fakeWorkers) addScope(tenant, supervisor, worker string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := tenant + "|" + supervisor
	if f.supervises[key] == nil {
		f.supervises[key] = map[string]bool{}
	}
	f.supervises[key][worker] = true
}

func (f *fakeWorkers) ResolveWorker(ctx context.Context, tenant, workerRef string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.exists[tenant+"|"+workerRef], nil
}

func (f *fakeWorkers) InScope(ctx context.Context, tenant, supervisorRef, workerRef string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.supervises[tenant+"|"+supervisorRef][workerRef], nil
}

// -- Notifier fake ---------------------------------------------------------

type fakeNotifier struct {
	mu            sync.Mutex
	shiftEvents   []crewshift.Notification
	missedPunches []string
}

func (f *fakeNotifier) NotifyShift(ctx context.Context, tenant string, n crewshift.Notification) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.shiftEvents = append(f.shiftEvents, n)
	return nil
}

func (f *fakeNotifier) NotifyMissedPunch(ctx context.Context, tenant, workerRef, requestID, outcome string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.missedPunches = append(f.missedPunches, outcome)
	return nil
}

// -- IDs fake ---------------------------------------------------------------

type fakeIDs struct{ n int }

func (f *fakeIDs) NewID(tenant, actor, operation, idempotencyKey string) string {
	f.n++
	return operation + "-" + idempotencyKey
}

// -- TimecardStore fake ------------------------------------------------------

type fakeTimecardRecord struct {
	tc        timecard.Timecard
	repliesBy map[string]replayEntry // idempotencyKey -> (digest, result)
}

type replayEntry struct {
	digest string
	result timecard.Timecard
}

type fakeTimecardStore struct {
	mu      sync.Mutex
	records map[string]*fakeTimecardRecord // tenant|id
}

func newFakeTimecardStore() *fakeTimecardStore {
	return &fakeTimecardStore{records: map[string]*fakeTimecardRecord{}}
}

func (f *fakeTimecardStore) key(tenant, id string) string { return tenant + "|" + id }

func (f *fakeTimecardStore) Create(ctx context.Context, tenant string, tc timecard.Timecard, idempotencyKey string) (timecard.Timecard, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := f.key(tenant, tc.AssignmentID+":"+tc.WorkerID)
	if rec, ok := f.records[k]; ok {
		return rec.tc, nil
	}
	rec := &fakeTimecardRecord{tc: tc, repliesBy: map[string]replayEntry{}}
	f.records[k] = rec
	f.records[f.key(tenant, tc.WorkerID)] = rec
	return tc, nil
}

func (f *fakeTimecardStore) Get(ctx context.Context, tenant, id string) (timecard.Timecard, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.records[f.key(tenant, id)]
	if !ok {
		return timecard.Timecard{}, ErrNotFound
	}
	return rec.tc, nil
}

func (f *fakeTimecardStore) Execute(ctx context.Context, tenant, id, actor, idempotencyKey, dig string, expectedRevision uint64,
	mutate func(timecard.Timecard) (timecard.Timecard, error)) (timecard.Timecard, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.records[f.key(tenant, id)]
	if !ok {
		return timecard.Timecard{}, ErrNotFound
	}
	if prior, ok := rec.repliesBy[idempotencyKey]; ok {
		if prior.digest != dig {
			return timecard.Timecard{}, ErrIdempotencyConflict
		}
		return prior.result, nil
	}
	if rec.tc.Revision != expectedRevision {
		return timecard.Timecard{}, ErrRevisionConflict
	}
	result, err := mutate(rec.tc)
	if err != nil {
		return timecard.Timecard{}, err
	}
	rec.tc = result
	rec.repliesBy[idempotencyKey] = replayEntry{digest: dig, result: result}
	return result, nil
}

// register makes an already-constructed timecard directly gettable under
// id, for tests that need a preset starting state.
func (f *fakeTimecardStore) register(tenant, id string, tc timecard.Timecard) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records[f.key(tenant, id)] = &fakeTimecardRecord{tc: tc, repliesBy: map[string]replayEntry{}}
}

// -- ShiftStore fake -----------------------------------------------------

type fakeShiftRecord struct {
	shift   crewshift.Shift
	history crewshift.History
	replies map[string]shiftReplay
}

type shiftReplay struct {
	digest string
	result crewshift.LifecycleResult
}

type fakeShiftStore struct {
	mu      sync.Mutex
	records map[string]*fakeShiftRecord
}

func newFakeShiftStore() *fakeShiftStore {
	return &fakeShiftStore{records: map[string]*fakeShiftRecord{}}
}

func (f *fakeShiftStore) key(tenant, id string) string { return tenant + "|" + id }

func (f *fakeShiftStore) Create(ctx context.Context, tenant string, sh crewshift.Shift, idempotencyKey string) (crewshift.Shift, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := f.key(tenant, sh.ID)
	if rec, ok := f.records[k]; ok {
		return rec.shift, nil
	}
	f.records[k] = &fakeShiftRecord{shift: sh, replies: map[string]shiftReplay{}}
	return sh, nil
}

func (f *fakeShiftStore) Get(ctx context.Context, tenant, id string) (crewshift.Shift, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.records[f.key(tenant, id)]
	if !ok {
		return crewshift.Shift{}, ErrNotFound
	}
	return rec.shift, nil
}

func (f *fakeShiftStore) History(ctx context.Context, tenant, id string) (crewshift.History, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.records[f.key(tenant, id)]
	if !ok {
		return crewshift.History{}, ErrNotFound
	}
	return rec.history, nil
}

func (f *fakeShiftStore) Execute(ctx context.Context, tenant, id, actor, idempotencyKey, dig string, expectedRevision int64,
	mutate func(crewshift.Shift, crewshift.History) (crewshift.LifecycleResult, error)) (crewshift.LifecycleResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rec, ok := f.records[f.key(tenant, id)]
	if !ok {
		return crewshift.LifecycleResult{}, ErrNotFound
	}
	if prior, ok := rec.replies[idempotencyKey]; ok {
		if prior.digest != dig {
			return crewshift.LifecycleResult{}, ErrIdempotencyConflict
		}
		return prior.result, nil
	}
	if rec.shift.Revision != expectedRevision {
		return crewshift.LifecycleResult{}, ErrRevisionConflict
	}
	result, err := mutate(rec.shift, rec.history)
	if err != nil {
		return crewshift.LifecycleResult{}, err
	}
	rec.shift = result.Updated
	rec.history = result.History
	rec.replies[idempotencyKey] = shiftReplay{digest: dig, result: result}
	return result, nil
}

// -- ProfileStore fake -------------------------------------------------

type fakeProfileStore struct {
	mu    sync.Mutex
	rules []timeprofile.EligibilityRule
	pins  map[string]ProfilePin // tenant|assignment
}

func newFakeProfileStore(rules []timeprofile.EligibilityRule) *fakeProfileStore {
	return &fakeProfileStore{rules: rules, pins: map[string]ProfilePin{}}
}

func (f *fakeProfileStore) Rules(ctx context.Context, tenant string) ([]timeprofile.EligibilityRule, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]timeprofile.EligibilityRule, 0, len(f.rules))
	for _, r := range f.rules {
		if string(r.TenantRef) == tenant {
			out = append(out, r)
		}
	}
	return out, nil
}

func (f *fakeProfileStore) PinFor(ctx context.Context, tenant, assignmentRef string) (ProfilePin, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	pin, ok := f.pins[tenant+"|"+assignmentRef]
	return pin, ok, nil
}

func (f *fakeProfileStore) Pin(ctx context.Context, tenant, assignmentRef string, profile timeprofile.TimeProfile, expectedRevision int64, resolvedAt time.Time) (ProfilePin, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	pin := ProfilePin{AssignmentRef: assignmentRef, Revision: expectedRevision + 1, Profile: profile, ResolvedAt: resolvedAt}
	f.pins[tenant+"|"+assignmentRef] = pin
	return pin, nil
}

// -- MissedPunchStore fake -----------------------------------------------

type fakeMissedPunchStore struct {
	mu   sync.Mutex
	byID map[string]timesession.MissedPunchRequest
	seq  int
}

func newFakeMissedPunchStore() *fakeMissedPunchStore {
	return &fakeMissedPunchStore{byID: map[string]timesession.MissedPunchRequest{}}
}

func (f *fakeMissedPunchStore) Create(ctx context.Context, tenant string, r timesession.MissedPunchRequest, sessionID, idempotencyKey string) (timesession.MissedPunchRequest, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	id := "mp-" + idempotencyKey
	f.byID[tenant+"|"+id] = r
	return r, id, nil
}

func (f *fakeMissedPunchStore) Get(ctx context.Context, tenant, requestID string) (timesession.MissedPunchRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.byID[tenant+"|"+requestID]
	if !ok {
		return timesession.MissedPunchRequest{}, ErrNotFound
	}
	return r, nil
}

func (f *fakeMissedPunchStore) Decide(ctx context.Context, tenant, requestID string, decided timesession.MissedPunchRequest, decidedBy, reopenRef string) (timesession.MissedPunchRequest, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.byID[tenant+"|"+requestID]; !ok {
		return timesession.MissedPunchRequest{}, ErrNotFound
	}
	f.byID[tenant+"|"+requestID] = decided
	return decided, nil
}

// -- AllocationStore fake -------------------------------------------------

type fakeAllocationStore struct {
	mu   sync.Mutex
	byID map[string]timecard.TimeAllocation
}

func newFakeAllocationStore() *fakeAllocationStore {
	return &fakeAllocationStore{byID: map[string]timecard.TimeAllocation{}}
}

func (f *fakeAllocationStore) Get(ctx context.Context, tenant, timecardID string) (timecard.TimeAllocation, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.byID[tenant+"|"+timecardID]
	return a, ok, nil
}

func (f *fakeAllocationStore) Save(ctx context.Context, tenant string, alloc timecard.TimeAllocation, idempotencyKey string) (timecard.TimeAllocation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.byID[tenant+"|"+alloc.TimecardID] = alloc
	return alloc, nil
}

// -- LedgerStore fake --------------------------------------------------

type fakeLedgerStore struct {
	mu          sync.Mutex
	corrections []timecard.AllocationCorrection
	traces      []PremiumTrace
	receipts    []DestinationReceipt
}

func (f *fakeLedgerStore) RecordAllocationCorrection(ctx context.Context, tenant string, c timecard.AllocationCorrection, idempotencyKey string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.corrections = append(f.corrections, c)
	return nil
}

func (f *fakeLedgerStore) RecordPremiumTrace(ctx context.Context, tenant, timecardID string, trace PremiumTrace, idempotencyKey string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.traces = append(f.traces, trace)
	return nil
}

func (f *fakeLedgerStore) RecordReceipt(ctx context.Context, tenant string, r DestinationReceipt, idempotencyKey string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.receipts = append(f.receipts, r)
	return nil
}

// -- Dispatcher fake -----------------------------------------------------

type fakeDispatcher struct {
	mu       sync.Mutex
	accept   bool
	reason   string
	received []DestinationPayload
}

func (f *fakeDispatcher) Dispatch(ctx context.Context, tenant string, payload DestinationPayload) (DestinationReceipt, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.received = append(f.received, payload)
	if f.accept {
		return DestinationReceipt{Destination: payload.Destination, Accepted: true, ReceiptRef: "receipt-1", At: time.Now()}, nil
	}
	return DestinationReceipt{Destination: payload.Destination, Accepted: false, Reason: f.reason, At: time.Now()}, nil
}

// -- PremiumCalculator fake -----------------------------------------------

type fakePremiumCalculator struct{}

func (fakePremiumCalculator) Calculate(ctx context.Context, profile timeprofile.TimeProfile, req PremiumRequest) (PremiumTrace, error) {
	var regular int64
	for _, iv := range req.Intervals {
		regular += int64(iv.End.Sub(iv.Start).Minutes())
	}
	return PremiumTrace{TimecardID: req.TimecardID, RegularMinutes: regular, Trace: []string{fmt.Sprintf("regular=%d", regular)}, Digest: "digest"}, nil
}
