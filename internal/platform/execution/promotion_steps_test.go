package execution

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	domaincommit "github.com/monstercameron/human-capital-management-suite/internal/domains/promotion/commit"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/wire/digest"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/execution/promotionsteps"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// scriptTx is a tenant transaction whose reads are scripted by SQL fragment.
// An unscripted read answers no rows.
type scriptTx struct {
	rows    map[string][]any
	queries map[string][][]any
	execs   int
}

type scriptRow struct {
	vals []any
	err  error
}

func (r scriptRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != len(r.vals) {
		return fmt.Errorf("scan: %d destinations for %d values", len(dest), len(r.vals))
	}
	for i := range dest {
		reflect.ValueOf(dest[i]).Elem().Set(reflect.ValueOf(r.vals[i]))
	}
	return nil
}

type scriptRows struct {
	rows [][]any
	at   int
}

func (r *scriptRows) Next() bool             { r.at++; return r.at <= len(r.rows) }
func (r *scriptRows) Scan(dest ...any) error { return scriptRow{vals: r.rows[r.at-1]}.Scan(dest...) }
func (r *scriptRows) Err() error             { return nil }
func (r *scriptRows) Close()                 {}

func (t *scriptTx) Exec(context.Context, string, ...any) (int64, error) { t.execs++; return 1, nil }
func (t *scriptTx) Query(_ context.Context, sql string, _ ...any) (dbport.Rows, error) {
	for fragment, rows := range t.queries {
		if strings.Contains(sql, fragment) {
			return &scriptRows{rows: rows}, nil
		}
	}
	return &scriptRows{}, nil
}
func (t *scriptTx) QueryRow(_ context.Context, sql string, _ ...any) dbport.Row {
	for fragment, vals := range t.rows {
		if strings.Contains(sql, fragment) {
			return scriptRow{vals: vals}
		}
	}
	return scriptRow{err: dbport.ErrNoRows}
}
func (t *scriptTx) Commit(context.Context) error   { return nil }
func (t *scriptTx) Rollback(context.Context) error { return nil }

type scriptDB struct{ tx *scriptTx }

func (d scriptDB) Begin(context.Context) (dbport.Tx, error) { return d.tx, nil }

var (
	wfrun034Tenant   = uuid.MustParse("6c79cadf-0b73-5a49-8044-5c9d91cf81bc")
	wfrun034Intent   = uuid.MustParse("7d1a1f7e-4f7b-4f2b-9d8a-1c2e3f4a5b6c")
	wfrun034Proposal = uuid.MustParse("8e2b2a8f-5a8c-4a3c-8e9b-2d3f4a5b6c7d")
)

func delegationRow() []any {
	return []any{"hc-050-rafael-torres", "human", "harborcare", "org:harborcare:people-ops",
		[]string{"promotion_operator"}, []string{"compensation_review"}, "bearer_token", "substantial",
		"session-1", "ev:authn:abc", time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)}
}

// fakeStepServices records every governed call and answers from its script.
type fakeStepServices struct {
	calls         []app.PromotionStepCall
	reads         []string
	err           error
	threshold     rules.PromotionApprovalInput
	standingCalls int
}

func (f *fakeStepServices) answer(call app.PromotionStepCall, name string) (app.PromotionStepAnswer, error) {
	f.calls = append(f.calls, call)
	return app.PromotionStepAnswer{Digest: "sha256:" + name, EvidenceIDs: []string{"ev:" + name}, Subject: call.Delegation.Subject}, f.err
}
func (f *fakeStepServices) SnapshotWorker(_ context.Context, c app.PromotionStepCall) (app.PromotionStepAnswer, error) {
	return f.answer(c, "snapshot")
}
func (f *fakeStepServices) SimulateCompensation(_ context.Context, c app.PromotionStepCall) (app.PromotionStepAnswer, error) {
	return f.answer(c, "simulate")
}
func (f *fakeStepServices) EvaluateBand(_ context.Context, c app.PromotionStepCall) (app.PromotionStepAnswer, error) {
	return f.answer(c, "band")
}
func (f *fakeStepServices) ThresholdInputs(_ context.Context, c app.PromotionStepCall) (rules.PromotionApprovalInput, app.PromotionStepAnswer, error) {
	a, err := f.answer(c, "threshold")
	return f.threshold, a, err
}
func (f *fakeStepServices) AuthorizeCommit(_ context.Context, c app.PromotionStepCall) (app.PromotionStepAnswer, error) {
	return f.answer(c, "commit")
}
func (f *fakeStepServices) GovernanceStanding(_ context.Context, c app.PromotionStepCall) (app.GovernanceStanding, error) {
	f.standingCalls++
	if f.err != nil {
		return app.GovernanceStanding{}, f.err
	}
	return app.GovernanceStanding{
		Authorized: true, Subject: c.Delegation.Subject, SessionRef: c.Delegation.SessionRef,
		Assurance: c.Delegation.Assurance, RequiredRole: "promotion_operator", Purpose: "compensation_review",
		PolicyBundleDigest: "sha256:policy", LegalContextDigest: "sha256:legal", ClassificationDigest: "sha256:classification",
		CapabilityDigest: "sha256:capability", ControlDigest: "sha256:control", SourceAuthorityDigest: "sha256:source",
		RiskClass: "R3",
	}, nil
}

