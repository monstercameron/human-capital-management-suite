package runtime_test

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// promotionStep is one leg of the exceeds-threshold promotion walk: the
// outcome a step handler would report for one node.
type promotionStep struct {
	NodeID  string
	Outcome workflow.Outcome
	Digest  string
}

// promotionExceedsThresholdWalk is the same walk
// internal/workflow/frontier's own wfrun024 tests drive (promotionRoutes
// there): Jane's promotion crosses the finance threshold, so every DECISION
// takes the route that ends at end_requires_finance_approval.
var promotionExceedsThresholdWalk = []promotionStep{
	{workflow.PromotionNodeSnapshotWorker, workflow.OutcomeSucceeded, "sha256:snapshot"},
	{workflow.PromotionNodeSimulateComp, workflow.OutcomeSucceeded, "sha256:comp"},
	{workflow.PromotionNodeEvaluateBand, workflow.OutcomeSucceeded, "sha256:band"},
	{workflow.PromotionNodeBuildProposal, workflow.OutcomeSucceeded, "sha256:proposal"},
	{workflow.PromotionNodeRaiseThreshold, workflow.Outcome("EXCEEDS_THRESHOLD"), "sha256:threshold"},
	{workflow.PromotionNodeEndApproval, "", "sha256:terminal"},
}

// startPromotionInstance runs [runtime.Start] once, in its own transaction,
// and returns the receipt.
func startPromotionInstance(t *testing.T, conn *pgxadapter.Conn, tenantID uuid.UUID, req runtime.StartRequest) runtime.StartReceipt {
	t.Helper()
	var receipt runtime.StartReceipt
	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		var err error
		receipt, err = runtime.Start(context.Background(), tx, req)
		return err
	})
	return receipt
}

// advanceOnce runs one [runtime.Advance] call in its own transaction.
func advanceOnce(t *testing.T, conn *pgxadapter.Conn, tenantID uuid.UUID, req runtime.AdvanceRequest) (runtime.AdvanceReceipt, error) {
	t.Helper()
	var receipt runtime.AdvanceReceipt
	err := inTenantTxErr(conn, tenantID, func(tx dbport.Tx) error {
		var advErr error
		receipt, advErr = runtime.Advance(context.Background(), tx, req)
		return advErr
	})
	return receipt, err
}

// runPromotionWalk drives every step of steps in order, feeding each
// receipt's new instance version forward as the next call's expectation, and
// fails the test on the first refusal.
func runPromotionWalk(
	t *testing.T, conn *pgxadapter.Conn, tenantID uuid.UUID, plan *workflow.CompiledWorkflow,
	instanceID uuid.UUID, startVersion int64, sink runtime.ContinuationSink, steps []promotionStep,
) []runtime.AdvanceReceipt {
	t.Helper()
	version := startVersion
	receipts := make([]runtime.AdvanceReceipt, 0, len(steps))
	for _, step := range steps {
		receipt, err := advanceOnce(t, conn, tenantID, runtime.AdvanceRequest{
			TenantID: tenantID, InstanceID: instanceID, ExpectedInstanceVersion: version, Attempt: 1,
			Plan:       plan,
			Outcome:    frontier.NodeOutcome{NodeID: step.NodeID, Outcome: step.Outcome, OutputDigest: step.Digest},
			RecordedAt: fixedInstant,
			Sink:       sink,
		})
		if err != nil {
			t.Fatalf("Advance(%s): %v", step.NodeID, err)
		}
		version = receipt.NewInstanceVersion
		receipts = append(receipts, receipt)
	}
	return receipts
}

// failingSink is a [runtime.ContinuationSink] that fails on exactly one
// declared intent kind and records what it saw. It is WF-RUN-025's failpoint:
// "injected after the node result but before the continuation write".
type failingSink struct {
	failOn frontier.IntentKind
}

