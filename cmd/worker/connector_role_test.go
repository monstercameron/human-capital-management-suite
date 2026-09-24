package main

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/operation"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/lease"
)

var connectorTestNow = time.Date(2026, 9, 11, 12, 0, 0, 0, time.UTC)

// --- journal / operation fixtures -----------------------------------------

func connectorJournalFor(_ *testing.T) *operation.MemoryJournal {
	return operation.NewMemoryJournal(func() time.Time { return connectorTestNow })
}

func connectorPlanRequest(id uuid.UUID, destination string) operation.PlanRequest {
	return operation.PlanRequest{
		OperationID: id, TenantID: "tenant-connector", ConnectionID: "payroll-connection", ConnectorVersion: "payroll.v1",
		BusinessTransactionID: "business-connector-1", WorkflowInstanceID: "workflow-connector-1", SemanticOperation: "payroll.sync",
		Direction: "OUTBOUND", Criticality: "P1", ExternalResourceKey: "worker:" + id.String(), OrderingClass: operation.OrderingIndependent,
		ExpectedExternalVersion: "v1", SourceAuthorityDecisionRef: "authority.payroll", AuthorityPolicyFingerprint: "authz-v1", WriterFenceEpoch: 1,
		CanonicalInputRef: "input/connector-1", CanonicalInputDigest: "sha256:canonical", MappingProfileVersion: "payroll-map-v1",
		MappedPayloadRef: "payload/connector-1", MappedPayloadDigest: "sha256:mapped", MappedPayload: []byte(`{"worker":"w-1"}`),
		Classification: "CONFIDENTIAL", Purpose: "PAYROLL_SYNC", DestinationRef: destination, CredentialRef: "secretref://payroll/production",
		IdempotencyKey: "connector-" + id.String(), ObservationRequirement: operation.ObservationBySemanticIdentity,
		CreatedAt: connectorTestNow, DeadlineAt: connectorTestNow.Add(time.Hour),
	}
}

func planQueuedConnectorOperation(t *testing.T, j *operation.MemoryJournal, id uuid.UUID, destination string) operation.Operation {
	t.Helper()
	if _, err := j.Plan(context.Background(), connectorPlanRequest(id, destination)); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	op, err := j.Queue(context.Background(), "tenant-connector", id)
	if err != nil {
		t.Fatalf("Queue: %v", err)
	}
	return op
}

func confirmedConnectorRevalidation(op operation.Operation) operation.Revalidation {
	return operation.Revalidation{OperationID: op.OperationID, Confirmed: true, PlanDigest: "plan-v1", CurrentPlanDigest: "plan-v1", Explanation: "confirmed for test"}
}

type stubConnectorRevalidator struct {
	fn func(operation.Operation) operation.Revalidation
}

func (s stubConnectorRevalidator) RevalidateConnectorOperation(op operation.Operation) operation.Revalidation {
	return s.fn(op)
}

// --- credential fixtures (real TRUST-016/029 stack, no shortcuts) ---------

// connectorCustodyPort is a minimal lease.Port: it issues a custody lease
// coordinate without ever handling secret material, exactly the shape
// internal/connectivity/operation's own tests use.
type connectorCustodyPort struct{ now time.Time }

func (p connectorCustodyPort) IssueLease(_ custody.Context, handle custody.Handle, op custody.Operation, ttl time.Duration) (custody.Lease, error) {
	return custody.Lease{ID: "custody-" + handle.ID, Handle: handle, Operation: op, ExpiresAt: p.now.Add(ttl)}, nil
}