func (f *fakeStepServices) GovernedRead(ctx context.Context, c app.PromotionStepCall, id string, read func(context.Context) (any, error)) (any, app.PromotionStepAnswer, error) {
	f.reads = append(f.reads, id)
	a, err := f.answer(c, id)
	if err != nil {
		return nil, a, err
	}
	got, err := read(ctx)
	return got, a, err
}

func wfrun034Request(t *testing.T, nodeID string) execute.StepRequest {
	t.Helper()
	plan, err := promotionexec.Compile()
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	node, ok := plan.Node(nodeID)
	if !ok {
		t.Fatalf("plan has no %s", nodeID)
	}
	return execute.StepRequest{
		TenantID: wfrun034Tenant, InstanceID: uuid.MustParse("22222222-2222-2222-2222-222222222222"), Attempt: 1,
		Node: node, Plan: plan, CorrelationID: "corr",
		Proposal: runtime.ProposalBinding{Revision: intent.ProposalRevision{
			IntentID: wfrun034Intent.String(), ProposalRevisionID: wfrun034Proposal.String(), Revision: 1,
			MaterialDigest: digest.Reference{Digest: "sha256:material"},
		}},
		RecordedAt: time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC),
	}
}

func composedRunner(tx *scriptTx, services PromotionStepServices) promotionStepRunner {
	ports := &promotionStepPorts{db: scriptDB{tx: tx}, cellID: "cell-test", authorityDigest: "sha256:authority"}
	if services != nil {
		ports.services = services
	}
	return promotionStepRunner{plan: PLAN_EXECUTE, ports: ports}
}

// TestTodo_WF_RUN_034 proves the served runner composes promotionsteps for
// the governed nodes: each capability node invokes the step services with the
// pinned delegation, a node-attempt idempotency key, the step deadline and the
// node's declared effect set, and routes the real answers, including the real
// threshold decision.
func TestTodo_WF_RUN_034(t *testing.T) {
	tx := &scriptTx{rows: map[string][]any{"FROM workflow_execution_delegation": delegationRow()}}
	services := &fakeStepServices{threshold: rules.PromotionApprovalInput{
		IncreasePercent: values.MustDecimal("8.8889", 4, values.RoundingHalfEven), BandPosition: rules.BandPositionInBand,
		BudgetAuthority: rules.BudgetAuthoritySufficient, GradeChange: true,
	}}
	runner := composedRunner(tx, services)
	ctx := context.Background()

	for _, nodeID := range []string{promotionexec.NodeSnapshotWorker, promotionexec.NodeSimulateCompensation, promotionexec.NodeEvaluateBand} {
		req := wfrun034Request(t, nodeID)
		out, refs, err := runner.Run(ctx, req)
		if err != nil || out.Failed || out.Outcome != workflow.OutcomeSucceeded || !strings.HasPrefix(out.OutputDigest, "sha256:") {
			t.Fatalf("%s = %+v, %v", nodeID, out, err)
		}
		if refs.AuthorizationDecisionID != "hc-050-rafael-torres" || len(refs.EffectRefs) != 1 || refs.ProposalRef != "sha256:material" {
			t.Fatalf("%s refs = %+v, want the delegated subject and the gateway evidence", nodeID, refs)
		}
	}
	call := services.calls[0]
	if call.IntentID != wfrun034Intent.String() || call.Delegation.Subject != "hc-050-rafael-torres" ||
		call.IdempotencyKey != "workflow:"+wfrun034Tenant.String()+":22222222-2222-2222-2222-222222222222:snapshot_worker:1" ||
		!call.Deadline.Equal(time.Date(2026, 10, 16, 12, 1, 0, 0, time.UTC)) ||
		!slices.Equal(call.DeclaredEffects, []capability.EffectClass{capability.EffectPure, capability.EffectReadOnly}) {
		t.Fatalf("snapshot call = %+v", call)
	}

	out, _, err := runner.Run(ctx, wfrun034Request(t, promotionexec.NodeRaiseThreshold))
	if err != nil || out.Outcome != "ABOVE_THRESHOLD" {
		t.Fatalf("raise_threshold = %+v, %v; want the real ABOVE_THRESHOLD decision", out, err)
	}
	services.threshold.GradeChange = false
	services.threshold.IncreasePercent = values.MustDecimal("3.0000", 4, values.RoundingHalfEven)
	if out, _, err := runner.Run(ctx, wfrun034Request(t, promotionexec.NodeRaiseThreshold)); err != nil || out.Outcome != "WITHIN_THRESHOLD" {
		t.Fatalf("raise_threshold on a small raise = %+v, %v; want WITHIN_THRESHOLD", out, err)
	}
}

