package execute_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/runtimestate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	datalogger "github.com/monstercameron/human-capital-management-suite/internal/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/admission"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/effects"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/timer"
)

const observationBackoffRef = "policy.retry.observation.bounded/v1"

// retryRunner fails the payroll OBSERVE node with classes[n-1] on its n-th
// run (an empty class, or a run past the list, observes PASS) and records the
// attempt number each run was handed. Every other node follows the promotion
// conformance runner through access PASS and a PARTIAL reconciliation, which
// the plan routes to its repair terminal.
type retryRunner struct {
	classes  []string
	calls    *atomic.Int32
	mu       *sync.Mutex
	attempts *[]int
}

func newRetryRunner(classes ...string) retryRunner {
	return retryRunner{classes: classes, calls: &atomic.Int32{}, mu: &sync.Mutex{}, attempts: &[]int{}}
}

func (r retryRunner) Run(ctx context.Context, req execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	if req.Node.ID != promotionexec.NodeObservePayroll {
		return promotionRunner{payroll: "PASS", recon: "PARTIAL"}.Run(ctx, req)
	}
	n := int(r.calls.Add(1))
	r.mu.Lock()
	*r.attempts = append(*r.attempts, req.Attempt)
	r.mu.Unlock()
	if n <= len(r.classes) && r.classes[n-1] != "" {
		return frontier.NodeOutcome{NodeID: req.Node.ID, Failed: true, ErrorClass: r.classes[n-1]}, runtime.GovernanceRefs{}, nil
	}
	return promotionRunner{payroll: "PASS"}.Run(ctx, req)
}

func (r retryRunner) seen() []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]int(nil), *r.attempts...)
}

func observationBackoff() execute.RetryBackoff {
	return execute.RetryBackoff{BaseDelay: 30 * time.Second, MaxDelay: 5 * time.Minute, JitterFraction: 0.5, Deadline: 24 * time.Hour}
}

func nodeRetryPolicy(provisioner *admission.Provisioner, backoff execute.RetryBackoff) *execute.NodeRetryPolicy {
	return &execute.NodeRetryPolicy{
		Backoffs:  map[string]execute.RetryBackoff{observationBackoffRef: backoff},
		Retryable: []runtime.FailureKind{runtime.FailureTransient, runtime.FailureTimeout},
		Budget:    execute.ProvisionedRetryBudget(provisioner, "payroll.authority", "v1"),
		Timers:    timer.RetryTimers{},
	}
}

func retryDriver(t *testing.T, f promotionFixture, conn dbport.Beginner, runner execute.StepRunner, policy *execute.NodeRetryPolicy, poison *execute.PoisonWorkPolicy) *execute.Driver {
	t.Helper()
	registry, err := datalogger.NewLedgerEventDigestRegistry()
	if err != nil {
		t.Fatal(err)
	}
	terminal := &effects.LedgerTerminalWriter{Appender: datalogger.NewAppender(registry), ProjectionName: "promotion_conformance_outcome", SourceRef: "hcmnext:test:wfrun006"}
	driver, err := execute.New(execute.Options{
		DB: conn, Steps: runner, Terminal: terminal, Repair: &repairRequester{}, Guard: idempotency.PostgresStore{},
		Retention: idempotency.RetentionPolicy{Retention: 72 * time.Hour, RetryWindow: 6 * time.Hour},
		Clock:     func() time.Time { return f.at }, TimerReader: timer.Reader{},
		NodeRetry: policy, PoisonWork: poison,
	})
	if err != nil {
		t.Fatal(err)
	}
	return driver
}

type timerRow struct {
	id            uuid.UUID
	key, kind, st string
	firesAt       time.Time
	version       int64
	nodeID        string
}

