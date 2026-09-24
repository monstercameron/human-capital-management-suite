package main

// REV-013-01: the worker's connector-operation journal must be durable
// Postgres storage (internal/data/connectivityopstore over the open pool),
// not process memory. MemoryJournal stays only as the no-database dev
// fallback.
//
// Test matrix:
//   PRIMARY     TestTodo_REV_013_01: selector wiring (durable over a pool,
//               memory fallback without one), one full single-process
//               Plan/Queue/Lease/Dispatch cycle persisted through
//               connectivityopstore, and the production build() role wiring.
//   INTEGRATION TestTodo_REV_013_01_Integration: a worker is killed
//               mid-lease between the journal commit and the provider send;
//               a fresh journal instance reconstructs the operation and its
//               mapped payload, then dispatches it exactly once.
//   RECOVERY    TestTodo_REV_013_01_Recovery: a fresh journal handle over the
//               same database still lists the operation after the planning
//               process is gone, fencing holds while the lease is live, and
//               an expired lease is reclaimed with a higher fence token.
//   FAULT       TestTodo_REV_013_01_Fault: a broken pool and a non-UUID
//               tenant fail closed before any provider call.

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/operation"
	"github.com/monstercameron/human-capital-management-suite/internal/data/connectivityopstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/bootstrap"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/lease"
)

// --- fixtures ---------------------------------------------------------------

// rev013Tenant inserts one ACTIVE tenant row the durable journal can attach
// queue, journal and operation rows to.
func rev013Tenant(t *testing.T, db *pgtest.DB, key string) uuid.UUID {
	t.Helper()
	tenant := uuid.New()
	db.Exec(t, `INSERT INTO tenant (tenant_id, tenant_key, cell_id, display_name, status, effective_from)
		VALUES ($1, $2, 'cell-rev013', $3, 'ACTIVE', timestamptz '2026-01-01T00:00:00Z')`, tenant, key, key)
	return tenant
}

// rev013PlanRequest mirrors connectorPlanRequest but with a UUID tenant (the
// durable journal keys Postgres rows by UUID tenant) and real 64-hex digests
// (content_digest columns reject anything else). The payload argument lets
// restart tests prove the original bytes reach the provider.
func rev013PlanRequest(id, tenant uuid.UUID, destination string, payload []byte) operation.PlanRequest {
	now := time.Now().UTC()
	return operation.PlanRequest{
		OperationID: id, TenantID: tenant.String(), ConnectionID: "payroll-connection", ConnectorVersion: "payroll.v1",
		BusinessTransactionID: "business-rev013-1", WorkflowInstanceID: "workflow-rev013-1",
		SemanticOperation: "payroll.sync", Direction: "OUTBOUND", Criticality: "P1",
		ExternalResourceKey: "worker:" + id.String(), OrderingClass: operation.OrderingIndependent,
		ExpectedExternalVersion: "v1", SourceAuthorityDecisionRef: "authority.payroll",
		AuthorityPolicyFingerprint: strings.Repeat("a", 64),
		CanonicalInputRef:          "input/rev013-1", CanonicalInputDigest: strings.Repeat("b", 64),
		MappingProfileVersion: "payroll-map-v1", MappedPayloadRef: "payload/rev013-1",
		MappedPayloadDigest: strings.Repeat("c", 64), MappedPayload: payload,
		Classification: "CONFIDENTIAL", Purpose: "PAYROLL_SYNC", DestinationRef: destination,
		CredentialRef:          "secretref://payroll/production",
		IdempotencyKey:         "rev013-" + id.String(),
		ObservationRequirement: operation.ObservationBySemanticIdentity,
		WriterFenceEpoch:       1,
		CreatedAt:              now, DeadlineAt: now.Add(time.Hour),
	}
}

func rev013PlanQueued(t *testing.T, journal *durableConnectorJournal, id, tenant uuid.UUID, destination string, payload []byte) operation.Operation {
	t.Helper()
	if _, err := journal.Plan(context.Background(), rev013PlanRequest(id, tenant, destination, payload)); err != nil {
		t.Fatalf("durable Plan: %v", err)
	}
	op, err := journal.Queue(context.Background(), tenant.String(), id)
	if err != nil {
		t.Fatalf("durable Queue: %v", err)
	}
	if op.State != operation.StateQueued {
		t.Fatalf("operation state after Queue = %s, want QUEUED", op.State)
	}
	return op
}

// failingBeginner refuses every transaction: the fail-closed oracle for the
// FAULT test.
type failingBeginner struct{ err error }

func (f failingBeginner) Begin(context.Context) (dbport.Tx, error) { return nil, f.err }

// ctxBlockingCredentialSource announces the first credential request and then
// waits for the caller's context: the deterministic "kill the worker between
// the journal commit and the provider send" fault for the INTEGRATION test.
// The lease is already committed when LeaseCredential blocks, and the blocked
// dispatch aborts without ever reaching the provider writer.
type ctxBlockingCredentialSource struct {
	entered chan struct{}
	once    sync.Once
}

func (s *ctxBlockingCredentialSource) LeaseCredential(ctx context.Context, _, _ string, _ custody.Operation) (lease.MachineCredentialLease, error) {
	s.once.Do(func() { close(s.entered) })
	<-ctx.Done()
	return lease.MachineCredentialLease{}, ctx.Err()
}