// TestTodo_WF_RUN_034_Security proves every governed node fails closed when
// the step services are unbound, the instance pinned no delegation, or the
// services refuse the delegation; a failure never reads as success.
func TestTodo_WF_RUN_034_Security(t *testing.T) {
	ctx := context.Background()
	nodes := []string{promotionexec.NodeSnapshotWorker, promotionexec.NodeSimulateCompensation, promotionexec.NodeEvaluateBand,
		promotionexec.NodeRaiseThreshold, promotionexec.NodeRevalidate, promotionexec.NodeStillValid,
		promotionexec.NodeObservePayroll, promotionexec.NodeObserveAccess, promotionexec.NodeObserveReconciliation}
	cases := []struct {
		name     string
		tx       *scriptTx
		services PromotionStepServices
		class    string
	}{
		{"unbound", &scriptTx{}, nil, promotionsteps.FailureNotWired},
		{"no delegation", &scriptTx{}, &fakeStepServices{}, promotionsteps.FailurePort},
		{"revoked", &scriptTx{rows: map[string][]any{"FROM workflow_execution_delegation": delegationRow()}}, &fakeStepServices{err: app.ErrDelegationRevoked}, promotionsteps.FailurePort},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := composedRunner(tc.tx, tc.services)
			for _, nodeID := range nodes {
				out, _, err := runner.Run(ctx, wfrun034Request(t, nodeID))
				if err != nil || !out.Failed || out.ErrorClass != tc.class || out.Outcome != "" {
					t.Fatalf("%s = %+v, %v; want failed %s", nodeID, out, err, tc.class)
				}
			}
			out, _, err := runner.RunInTx(ctx, tc.tx, wfrun034Request(t, promotionexec.NodeExecutePromotion))
			if err != nil || !out.Failed {
				t.Fatalf("execute_promotion = %+v, %v; want a failed outcome", out, err)
			}
		})
	}
}

func TestTodo_REV_057_01_TransactionalStepRequiresRecordedAt(t *testing.T) {
	runner := promotionStepRunner{plan: PLAN_EXECUTE, ports: &promotionStepPorts{}}
	_, _, err := runner.RunInTx(context.Background(), nil, execute.StepRequest{
		Node: workflow.CompiledNode{ID: promotionexec.NodeRaiseThreshold},
	})
	if err == nil || !strings.Contains(err.Error(), "transactional step raise_threshold has no recorded_at instant") {
		t.Fatalf("RunInTx without RecordedAt = %v, want a fail-closed timestamp diagnostic", err)
	}
}