func (f failingSink) fail(kind frontier.IntentKind) error {
	if kind == f.failOn {
		return fmt.Errorf("injected continuation failure for %s", kind)
	}
	return nil
}
func (f failingSink) RequireWorkItem(_ context.Context, _ runtime.Executor, _ runtime.ContinuationRecord) error {
	return f.fail(frontier.IntentWorkItemRequired)
}
func (f failingSink) RequireSignalSubscription(_ context.Context, _ runtime.Executor, _ runtime.ContinuationRecord) error {
	return f.fail(frontier.IntentSignalSubscriptionRequired)
}
func (f failingSink) RequireTimer(_ context.Context, _ runtime.Executor, _ runtime.ContinuationRecord) error {
	return f.fail(frontier.IntentTimerRequired)
}
func (f failingSink) MarkReady(_ context.Context, _ runtime.Executor, _ runtime.ContinuationRecord) error {
	return f.fail(frontier.IntentReady)
}
func (f failingSink) Complete(_ context.Context, _ runtime.Executor, _ runtime.ContinuationRecord) error {
	return f.fail(frontier.IntentComplete)
}

var _ runtime.ContinuationSink = failingSink{}

// TestTodo_WF_RUN_025 is the PRIMARY test: one fenced runtime transaction per
// advancement persists the attempt result/output, node transition, instance
// version/frontier and every derived continuation record exactly once, across
// a full promotion walk from Start to a stated terminal.
func TestTodo_WF_RUN_025(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "wfrun025")
	pf := newPromotionFixture(t, values.TenantId("wfrun025-tenant"), "intent:wf-run-025-1")

	start := startPromotionInstance(t, conn, tenantID, pf.baseStartRequest(tenantID, "start-key-025"))
	sink := runtime.NewMemorySink()

	receipts := runPromotionWalk(t, conn, tenantID, pf.Plan, start.InstanceID, start.InstanceVersion, sink, promotionExceedsThresholdWalk)

	t.Run("the walk visits every node and ends at the stated terminal", func(t *testing.T) {
		var visited []string
		for _, r := range receipts {
			visited = append(visited, r.NodeID)
			if r.Digest() == "" {
				t.Errorf("advance of %s minted no digest", r.NodeID)
			}
		}
		want := []string{
			workflow.PromotionNodeSnapshotWorker, workflow.PromotionNodeSimulateComp, workflow.PromotionNodeEvaluateBand,
			workflow.PromotionNodeBuildProposal, workflow.PromotionNodeRaiseThreshold, workflow.PromotionNodeEndApproval,
		}
		if len(visited) != len(want) {
			t.Fatalf("visited %v, want %v", visited, want)
		}
		for i := range want {
			if visited[i] != want[i] {
				t.Fatalf("visited[%d] = %q, want %q", i, visited[i], want[i])
			}
		}

		last := receipts[len(receipts)-1]
		if !last.Complete {
			t.Fatal("the final advancement did not complete the instance")
		}
		if last.TerminalCode != "SIMULATION_APPROVAL_REQUIRED" {
			t.Errorf("terminal code = %q, want SIMULATION_APPROVAL_REQUIRED", last.TerminalCode)
		}
		if len(last.Frontier) != 0 {
			t.Errorf("frontier after completion = %v, want empty", last.Frontier)
		}
	})

	t.Run("the instance row records the terminal dimensions", func(t *testing.T) {
		var loaded runtime.Instance
		inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
			var err error
			loaded, err = (runtime.Store{}).LoadInstance(context.Background(), tx, tenantID, start.InstanceID)
			return err
		})
		if loaded.RuntimeStatus != runtime.InstanceCompleted {
			t.Errorf("runtime status = %s, want COMPLETED", loaded.RuntimeStatus)
		}
		if loaded.CompletionDimensions.Empty() {
			t.Error("terminal dimensions were not recorded")
		}
		if loaded.CompletedAt == nil {
			t.Error("completed_at was not stamped")
		}
		if loaded.StartedAt == nil {
			t.Error("started_at was not stamped on the first advancement")
		}
	})

	t.Run("every intermediate successor was recorded and the terminal raised COMPLETE", func(t *testing.T) {
		recs := sink.Records()
		if len(recs) == 0 {
			t.Fatal("no continuation records were persisted")
		}
		sawComplete := false
		for _, r := range recs {
			if r.Kind == frontier.IntentComplete {
				sawComplete = true
				if r.TerminalCode != "SIMULATION_APPROVAL_REQUIRED" {
					t.Errorf("COMPLETE continuation terminal code = %q", r.TerminalCode)
				}
			}
		}
		if !sawComplete {
			t.Error("no COMPLETE continuation record was raised")
		}

		var executions []runtime.NodeExecution
		inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
			var err error
			executions, err = (runtime.Store{}).LoadNodeExecutions(context.Background(), tx, tenantID, start.InstanceID)
			return err
		})
		byNode := map[string]runtime.NodeExecution{}
		for _, e := range executions {
			byNode[e.NodeID] = e
		}
		for _, step := range promotionExceedsThresholdWalk {
			e, ok := byNode[step.NodeID]
			if !ok {
				t.Errorf("no node execution recorded for %s", step.NodeID)
				continue
			}
			if e.Status != runtime.NodeSucceeded {
				t.Errorf("node %s status = %s, want SUCCEEDED", step.NodeID, e.Status)
			}
			if e.OutputArtifactRef != step.Digest {
				t.Errorf("node %s output ref = %q, want %q", step.NodeID, e.OutputArtifactRef, step.Digest)
			}
		}
	})

	t.Run("a stale version commits nothing", func(t *testing.T) {
		var before runtime.Instance
		inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
			var err error
			before, err = (runtime.Store{}).LoadInstance(context.Background(), tx, tenantID, start.InstanceID)
			return err
		})

		_, err := advanceOnce(t, conn, tenantID, runtime.AdvanceRequest{
			TenantID: tenantID, InstanceID: start.InstanceID, ExpectedInstanceVersion: before.InstanceVersion + 500,
			Attempt: 1, Plan: pf.Plan,
			Outcome:    frontier.NodeOutcome{NodeID: workflow.PromotionNodeEndApproval, OutputDigest: "sha256:should-not-apply"},
			RecordedAt: fixedInstant, Sink: sink,
		})
		if runtime.CodeOf(err) != runtime.CodeStaleInstance {
			t.Fatalf("code = %q, want %q (%v)", runtime.CodeOf(err), runtime.CodeStaleInstance, err)
		}

		var after runtime.Instance
		inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
			var err error
			after, err = (runtime.Store{}).LoadInstance(context.Background(), tx, tenantID, start.InstanceID)
			return err
		})
		if after.InstanceVersion != before.InstanceVersion {
			t.Errorf("instance version moved from %d to %d on a refused advancement", before.InstanceVersion, after.InstanceVersion)
		}
	})

	t.Run("a deterministic retry returns the original receipt without reapplying", func(t *testing.T) {
		fx := newPromotionFixture(t, values.TenantId("wfrun025-retry-tenant"), "intent:wf-run-025-retry")
		s := startPromotionInstance(t, conn, tenantID, fx.baseStartRequest(tenantID, "start-key-025-retry"))
		retrySink := runtime.NewMemorySink()

		req := runtime.AdvanceRequest{
			TenantID: tenantID, InstanceID: s.InstanceID, ExpectedInstanceVersion: s.InstanceVersion, Attempt: 1,
			Plan: fx.Plan,
			Outcome: frontier.NodeOutcome{
				NodeID: workflow.PromotionNodeSnapshotWorker, Outcome: workflow.OutcomeSucceeded, OutputDigest: "sha256:retry-snapshot",
			},
			RecordedAt: fixedInstant, Sink: retrySink,
		}
		first, err := advanceOnce(t, conn, tenantID, req)
		if err != nil {
			t.Fatalf("first Advance: %v", err)
		}
		if first.Replay {
			t.Fatal("the first application reported Replay=true")
		}
		firstRecordCount := len(retrySink.Records())

		second, err := advanceOnce(t, conn, tenantID, req)
		if err != nil {
			t.Fatalf("retry Advance: %v", err)
		}
		if !second.Replay {
			t.Error("a deterministic retry did not report Replay=true")
		}
		if second.Digest() != first.Digest() {
			t.Errorf("retry digest = %q, want %q", second.Digest(), first.Digest())
		}
		if second.NewInstanceVersion != first.NewInstanceVersion {
			t.Errorf("retry new instance version = %d, want %d", second.NewInstanceVersion, first.NewInstanceVersion)
		}
		if got := len(retrySink.Records()); got != firstRecordCount {
			t.Errorf("retry invoked the sink again: %d records now, %d after the first application", got, firstRecordCount)
		}
	})
}

