package workflowcontrol

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/intent/operator"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	opsreconcile "github.com/monstercameron/human-capital-management-suite/internal/operations/reconcile"
	operationrepair "github.com/monstercameron/human-capital-management-suite/internal/operations/repair"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/jit"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
)

const (
	repairTenantKey  = values.TenantId("acme")
	repairOperatorID = "operator:repair"
	repairAuthorID   = "analyst:diagnosis"
)

var repairTenantID = uuid.MustParse("6f1a6f68-1f2f-4f5d-9d4a-1f9c0c0a0016")

// repairResolver hands out the operator's current authority for a repair. A
// grant narrowed to the repair plan carries the second approver DATABASE_REPAIR
// demands; a broad grant carries none, exactly like JITAuthority in production.
type repairResolver struct {
	mu         sync.Mutex
	role       jit.Role
	narrowed   bool
	simulation *operator.Simulation
	withhold   bool
	fail       error
	seenTarget string
}

func (r *repairResolver) ResolveAuthority(_ context.Context, tenant values.TenantId, op string, kind operator.Kind, target string) (Authority, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seenTarget = target
	if r.fail != nil {
		return Authority{}, r.fail
	}
	if r.withhold {
		return Authority{}, nil
	}
	role := r.role
	if role == "" {
		role = jit.RoleIntegrityRepair
	}
	fields := []string(nil)
	if r.narrowed {
		fields = []string{RepairApprovalField(target)}
	}
	grant, err := jit.New("jit-"+uuid.NewString(), jit.Request{
		Principal: op, Tenant: tenant, Role: role, TicketRef: "INC-REPAIR-1",
		Justification: "degraded promotion consistency", Capabilities: []string{string(kind)},
		Purpose: "repair plan execution", TTL: time.Hour, Fields: fields,
	}, jit.Approval{Approver: "approver:lead", At: fixedNow}, fixedNow)
	if err != nil {
		return Authority{}, err
	}
	auth := Authority{JIT: grant}
	if r.narrowed {
		auth.SecondApprover = grant.Approver
	}
	auth.Simulation = r.simulation
	return auth, nil
}

type repairEffectSpy struct {
	mu    sync.Mutex
	calls []execute.RepairEffectRequest
	err   error
}

func (s *repairEffectSpy) ExecuteRepairEffect(_ context.Context, req execute.RepairEffectRequest) (execute.RepairEffectResult, error) {
	s.mu.Lock()
	s.calls = append(s.calls, req)
	failure := s.err
	s.mu.Unlock()
	if failure != nil {
		return execute.RepairEffectResult{}, failure
	}
	return execute.RepairEffectResult{EffectKey: req.Step.EffectKey, EffectRef: req.Step.EffectRef, Accepted: true, ResultRef: "payroll:accepted"}, nil
}

func (s *repairEffectSpy) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

type repairObserverStub struct{}

func (repairObserverStub) ObserveRepair(context.Context, operationrepair.RepairPlan, execute.RepairEffectResult) (execute.RepairObservation, error) {
	return execute.RepairObservation{Observed: true, Complete: true, Digest: "sha256:observed", State: "PAYROLL_EXPECTED"}, nil
}

type repairVerifierStub struct {
	decision opsreconcile.CompletionDecision
}

func (v repairVerifierStub) ReconcileRepair(context.Context, execute.RepairReconciliationRequest) (opsreconcile.CompletionDecision, error) {
	return v.decision, nil
}

func repairPlan() operationrepair.RepairPlan {
	return operationrepair.RepairPlan{
		ID: "repair-promotion-1", Digest: "sha256:plan-1", FindingDigest: "sha256:finding-1",
		ObservationDigest: "sha256:observation-1", AuthorityPolicy: "authority.promotion/1",
		MappingVersion: "mapping.payroll/1", CredentialRef: "credential:payroll-1", TargetVersion: "payroll:worker-1@7",
		OriginalSemanticKey: "promotion:worker-1:proposal-1", FailedEffectKey: "effect:payroll-provision",
		Steps: []operationrepair.Step{
			{Ordinal: 1, EffectKey: "effect:payroll-provision", EffectRef: "operation:payroll-1", Target: "payroll:worker-1", ExpectedVersion: "payroll:worker-1@7", MaxAttempts: 2},
		},
	}
}