// TestTodo_WF_RUN_034_Fault proves the commit runs only inside the advance
// transaction, a refused commit authorization or an unresolvable command
// fails the node (routing its compiled failure route), and an observation
// or revalidation that cannot see what it needs routes a real FAIL, BLOCKED
// or DEGRADED outcome instead of PASS.
func TestTodo_WF_RUN_034_Fault(t *testing.T) {
	ctx := context.Background()
	delegated := func() *scriptTx {
		return &scriptTx{rows: map[string][]any{"FROM workflow_execution_delegation": delegationRow()}}
	}

	runner := composedRunner(delegated(), &fakeStepServices{})
	if out, _, err := runner.Run(ctx, wfrun034Request(t, promotionexec.NodeExecutePromotion)); err != nil || !out.Failed || !strings.HasPrefix(out.ErrorClass, promotionsteps.FailurePort) {
		t.Fatalf("execute_promotion outside the transaction = %+v, %v; want a port failure", out, err)
	}
	if _, _, err := runner.RunInTx(ctx, notATx{}, wfrun034Request(t, promotionexec.NodeExecutePromotion)); err == nil {
		t.Fatal("RunInTx accepted an executor that is not a transaction")
	}
	commitTx := delegated()
	services := &fakeStepServices{}
	if out, _, err := composedRunner(commitTx, services).RunInTx(ctx, commitTx, wfrun034Request(t, promotionexec.NodeExecutePromotion)); err != nil || !out.Failed {
		t.Fatalf("execute_promotion with no approved revision = %+v, %v; want a failure", out, err)
	}
	if len(services.calls) != 1 || !slices.Contains(services.calls[0].DeclaredEffects, capability.EffectInternalMutation) {
		t.Fatalf("commit authorization calls = %+v, want one with the node's mutation effect", services.calls)
	}

	for _, nodeID := range []string{promotionexec.NodeObservePayroll, promotionexec.NodeObserveAccess} {
		out, _, err := runner.Run(ctx, wfrun034Request(t, nodeID))
		if err != nil || out.Failed || out.Outcome != workflow.OutcomeFail {
			t.Fatalf("%s with no committed promotion = %+v, %v; want FAIL", nodeID, out, err)
		}
	}
	if out, _, err := runner.Run(ctx, wfrun034Request(t, promotionexec.NodeObserveReconciliation)); err != nil || out.Outcome != workflow.OutcomePartial {
		t.Fatalf("observe_reconciliation with no committed promotion = %+v, %v; want the DEGRADED (PARTIAL) route", out, err)
	}

	// An instance with no durable GOVERN-002 record has nothing to confirm
	// against and blocks, naming the absence rather than guessing.
	services = &fakeStepServices{}
	blocked := composedRunner(delegated(), services)
	result, err := blocked.ports.evaluateRevalidation(ctx, wfrun034Request(t, promotionexec.NodeRevalidate),
		app.GovernanceStanding{Authorized: true})
	if err != nil || result.confirmed || result.requirement != "BLOCK" || !slices.Contains(result.sourced, "governance.record=absent") {
		t.Fatalf("revalidation with no approval record = %+v, %v; want BLOCK naming the absent record", result, err)
	}
	if out, _, err := blocked.Run(ctx, wfrun034Request(t, promotionexec.NodeRevalidate)); err != nil || out.Outcome != workflow.OutcomeSucceeded {
		t.Fatalf("revalidate = %+v, %v", out, err)
	}
	if out, _, err := blocked.Run(ctx, wfrun034Request(t, promotionexec.NodeStillValid)); err != nil || out.Outcome != "BLOCKED" {
		t.Fatalf("still_valid with no approval record = %+v, %v; want BLOCKED", out, err)
	}
	if services.standingCalls == 0 {
		t.Fatal("revalidation never asked the cell for the delegation's current standing")
	}
}

type notATx struct{}

func (notATx) Exec(context.Context, string, ...any) (int64, error) { return 0, nil }
func (notATx) Query(context.Context, string, ...any) (dbport.Rows, error) {
	return nil, errors.New("not a transaction")
}
func (notATx) QueryRow(context.Context, string, ...any) dbport.Row {
	return scriptRow{err: dbport.ErrNoRows}
}