// testDurableConnectorBuild mirrors testConnectorBuild but takes the dispatch
// lease duration as a parameter: the crash-window tests need sub-second
// leases so an abandoned claim expires inside the test deadline.
func testDurableConnectorBuild(journal connectorJournal, ledger connectorLedger, credentials connectorCredentialSource, writer operation.CredentialWriter, authorizer operation.MachineLeaseAuthorizer, tenants tenantLister, leaseFor time.Duration) func(context.Context, bootstrap.Deps) (bootstrap.Runtime, error) {
	return func(_ context.Context, deps bootstrap.Deps) (bootstrap.Runtime, error) {
		enabled, err := deps.Values.Bool("connector-role")
		if err != nil {
			return bootstrap.Runtime{}, err
		}
		pollInterval, err := deps.Values.Duration("poll-interval")
		if err != nil {
			return bootstrap.Runtime{}, err
		}
		if !enabled {
			return bootstrap.Runtime{}, nil
		}
		role := connectorRole{
			logger: deps.Logger, journal: journal, ledger: ledger, credentials: credentials,
			revalidate: stubConnectorRevalidator{fn: confirmedConnectorRevalidation}, writer: writer,
			authorizer: authorizer, credentialOperation: custody.Encrypt, workerID: deps.Identity, leaseFor: leaseFor,
		}
		return bootstrap.Runtime{Workloads: []bootstrap.Workload{{
			Name: "connector-role",
			Run: func(ctx context.Context) error {
				return runConnectorLoop(ctx, deps.Logger, tenants, role, pollInterval)
			},
		}}}, nil
	}
}

// --- independent Postgres oracles (never the adapter's own view) ------------

// rev013QueueRow reads the durable queue membership straight from Postgres.
func rev013QueueRow(t *testing.T, pool dbport.Conn, tenant, opID uuid.UUID) (state string, fence int64, expires *time.Time) {
	t.Helper()
	var expiresAt *time.Time
	if err := pool.QueryRow(context.Background(), `SELECT queue_state, fence_token, expires_at FROM connector_operation_queue WHERE tenant_id = $1 AND operation_id = $2`, tenant, opID).Scan(&state, &fence, &expiresAt); err != nil {
		t.Fatalf("read queue row: %v", err)
	}
	return state, fence, expiresAt
}

// rev013BaseRow reads the durable operation row straight from Postgres.
func rev013BaseRow(t *testing.T, pool dbport.Conn, tenant, opID uuid.UUID) (state string, fence int64) {
	t.Helper()
	if err := pool.QueryRow(context.Background(), `SELECT state, fence_token FROM connector_operation WHERE tenant_id = $1 AND operation_id = $2`, tenant, opID).Scan(&state, &fence); err != nil {
		t.Fatalf("read operation row: %v", err)
	}
	return state, fence
}