func repairEvidence() operationrepair.CurrentEvidence {
	plan := repairPlan()
	return operationrepair.CurrentEvidence{
		PlanDigest: plan.Digest, FindingDigest: plan.FindingDigest, ObservationDigest: plan.ObservationDigest,
		AuthorityPolicy: plan.AuthorityPolicy, MappingVersion: plan.MappingVersion, CredentialRef: plan.CredentialRef,
		TargetVersion: plan.TargetVersion,
	}
}

func repairCommand() RepairCommand {
	return RepairCommand{
		TenantID: repairTenantID, Tenant: repairTenantKey, Plan: repairPlan(), Current: repairEvidence(),
		Operator: repairOperatorID, Author: repairAuthorID,
		IdempotencyKey: "repair-key-1", ReasonRef: "INC-REPAIR-1",
	}
}

// newRepairController composes the governed repair door over a shared journal
// and record store, so a test can compose a second one -- a restarted cell --
// over exactly the same durable state.
func newRepairController(t *testing.T, journal operator.Journal, records execute.RepairIdempotencyStore,
	effect execute.RepairEffectPort, decision opsreconcile.CompletionDecision, resolver AuthorityResolver) *RepairController {
	t.Helper()
	executor, err := execute.NewRepairExecutor(execute.RepairExecutionOptions{
		Admission: operationrepair.NewMemoryStore(), Effect: effect,
		Observation: repairObserverStub{}, Reconciliation: repairVerifierStub{decision: decision},
		Records: records,
	})
	if err != nil {
		t.Fatal(err)
	}
	controller, err := NewRepairController(journal, executor, resolver,
		func() time.Time { return fixedNow }, WithRepairPreflightSimulation())
	if err != nil {
		t.Fatal(err)
	}
	return controller
}

func passingRepair() opsreconcile.CompletionDecision {
	return opsreconcile.CompletionDecision{Status: opsreconcile.CompletionPass, Terminal: true, Route: opsreconcile.RouteConsistent, Reason: "observation matches intended and canonical values"}
}

// TestTodo_WF_RUN_016 proves a RepairPlan execution is reachable only through
// the governed operator door: a narrowed JIT grant supplies the second
// approver DATABASE_REPAIR requires, the preflight seals the simulation into
// the receipt, exactly the failed effect is redriven, and the receipt is a
// verifiable journaled record of it.
func TestTodo_WF_RUN_016(t *testing.T) {
	journal := operator.NewMemoryJournal()
	effect := &repairEffectSpy{}
	resolver := &repairResolver{narrowed: true}
	controller := newRepairController(t, journal, execute.NewMemoryRepairRecords(), effect, passingRepair(), resolver)

	result, err := controller.Execute(context.Background(), repairCommand())
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != OutcomeApplied || result.Status != execute.RepairCompleted || !result.Executed || result.ConsistencyState != "CONSISTENT" {
		t.Fatalf("repair result = %+v, want APPLIED/COMPLETED/CONSISTENT", result)
	}
	if result.PlanID != "repair-promotion-1" || result.FailedEffectKey != "effect:payroll-provision" || result.FenceID == "" {
		t.Fatalf("repair identities = %+v", result)
	}
	if effect.count() != 1 {
		t.Fatalf("effect calls = %d, want exactly the failed effect", effect.count())
	}
	if resolver.seenTarget != "repair-promotion-1" {
		t.Fatalf("authority resolved for %q, want the repair plan", resolver.seenTarget)
	}

	receipts := journal.Receipts()
	if len(receipts) != 1 {
		t.Fatalf("journal holds %d receipts, want one", len(receipts))
	}
	receipt := receipts[0]
	if err := receipt.Verify(); err != nil {
		t.Fatalf("receipt digest: %v", err)
	}
	if receipt.Kind != RepairKind || receipt.Outcome != operator.OutcomeApplied {
		t.Fatalf("receipt = %+v, want an applied DATABASE_REPAIR", receipt)
	}
	if receipt.SecondApprover == "" || strings.EqualFold(receipt.SecondApprover, repairOperatorID) {
		t.Fatalf("receipt second approver = %q, want a distinct person", receipt.SecondApprover)
	}
	if receipt.SimulationDigest == "" {
		t.Fatal("receipt records no simulation for a simulation-required kind")
	}
	if receipt.Scope.Resource != "repair_plan" || len(receipt.Scope.IDs) != 1 {
		t.Fatalf("receipt scope = %+v, want exactly the one plan and effect", receipt.Scope)
	}
	if receipt.ExpectedVersion != repairPlan().TargetVersion {
		t.Fatalf("receipt expected version = %q, want the plan's pinned target version", receipt.ExpectedVersion)
	}
}