// TestTodo_WF_RUN_034_Mutation pins the observation and reconciliation rules
// against a resolved command: each observation holds only when both its
// projection and its outbox leg agree, and reconciliation is CONSISTENT only
// when all three observations hold.
func TestTodo_WF_RUN_034_Mutation(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 10, 16, 0, 0, 0, 0, time.UTC)
	pay, err := values.NewMoney("98000.00", "USD", 2, values.RoundingExactRequired)
	if err != nil {
		t.Fatal(err)
	}
	worker := uuid.New()
	cmd := domaincommit.Command{
		TenantID: wfrun034Tenant.String(), ProposalRevisionID: wfrun034Proposal.String(), WorkerID: worker.String(),
		AssignmentID: uuid.NewString(), BasePayComponentID: uuid.NewString(), PositionOccupancyID: uuid.NewString(),
		TargetJobCode: "OPS-HRBP3", TargetGrade: "P3", BasePay: pay, EffectiveAt: at,
	}
	envelope := func() []any { // Envelope columns before the typed ones.
		return []any{uuid.New(), wfrun034Tenant, uuid.New(), "canonical"}
	}
	tail := func() []any {
		return []any{at, (*time.Time)(nil), at, (*time.Time)(nil), "sha256", "digest"}
	}
	component := func(amount string) []any {
		return append(append(envelope(), uuid.New(), "BASE_PAY", amount, "USD", "ANNUAL"), tail()...)
	}
	text := func(s string) *string { return &s }
	assignment := func(job, grade string) []any {
		return append(append(envelope(), uuid.New(), true, job, text(grade), (*uuid.UUID)(nil), (*uuid.UUID)(nil), text("Boston"), text("US-EAST"), "1.0000", text("hc-050")), tail()...)
	}
	occupancy := func(w uuid.UUID) []any {
		return append(append(envelope(), uuid.New(), (*uuid.UUID)(nil), &w, "1.0000", true), tail()...)
	}

	for _, tc := range []struct {
		name  string
		rows  map[string][]any
		check func(context.Context, dbport.Tx, domaincommit.Command) (observation, error)
		want  string
	}{
		{"payroll observed", map[string][]any{"FROM compensation_component": component("98000.0000"), "FROM outbox": {1}}, payrollObservation, promotionsteps.ObservationObserved},
		{"payroll pay not moved", map[string][]any{"FROM compensation_component": component("90000.0000"), "FROM outbox": {1}}, payrollObservation, promotionsteps.ObservationFailed},
		{"payroll leg missing", map[string][]any{"FROM compensation_component": component("98000.0000"), "FROM outbox": {0}}, payrollObservation, promotionsteps.ObservationFailed},
		{"access observed", map[string][]any{"FROM assignment": assignment("OPS-HRBP3", "P3"), "FROM outbox": {1}}, accessObservation, promotionsteps.ObservationObserved},
		{"access not placed", map[string][]any{"FROM assignment": assignment("OPS-HRBP2", "P2"), "FROM outbox": {1}}, accessObservation, promotionsteps.ObservationFailed},
		{"occupancy observed", map[string][]any{"FROM position_occupancy": occupancy(worker)}, occupancyObservation, promotionsteps.ObservationObserved},
		{"occupancy names another worker", map[string][]any{"FROM position_occupancy": occupancy(uuid.New())}, occupancyObservation, promotionsteps.ObservationFailed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.check(ctx, &scriptTx{rows: tc.rows}, cmd)
			if err != nil || got.status != tc.want {
				t.Fatalf("observation = %+v, %v; want %s", got, err, tc.want)
			}
		})
	}
	bad := cmd
	bad.TenantID = "not-a-uuid"
	for _, check := range []func(context.Context, dbport.Tx, domaincommit.Command) (observation, error){payrollObservation, accessObservation, occupancyObservation} {
		if got, err := check(ctx, &scriptTx{}, bad); err != nil || got.status != promotionsteps.ObservationFailed {
			t.Fatalf("malformed command observation = %+v, %v; want FAIL", got, err)
		}
	}

	observed := observation{status: promotionsteps.ObservationObserved}
	failed := observation{status: promotionsteps.ObservationFailed}
	if got := reconcile(observed, observed, observed); got.status != promotionsteps.ReconciliationConsistent {
		t.Fatalf("all observed = %s, want CONSISTENT", got.status)
	}
	for _, trio := range [][3]observation{{failed, observed, observed}, {observed, failed, observed}, {observed, observed, failed}} {
		if got := reconcile(trio[0], trio[1], trio[2]); got.status != promotionsteps.ReconciliationDegraded {
			t.Fatalf("reconcile %+v = %s, want DEGRADED", trio, got.status)
		}
	}
}