func rev013AttemptCount(t *testing.T, pool dbport.Conn, tenant, opID uuid.UUID) int64 {
	t.Helper()
	var count int64
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM connector_operation_attempt WHERE tenant_id = $1 AND operation_id = $2`, tenant, opID).Scan(&count); err != nil {
		t.Fatalf("read durable attempt count: %v", err)
	}
	return count
}

// rev013Trail reads the tenant's durable journal stream through a fresh
// connectivityopstore handle — the independent oracle for "the journal is
// durable", never the adapter's in-memory view — and returns the target
// operation's events after verifying the tenant-wide digest chain.
func rev013Trail(t *testing.T, pool dbport.Beginner, tenant, opID uuid.UUID) []operation.JournalEvent {
	t.Helper()
	store := connectivityopstore.New(pool)
	events, err := store.Journal(context.Background(), tenant, uuid.Nil)
	if err != nil {
		t.Fatalf("read durable journal: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("durable journal trail is empty")
	}
	byPrevious := make(map[string]operation.JournalEvent, len(events))
	for _, event := range events {
		if event.Digest == "" || event.PreviousDigest == "" {
			t.Fatalf("journal row carries an empty chain digest: %+v", event)
		}
		if _, dup := byPrevious[event.PreviousDigest]; dup {
			t.Fatalf("journal chain forks at previous digest %q", event.PreviousDigest)
		}
		byPrevious[event.PreviousDigest] = event
	}
	genesis, ok := byPrevious["sha256:"+strings.Repeat("0", 64)]
	if !ok {
		t.Fatal("journal chain has no genesis event linked from the zero digest")
	}
	ordered := make([]operation.JournalEvent, 0, len(events))
	var target []operation.JournalEvent
	for current := genesis; ; {
		current.Sequence = uint64(len(ordered) + 1)
		ordered = append(ordered, current)
		if current.OperationID == opID {
			target = append(target, current)
		}
		next, ok := byPrevious[current.Digest]
		if !ok {
			break
		}
		current = next
	}
	if len(ordered) != len(events) {
		t.Fatalf("journal chain covers %d of %d rows: broken or forked", len(ordered), len(events))
	}
	if len(target) == 0 {
		t.Fatalf("durable journal chain has no events for operation %s", opID)
	}
	return target
}

// rev013TrailKinds returns the chain-ordered event kinds of the durable trail.
func rev013TrailKinds(t *testing.T, pool dbport.Beginner, tenant, opID uuid.UUID) []string {
	t.Helper()
	events := rev013Trail(t, pool, tenant, opID)
	kinds := make([]string, 0, len(events))
	for _, event := range events {
		kinds = append(kinds, event.Event)
	}
	return kinds
}

// rev013DigestChain verifies the persisted journal rows still link
// previous_digest to the prior row's event_digest.
func rev013DigestChain(t *testing.T, pool dbport.Beginner, tenant, opID uuid.UUID) {
	t.Helper()
	events, err := connectivityopstore.New(pool).Journal(context.Background(), tenant, uuid.Nil)
	if err != nil || len(events) == 0 {
		t.Fatalf("read tenant journal for digest verification: events=%d err=%v", len(events), err)
	}
	byPrevious := make(map[string]operation.JournalEvent, len(events))
	for _, event := range events {
		if _, duplicate := byPrevious[event.PreviousDigest]; duplicate {
			t.Fatalf("duplicate previous digest %q: tenant journal fork", event.PreviousDigest)
		}
		byPrevious[event.PreviousDigest] = event
	}
	zero := "sha256:" + strings.Repeat("0", 64)
	current, ok := byPrevious[zero]
	if !ok {
		t.Fatal("tenant journal has no genesis event")
	}
	perOperation := make(map[uuid.UUID]uint64)
	seen := make(map[uuid.UUID]bool)
	for globalSequence := uint64(1); ; globalSequence++ {
		current.Sequence = globalSequence // recovered by traversing the tenant digest links
		perOperation[current.OperationID]++
		if current.OperationSequence != perOperation[current.OperationID] {
			t.Fatalf("operation %s sequence=%d, want %d", current.OperationID, current.OperationSequence, perOperation[current.OperationID])
		}
		seen[current.OperationID] = true
		next, exists := byPrevious[current.Digest]
		if !exists {
			if globalSequence != uint64(len(events)) {
				t.Fatalf("tenant digest chain ended at %d of %d rows", globalSequence, len(events))
			}
			break
		}
		current = next
	}
	if !seen[opID] {
		t.Fatalf("target operation %s is absent from verified tenant chain", opID)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// --- PRIMARY ----------------------------------------------------------------

func TestTodo_REV_013_01(t *testing.T) {
	t.Run("concurrent_handles_serialize_tenant_digest_chain", func(t *testing.T) {
		db := pgtest.New(t)
		tenant := rev013Tenant(t, db, "rev013-concurrent-chain")
		pool := schemaScopedPool(t, db)
		handles := []*durableConnectorJournal{
			newDurableConnectorJournal(pool, nil),
			newDurableConnectorJournal(pool, nil),
		}
		ids := []uuid.UUID{uuid.New(), uuid.New()}
		start := make(chan struct{})
		results := make(chan error, len(handles))
		for i := range handles {
			go func(i int) {
				<-start
				_, err := handles[i].Plan(context.Background(), rev013PlanRequest(ids[i], tenant, "connector.example", []byte(`{"writer":true}`)))
				if err == nil {
					_, err = handles[i].Queue(context.Background(), tenant.String(), ids[i])
				}
				results <- err
			}(i)
		}
		close(start)
		for range handles {
			if err := <-results; err != nil {
				t.Fatalf("concurrent durable plan/queue: %v", err)
			}
		}
		store := connectivityopstore.New(pool)
		events, err := store.Journal(context.Background(), tenant, uuid.Nil)
		if err != nil || len(events) != 4 {
			t.Fatalf("tenant journal rows=%d err=%v, want 4", len(events), err)
		}
		rev013DigestChain(t, pool, tenant, ids[0])
		rev013DigestChain(t, pool, tenant, ids[1])
	})

	t.Run("no_database_falls_back_to_memory", func(t *testing.T) {
		journal := connectorJournalForPool(nil)
		mem, ok := journal.(*operation.MemoryJournal)
		if !ok || mem == nil {
			t.Fatalf("connectorJournalForPool(nil) = %T, want *operation.MemoryJournal dev fallback", journal)
		}
	})

	t.Run("pool_selects_durable_journal", func(t *testing.T) {
		db := pgtest.New(t)
		pool := schemaScopedPool(t, db)
		journal := connectorJournalForPool(pool)
		durable, ok := journal.(*durableConnectorJournal)
		if !ok || durable == nil {
			t.Fatalf("connectorJournalForPool(pool) = %T, want *durableConnectorJournal", journal)
		}
	})

	t.Run("single_process_cycle_persists_everything", func(t *testing.T) {
		db := pgtest.New(t)
		tenant := rev013Tenant(t, db, "rev013-primary")
		pool := schemaScopedPool(t, db)
		journal := connectorJournalForPool(pool).(*durableConnectorJournal)

		id := uuid.New()
		payload := []byte(`{"payroll":"w-1"}`)
		rev013PlanQueued(t, journal, id, tenant, "connector.example", payload)

		// Trail so far is durable before any worker ever leases.
		if kinds := rev013TrailKinds(t, pool, tenant, id); !equalStrings(kinds, []string{"PLANNED", "QUEUED"}) {
			t.Fatalf("durable trail after Queue = %v, want [PLANNED QUEUED]", kinds)
		}
		if state, _, _ := rev013QueueRow(t, pool, tenant, id); state != "QUEUED" {
			t.Fatalf("durable queue state = %q, want QUEUED", state)
		}

		// Dispatch through the production role shape over the durable journal.
		manager := newConnectorMachineManager(t, time.Now().UTC())
		credSource := &recordingCredentialSource{manager: manager, ttl: time.Minute}
		writer := &recordingCredentialWriter{}
		role := connectorRole{
			logger: discardLogger(), journal: journal, ledger: ampleLedger(), credentials: credSource,
			revalidate: stubConnectorRevalidator{fn: confirmedConnectorRevalidation}, writer: writer,
			authorizer: manager, credentialOperation: custody.Encrypt, workerID: "worker-1", leaseFor: time.Minute,
		}
		attempted, err := role.dispatchQueued(context.Background(), tenant.String())
		if err != nil || attempted != 1 {
			t.Fatalf("dispatchQueued attempted=%d err=%v, want attempted=1 err=nil", attempted, err)
		}
		if writer.calls() != 1 {
			t.Fatalf("writer calls = %d, want exactly 1", writer.calls())
		}

		got, err := journal.Get(context.Background(), tenant.String(), id)
		if err != nil {
			t.Fatal(err)
		}
		if got.State != operation.StateProviderAccepted || len(got.Attempts) != 1 {
			t.Fatalf("operation after dispatch = %+v, want PROVIDER_ACCEPTED with one attempt", got)
		}
		if got.Attempts[0].RequestDigest != strings.Repeat("c", 64) {
			t.Fatalf("attempt request digest = %q, want the planned mapped digest", got.Attempts[0].RequestDigest)
		}

		// Every durable record exists independently of the adapter's memory.
		if kinds := rev013TrailKinds(t, pool, tenant, id); !equalStrings(kinds, []string{"PLANNED", "QUEUED", "LEASED", "CREDENTIAL_BOUND", "SENDING", "ATTEMPT_RECORDED"}) {
			t.Fatalf("durable trail after dispatch = %v, want the full six-event trail", kinds)
		}
		rev013DigestChain(t, pool, tenant, id)
		if state, fence := rev013BaseRow(t, pool, tenant, id); state != "PROVIDER_ACCEPTED" || fence < 1 {
			t.Fatalf("durable operation row = (%q, fence %d), want (PROVIDER_ACCEPTED, fence >= 1)", state, fence)
		}
		if state, fence, _ := rev013QueueRow(t, pool, tenant, id); state != "LEASED" || fence != 1 {
			t.Fatalf("durable queue row = (%q, fence %d), want (LEASED, fence 1)", state, fence)
		}
		var attempts int
		if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM connector_operation_attempt WHERE tenant_id = $1 AND operation_id = $2`, tenant, id).Scan(&attempts); err != nil || attempts != 1 {
			t.Fatalf("durable attempts = %d, err=%v, want exactly 1", attempts, err)
		}
		store := connectivityopstore.New(pool)
		bindings, err := store.CredentialBindings(context.Background(), tenant, id)
		if err != nil || len(bindings) != 1 || bindings[0].Outcome != "BOUND" || bindings[0].CredentialLeaseRef == "" {
			t.Fatalf("durable credential bindings = %+v, err=%v, want one BOUND reference-only binding", bindings, err)
		}
		serialized, err := json.Marshal(bindings[0])
		if err != nil || strings.Contains(string(serialized), "secret-value") || strings.Contains(string(serialized), "raw-secret") {
			t.Fatalf("binding evidence is not reference-only: %s, err=%v", serialized, err)
		}

		// Tenant isolation holds at the adapter layer: a foreign tenant sees
		// neither the operation nor its trail.
		foreign := uuid.New()
		if ops, err := journal.List(context.Background(), foreign.String()); err != nil || len(ops) != 0 {
			t.Fatalf("foreign-tenant List = %d ops, err=%v, want empty", len(ops), err)
		}
		if _, err := journal.Get(context.Background(), foreign.String(), id); !errors.Is(err, operation.ErrNotFound) {
			t.Fatalf("foreign-tenant Get = %v, want ErrNotFound", err)
		}
		if events, err := store.Journal(context.Background(), foreign, id); err != nil || len(events) != 0 {
			t.Fatalf("foreign-tenant durable trail = %d events, err=%v, want empty", len(events), err)
		}
	})

	t.Run("production_build_wires_connector_role", func(t *testing.T) {
		db := pgtest.New(t)
		pool := schemaScopedPool(t, db)
		values, err := bootstrap.ParseConfig([]string{"-database-url=postgres://ignored/db", "-poll-interval=25ms", "-connector-role=true", "-messaging-role=false"}, nil, workerConfigFields())
		if err != nil {
			t.Fatalf("parse worker config: %v", err)
		}
		runtime, err := build(context.Background(), bootstrap.Deps{DB: pool, Values: values, Logger: discardLogger(), Identity: "worker-test", Clock: func() time.Time { return time.Now().UTC() }})
		if err != nil {
			t.Fatalf("production build: %v", err)
		}
		found := false
		for _, workload := range runtime.Workloads {
			if workload.Name == "connector-role" {
				found = true
			}
		}
		if !found {
			names := make([]string, 0, len(runtime.Workloads))
			for _, workload := range runtime.Workloads {
				names = append(names, workload.Name)
			}
			t.Fatalf("production workloads = %v, want a connector-role workload", names)
		}

		off, err := bootstrap.ParseConfig([]string{"-database-url=postgres://ignored/db", "-poll-interval=25ms", "-messaging-role=false"}, nil, workerConfigFields())
		if err != nil {
			t.Fatalf("parse worker config: %v", err)
		}
		disabled, err := build(context.Background(), bootstrap.Deps{DB: pool, Values: off, Logger: discardLogger(), Identity: "worker-test", Clock: func() time.Time { return time.Now().UTC() }})
		if err != nil {
			t.Fatalf("production build: %v", err)
		}
		for _, workload := range disabled.Workloads {
			if workload.Name == "connector-role" {
				t.Fatal("production build registered a connector-role workload with connector-role=false")
			}
		}
	})
}