func newConnectorMachineManager(t *testing.T, now time.Time) *lease.MachineManager {
	t.Helper()
	manager, err := lease.NewMachineManager(connectorCustodyPort{now: now}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	return manager
}

func mintConnectorCredential(t *testing.T, manager *lease.MachineManager, tenant, destination string, ttl time.Duration) lease.MachineCredentialLease {
	t.Helper()
	handle := custody.Handle{ID: "connector-secret", Kind: custody.Secret, Version: "v1", Tenant: tenant, Region: "us-east-1"}
	credential, _, err := manager.Mint(lease.MachineRequest{Handle: handle, Workload: "connector-worker", Tenant: tenant, Purpose: "CONNECTOR_SYNC", Destination: destination, Operation: custody.Encrypt, TTL: ttl})
	if err != nil {
		t.Fatal(err)
	}
	return credential
}

// recordingCredentialSource mints one fresh machine credential lease per
// call, bound to the destination and tenant it is asked for, so each dispatch
// attempt is bound at call time rather than reusing a captured value.
type recordingCredentialSource struct {
	mu      sync.Mutex
	manager *lease.MachineManager
	ttl     time.Duration
	calls   int
	err     error
	// delay, when set, is slept before minting. TestTodo_SVC_008_Race uses
	// this to widen the window an operation spends LEASED-but-not-yet-
	// dispatched, so every competing goroutine's Lease call lands while the
	// winner is still mid-flight rather than after it has already finished.
	delay time.Duration
}

func (s *recordingCredentialSource) LeaseCredential(_ context.Context, tenant, destination string, op custody.Operation) (lease.MachineCredentialLease, error) {
	s.mu.Lock()
	s.calls++
	failErr := s.err
	delay := s.delay
	s.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay)
	}
	if failErr != nil {
		return lease.MachineCredentialLease{}, failErr
	}
	handle := custody.Handle{ID: "connector-secret", Kind: custody.Secret, Version: "v1", Tenant: tenant, Region: "us-east-1"}
	return dedicatedMint(s.manager, handle, tenant, destination, op, s.ttl)
}

func dedicatedMint(manager *lease.MachineManager, handle custody.Handle, tenant, destination string, op custody.Operation, ttl time.Duration) (lease.MachineCredentialLease, error) {
	credential, _, err := manager.Mint(lease.MachineRequest{Handle: handle, Workload: "connector-worker", Tenant: tenant, Purpose: "CONNECTOR_SYNC", Destination: destination, Operation: op, TTL: ttl})
	return credential, err
}

func (s *recordingCredentialSource) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// fixedCredentialSource always returns one pre-minted credential, used to
// prove a credential minted for a different destination is refused.
type fixedCredentialSource struct{ credential lease.MachineCredentialLease }

func (f fixedCredentialSource) LeaseCredential(context.Context, string, string, custody.Operation) (lease.MachineCredentialLease, error) {
	return f.credential, nil
}

// recordingCredentialWriter is the provider-neutral stand-in for a real
// connector adapter: it records the reference-only lease it received (never
// secret material) and returns a scripted result.
type recordingCredentialWriter struct {
	mu      sync.Mutex
	n       int
	last    lease.MachineCredentialLease
	request operation.WriteRequest
	err     error
}

func (w *recordingCredentialWriter) WriteWithCredential(_ context.Context, request operation.WriteRequest, got lease.MachineCredentialLease) (operation.WriteResponse, error) {
	w.mu.Lock()
	w.n++
	w.last = got
	w.request = request
	w.request.Payload = append([]byte(nil), request.Payload...)
	failErr := w.err
	w.mu.Unlock()
	if failErr != nil {
		return operation.WriteResponse{}, failErr
	}
	return operation.WriteResponse{Result: operation.ResponseSuccess, ProviderRequestID: "connector-ok"}, nil
}

func (w *recordingCredentialWriter) payload() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.request.Payload...)
}

func (w *recordingCredentialWriter) calls() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.n
}

// --- spy journal / ledger used to prove no bypass is possible -------------

// spyConnectorJournal wraps a real journal and counts how many times Lease
// and DispatchWithCredential are actually invoked, so a SECURITY test can
// prove the provider path was never reached rather than merely that the
// final error looked right.
type spyConnectorJournal struct {
	real          connectorJournal
	leaseErr      error
	leaseCalls    int
	dispatchCalls int
}

func (s *spyConnectorJournal) List(ctx context.Context, tenant string) ([]operation.Operation, error) {
	return s.real.List(ctx, tenant)
}

func (s *spyConnectorJournal) Lease(ctx context.Context, req operation.LeaseRequest) (operation.Lease, error) {
	s.leaseCalls++
	if s.leaseErr != nil {
		return operation.Lease{}, s.leaseErr
	}
	return s.real.Lease(ctx, req)
}

func (s *spyConnectorJournal) DispatchWithCredential(ctx context.Context, req operation.CredentialDispatchRequest, authorizer operation.MachineLeaseAuthorizer) (operation.DispatchResult, error) {
	s.dispatchCalls++
	return s.real.DispatchWithCredential(ctx, req, authorizer)
}

func (s *spyConnectorJournal) Recover(ctx context.Context, at time.Time) ([]operation.Operation, error) {
	return s.real.Recover(ctx, at)
}

