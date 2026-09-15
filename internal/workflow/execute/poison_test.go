package execute_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/effects"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// poisonRunner fails the payroll OBSERVE node on every attempt with one error
// class and otherwise behaves like the promotion conformance runner.
type poisonRunner struct {
	class   string
	payroll *atomic.Int32
}

func (r poisonRunner) Run(ctx context.Context, req execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	if req.Node.ID == promotionexec.NodeObservePayroll {
		r.payroll.Add(1)
		return frontier.NodeOutcome{NodeID: req.Node.ID, Failed: true, ErrorClass: r.class}, runtime.GovernanceRefs{}, nil
	}
	return promotionRunner{}.Run(ctx, req)
}

// newPoisonFixture is the promotion conformance fixture parked just before
// execute_promotion, on a plan whose payroll OBSERVE node keeps its bounded
// retry budget (two attempts) and no failure route, and whose declared
// retry-exhaustion route names a node the payroll node has no edge to (the
// compiler requires the route to name a declared node, not an edge). The
// driver therefore cannot route the exhaustion: exactly the node that used to
// be refused NO_FAILURE_ROUTE with nothing durable left behind.
func newPoisonFixture(t *testing.T, key string) promotionFixture {
	t.Helper()
	f := newPromotionFixtureBase(t, key)
	def := promotionexec.Definition()
	found := false
	for i := range def.Nodes {
		if def.Nodes[i].ID == promotionexec.NodeObservePayroll {
			observe := *def.Nodes[i].Observe
			observe.RetryExhaustionRoute = promotionexec.NodeEndBlocked
			def.Nodes[i].Observe = &observe
			def.Nodes[i].FailureRoute = ""
			found = true
		}
	}
	if !found {
		t.Fatal("promotion definition has no payroll OBSERVE node")
	}
	plan, err := promotionexec.Compile(def)
	if err != nil {
		t.Fatalf("compile poison plan: %v", err)
	}
	if node, ok := plan.Node(promotionexec.NodeObservePayroll); !ok || node.Retry == nil || node.Retry.MaxAttempts != 2 {
		t.Fatalf("payroll node = %+v, want a two-attempt retry budget", node)
	}
	f.plan = plan
	f.start.Resolver = effects.PolicyResolver{Entries: []effects.PolicyEntry{{WorkflowID: plan.WorkflowID, Pin: version.Pin{CompiledPlanDigest: plan.Digest()}, Plan: plan}}}
	f.start.Versions = promotionVersionStore{plan: plan}
	preparePromotionAt(t, f, 8)
	return f
}

func poisonDriver(t *testing.T, f promotionFixture, runner execute.StepRunner, quarantine *execute.PoisonWorkPolicy) *execute.Driver {
	t.Helper()
	driver, err := execute.New(execute.Options{
		DB: f.conn, Steps: runner, Guard: idempotency.PostgresStore{},
		Retention: idempotency.RetentionPolicy{Retention: 72 * time.Hour, RetryWindow: 6 * time.Hour},
		Clock:     func() time.Time { return f.at }, PoisonWork: quarantine,
	})
	if err != nil {
		t.Fatal(err)
	}
	return driver
}

type quarantineRow struct {
	route, lastError, owner, nextAction, repairRoute, digest string
	attempts                                                 int
	ambiguous                                                bool
	slaNanos                                                 int64
}