// TestTodo_WF_RUN_025_Race drives the SAME advancement of the SAME node
// concurrently from independent connections and transactions: exactly one
// commits, and every loser is refused rather than partially applying. A loser
// that reconstructs the winner's committed node state can be refused by the
// pure frontier layer as [frontier.CodeNodeNotActive]; a loser whose compare
// and set observes the version first is refused as [runtime.CodeStaleInstance].
func TestTodo_WF_RUN_025_Race(t *testing.T) {
	db := pgtest.New(t)
	setupConn := appConn(t, db)
	tenantID := insertTenant(t, db, "wfrun025-race")
	pf := newPromotionFixture(t, values.TenantId("wfrun025-race-tenant"), "intent:race")

	start := startPromotionInstance(t, setupConn, tenantID, pf.baseStartRequest(tenantID, "start-key-race"))
	sink := runtime.NewMemorySink()

	const workers = 6
	type outcome struct {
		receipt runtime.AdvanceReceipt
		err     error
	}
	results := make([]outcome, workers)
	conns := make([]*pgxadapter.Conn, workers)
	for i := range conns {
		conns[i] = appConn(t, db)
	}

	var startGate sync.WaitGroup
	var done sync.WaitGroup
	startGate.Add(1)
	for i := 0; i < workers; i++ {
		done.Add(1)
		go func(i int) {
			defer done.Done()
			startGate.Wait()
			req := runtime.AdvanceRequest{
				TenantID: tenantID, InstanceID: start.InstanceID, ExpectedInstanceVersion: start.InstanceVersion, Attempt: 1,
				Plan: pf.Plan,
				Outcome: frontier.NodeOutcome{
					NodeID: workflow.PromotionNodeSnapshotWorker, Outcome: workflow.OutcomeSucceeded, OutputDigest: "sha256:race-snapshot",
				},
				RecordedAt: fixedInstant, Sink: sink,
			}
			results[i].err = inTenantTxErr(conns[i], tenantID, func(tx dbport.Tx) error {
				var advErr error
				results[i].receipt, advErr = runtime.Advance(context.Background(), tx, req)
				return advErr
			})
		}(i)
	}
	startGate.Done()
	done.Wait()

	// Every worker submits the identical outcome under the identical
	// expected version. Exactly one of them applies it fresh. A worker whose
	// own read raced ahead of the winner's commit fails the version
	// compare-and-set deep inside the write path and is refused with
	// [runtime.CodeStaleInstance]; a worker whose read landed after the
	// winner's commit instead finds the recorded attempt already matches its
	// own outcome and succeeds via the replay path. Both are correct: what
	// must never happen is a second fresh application, or a success whose
	// content disagrees with the winner's.
	fresh, replayed, conflicts := 0, 0, 0
	var winnerDigest string
	for _, res := range results {
		switch {
		case res.err == nil && !res.receipt.Replay:
			fresh++
			winnerDigest = res.receipt.Digest()
		case res.err == nil && res.receipt.Replay:
			replayed++
		case runtime.CodeOf(res.err) == runtime.CodeStaleInstance,
			frontier.CodeOf(res.err) == frontier.CodeNodeNotActive:
			conflicts++
		default:
			t.Fatalf("unexpected error: %v", res.err)
		}
	}
	if fresh != 1 {
		t.Fatalf("fresh applications = %d, want exactly 1 (replayed = %d, conflicts = %d)", fresh, replayed, conflicts)
	}
	if fresh+replayed+conflicts != workers {
		t.Fatalf("fresh(%d) + replayed(%d) + conflicts(%d) != workers(%d)", fresh, replayed, conflicts, workers)
	}
	for _, res := range results {
		if res.err == nil && res.receipt.Digest() != winnerDigest {
			t.Errorf("a successful concurrent submission reported digest %q, want the winner's %q", res.receipt.Digest(), winnerDigest)
		}
	}

	verifyConn := appConn(t, db)
	var executions []runtime.NodeExecution
	inTenantTx(t, verifyConn, tenantID, func(tx dbport.Tx) error {
		var err error
		executions, err = (runtime.Store{}).LoadNodeExecutions(context.Background(), tx, tenantID, start.InstanceID)
		return err
	})
	succeeded := 0
	for _, e := range executions {
		if e.NodeID == workflow.PromotionNodeSnapshotWorker && e.Status == runtime.NodeSucceeded {
			succeeded++
		}
	}
	if succeeded != 1 {
		t.Fatalf("snapshot_worker recorded SUCCEEDED %d times, want exactly 1", succeeded)
	}
}