// refusingLedger always refuses reservation, simulating exhausted fair
// scheduling capacity for every candidate.
type refusingLedger struct{}

func (refusingLedger) TryReserve(time.Time, operation.ScheduleCandidate) (bool, string) {
	return false, "TEST_REFUSED"
}
func (refusingLedger) Release(uuid.UUID, string) bool { return false }

func newConnectorRole(journal connectorJournal, ledger connectorLedger, credentials connectorCredentialSource, writer *recordingCredentialWriter, authorizer operation.MachineLeaseAuthorizer, workerID string) connectorRole {
	return connectorRole{
		logger: discardLogger(), journal: journal, ledger: ledger, credentials: credentials,
		revalidate: stubConnectorRevalidator{fn: confirmedConnectorRevalidation}, writer: writer,
		authorizer: authorizer, credentialOperation: custody.Encrypt, workerID: workerID, leaseFor: time.Minute,
		now: func() time.Time { return connectorTestNow },
	}
}

func ampleLedger() *operation.ConnectorLedger {
	return operation.NewConnectorLedger(map[string]operation.ConnectorPolicy{
		"payroll-connection": {Quota: operation.ConnectorQuota{Limit: 100, Window: time.Minute, MaxConcurrent: 100}},
	})
}

// --- PRIMARY ---------------------------------------------------------------

// TestTodo_SVC_008 proves the configured role consumes only operations that
// exist in the journal: it lists a QUEUED operation, claims it with a fenced
// lease, reserves fair-scheduling capacity, obtains a destination-bound
// credential lease, dispatches through the journal's own credentialed path
// (which persists the attempt), and releases the reservation afterward. It
// then proves the negative half of GREEN directly: an operation this role
// never listed out of the journal is never executed.
func TestTodo_SVC_008(t *testing.T) {
	j := connectorJournalFor(t)
	id := uuid.New()
	op := planQueuedConnectorOperation(t, j, id, "connector.example")

	manager := newConnectorMachineManager(t, connectorTestNow)
	credSource := &recordingCredentialSource{manager: manager, ttl: time.Minute}
	writer := &recordingCredentialWriter{}
	ledger := ampleLedger()
	role := newConnectorRole(j, ledger, credSource, writer, manager, "worker-1")

	attempted, err := role.dispatchQueued(context.Background(), "tenant-connector")
	if err != nil || attempted != 1 {
		t.Fatalf("dispatchQueued attempted=%d err=%v, want attempted=1 err=nil", attempted, err)
	}
	if writer.calls() != 1 {
		t.Fatalf("writer calls = %d, want exactly 1", writer.calls())
	}
	if writer.last.Lease.Destination != "connector.example" {
		t.Fatalf("writer received lease bound to %q, want %q", writer.last.Lease.Destination, "connector.example")
	}
	if credSource.callCount() != 1 {
		t.Fatalf("credential leases minted = %d, want exactly 1", credSource.callCount())
	}

	got, err := j.Get(context.Background(), "tenant-connector", id)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != operation.StateProviderAccepted || len(got.Attempts) != 1 {
		t.Fatalf("operation after dispatch = %+v, want PROVIDER_ACCEPTED with one persisted attempt", got)
	}
	if ledger.InFlight("payroll-connection") != 0 {
		t.Fatalf("fair-scheduling reservation leaked: in-flight = %d, want 0", ledger.InFlight("payroll-connection"))
	}

	// GREEN's negative half: an operation this role never listed out of the
	// journal (a fabricated OperationID with no Plan/Queue row) must never
	// reach the provider, even when dispatchOperation is called on it
	// directly with an otherwise-fully-configured role.
	ghost := op
	ghost.OperationID = uuid.New()
	if _, err := role.dispatchOperation(context.Background(), ghost, connectorTestNow); !errors.Is(err, operation.ErrNotFound) {
		t.Fatalf("dispatch of an unjournaled operation = %v, want ErrNotFound", err)
	}
	if writer.calls() != 1 {
		t.Fatalf("writer calls after unjournaled attempt = %d, want still 1 (unchanged)", writer.calls())
	}
}

// --- SECURITY ---------------------------------------------------------------