// TestTodo_WF_RUN_016_Race releases six operators at the same repair under the
// same idempotency key. The gateway admits one, and every other attempt is
// either a duplicate of it or a governed refusal -- the provider is redriven
// once.
func TestTodo_WF_RUN_016_Race(t *testing.T) {
	const attempts = 6
	journal := operator.NewMemoryJournal()
	effect := &repairEffectSpy{}
	controller := newRepairController(t, journal, execute.NewMemoryRepairRecords(), effect, passingRepair(), &repairResolver{narrowed: true})

	var start sync.WaitGroup
	var done sync.WaitGroup
	start.Add(1)
	results := make([]RepairResult, attempts)
	errs := make([]error, attempts)
	for i := 0; i < attempts; i++ {
		done.Add(1)
		go func(i int) {
			defer done.Done()
			start.Wait()
			results[i], errs[i] = controller.Execute(context.Background(), repairCommand())
		}(i)
	}
	start.Done()
	done.Wait()

	applied := 0
	for i, result := range results {
		if errs[i] != nil && operator.CodeOf(errs[i]) == "" {
			t.Fatalf("attempt %d: %v", i, errs[i])
		}
		if result.Outcome == OutcomeApplied {
			applied++
		}
	}
	if applied == 0 {
		t.Fatal("no concurrent operator completed the repair")
	}
	if effect.count() != 1 {
		t.Fatalf("effect calls = %d under %d concurrent operators, want one redrive", effect.count(), attempts)
	}
	if len(journal.Receipts()) != 1 {
		t.Fatalf("journal holds %d receipts for one idempotency key", len(journal.Receipts()))
	}
}

// TestTodo_WF_RUN_016_Fault proves a repair whose corrective effect failed is
// recorded as REPAIR_REQUIRED rather than retried blindly, and that the same
// idempotency key can no longer drive a second redrive.
func TestTodo_WF_RUN_016_Fault(t *testing.T) {
	journal := operator.NewMemoryJournal()
	effect := &repairEffectSpy{err: errors.New("payroll provider unavailable")}
	controller := newRepairController(t, journal, execute.NewMemoryRepairRecords(), effect, passingRepair(), &repairResolver{narrowed: true})

	result, err := controller.Execute(context.Background(), repairCommand())
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != OutcomeRepairRequired || result.Status != execute.RepairFailed {
		t.Fatalf("failed repair = %+v, want REPAIR_REQUIRED", result)
	}
	if result.ConsistencyState == "CONSISTENT" || result.Executed {
		t.Fatalf("failed repair claimed progress: %+v", result)
	}

	// The provider recovers; the same key must not drive the effect again.
	effect.mu.Lock()
	effect.err = nil
	effect.mu.Unlock()
	replay, err := controller.Execute(context.Background(), repairCommand())
	if operator.CodeOf(err) != operator.CodeRepairRequired && replay.Outcome != OutcomeRepairRequired {
		t.Fatalf("replay after a failed repair = %+v, %v, want a repair-required refusal", replay, err)
	}
	if effect.count() != 1 {
		t.Fatalf("effect calls = %d, want no blind retry", effect.count())
	}
}