func TestPromotionStepPortsComposition(t *testing.T) {
	for _, tc := range []struct {
		node workflow.CompiledNode
		want []capability.EffectClass
	}{
		{workflow.CompiledNode{Type: workflow.StepCapability, EffectClass: capability.EffectReadOnly}, []capability.EffectClass{capability.EffectPure, capability.EffectReadOnly}},
		{workflow.CompiledNode{Type: workflow.StepDecision, EffectClass: capability.EffectPure}, []capability.EffectClass{capability.EffectPure, capability.EffectReadOnly}},
		{workflow.CompiledNode{Type: workflow.StepCapability, EffectClass: capability.EffectInternalMutation}, []capability.EffectClass{capability.EffectPure, capability.EffectReadOnly, capability.EffectInternalMutation}},
		{workflow.CompiledNode{Type: workflow.StepEnd}, []capability.EffectClass{capability.EffectPure}},
	} {
		if got := declaredEffects(tc.node); !slices.Equal(got, tc.want) {
			t.Errorf("declaredEffects(%+v) = %v, want %v", tc.node, got, tc.want)
		}
	}

	execution, err := NewPromotionExecution(PromotionExecutionConfig{DB: stubBeginner{}, Terminal: stubTerminal{}, Plan: PLAN_EXECUTE})
	if err != nil {
		t.Fatalf("NewPromotionExecution: %v", err)
	}
	if err := execution.BindStepServices(nil); err == nil {
		t.Fatal("BindStepServices accepted nil services")
	}
	if err := execution.BindStepServices(&fakeStepServices{}); err != nil {
		t.Fatalf("BindStepServices: %v", err)
	}
	if err := execution.BindStepServices(&fakeStepServices{}); !errors.Is(err, ErrStepServicesBound) {
		t.Fatalf("second BindStepServices = %v, want ErrStepServicesBound", err)
	}
	if err := (&PromotionExecution{}).BindStepServices(&fakeStepServices{}); err == nil {
		t.Fatal("a composition with no step ports accepted services")
	}

	runner := promotionStepRunner{plan: PLAN_EXECUTE}
	prototypeRunner := promotionStepRunner{plan: PLAN_PROTOTYPE}
	// WF-EXT-002: the transactional claim derives from the compiled
	// EffectRole, so the fixtures carry the roles the compiler assigns
	// (execute_promotion is AUTHORITATIVE_CORE, compensate_budget_hold is
	// DOWNSTREAM_EFFECT); a bare node id claims nothing.
	execNode := workflow.CompiledNode{ID: promotionexec.NodeExecutePromotion, EffectRole: workflow.RoleAuthoritativeCore}
	compensateNode := workflow.CompiledNode{ID: promotionexec.NodeCompensateHold, EffectRole: workflow.RoleDownstreamEffect}
	if !runner.RunsInTransaction(execNode) || !runner.RunsInTransaction(compensateNode) ||
		runner.RunsInTransaction(workflow.CompiledNode{ID: promotionexec.NodeSnapshotWorker}) ||
		prototypeRunner.RunsInTransaction(execNode) || prototypeRunner.RunsInTransaction(compensateNode) {
		t.Fatal("the executable plan's execute_promotion and compensate_budget_hold run in the advance transaction, nothing else does")
	}
	if _, _, err := (promotionStepRunner{plan: PLAN_EXECUTE, ports: &promotionStepPorts{}}).RunInTx(context.Background(), &scriptTx{}, execute.StepRequest{}); err == nil {
		t.Fatal("RunInTx accepted a request naming no node")
	}
	if err := (&promotionStepPorts{}).inTenantRead(context.Background(), wfrun034Tenant, func(dbport.Tx) error { return nil }); err == nil {
		t.Fatal("a port with no database read")
	}
	if digestOf("a", "b") == digestOf("ab") {
		t.Fatal("digest parts are not delimited")
	}
}

// TestNoFabricatedPromotionOutcomeRemains is the grep-level guard: the stub
// that returned SUCCEEDED/VALID/ABOVE_THRESHOLD/PASS without evaluating a node
// is gone from the served composition.
func TestNoFabricatedPromotionOutcomeRemains(t *testing.T) {
	for _, file := range []string{"execution.go", "promotion_steps.go"} {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		body := string(raw)
		for _, forbidden := range []string{"runExecuteStep", `success("SUCCEEDED")`, `success("PASS")`, `route = "ABOVE_THRESHOLD"`} {
			if strings.Contains(body, forbidden) {
				t.Errorf("%s still contains the fabricated outcome %q", file, forbidden)
			}
		}
	}
}