// TestTodo_SVC_008_Security independently proves each of the four RED
// bypasses is impossible: skipping the queue claim, skipping the credential
// lease, skipping the rate policy, and executing an operation absent from
// the journal. It also proves the destination-binding half of the credential
// requirement named in the brief.
func TestTodo_SVC_008_Security(t *testing.T) {
	t.Run("no_queue_claim", func(t *testing.T) {
		j := connectorJournalFor(t)
		id := uuid.New()
		op := planQueuedConnectorOperation(t, j, id, "connector.example")
		manager := newConnectorMachineManager(t, connectorTestNow)
		credSource := &recordingCredentialSource{manager: manager, ttl: time.Minute}
		writer := &recordingCredentialWriter{}
		spy := &spyConnectorJournal{real: j, leaseErr: operation.ErrLeaseFenced}
		role := newConnectorRole(spy, ampleLedger(), credSource, writer, manager, "worker-1")

		_, err := role.dispatchOperation(context.Background(), op, connectorTestNow)
		if !errors.Is(err, operation.ErrLeaseFenced) {
			t.Fatalf("dispatch with a refused queue claim = %v, want ErrLeaseFenced", err)
		}
		if spy.dispatchCalls != 0 || writer.calls() != 0 || credSource.callCount() != 0 {
			t.Fatalf("dispatch reached the provider without a queue claim: dispatchCalls=%d writer=%d credentialMints=%d",
				spy.dispatchCalls, writer.calls(), credSource.callCount())
		}
	})

	t.Run("no_credential_lease", func(t *testing.T) {
		j := connectorJournalFor(t)
		id := uuid.New()
		op := planQueuedConnectorOperation(t, j, id, "connector.example")
		manager := newConnectorMachineManager(t, connectorTestNow)
		writer := &recordingCredentialWriter{}
		spy := &spyConnectorJournal{real: j}
		refusing := &recordingCredentialSource{manager: manager, ttl: time.Minute, err: errors.New("no credential available")}
		role := newConnectorRole(spy, ampleLedger(), refusing, writer, manager, "worker-1")

		_, err := role.dispatchOperation(context.Background(), op, connectorTestNow)
		if err == nil {
			t.Fatal("dispatch without a credential lease unexpectedly succeeded")
		}
		if spy.dispatchCalls != 0 || writer.calls() != 0 {
			t.Fatalf("dispatch reached the provider without a credential lease: dispatchCalls=%d writer=%d", spy.dispatchCalls, writer.calls())
		}
		// The queue claim itself is real and unaffected: the operation is
		// legitimately LEASED (claimed), just never executed.
		got, err := j.Get(context.Background(), "tenant-connector", id)
		if err != nil {
			t.Fatal(err)
		}
		if got.State != operation.StateLeased || len(got.Attempts) != 0 {
			t.Fatalf("operation after refused credential = %+v, want LEASED with zero attempts", got)
		}
	})

	t.Run("no_rate_policy", func(t *testing.T) {
		j := connectorJournalFor(t)
		id := uuid.New()
		op := planQueuedConnectorOperation(t, j, id, "connector.example")
		manager := newConnectorMachineManager(t, connectorTestNow)
		credSource := &recordingCredentialSource{manager: manager, ttl: time.Minute}
		writer := &recordingCredentialWriter{}
		spy := &spyConnectorJournal{real: j}
		role := newConnectorRole(spy, refusingLedger{}, credSource, writer, manager, "worker-1")

		_, err := role.dispatchOperation(context.Background(), op, connectorTestNow)
		if !errors.Is(err, ErrConnectorDeferred) {
			t.Fatalf("dispatch without admitted fair-scheduling capacity = %v, want ErrConnectorDeferred", err)
		}
		if spy.leaseCalls != 0 || spy.dispatchCalls != 0 || writer.calls() != 0 || credSource.callCount() != 0 {
			t.Fatalf("dispatch bypassed fair scheduling: leaseCalls=%d dispatchCalls=%d writer=%d credentialMints=%d",
				spy.leaseCalls, spy.dispatchCalls, writer.calls(), credSource.callCount())
		}
		got, err := j.Get(context.Background(), "tenant-connector", id)
		if err != nil {
			t.Fatal(err)
		}
		if got.State != operation.StateQueued {
			t.Fatalf("operation state after refused capacity = %s, want unchanged QUEUED", got.State)
		}
	})

	t.Run("no_journal_entry", func(t *testing.T) {
		j := connectorJournalFor(t)
		manager := newConnectorMachineManager(t, connectorTestNow)
		credSource := &recordingCredentialSource{manager: manager, ttl: time.Minute}
		writer := &recordingCredentialWriter{}
		ghost := operation.Operation{
			OperationID: uuid.New(), TenantID: "tenant-connector", ConnectionID: "payroll-connection",
			ExternalResourceKey: "worker:ghost", DestinationRef: "connector.example", UpdatedAt: connectorTestNow,
		}
		role := newConnectorRole(j, ampleLedger(), credSource, writer, manager, "worker-1")

		_, err := role.dispatchOperation(context.Background(), ghost, connectorTestNow)
		if !errors.Is(err, operation.ErrNotFound) {
			t.Fatalf("dispatch of an operation with no journal row = %v, want ErrNotFound", err)
		}
		if writer.calls() != 0 {
			t.Fatalf("unjournaled operation reached the provider: writer calls = %d, want 0", writer.calls())
		}
	})

	t.Run("credential_destination_binding", func(t *testing.T) {
		// Named explicitly in the brief: a credential lease bound to
		// destination A must not be usable for destination B.
		j := connectorJournalFor(t)
		id := uuid.New()
		op := planQueuedConnectorOperation(t, j, id, "connector.example")
		manager := newConnectorMachineManager(t, connectorTestNow)
		wrong := mintConnectorCredential(t, manager, "tenant-connector", "other.example", time.Minute)
		writer := &recordingCredentialWriter{}
		role := newConnectorRole(j, ampleLedger(), fixedCredentialSource{credential: wrong}, writer, manager, "worker-1")

		_, err := role.dispatchOperation(context.Background(), op, connectorTestNow)
		var rejected *operation.CredentialRejection
		if !errors.As(err, &rejected) || rejected.Field != "destination" {
			t.Fatalf("dispatch with a wrong-destination credential = %v, want CredentialRejection{Field: \"destination\"}", err)
		}
		if writer.calls() != 0 {
			t.Fatalf("wrong-destination credential reached the provider: writer calls = %d, want 0", writer.calls())
		}
	})
}