func retryTimerRows(t *testing.T, f promotionFixture) []timerRow {
	t.Helper()
	rows, err := f.db.Conn.Query(context.Background(), `SELECT timer_id, timer_key, timer_kind, timer_state, fires_at, timer_version, node_id
		FROM workflow_timer WHERE tenant_id = $1 ORDER BY fires_at`, f.tenantID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []timerRow
	for rows.Next() {
		var r timerRow
		if err := rows.Scan(&r.id, &r.key, &r.kind, &r.st, &r.firesAt, &r.version, &r.nodeID); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func payrollAttempts(t *testing.T, f promotionFixture, instanceID uuid.UUID) int {
	t.Helper()
	return nodeExecutions(t, f, instanceID, promotionexec.NodeObservePayroll)
}

// nodeExecutions counts a node's attempts that were activated rather than
// settled SKIPPED by a route that excluded them.
func nodeExecutions(t *testing.T, f promotionFixture, instanceID uuid.UUID, nodeID string) int {
	t.Helper()
	var n int
	if err := f.db.Conn.QueryRow(context.Background(), `SELECT count(*) FROM workflow_node_execution
		WHERE tenant_id = $1 AND instance_id = $2 AND node_id = $3 AND status <> 'SKIPPED'`, f.tenantID, instanceID, nodeID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func fireRetryTimer(t *testing.T, f promotionFixture, row timerRow) {
	t.Helper()
	ctx := context.Background()
	tx, err := f.conn.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := tenancy.WithTenant(ctx, tx, f.tenantID); err != nil {
		t.Fatal(err)
	}
	if err := (runtimestate.TimerStore{}).Fire(ctx, tx, f.tenantID, row.id, uint64(row.version), row.firesAt); err != nil {
		t.Fatalf("fire retry timer: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func resumeRetry(f promotionFixture, inst runtime.Instance, timerID uuid.UUID) execute.ResumeTimerRequest {
	return execute.ResumeTimerRequest{
		Start: f.start, InstanceID: inst.InstanceID, ExpectedInstanceVersion: inst.InstanceVersion, TimerID: timerID,
		Outcome: frontier.NodeOutcome{NodeID: promotionexec.NodeObservePayroll, Outcome: workflow.OutcomeSucceeded},
	}
}

// parkOnTransientFailure runs the fixture until the payroll node's first
// TRANSIENT failure parks it, asserts the exact durable backoff and returns
// the timer row.
func parkOnTransientFailure(t *testing.T, f promotionFixture, driver *execute.Driver, runner retryRunner) (runtime.Instance, timerRow) {
	t.Helper()
	result, err := driver.Execute(context.Background(), execute.ExecuteRequest{Start: f.start})
	if err != nil {
		t.Fatalf("Execute = %v, want a parked retry", err)
	}
	instanceID := result.Start.InstanceID
	want := f.at.Add(execute.RetryJitter(f.tenantID, instanceID, promotionexec.NodeObservePayroll, 0.5)(2, 30*time.Second))
	if result.Status != execute.StatusParked || len(result.Timers) != 1 || !result.Timers[0].RetryBackoff ||
		result.Timers[0].NodeID != promotionexec.NodeObservePayroll || !result.Timers[0].FiresAt.Equal(want) ||
		result.Timers[0].Key != execute.RetryBackoffKey(2) {
		t.Fatalf("result = %s timers %+v, want PARKED on one retry timer at %s", result.Status, result.Timers, want)
	}
	if want.Equal(f.at.Add(30*time.Second)) || want.Before(f.at.Add(15*time.Second)) {
		t.Fatalf("backoff instant %s shows no bounded jitter", want)
	}
	rows := retryTimerRows(t, f)
	if len(rows) != 1 || rows[0].kind != execute.TimerKindRetryBackoff || rows[0].st != runtimestate.TimerPending ||
		!rows[0].firesAt.Equal(want) || rows[0].key != execute.RetryBackoffKey(2) || rows[0].id != result.Timers[0].TimerID {
		t.Fatalf("durable timers = %+v, want one PENDING RETRY_BACKOFF at %s", rows, want)
	}
	if status, _ := nodeAttempt(t, f, instanceID, 1); status != string(runtime.NodeRetrying) {
		t.Fatalf("payroll attempt 1 = %s, want RETRYING", status)
	}
	if n := payrollAttempts(t, f, instanceID); n != 1 || runner.calls.Load() != 1 {
		t.Fatalf("payroll attempts = %d, runs = %d before the backoff fired, want 1/1", n, runner.calls.Load())
	}
	return instance(t, f, instanceID), rows[0]
}

// TestTodo_WF_RUN_006_Integration drives the served driver over PostgreSQL:
// a TRANSIENT failure parks on a durable RETRY_BACKOFF timer at the exact
// jittered backoff instant, a re-run does not skip the backoff, and resuming
// the fired timer runs attempt 2 through to completion exactly once.
func TestTodo_WF_RUN_006_Integration(t *testing.T) {
	ctx := context.Background()
	f := newPromotionFixtureV1_0(t, "wfrun006-transient")
	runner := newRetryRunner("TRANSIENT")
	provisioner := admission.NewProvisioner()
	driver := retryDriver(t, f, f.conn, runner, nodeRetryPolicy(provisioner, observationBackoff()), nil)

	inst, row := parkOnTransientFailure(t, f, driver, runner)
	if _, err := driver.Execute(ctx, execute.ExecuteRequest{Start: f.start}); !errors.Is(err, execute.ErrRetryBackoffPending) {
		t.Fatalf("re-Execute before the backoff = %v, want ErrRetryBackoffPending", err)
	}
	if runner.calls.Load() != 1 {
		t.Fatalf("re-Execute ran payroll again (%d runs)", runner.calls.Load())
	}
	if _, err := driver.ResumeTimer(ctx, resumeRetry(f, inst, row.id)); !errors.Is(err, execute.ErrTimerDrift) {
		t.Fatalf("resume of a pending retry timer = %v, want ErrTimerDrift", err)
	}

	fireRetryTimer(t, f, row)
	result, err := driver.ResumeTimer(ctx, resumeRetry(f, inst, row.id))
	if err != nil {
		t.Fatalf("ResumeTimer = %v", err)
	}
	if result.Status != execute.StatusComplete {
		t.Fatalf("resumed status = %s, want COMPLETE", result.Status)
	}
	if seen := runner.seen(); len(seen) != 2 || seen[0] != 1 || seen[1] != 2 {
		t.Fatalf("payroll ran attempts %v, want [1 2]", seen)
	}
	if status, _ := nodeAttempt(t, f, inst.InstanceID, 2); status != string(runtime.NodeSucceeded) {
		t.Fatalf("payroll attempt 2 = %s, want SUCCEEDED", status)
	}
	if n := nodeExecutions(t, f, inst.InstanceID, promotionexec.NodeObserveAccess); n != 1 {
		t.Fatalf("access observations = %d, want the PASS route taken after attempt 2", n)
	}
	snapshot, _ := provisioner.Snapshot(provisioner.Ledger()[0])
	if snapshot.Consumed != 1 || snapshot.Allowed != 1 {
		t.Fatalf("budget = %+v, want the one scheduled retry spent of one allowed", snapshot)
	}
	if _, err := driver.ResumeTimer(ctx, resumeRetry(f, instance(t, f, inst.InstanceID), row.id)); !errors.Is(err, execute.ErrTimerDrift) {
		t.Fatalf("second resume of a consumed retry timer = %v, want ErrTimerDrift", err)
	}
	if runner.calls.Load() != 2 {
		t.Fatalf("payroll runs = %d after a replayed resume, want 2", runner.calls.Load())
	}
}

// TestTodo_WF_RUN_006_IntegrationTerminal proves terminal decisions never
// retry: a nonretryable class, a caller-mapped DO_NOT_RETRY and a deadline the
// next backoff would cross route the compiled exhaustion route on attempt 1,
// and a second failure exhausts the attempt cap into the same route.
func TestTodo_WF_RUN_006_IntegrationTerminal(t *testing.T) {
	short := observationBackoff()
	short.Deadline = 10 * time.Second
	for _, tc := range []struct {
		name    string
		classes []string
		backoff execute.RetryBackoff
		runs    int
	}{
		{name: "nonretryable", classes: []string{"PROVIDER_REJECTED"}, backoff: observationBackoff(), runs: 1},
		{name: "do not retry", classes: []string{runtime.ReasonDoNotRetry}, backoff: observationBackoff(), runs: 1},
		{name: "deadline", classes: []string{"TRANSIENT"}, backoff: short, runs: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newPromotionFixtureV1_0(t, "wfrun006-"+tc.name)
			runner := newRetryRunner(tc.classes...)
			driver := retryDriver(t, f, f.conn, runner, nodeRetryPolicy(admission.NewProvisioner(), tc.backoff), nil)
			result, err := driver.Execute(context.Background(), execute.ExecuteRequest{Start: f.start})
			if err != nil {
				t.Fatalf("Execute = %v", err)
			}
			assertExhaustionRoute(t, f, result, runner, tc.runs)
		})
	}

	t.Run("attempts exhausted", func(t *testing.T) {
		ctx := context.Background()
		f := newPromotionFixtureV1_0(t, "wfrun006-exhausted")
		runner := newRetryRunner("TRANSIENT", "TIMEOUT")
		driver := retryDriver(t, f, f.conn, runner, nodeRetryPolicy(admission.NewProvisioner(), observationBackoff()), nil)
		inst, row := parkOnTransientFailure(t, f, driver, runner)
		fireRetryTimer(t, f, row)
		result, err := driver.ResumeTimer(ctx, resumeRetry(f, inst, row.id))
		if err != nil {
			t.Fatalf("ResumeTimer = %v", err)
		}
		assertExhaustionRoute(t, f, result, runner, 2)
		if n := len(retryTimerRows(t, f)); n != 1 {
			t.Fatalf("retry timers = %d after exhaustion, want only the first backoff", n)
		}
	})
}

func assertExhaustionRoute(t *testing.T, f promotionFixture, result execute.Result, runner retryRunner, runs int) {
	t.Helper()
	if result.Status != execute.StatusComplete || len(result.Timers) != 0 {
		t.Fatalf("result = %s timers %+v, want COMPLETE through the exhaustion route with no new timer", result.Status, result.Timers)
	}
	got := instance(t, f, result.Advances[len(result.Advances)-1].InstanceID)
	if got.RuntimeStatus != runtime.InstanceRepairRequired {
		t.Fatalf("instance = %s, want REPAIR_REQUIRED from the retry exhaustion route", got.RuntimeStatus)
	}
	if int(runner.calls.Load()) != runs || payrollAttempts(t, f, got.InstanceID) != runs {
		t.Fatalf("payroll runs = %d, attempts = %d, want %d", runner.calls.Load(), payrollAttempts(t, f, got.InstanceID), runs)
	}
	if n := nodeExecutions(t, f, got.InstanceID, promotionexec.NodeObserveAccess); n != 0 {
		t.Fatalf("access observations = %d, want none: the failed payroll node took its exhaustion route", n)
	}
}

// TestTodo_WF_RUN_006_IntegrationPoison proves exhausted work with no route
// still reaches the durable poison-work path: DO_NOT_RETRY files BLOCKED on
// attempt 1, and a spent shared budget files REPAIR_REQUIRED with the budget's
// repair route, both without a second run.
func TestTodo_WF_RUN_006_IntegrationPoison(t *testing.T) {
	policy := func() *execute.PoisonWorkPolicy {
		return &execute.PoisonWorkPolicy{Store: runtime.QuarantineStore{}, Owner: "principal:workflow-operations", SLA: time.Hour}
	}
	t.Run("do not retry", func(t *testing.T) {
		f := newPoisonFixture(t, "wfrun006-poison-dnr")
		runner := newRetryRunner(runtime.ReasonDoNotRetry)
		_, err := retryDriver(t, f, f.conn, runner, nodeRetryPolicy(admission.NewProvisioner(), observationBackoff()), policy()).
			Execute(context.Background(), execute.ExecuteRequest{Start: f.start})
		var poisoned *execute.PoisonWorkError
		if !errors.As(err, &poisoned) || poisoned.Status != runtime.InstanceBlocked || poisoned.Work.Attempts != 1 {
			t.Fatalf("Execute = %v, want BLOCKED poison work after attempt 1", err)
		}
		if runner.calls.Load() != 1 || len(retryTimerRows(t, f)) != 0 {
			t.Fatalf("runs = %d, timers = %d, want one run and no backoff", runner.calls.Load(), len(retryTimerRows(t, f)))
		}
	})
	t.Run("budget exhausted", func(t *testing.T) {
		f := newPoisonFixture(t, "wfrun006-poison-budget")
		inst := instanceByTenant(t, f)
		provisioner := admission.NewProvisioner()
		// The node's logical operation already holds a budget with no
		// allowance: another layer spent it, so this failure may not retry.
		if _, err := provisioner.Provision(admission.ProvisionSpec{
			TenantID: f.tenantID.String(), Service: "workflow", Dependency: "payroll.authority",
			LogicalOperationID: "workflow-node:" + inst.InstanceID.String() + "/" + promotionexec.NodeObservePayroll,
			OperationKind:      "workflow.node.retry", Allowed: 0,
			Retryable: []admission.FailureClass{admission.FailureTransient}, Version: "v1",
		}); err != nil {
			t.Fatal(err)
		}
		runner := newRetryRunner("TRANSIENT")
		_, err := retryDriver(t, f, f.conn, runner, nodeRetryPolicy(provisioner, observationBackoff()), policy()).
			Execute(context.Background(), execute.ExecuteRequest{Start: f.start})
		var poisoned *execute.PoisonWorkError
		if !errors.As(err, &poisoned) || poisoned.Status != runtime.InstanceRepairRequired || poisoned.Work.Attempts != 1 ||
			poisoned.Work.RepairRoute == "" {
			t.Fatalf("Execute = %v, want REPAIR_REQUIRED poison work with the budget repair route", err)
		}
		if runner.calls.Load() != 1 || len(retryTimerRows(t, f)) != 0 {
			t.Fatalf("runs = %d, timers = %d, want one run and no backoff", runner.calls.Load(), len(retryTimerRows(t, f)))
		}
	})
}

// TestTodo_WF_RUN_006_IntegrationImmediate proves RETRY_NOW is bounded: a
// backoff that admits zero delay retries in the same drain with no timer, and
// only up to the compiled attempt cap.
func TestTodo_WF_RUN_006_IntegrationImmediate(t *testing.T) {
	backoff := observationBackoff()
	backoff.Immediate = true
	f := newPromotionFixtureV1_0(t, "wfrun006-immediate")
	runner := newRetryRunner("TIMEOUT", "TIMEOUT", "TIMEOUT")
	result, err := retryDriver(t, f, f.conn, runner, nodeRetryPolicy(admission.NewProvisioner(), backoff), nil).
		Execute(context.Background(), execute.ExecuteRequest{Start: f.start})
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	assertExhaustionRoute(t, f, result, runner, 2)
	if seen := runner.seen(); len(seen) != 2 || seen[1] != 2 {
		t.Fatalf("payroll attempts = %v, want [1 2]", seen)
	}
	if n := len(retryTimerRows(t, f)); n != 0 {
		t.Fatalf("retry timers = %d for an immediate retry, want 0", n)
	}
}

// TestTodo_WF_RUN_006_IntegrationRace resumes one fired backoff timer from six
// drivers on six connections at once: exactly one resume succeeds, exactly one
// attempt 2 exists and the node runs attempt 2 exactly once.
func TestTodo_WF_RUN_006_IntegrationRace(t *testing.T) {
	f := newPromotionFixtureV1_0(t, "wfrun006-race")
	runner := newRetryRunner("TRANSIENT")
	policy := nodeRetryPolicy(admission.NewProvisioner(), observationBackoff())
	inst, row := parkOnTransientFailure(t, f, retryDriver(t, f, f.conn, runner, policy, nil), runner)
	fireRetryTimer(t, f, row)

	const workers = 6
	drivers := make([]*execute.Driver, workers)
	for i := range drivers {
		drivers[i] = retryDriver(t, f, appConn(t, f.db), runner, policy, nil)
	}
	errs := make([]error, workers)
	var start, done sync.WaitGroup
	start.Add(1)
	for i := 0; i < workers; i++ {
		done.Add(1)
		go func(i int) {
			defer done.Done()
			start.Wait()
			_, errs[i] = drivers[i].ResumeTimer(context.Background(), resumeRetry(f, inst, row.id))
		}(i)
	}
	start.Done()
	done.Wait()

	successes := 0
	for _, err := range errs {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("concurrent resumes succeeded %d times (%v), want exactly 1", successes, errs)
	}
	if n := payrollAttempts(t, f, inst.InstanceID); n != 2 {
		t.Fatalf("payroll attempts = %d, want exactly attempt 1 and one attempt 2", n)
	}
	if seen := runner.seen(); len(seen) != 2 || seen[1] != 2 {
		t.Fatalf("payroll ran attempts %v, want attempt 2 exactly once", seen)
	}
}