func quarantineRows(t *testing.T, f promotionFixture) []quarantineRow {
	t.Helper()
	rows, err := f.db.Conn.Query(context.Background(), `SELECT route, last_error, owner, next_action, repair_route, record_digest, attempts, ambiguous, sla_nanos
		FROM workflow_quarantined_work WHERE tenant_id = $1`, f.tenantID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []quarantineRow
	for rows.Next() {
		var r quarantineRow
		if err := rows.Scan(&r.route, &r.lastError, &r.owner, &r.nextAction, &r.repairRoute, &r.digest, &r.attempts, &r.ambiguous, &r.slaNanos); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func nodeAttempt(t *testing.T, f promotionFixture, instanceID uuid.UUID, attempt int) (status, errorClass string) {
	t.Helper()
	var class *string
	if err := f.db.Conn.QueryRow(context.Background(), `SELECT status, error_class FROM workflow_node_execution
		WHERE tenant_id = $1 AND instance_id = $2 AND node_id = $3 AND attempt = $4`,
		f.tenantID, instanceID, promotionexec.NodeObservePayroll, attempt).Scan(&status, &class); err != nil {
		t.Fatalf("load payroll attempt %d: %v", attempt, err)
	}
	if class != nil {
		errorClass = *class
	}
	return status, errorClass
}

// TestTodo_WF_RUN_007_Integration drives the served driver over PostgreSQL:
// an OBSERVE node whose retries are exhausted runs its budget exactly once,
// then lands a durable QuarantinedWork row and routes the instance BLOCKED,
// REPAIR_REQUIRED or QUARANTINED as runtime.Admit decides -- never COMPLETE,
// never another attempt, never a ledger fact.
func TestTodo_WF_RUN_007_Integration(t *testing.T) {
	for _, tc := range []struct {
		name, class, repairRoute string
		status                   runtime.InstanceStatus
		nextAction               string
		ambiguous                bool
	}{
		{name: "attempts exhausted", class: "TIMEOUT", status: runtime.InstanceBlocked, nextAction: "operator-decision"},
		{name: "shared budget exhausted", class: runtime.ReasonBudgetExhausted, repairRoute: "operations.repair.retry_budget",
			status: runtime.InstanceRepairRequired, nextAction: "repair:operations.repair.retry_budget"},
		{name: "ambiguous outcome", class: "UNKNOWN_PAYROLL_OUTCOME", status: runtime.InstanceQuarantined,
			nextAction: "reconcile-outcome:" + promotionexec.NodeObservePayroll, ambiguous: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			f := newPoisonFixture(t, "wfrun007-"+string(tc.status))
			var calls atomic.Int32
			policy := &execute.PoisonWorkPolicy{Store: runtime.QuarantineStore{}, Owner: "principal:workflow-operations", SLA: 4 * time.Hour, RepairRoute: tc.repairRoute}
			driver := poisonDriver(t, f, poisonRunner{class: tc.class, payroll: &calls}, policy)

			result, err := driver.Execute(ctx, execute.ExecuteRequest{Start: f.start})
			if !errors.Is(err, execute.ErrPoisonWorkQuarantined) {
				t.Fatalf("Execute = %+v, %v; want ErrPoisonWorkQuarantined", result, err)
			}
			if result.Status == execute.StatusComplete {
				t.Fatal("a poisoned workflow reported COMPLETE")
			}
			var quarantined *execute.PoisonWorkError
			if !errors.As(err, &quarantined) {
				t.Fatalf("error %v carries no PoisonWorkError", err)
			}
			work := quarantined.Work
			if quarantined.Status != tc.status || work.Route != string(tc.status) || work.Attempts != 2 || work.LastError != tc.class ||
				work.Owner != policy.Owner || work.SLA != policy.SLA || work.NextAction != tc.nextAction || work.Ambiguous != tc.ambiguous ||
				work.NodeID != promotionexec.NodeObservePayroll || work.WorkflowID != f.plan.WorkflowID || work.Verify() != nil {
				t.Fatalf("quarantined work = %+v status %s, want route %s after 2 attempts", work, quarantined.Status, tc.status)
			}
			if calls.Load() != 2 {
				t.Fatalf("payroll ran %d times, want exactly its two-attempt budget", calls.Load())
			}

			inst := instanceByTenant(t, f)
			if inst.RuntimeStatus != tc.status || inst.CompletedAt != nil {
				t.Fatalf("instance = %s completed_at %v, want %s and not completed", inst.RuntimeStatus, inst.CompletedAt, tc.status)
			}
			rows := quarantineRows(t, f)
			if len(rows) != 1 {
				t.Fatalf("durable quarantined work rows = %d, want 1", len(rows))
			}
			row := rows[0]
			if row.route != string(tc.status) || row.attempts != 2 || row.lastError != tc.class || row.owner != policy.Owner ||
				row.nextAction != tc.nextAction || row.repairRoute != tc.repairRoute || row.ambiguous != tc.ambiguous ||
				row.slaNanos != int64(policy.SLA) || row.digest != work.Digest {
				t.Fatalf("durable row = %+v, want the sealed record %+v", row, work)
			}
			if status, _ := nodeAttempt(t, f, inst.InstanceID, 1); status != string(runtime.NodeRetrying) {
				t.Fatalf("payroll attempt 1 = %s, want RETRYING", status)
			}
			if status, class := nodeAttempt(t, f, inst.InstanceID, 2); status != string(runtime.NodeFailed) || class != tc.class {
				t.Fatalf("payroll attempt 2 = %s/%q, want FAILED/%q: the exhausted attempt must not be dropped", status, class, tc.class)
			}
			assertNoPromotionFact(t, f)

			// Running the same start again neither re-runs the poisoned node
			// nor duplicates the record: the instance is no longer RUNNING.
			if _, err := driver.Execute(ctx, execute.ExecuteRequest{Start: f.start}); !errors.Is(err, execute.ErrPoisonWorkQuarantined) {
				t.Fatalf("re-executing a quarantined instance = %v, want ErrPoisonWorkQuarantined before any handler runs", err)
			}
			if calls.Load() != 2 || len(quarantineRows(t, f)) != 1 || instanceByTenant(t, f).RuntimeStatus != tc.status {
				t.Fatalf("re-execution ran payroll %d times, rows %d: a quarantined node retried", calls.Load(), len(quarantineRows(t, f)))
			}
		})
	}
}

// TestTodo_WF_RUN_007_IntegrationWithoutPolicyKeepsRefusal pins the
// pre-quarantine behaviour for drivers composed without the port: the same
// exhausted node is refused NO_FAILURE_ROUTE and writes no quarantine row.
func TestTodo_WF_RUN_007_IntegrationWithoutPolicyKeepsRefusal(t *testing.T) {
	f := newPoisonFixture(t, "wfrun007-no-policy")
	var calls atomic.Int32
	_, err := poisonDriver(t, f, poisonRunner{class: "TIMEOUT", payroll: &calls}, nil).Execute(context.Background(), execute.ExecuteRequest{Start: f.start})
	if frontier.CodeOf(err) != frontier.CodeNoFailureRoute || errors.Is(err, execute.ErrPoisonWorkQuarantined) {
		t.Fatalf("Execute = %v, want the NO_FAILURE_ROUTE refusal", err)
	}
	if n := len(quarantineRows(t, f)); n != 0 {
		t.Fatalf("quarantine rows = %d without a policy, want 0", n)
	}
	if status := instanceByTenant(t, f).RuntimeStatus; status == runtime.InstanceBlocked || status == runtime.InstanceCompleted {
		t.Fatalf("instance = %s without a policy", status)
	}
}

// failingPoisonWorkStore refuses every filing.
type failingPoisonWorkStore struct{}

func (failingPoisonWorkStore) File(context.Context, runtime.Executor, uuid.UUID, uuid.UUID, runtime.QuarantinedWork, time.Time) (runtime.QuarantinedWork, error) {
	return runtime.QuarantinedWork{}, errors.New("quarantine storage unavailable")
}

// TestTodo_WF_RUN_007_IntegrationFault proves the record and the route are
// one transaction: when filing fails nothing commits, the instance is not
// routed, and the caller still sees the original refusal alongside the
// storage failure rather than a success.
func TestTodo_WF_RUN_007_IntegrationFault(t *testing.T) {
	f := newPoisonFixture(t, "wfrun007-fault")
	var calls atomic.Int32
	policy := &execute.PoisonWorkPolicy{Store: failingPoisonWorkStore{}, Owner: "principal:workflow-operations", SLA: time.Hour}
	_, err := poisonDriver(t, f, poisonRunner{class: "TIMEOUT", payroll: &calls}, policy).Execute(context.Background(), execute.ExecuteRequest{Start: f.start})
	if err == nil || errors.Is(err, execute.ErrPoisonWorkQuarantined) || frontier.CodeOf(err) != frontier.CodeNoFailureRoute {
		t.Fatalf("Execute = %v, want the refusal joined with the filing failure", err)
	}
	if n := len(quarantineRows(t, f)); n != 0 {
		t.Fatalf("quarantine rows = %d after a failed filing, want 0", n)
	}
	inst := instanceByTenant(t, f)
	if inst.RuntimeStatus == runtime.InstanceBlocked {
		t.Fatal("instance routed BLOCKED although its record never committed")
	}
	if status, _ := nodeAttempt(t, f, inst.InstanceID, 2); status == string(runtime.NodeFailed) {
		t.Fatal("exhausted attempt marked FAILED although its record never committed")
	}
}

// TestPoisonWorkPolicyConfigurationIsValidated refuses a policy that could not
// file an accountable record.
func TestPoisonWorkPolicyConfigurationIsValidated(t *testing.T) {
	steps := poisonRunner{payroll: &atomic.Int32{}}
	for name, policy := range map[string]*execute.PoisonWorkPolicy{
		"no store":     {Owner: "principal:ops"},
		"no owner":     {Store: runtime.QuarantineStore{}, Owner: " "},
		"negative sla": {Store: runtime.QuarantineStore{}, Owner: "principal:ops", SLA: -time.Second},
	} {
		if _, err := execute.New(execute.Options{DB: noBeginner{}, Steps: steps, PoisonWork: policy}); !errors.Is(err, execute.ErrInvalidConfiguration) {
			t.Errorf("%s: New = %v, want ErrInvalidConfiguration", name, err)
		}
	}
	if _, err := execute.New(execute.Options{DB: noBeginner{}, Steps: steps, PoisonWork: &execute.PoisonWorkPolicy{Store: runtime.QuarantineStore{}, Owner: "principal:ops"}}); err != nil {
		t.Fatalf("valid policy refused: %v", err)
	}
}

// noBeginner is a Beginner New never calls.
type noBeginner struct{}

func (noBeginner) Begin(context.Context) (dbport.Tx, error) { return nil, errors.New("not used") }