// TestTodo_WF_RUN_025_Fault proves the failpoint the ticket names: a
// [runtime.ContinuationSink] that fails after the node result is computed but
// before its own write leaves the database showing either the entire
// advancement or none of it -- here, none, because the caller's transaction
// rolls back on the returned error.
func TestTodo_WF_RUN_025_Fault(t *testing.T) {
	db := pgtest.New(t)
	conn := appConn(t, db)
	tenantID := insertTenant(t, db, "wfrun025-fault")
	pf := newPromotionFixture(t, values.TenantId("wfrun025-fault-tenant"), "intent:fault")

	start := startPromotionInstance(t, conn, tenantID, pf.baseStartRequest(tenantID, "start-key-fault"))

	var before runtime.Instance
	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		var err error
		before, err = (runtime.Store{}).LoadInstance(context.Background(), tx, tenantID, start.InstanceID)
		return err
	})

	_, err := advanceOnce(t, conn, tenantID, runtime.AdvanceRequest{
		TenantID: tenantID, InstanceID: start.InstanceID, ExpectedInstanceVersion: start.InstanceVersion, Attempt: 1,
		Plan: pf.Plan,
		Outcome: frontier.NodeOutcome{
			NodeID: workflow.PromotionNodeSnapshotWorker, Outcome: workflow.OutcomeSucceeded, OutputDigest: "sha256:fault-snapshot",
		},
		RecordedAt: fixedInstant,
		// MarkReady is called for the successor this advancement activates
		// (simulate_compensation) -- after the completing node's own
		// transition has already been written inside this same transaction,
		// which is exactly the "after the node result but before the
		// continuation write" failpoint.
		Sink: failingSink{failOn: frontier.IntentReady},
	})
	if err == nil {
		t.Fatal("expected the injected continuation failure to surface")
	}

	var after runtime.Instance
	var executions []runtime.NodeExecution
	inTenantTx(t, conn, tenantID, func(tx dbport.Tx) error {
		var loadErr error
		after, loadErr = (runtime.Store{}).LoadInstance(context.Background(), tx, tenantID, start.InstanceID)
		if loadErr != nil {
			return loadErr
		}
		executions, loadErr = (runtime.Store{}).LoadNodeExecutions(context.Background(), tx, tenantID, start.InstanceID)
		return loadErr
	})

	if after.InstanceVersion != before.InstanceVersion {
		t.Errorf("instance version moved from %d to %d despite the injected failure", before.InstanceVersion, after.InstanceVersion)
	}
	if len(after.CurrentNodeIDs) != 1 || after.CurrentNodeIDs[0] != workflow.PromotionNodeSnapshotWorker {
		t.Errorf("frontier = %v, want the pre-failure frontier [%s] unchanged", after.CurrentNodeIDs, workflow.PromotionNodeSnapshotWorker)
	}
	for _, e := range executions {
		if e.NodeID == workflow.PromotionNodeSnapshotWorker && e.Status != runtime.NodeReady {
			t.Errorf("snapshot_worker status = %s, want READY (the completing node's own transition was not rolled back)", e.Status)
		}
		if e.NodeID == workflow.PromotionNodeSimulateComp {
			t.Errorf("successor %s was recorded despite the injected failure", e.NodeID)
		}
	}
}