// TestTodo_WF_RUN_016_Mutation kills the mutations that would make this door
// decorative: authority that is absent, broad (no second approver), of the
// wrong role, or an idempotency key rebound to a different plan must each
// refuse the repair before the provider is touched.
func TestTodo_WF_RUN_016_Mutation(t *testing.T) {
	cases := map[string]struct {
		resolver *repairResolver
		wantCode string
	}{
		"no current grant":   {resolver: &repairResolver{withhold: true}, wantCode: operator.CodeAuthorityRequired},
		"broad grant only":   {resolver: &repairResolver{}, wantCode: operator.CodeDualControlRequired},
		"wrong repair role":  {resolver: &repairResolver{narrowed: true, role: jit.RoleSupportReadOnly}, wantCode: operator.CodeAuthorityMismatch},
		"unrelated approver": {resolver: &repairResolver{}, wantCode: operator.CodeDualControlRequired},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			effect := &repairEffectSpy{}
			controller := newRepairController(t, operator.NewMemoryJournal(), execute.NewMemoryRepairRecords(), effect, passingRepair(), tc.resolver)
			result, err := controller.Execute(context.Background(), repairCommand())
			if err != nil {
				t.Fatal(err)
			}
			if result.Outcome != OutcomeDenied || result.Code != tc.wantCode {
				t.Fatalf("result = %+v, want DENIED/%s", result, tc.wantCode)
			}
			if effect.count() != 0 {
				t.Fatalf("a denied repair redrove the effect %d times", effect.count())
			}
		})
	}

	// One key, two different plans: the second is a conflict, not a replay.
	journal := operator.NewMemoryJournal()
	effect := &repairEffectSpy{}
	controller := newRepairController(t, journal, execute.NewMemoryRepairRecords(), effect, passingRepair(), &repairResolver{narrowed: true})
	if _, err := controller.Execute(context.Background(), repairCommand()); err != nil {
		t.Fatal(err)
	}
	rebound := repairCommand()
	rebound.Plan.Digest = "sha256:plan-2"
	rebound.Current.PlanDigest = "sha256:plan-2"
	result, err := controller.Execute(context.Background(), rebound)
	if err != nil {
		t.Fatal(err)
	}
	if result.Outcome != OutcomeDenied || result.Code != operator.CodeIdempotencyConflict {
		t.Fatalf("rebound key = %+v, want an idempotency conflict", result)
	}
	if effect.count() != 1 {
		t.Fatalf("effect calls = %d after a rebound key", effect.count())
	}
}

// TestTodo_WF_RUN_016_SeparationOfDuties proves the author of an approved plan
// cannot execute it through this door, and that no effect or durable claim is
// made when they try.
func TestTodo_WF_RUN_016_SeparationOfDuties(t *testing.T) {
	plan := repairPlan()
	plan.RequiresApproval = true
	plan.ApprovalDigest = "approval:repair-1"
	current := repairEvidence()
	current.ApprovalValid = true
	current.ApprovalDigest = plan.ApprovalDigest

	effect := &repairEffectSpy{}
	controller := newRepairController(t, operator.NewMemoryJournal(), execute.NewMemoryRepairRecords(), effect, passingRepair(), &repairResolver{narrowed: true})
	cmd := repairCommand()
	cmd.Plan, cmd.Current = plan, current
	cmd.Author = cmd.Operator

	result, err := controller.Execute(context.Background(), cmd)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != execute.RepairSeparationRequired || result.Outcome != OutcomeDenied || result.Executed {
		t.Fatalf("self-executed repair = %+v, want a separation-of-duties denial", result)
	}
	if effect.count() != 0 {
		t.Fatalf("a self-executed repair redrove the effect %d times", effect.count())
	}
}

// TestRepairSimulationIsPureAndScoped proves the preflight predicts without
// touching the provider and seals a digest over exactly the command's scope,
// so the gateway's scope check cannot be satisfied by a simulation of
// something else.
func TestRepairSimulationIsPureAndScoped(t *testing.T) {
	effect := &repairEffectSpy{}
	controller := newRepairController(t, operator.NewMemoryJournal(), execute.NewMemoryRepairRecords(), effect, passingRepair(), &repairResolver{narrowed: true})
	cmd := repairCommand()

	simulation, status, err := controller.Simulate(context.Background(), cmd)
	if err != nil {
		t.Fatal(err)
	}
	if status != execute.RepairStatus(operationrepair.StatusReady) {
		t.Fatalf("simulated status = %s, want READY", status)
	}
	want := cmd.Scope()
	if simulation == nil || simulation.Digest == "" || simulation.Scope.Resource != want.Resource ||
		len(simulation.Scope.IDs) != 1 || simulation.Scope.IDs[0] != want.IDs[0] {
		t.Fatalf("simulation = %+v, want a sealed digest over exactly %+v", simulation, want)
	}
	if effect.count() != 0 {
		t.Fatalf("a simulation redrove the effect %d times", effect.count())
	}

	stale := cmd
	stale.Current.FindingDigest = "sha256:a-newer-finding"
	_, staleStatus, err := controller.Simulate(context.Background(), stale)
	if err != nil {
		t.Fatal(err)
	}
	if staleStatus != execute.RepairStatus(operationrepair.StatusReplanRequired) {
		t.Fatalf("stale finding simulated as %s, want REPLAN_REQUIRED", staleStatus)
	}
}