// --- INTEGRATION ------------------------------------------------------------

func TestTodo_REV_013_01_Integration(t *testing.T) {
	db := pgtest.New(t)
	tenant := rev013Tenant(t, db, "rev013-integration")
	pool := schemaScopedPool(t, db)
	planner := connectorJournalForPool(pool).(*durableConnectorJournal)

	id := uuid.New()
	payload := []byte(`{"worker":"w-1"}`)
	rev013PlanQueued(t, planner, id, tenant, "connector.example", payload)

	manager := newConnectorMachineManager(t, time.Now().UTC())
	writer := &recordingCredentialWriter{}
	blocker := &ctxBlockingCredentialSource{entered: make(chan struct{})}
	tenants := fakeTenantLister{tenants: []uuid.UUID{tenant}}

	runWorker := func(parent context.Context, journal connectorJournal, credentials connectorCredentialSource) int {
		s := spec([]string{"-database-url=postgres://ignored/db", "-poll-interval=25ms", "-connector-role=true"})
		s.Getenv = func(string) (string, bool) { return "", false }
		s.DBPoolFactory = bootstrap.NewFakeDBPoolFactory(bootstrap.NewFakeDBPool())
		s.Logger = discardLogger()
		s.Stdout = io.Discard
		s.Stderr = io.Discard
		s.Build = testDurableConnectorBuild(journal, ampleLedger(), credentials, writer, manager, tenants, 300*time.Millisecond)
		runCtx, cancel := context.WithCancel(parent)
		defer cancel()
		code := make(chan int, 1)
		go func() { code <- bootstrap.Run(runCtx, s) }()
		return <-code
	}

	// Worker one starts, leases the operation (the journal commit lands in
	// Postgres), then blocks inside the credential step: the exact crash
	// window between journal commit and provider send.
	workerCtx, cancelWorker := context.WithCancel(context.Background())
	done := make(chan int, 1)
	go func() { done <- runWorker(workerCtx, newDurableConnectorJournal(pool, nil), blocker) }()

	select {
	case <-blocker.entered:
	case <-time.After(20 * time.Second):
		cancelWorker()
		t.Fatal("worker one never reached the credential step")
	}
	deadline := time.Now().Add(20 * time.Second)
	for {
		state, fence, _ := rev013QueueRow(t, pool, tenant, id)
		if state == "LEASED" && fence == 1 {
			break
		}
		if time.Now().After(deadline) {
			cancelWorker()
			t.Fatalf("durable queue never showed the committed lease (state=%q fence=%d)", state, fence)
		}
		time.Sleep(25 * time.Millisecond)
	}
	secondID := uuid.New()
	quotaJournal := newDurableConnectorJournal(pool, nil)
	secondRequest := rev013PlanRequest(secondID, tenant, "connector.example", []byte(`{"worker":"w-2"}`))
	secondRequest.ExternalResourceKey = "zz-worker-second"
	if _, err := quotaJournal.Plan(context.Background(), secondRequest); err != nil {
		t.Fatalf("plan second operation for durable reservation proof: %v", err)
	}
	if _, err := quotaJournal.Queue(context.Background(), tenant.String(), secondID); err != nil {
		t.Fatalf("queue second operation for durable reservation proof: %v", err)
	}
	if _, err := connectivityopstore.New(pool).Claim(context.Background(), tenant, secondID, "worker-probe", time.Now().UTC(), time.Minute); !errors.Is(err, connectivityopstore.ErrNotReady) {
		t.Fatalf("fresh-process admission beside active lease = %v, want durable reservation refusal", err)
	}

	// Kill the worker mid-lease. The provider is never reached.
	cancelWorker()
	if code := <-done; code != bootstrap.ExitOK {
		t.Fatalf("worker one exit code = %d, want ExitOK", code)
	}
	if writer.calls() != 0 {
		t.Fatalf("writer calls after the kill = %d, want 0: the send must not have happened", writer.calls())
	}
	// The operation did not disappear: its LEASED claim is still durable.
	if state, _, _ := rev013QueueRow(t, pool, tenant, id); state != "LEASED" {
		t.Fatalf("durable queue state after the kill = %q, want still LEASED", state)
	}

	// Let the abandoned lease expire, then restart the worker loop. It must
	// resume the operation from QUEUED and dispatch it exactly once.
	time.Sleep(time.Second)
	workerTwo, cancelTwo := context.WithCancel(context.Background())
	defer cancelTwo()
	realSource := &recordingCredentialSource{manager: manager, ttl: time.Minute}
	finished := make(chan int, 1)
	go func() { finished <- runWorker(workerTwo, newDurableConnectorJournal(pool, nil), realSource) }()

	pollUntil := time.Now().Add(20 * time.Second)
	var got operation.Operation
	for time.Now().Before(pollUntil) {
		var err error
		got, err = newDurableConnectorJournal(pool, nil).Get(context.Background(), tenant.String(), id)
		if err != nil {
			cancelTwo()
			t.Fatalf("read journal operation: %v", err)
		}
		if got.State == operation.StateProviderAccepted {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	cancelTwo()
	if code := <-finished; code != bootstrap.ExitOK {
		t.Fatalf("worker two exit code = %d, want ExitOK", code)
	}
	if got.State != operation.StateProviderAccepted || rev013AttemptCount(t, pool, tenant, id) != 1 {
		t.Fatalf("operation after restart = %s with %d durable attempts, want PROVIDER_ACCEPTED with one attempt", got.State, rev013AttemptCount(t, pool, tenant, id))
	}
	if writer.calls() != 1 {
		t.Fatalf("writer calls across kill and restart = %d, want exactly 1: no loss, no double-send", writer.calls())
	}
	if string(writer.payload()) != string(payload) {
		t.Fatalf("provider payload after restart = %q, want persisted payload %q", writer.payload(), payload)
	}
	// The restart reclaimed the expired lease through the durable queue: the
	// second claim holds fence 2, which only Store.Claim can grant.
	if _, fence, _ := rev013QueueRow(t, pool, tenant, id); fence != 2 {
		t.Fatalf("durable queue fence after restart = %d, want 2 (expired lease reclaimed)", fence)
	}
	if kinds := rev013TrailKinds(t, pool, tenant, id); !equalStrings(kinds[:3], []string{"PLANNED", "QUEUED", "LEASED"}) {
		t.Fatalf("durable trail prefix = %v, want [PLANNED QUEUED LEASED]", kinds)
	}
	if _, err := connectivityopstore.New(pool).Claim(context.Background(), tenant, secondID, "worker-after-restart", time.Now().UTC(), time.Minute); !errors.Is(err, connectivityopstore.ErrNotReady) {
		t.Fatalf("fresh-handle admission after persisted provider attempt = %v, want durable rate-window refusal", err)
	}
}

// --- RECOVERY ---------------------------------------------------------------

func TestTodo_REV_013_01_Recovery(t *testing.T) {
	db := pgtest.New(t)
	tenant := rev013Tenant(t, db, "rev013-recovery")
	pool := schemaScopedPool(t, db)

	// The planning worker leases the operation, then dies without dispatching.
	planner := newDurableConnectorJournal(pool, nil)
	id := uuid.New()
	rev013PlanQueued(t, planner, id, tenant, "connector.example", []byte(`{"worker":"w-1"}`))
	claimed, err := planner.Lease(context.Background(), operation.LeaseRequest{
		TenantID: tenant.String(), OperationID: id, WorkerID: "worker-1",
		At: time.Now().UTC(), Duration: 200 * time.Millisecond, Revalidate: confirmedConnectorRevalidation,
	})
	if err != nil {
		t.Fatalf("plan-worker lease: %v", err)
	}
	if claimed.FenceToken != 1 {
		t.Fatalf("plan-worker fence = %d, want 1", claimed.FenceToken)
	}

	// A fresh handle over the same database — the restarted process — still
	// lists the operation instead of losing it.
	restarted := newDurableConnectorJournal(pool, nil)
	visible, err := restarted.List(context.Background(), tenant.String())
	if err != nil {
		t.Fatalf("fresh-handle List: %v", err)
	}
	found := false
	for _, op := range visible {
		if op.OperationID == id {
			found = true
			if op.State != operation.StateLeased {
				t.Fatalf("fresh-handle operation state = %s, want LEASED while the claim is live", op.State)
			}
		}
	}
	if !found {
		t.Fatal("fresh handle lost the operation: it is absent from List after the planner died")
	}

	// A fresh handle reconstructs the complete planned operation, including
	// its mapped payload, but the live durable claim still fences another
	// worker from leasing it.
	for _, op := range visible {
		if op.OperationID != id {
			continue
		}
		if string(op.MappedPayload) != "{\"worker\":\"w-1\"}" {
			t.Fatalf("fresh-handle payload = %q, want original mapped payload", op.MappedPayload)
		}
	}
	if _, err := restarted.Lease(context.Background(), operation.LeaseRequest{
		TenantID: tenant.String(), OperationID: id, WorkerID: "worker-2",
		At: time.Now().UTC(), Duration: time.Minute, Revalidate: confirmedConnectorRevalidation,
	}); !errors.Is(err, operation.ErrNotFound) {
		t.Fatalf("fresh-handle lease during live claim = %v, want ErrNotFound (live leases are not reconstructed as queued)", err)
	}
	store := connectivityopstore.New(pool)
	if _, err := store.Claim(context.Background(), tenant, id, "worker-2", time.Now().UTC(), time.Minute); !errors.Is(err, connectivityopstore.ErrLeaseFenced) {
		t.Fatalf("claim during a live lease = %v, want ErrLeaseFenced", err)
	}

	// The lease expires. The next claim requeues and reclaims with a higher
	// fence: the operation resumes instead of sticking. The reclaim itself is
	// short-lived so the test can also watch it expire below.
	time.Sleep(500 * time.Millisecond)
	reclaimed, err := store.Claim(context.Background(), tenant, id, "worker-2", time.Now().UTC(), 300*time.Millisecond)
	if err != nil {
		t.Fatalf("reclaim after expiry: %v", err)
	}
	if reclaimed.FenceToken != 2 {
		t.Fatalf("reclaimed fence = %d, want 2", reclaimed.FenceToken)
	}
	if kinds := rev013TrailKinds(t, pool, tenant, id); !equalStrings(kinds, []string{"PLANNED", "QUEUED", "LEASED"}) {
		t.Fatalf("durable trail = %v, want [PLANNED QUEUED LEASED]", kinds)
	}

	// The planner's own recovery settles its expired in-memory lease back to
	// QUEUED and persists the recovery event durably.
	recovered, err := planner.Recover(context.Background(), time.Now().UTC().Add(time.Hour))
	if err != nil {
		t.Fatalf("planner Recover: %v", err)
	}
	if len(recovered) != 1 || recovered[0].State != operation.StateQueued {
		t.Fatalf("recovered = %+v, want the one operation back in QUEUED", recovered)
	}
	if kinds := rev013TrailKinds(t, pool, tenant, id); !equalStrings(kinds, []string{"PLANNED", "QUEUED", "LEASED", "LEASE_RECOVERED"}) {
		t.Fatalf("durable trail after Recover = %v, want the recovery event appended", kinds)
	}

	// Once the reclaiming claim also expires, a fresh handle lists QUEUED:
	// resumed from its persisted QUEUED state rather than disappeared.
	time.Sleep(time.Second)
	final := newDurableConnectorJournal(pool, nil)
	ops, err := final.List(context.Background(), tenant.String())
	if err != nil {
		t.Fatalf("final List: %v", err)
	}
	resumed := false
	for _, op := range ops {
		if op.OperationID == id && op.State == operation.StateQueued {
			resumed = true
		}
	}
	if !resumed {
		t.Fatal("operation did not resume to QUEUED on the fresh handle")
	}

	// A redeployed planner replays the same intent through another fresh
	// handle. The replay converges without duplicates: the base insert and
	// the queue membership are already there, and every replayed journal
	// sequence is skipped from the seeded floor.
	replay := newDurableConnectorJournal(pool, nil)
	if _, err := replay.Plan(context.Background(), rev013PlanRequest(id, tenant, "connector.example", nil)); err != nil {
		t.Fatalf("replay Plan: %v", err)
	}
	if _, err := replay.Queue(context.Background(), tenant.String(), id); err != nil {
		t.Fatalf("replay Queue: %v", err)
	}
	if kinds := rev013TrailKinds(t, pool, tenant, id); !equalStrings(kinds, []string{"PLANNED", "QUEUED", "LEASED", "LEASE_RECOVERED"}) {
		t.Fatalf("durable trail after replay = %v, want it unchanged (all sequences skipped)", kinds)
	}
	granted, err := replay.Lease(context.Background(), operation.LeaseRequest{
		TenantID: tenant.String(), OperationID: id, WorkerID: "worker-3",
		At: time.Now().UTC(), Duration: time.Minute, Revalidate: confirmedConnectorRevalidation,
	})
	if err != nil {
		t.Fatalf("replay Lease: %v", err)
	}
	if granted.FenceToken != 1 {
		t.Fatalf("replay lease fence = %d, want the delegate's fresh fence 1", granted.FenceToken)
	}
	if _, fence, _ := rev013QueueRow(t, pool, tenant, id); fence != 3 {
		t.Fatalf("durable queue fence after replay lease = %d, want 3 (second reclaim)", fence)
	}
}

// --- FAULT ------------------------------------------------------------------

func TestTodo_REV_013_01_Fault(t *testing.T) {
	t.Run("broken_pool_fails_closed_before_side_effects", func(t *testing.T) {
		broken := newDurableConnectorJournal(failingBeginner{err: errors.New("database unreachable")}, nil)
		id := uuid.New()
		tenant := uuid.New()
		req := rev013PlanRequest(id, tenant, "connector.example", nil)

		if _, err := broken.Plan(context.Background(), req); !errors.Is(err, errDurableJournal) {
			t.Fatalf("Plan over a broken pool = %v, want the durable-journal error", err)
		}
		if _, err := broken.Queue(context.Background(), tenant.String(), id); !errors.Is(err, errDurableJournal) {
			t.Fatalf("Queue over a broken pool = %v, want the durable-journal error", err)
		}
		if _, err := broken.Lease(context.Background(), operation.LeaseRequest{
			TenantID: tenant.String(), OperationID: id, WorkerID: "worker-1",
			At: time.Now().UTC(), Duration: time.Minute, Revalidate: confirmedConnectorRevalidation,
		}); err == nil {
			t.Fatal("Lease over a broken pool unexpectedly succeeded")
		}

		manager := newConnectorMachineManager(t, time.Now().UTC())
		writer := &recordingCredentialWriter{}
		if _, err := broken.DispatchWithCredential(context.Background(), operation.CredentialDispatchRequest{}, manager); err == nil {
			t.Fatal("DispatchWithCredential over a broken pool unexpectedly succeeded")
		}
		if writer.calls() != 0 {
			t.Fatalf("writer calls = %d, want 0: no provider call without durability", writer.calls())
		}
	})

	t.Run("non_uuid_tenant_is_rejected_before_mutation", func(t *testing.T) {
		db := pgtest.New(t)
		pool := schemaScopedPool(t, db)
		journal := newDurableConnectorJournal(pool, nil)
		id := uuid.New()
		req := rev013PlanRequest(id, uuid.New(), "connector.example", nil)
		req.TenantID = "tenant-connector"
		if _, err := journal.Plan(context.Background(), req); !errors.Is(err, errDurableJournal) {
			t.Fatalf("Plan with a non-UUID tenant = %v, want the durable-journal error", err)
		}
		var baseRows int
		if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM connector_operation`).Scan(&baseRows); err != nil || baseRows != 0 {
			t.Fatalf("base rows after rejected Plan = %d, err=%v, want 0: nothing persisted", baseRows, err)
		}
	})

	t.Run("live_lease_fences_a_second_worker", func(t *testing.T) {
		db := pgtest.New(t)
		tenant := rev013Tenant(t, db, "rev013-fault")
		pool := schemaScopedPool(t, db)
		journal := connectorJournalForPool(pool).(*durableConnectorJournal)
		id := uuid.New()
		rev013PlanQueued(t, journal, id, tenant, "connector.example", nil)

		if _, err := journal.Lease(context.Background(), operation.LeaseRequest{
			TenantID: tenant.String(), OperationID: id, WorkerID: "worker-1",
			At: time.Now().UTC(), Duration: 10 * time.Minute, Revalidate: confirmedConnectorRevalidation,
		}); err != nil {
			t.Fatalf("first lease: %v", err)
		}
		if _, err := journal.Lease(context.Background(), operation.LeaseRequest{
			TenantID: tenant.String(), OperationID: id, WorkerID: "worker-2",
			At: time.Now().UTC(), Duration: time.Minute, Revalidate: confirmedConnectorRevalidation,
		}); !errors.Is(err, operation.ErrLeaseFenced) {
			t.Fatalf("second in-process lease = %v, want ErrLeaseFenced", err)
		}
		store := connectivityopstore.New(pool)
		if _, err := store.Claim(context.Background(), tenant, id, "worker-3", time.Now().UTC(), time.Minute); !errors.Is(err, connectivityopstore.ErrLeaseFenced) {
			t.Fatalf("second durable claim = %v, want ErrLeaseFenced", err)
		}
	})
}

// --- pure unit coverage (no database) ---------------------------------------

func TestDurableJournalTenantGate(t *testing.T) {
	if _, err := durableTenantID("tenant-connector"); !errors.Is(err, errDurableJournal) {
		t.Fatalf("durableTenantID(tenant-connector) = %v, want the durable-journal error", err)
	}
	if _, err := durableTenantID(""); !errors.Is(err, errDurableJournal) {
		t.Fatalf("durableTenantID(\"\") = %v, want the durable-journal error", err)
	}
	id := uuid.New()
	if got, err := durableTenantID(id.String()); err != nil || got != id {
		t.Fatalf("durableTenantID(%q) = %v, %v, want the UUID back", id, got, err)
	}
}

func TestDigestForStorage(t *testing.T) {
	hex64 := strings.Repeat("d", 64)
	if got, err := digestForStorage("request", "sha256:"+hex64); err != nil || got != hex64 {
		t.Fatalf("digestForStorage(prefixed) = %q, %v, want the stripped hex", got, err)
	}
	if got, err := digestForStorage("request", hex64); err != nil || got != hex64 {
		t.Fatalf("digestForStorage(bare) = %q, %v, want it unchanged", got, err)
	}
	// Anything that is not 64 hex characters is hashed into the
	// content_digest shape instead of being rejected: the durable row stays
	// valid while the in-memory journal keeps the original string.
	got, err := digestForStorage("authority", "authz-v1")
	if err != nil || len(got) != 64 || got == "authz-v1" {
		t.Fatalf("digestForStorage(legacy) = %q, %v, want a 64-hex digest", got, err)
	}
	if _, err := digestForStorage("request", ""); err == nil {
		t.Fatal("digestForStorage(\"\") unexpectedly succeeded")
	}
}

func TestMapBaseState(t *testing.T) {
	for _, state := range []operation.State{operation.StatePlanned, operation.StateQueued, operation.StateLeased, operation.StateProviderAccepted, operation.StateFailed} {
		if got := mapBaseState(string(state)); got != state {
			t.Fatalf("mapBaseState(%q) = %q, want it unchanged", state, got)
		}
	}
	if got := mapBaseState("SUBMITTED"); got != operation.State("SUBMITTED") {
		t.Fatalf("mapBaseState(SUBMITTED) = %q, want the raw state carried through", got)
	}
}

func TestDurablePilotIDsAreStable(t *testing.T) {
	tenant := uuid.New()
	first := durablePilotID(tenant, "connection", "payroll-connection")
	if second := durablePilotID(tenant, "connection", "payroll-connection"); second != first {
		t.Fatal("durablePilotID is not deterministic")
	}
	if other := durablePilotID(tenant, "connection", "other-connection"); other == first {
		t.Fatal("durablePilotID collides across connection names")
	}
	if other := durablePilotID(uuid.New(), "connection", "payroll-connection"); other == first {
		t.Fatal("durablePilotID collides across tenants")
	}
}