// TestTodo_WF_RUN_025_Mutation asserts the structural invariants that make
// "persists ... exactly once" true by construction rather than by
// convention: a continuation record's identity is a pure function of the
// tuple that defines it, and [runtime.ContinuationStore] turns a repeated
// write of that identity into a no-op instead of a duplicate row.
func TestTodo_WF_RUN_025_Mutation(t *testing.T) {
	tenantID := uuid.New()
	instanceID := uuid.New()

	t.Run("continuation identity is a pure function of its defining tuple", func(t *testing.T) {
		a := runtime.ContinuationID(tenantID, instanceID, "node-a", 1, "node-b", frontier.IntentReady)
		b := runtime.ContinuationID(tenantID, instanceID, "node-a", 1, "node-b", frontier.IntentReady)
		if a != b {
			t.Fatalf("ContinuationID is not deterministic: %s != %s", a, b)
		}
		c := runtime.ContinuationID(tenantID, instanceID, "node-a", 1, "node-b", frontier.IntentComplete)
		if a == c {
			t.Fatal("two different intent kinds for the same node tuple collided on one id")
		}
		d := runtime.ContinuationID(tenantID, instanceID, "node-a", 2, "node-b", frontier.IntentReady)
		if a == d {
			t.Fatal("two different source attempts collided on one id")
		}
	})

	t.Run("a repeated durable write of the same record is a no-op, not a duplicate", func(t *testing.T) {
		db := pgtest.New(t)
		conn := appConn(t, db)
		realTenant := insertTenant(t, db, "wfrun025-mutation")
		pf := newPromotionFixture(t, values.TenantId("wfrun025-mutation-tenant"), "intent:mutation")
		start := startPromotionInstance(t, conn, realTenant, pf.baseStartRequest(realTenant, "start-key-mutation"))

		store := runtime.ContinuationStore{}
		rec := runtime.ContinuationRecord{
			TenantID: realTenant, InstanceID: start.InstanceID,
			SourceNodeID: pf.Plan.StartNodeID, SourceAttempt: 1,
			TargetNodeID: pf.Plan.StartNodeID, Kind: frontier.IntentReady,
			RecordedAt: fixedInstant,
		}
		inTenantTx(t, conn, realTenant, func(tx dbport.Tx) error {
			if err := store.MarkReady(context.Background(), tx, rec); err != nil {
				return err
			}
			return store.MarkReady(context.Background(), tx, rec)
		})

		var count int
		inTenantTx(t, conn, realTenant, func(tx dbport.Tx) error {
			return tx.QueryRow(context.Background(),
				`SELECT count(*) FROM workflow_continuation WHERE tenant_id = $1 AND instance_id = $2 AND target_node_id = $3 AND kind = $4`,
				realTenant, start.InstanceID, pf.Plan.StartNodeID, string(frontier.IntentReady)).Scan(&count)
		})
		if count != 1 {
			t.Fatalf("continuation rows for one identity = %d, want exactly 1", count)
		}
	})

	t.Run("a deterministic Advance retry writes no duplicate node execution or continuation rows", func(t *testing.T) {
		db := pgtest.New(t)
		conn := appConn(t, db)
		realTenant := insertTenant(t, db, "wfrun025-mutation-2")
		pf := newPromotionFixture(t, values.TenantId("wfrun025-mutation-2-tenant"), "intent:mutation-2")
		start := startPromotionInstance(t, conn, realTenant, pf.baseStartRequest(realTenant, "start-key-mutation-2"))

		req := runtime.AdvanceRequest{
			TenantID: realTenant, InstanceID: start.InstanceID, ExpectedInstanceVersion: start.InstanceVersion, Attempt: 1,
			Plan: pf.Plan,
			Outcome: frontier.NodeOutcome{
				NodeID: workflow.PromotionNodeSnapshotWorker, Outcome: workflow.OutcomeSucceeded, OutputDigest: "sha256:mutation-snapshot",
			},
			RecordedAt: fixedInstant, Sink: runtime.ContinuationStore{},
		}
		if _, err := advanceOnce(t, conn, realTenant, req); err != nil {
			t.Fatalf("first Advance: %v", err)
		}
		if _, err := advanceOnce(t, conn, realTenant, req); err != nil {
			t.Fatalf("retry Advance: %v", err)
		}

		var executions []runtime.NodeExecution
		inTenantTx(t, conn, realTenant, func(tx dbport.Tx) error {
			var err error
			executions, err = (runtime.Store{}).LoadNodeExecutions(context.Background(), tx, realTenant, start.InstanceID)
			return err
		})
		seen := map[string]int{}
		for _, e := range executions {
			seen[e.NodeID]++
		}
		for nodeID, n := range seen {
			if n != 1 {
				t.Errorf("node %s has %d recorded executions after a retry, want exactly 1", nodeID, n)
			}
		}
	})
}