// TestRepairCommandValidationRefusesIncompleteCommands names every
// precondition, so a repair that could not identify itself never reaches the
// gateway.
func TestRepairCommandValidationRefusesIncompleteCommands(t *testing.T) {
	effect := &repairEffectSpy{}
	controller := newRepairController(t, operator.NewMemoryJournal(), execute.NewMemoryRepairRecords(), effect, passingRepair(), &repairResolver{narrowed: true})
	for name, mutate := range map[string]func(RepairCommand) RepairCommand{
		"no tenant id":       func(c RepairCommand) RepairCommand { c.TenantID = uuid.Nil; return c },
		"no tenant key":      func(c RepairCommand) RepairCommand { c.Tenant = ""; return c },
		"no idempotency key": func(c RepairCommand) RepairCommand { c.IdempotencyKey = " "; return c },
		"no reason":          func(c RepairCommand) RepairCommand { c.ReasonRef = ""; return c },
		"no operator":        func(c RepairCommand) RepairCommand { c.Operator = ""; return c },
		"invalid plan":       func(c RepairCommand) RepairCommand { c.Plan.FailedEffectKey = "effect:absent"; return c },
	} {
		if _, err := controller.Execute(context.Background(), mutate(repairCommand())); !errors.Is(err, ErrInvalidCommand) {
			t.Fatalf("%s: err = %v, want ErrInvalidCommand", name, err)
		}
	}
	if effect.count() != 0 {
		t.Fatalf("an invalid command redrove the effect %d times", effect.count())
	}
}

// TestNewRepairControllerRequiresItsCollaborators pins the composition
// preconditions.
func TestNewRepairControllerRequiresItsCollaborators(t *testing.T) {
	executor, err := execute.NewRepairExecutor(execute.RepairExecutionOptions{
		Admission: operationrepair.NewMemoryStore(), Effect: &repairEffectSpy{},
		Observation: repairObserverStub{}, Reconciliation: repairVerifierStub{decision: passingRepair()},
		Records: execute.NewMemoryRepairRecords(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewRepairController(operator.NewMemoryJournal(), nil, &repairResolver{}, nil); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("missing executor = %v", err)
	}
	if _, err := NewRepairController(operator.NewMemoryJournal(), executor, nil, nil); !errors.Is(err, ErrInvalidCommand) {
		t.Fatalf("missing authority resolver = %v", err)
	}
	if _, err := NewRepairController(nil, executor, &repairResolver{}, nil); err == nil {
		t.Fatal("a repair controller was composed without a journal")
	}
	// A nil clock defaults rather than panicking.
	if _, err := NewRepairController(operator.NewMemoryJournal(), executor, &repairResolver{}, nil, nil); err != nil {
		t.Fatalf("default clock composition: %v", err)
	}
}

// TestDecodeRepairEffectRefusesAnUnreadableReceipt proves a receipt whose
// effect reference cannot be read is reported, never guessed at.
func TestDecodeRepairEffectRefusesAnUnreadableReceipt(t *testing.T) {
	for _, ref := range []string{"", "nonsense", "workflowcontrol/repair/v1|a|b", repairEffectPrefix + "|COMPLETED|p|d|e|f|notabool|CONSISTENT"} {
		if _, err := decodeRepairEffect(ref); err == nil {
			t.Fatalf("decoded %q", ref)
		}
	}
	res, err := decodeRepairEffect(encodeRepairEffect(repairCommand(), execute.RepairExecutionResult{
		Status: execute.RepairReconciliationWait, PlanDigest: "sha256:plan-1",
		FailedEffectKey: "effect:payroll-provision", Executed: true, ConsistencyState: "DEGRADED",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != OutcomeDenied || res.Status != execute.RepairReconciliationWait || !res.Executed {
		t.Fatalf("decoded = %+v, want an unclosed repair reported as DENIED", res)
	}
}

// TestApprovalFieldIsKindSpecific proves a repair's dual-control grant is
// narrowed to its plan and a workflow control's to its instance, so a grant
// for one can never satisfy the other.
func TestApprovalFieldIsKindSpecific(t *testing.T) {
	if approvalField(RepairKind, "repair-1") != RepairApprovalField("repair-1") {
		t.Fatalf("repair approval field = %q", approvalField(RepairKind, "repair-1"))
	}
	if approvalField(operator.KindWorkflowCancel, "inst-1") != InstanceApprovalField("inst-1") {
		t.Fatalf("control approval field = %q", approvalField(operator.KindWorkflowCancel, "inst-1"))
	}
	if RepairApprovalField("x") == InstanceApprovalField("x") {
		t.Fatal("a repair grant and an instance grant share a field name")
	}
}