// --- RACE --------------------------------------------------------------------

// TestTodo_SVC_008_Race runs real goroutines racing to dispatch the same
// journaled operation. Each goroutine uses its own fair-scheduling ledger
// (so ledger contention cannot be the reason a racer loses) and its own
// connectorRole value, but every goroutine shares the same underlying
// journal - so the only thing that can decide the race is the journal's own
// fenced Lease, exactly as internal/connectivity/operation guarantees.
// Exactly one goroutine must succeed; every other loses with ErrLeaseFenced,
// and the provider writer, shared across all of them, must see exactly one
// call.
func TestTodo_SVC_008_Race(t *testing.T) {
	j := connectorJournalFor(t)
	id := uuid.New()
	op := planQueuedConnectorOperation(t, j, id, "connector.example")

	manager := newConnectorMachineManager(t, connectorTestNow)
	credSource := &recordingCredentialSource{manager: manager, ttl: time.Minute, delay: 30 * time.Millisecond}
	writer := &recordingCredentialWriter{}

	const workers = 8
	results := make([]error, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Each racer gets its own ledger with ample capacity, isolating
			// the race to the journal's fencing rather than fair scheduling.
			role := newConnectorRole(j, ampleLedger(), credSource, writer, manager, fmt.Sprintf("worker-%d", i))
			<-start
			_, err := role.dispatchOperation(context.Background(), op, connectorTestNow)
			results[i] = err
		}(i)
	}
	close(start)
	wg.Wait()

	successes, fenced := 0, 0
	for _, err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, operation.ErrLeaseFenced):
			fenced++
		default:
			t.Fatalf("unexpected race outcome: %v", err)
		}
	}
	if successes != 1 {
		t.Fatalf("successes = %d, want exactly 1 (every other racer must be fenced)", successes)
	}
	if fenced != workers-1 {
		t.Fatalf("fenced = %d, want %d", fenced, workers-1)
	}
	if writer.calls() != 1 {
		t.Fatalf("writer calls = %d, want exactly 1: more than one racer executed the same operation", writer.calls())
	}

	got, err := j.Get(context.Background(), "tenant-connector", id)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != operation.StateProviderAccepted || len(got.Attempts) != 1 {
		t.Fatalf("operation after the race = %+v, want PROVIDER_ACCEPTED with exactly one persisted attempt", got)
	}
}
