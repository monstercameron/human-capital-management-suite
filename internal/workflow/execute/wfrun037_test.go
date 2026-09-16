package execute_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/outbox"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	datalogger "github.com/monstercameron/human-capital-management-suite/internal/ledger"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/effects"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// nodeLeg is the dependent leg WF-RUN-037's fault fixture inserts between the
// promotion core commit and the payroll observation.
const nodeLeg = "dispatch_payroll_leg"

const legSchemaRef = "hcmnext.test.wfrun037/v1"

// legDefinition inserts one INTERNAL_MUTATION leg of role after the promotion
// core: execute_promotion --SUCCEEDED--> leg --SUCCEEDED--> observe_payroll,
// the leg failing to the existing repair terminal.
func legDefinition(t *testing.T, role workflow.EffectRole) workflow.Definition {
	t.Helper()
	def := promotionexec.Definition()
	var core workflow.Node
	for _, n := range def.Nodes {
		if n.ID == promotionexec.NodeExecutePromotion {
			core = n
		}
	}
	if core.EffectRole != workflow.RoleAuthoritativeCore {
		t.Fatalf("promotion core role = %q, want AUTHORITATIVE_CORE", core.EffectRole)
	}
	capRef := *core.Capability
	capRef.EffectBinding = "promotion.payroll_outbox_leg"
	leg := workflow.Node{
		ID: nodeLeg, Type: workflow.StepCapability, InputSchema: core.InputSchema, OutputSchema: core.OutputSchema,
		Inputs:  []workflow.Field{core.Inputs[0], core.Inputs[4]},
		Outputs: []workflow.Field{core.Outputs[0]},
		InputMappings: []workflow.Mapping{
			{Target: "worker_id", Source: workflow.Source{Kind: workflow.SourceNodeOutput, NodeID: promotionexec.NodeExecutePromotion, Path: "worker_id"}},
			{Target: "proposal_digest", Source: workflow.Source{Kind: workflow.SourceNodeOutput, NodeID: promotionexec.NodeSimulateCompensation, Path: "proposal_digest"}},
		},
		Capability: &capRef, DeclaredEffect: core.DeclaredEffect, EffectRole: role,
		FailureRoute: promotionexec.NodeEndRepairPlan, Governance: core.Governance,
	}
	def.Nodes = append(def.Nodes, leg)
	for i := range def.Edges {
		if def.Edges[i].From == promotionexec.NodeExecutePromotion && def.Edges[i].RouteKey == "SUCCEEDED" {
			def.Edges[i].To = nodeLeg
		}
	}
	def.Edges = append(def.Edges,
		workflow.Edge{From: nodeLeg, To: promotionexec.NodeObservePayroll, RouteKey: "SUCCEEDED"},
		workflow.Edge{From: nodeLeg, To: promotionexec.NodeEndRepairPlan, RouteKey: "REJECTED"},
		workflow.Edge{From: nodeLeg, To: promotionexec.NodeEndRepairPlan, RouteKey: "UNKNOWN"},
		workflow.Edge{From: nodeLeg, To: promotionexec.NodeEndRepairPlan, RouteKey: "AMBIGUOUS"},
	)
	def.Limits.MaxNodes++
	return def
}

func newLegFixture(t *testing.T, key string, role workflow.EffectRole) promotionFixture {
	t.Helper()
	f := newPromotionFixtureBase(t, key)
	plan, err := promotionexec.Compile(legDefinition(t, role))
	if err != nil {
		t.Fatalf("compile leg plan: %v", err)
	}
	if n, ok := plan.Node(nodeLeg); !ok || n.EffectRole != role {
		t.Fatalf("leg node = %+v, want role %s", n, role)
	}
	f.plan = plan
	f.start.Resolver = effects.PolicyResolver{Entries: []effects.PolicyEntry{{WorkflowID: plan.WorkflowID, Pin: version.Pin{CompiledPlanDigest: plan.Digest()}, Plan: plan}}}
	f.start.Versions = promotionVersionStore{plan: plan}
	f.db.Exec(t, `INSERT INTO payload_schema (tenant_id, schema_ref, schema_id, schema_version, message_full_name, wire_format, canonicalization_profile) VALUES ($1,$2,'hcmnext.test.wfrun037',1,'hcmnext.test.wfrun037','PROTOBUF','LEDGER_EVENT')`, f.tenantID, legSchemaRef)
	preparePromotionAt(t, f, 8)
	return f
}

var errLegDown = errors.New("payroll outbox leg: store unavailable")

// legRunner runs the promotion core and the dependent leg inside the advance
// transaction. The core enqueues its outbox fact; the leg enqueues its own,
// then breaks the transaction with a failing statement and returns an error.
type legRunner struct{ promotionRunner }

func (legRunner) RunsInTransaction(node workflow.CompiledNode) bool {
	return node.ID == promotionexec.NodeExecutePromotion || node.ID == nodeLeg
}

func (r legRunner) RunInTx(ctx context.Context, ex runtime.Executor, req execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	tx, ok := ex.(dbport.Tx)
	if !ok {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, errors.New("executor is not a transaction")
	}
	if _, err := outbox.Enqueue(ctx, tx, outbox.EnqueueRequest{Tenant: req.TenantID, EffectIdentity: "wfrun037:" + req.Node.ID, OrderingKey: "wfrun037", SchemaRef: legSchemaRef, Payload: []byte(req.Node.ID)}); err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
	}
	if req.Node.ID == nodeLeg {
		if _, err := ex.Exec(ctx, `SELECT 1/0`); err != nil {
			return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, errors.Join(errLegDown, err)
		}
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, errLegDown
	}
	return r.Run(ctx, req)
}

func legDriver(t *testing.T, f promotionFixture, repair *repairRequester, roles *execute.EffectRolePolicy) *execute.Driver {
	t.Helper()
	registry, err := datalogger.NewLedgerEventDigestRegistry()
	if err != nil {
		t.Fatal(err)
	}
	terminal := &effects.LedgerTerminalWriter{Appender: datalogger.NewAppender(registry), ProjectionName: "wfrun037_outcome", SourceRef: "hcmnext:test:wfrun037"}
	driver, err := execute.New(execute.Options{
		DB: f.conn, Steps: legRunner{promotionRunner{payroll: "PASS", recon: "PASS"}}, Terminal: terminal, Repair: repair,
		Guard: idempotency.PostgresStore{}, Retention: idempotency.RetentionPolicy{Retention: 72 * time.Hour, RetryWindow: 6 * time.Hour},
		Clock: func() time.Time { return f.at }, EffectRoles: roles,
	})
	if err != nil {
		t.Fatal(err)
	}
	return driver
}

func countWhere(t *testing.T, f promotionFixture, query string, args ...any) int {
	t.Helper()
	var n int
	if err := f.db.Conn.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func nodeStatus(t *testing.T, f promotionFixture, instanceID uuid.UUID, nodeID string) (status, class string) {
	t.Helper()
	var cls *string
	if err := f.db.Conn.QueryRow(context.Background(), `SELECT status, error_class FROM workflow_node_execution
		WHERE tenant_id = $1 AND instance_id = $2 AND node_id = $3 ORDER BY attempt DESC LIMIT 1`, f.tenantID, instanceID, nodeID).Scan(&status, &cls); err != nil {
		t.Fatalf("load %s: %v", nodeID, err)
	}
	if cls != nil {
		class = *cls
	}
	return status, class
}

// TestTodo_WF_RUN_037_Fault drives the served driver over PostgreSQL: the
// authoritative core commits, the dependent leg after it fails inside its own
// advance transaction (after a partial write and an aborting statement), and
// the instance keeps the core outcome, discards only the leg's write, takes
// the compiled failure route to the repair terminal and records the sealed
// settlement -- RECONCILIATION for a downstream effect, REBUILD_FROM_CORE for
// a derived update -- instead of rolling back or hiding behind an error.
func TestTodo_WF_RUN_037_Fault(t *testing.T) {
	for _, tc := range []struct {
		role  workflow.EffectRole
		route runtime.EffectRoleRoute
		class string
	}{
		{workflow.RoleDownstreamEffect, runtime.RouteReconciliation, execute.ErrorClassDownstreamEffectFailed},
		{workflow.RoleDerivedUpdate, runtime.RouteRebuildFromCore, execute.ErrorClassDerivedUpdateFailed},
	} {
		t.Run(string(tc.role), func(t *testing.T) {
			ctx := context.Background()
			f := newLegFixture(t, "wfrun037-"+string(tc.role), tc.role)
			repair := &repairRequester{}
			result, err := legDriver(t, f, repair, &execute.EffectRolePolicy{Settlements: runtime.EffectRoleSettlementStore{}}).Execute(ctx, execute.ExecuteRequest{Start: f.start})
			if err != nil {
				t.Fatalf("Execute = %v; a failed %s must not abort the run", err, tc.role)
			}
			if result.Status != execute.StatusComplete {
				t.Fatalf("status = %s, want COMPLETE on the repair terminal", result.Status)
			}
			instanceID := result.Start.InstanceID
			got := instance(t, f, instanceID)
			if got.RuntimeStatus != runtime.InstanceRepairRequired || got.CompletionDimensions.ConsistencyState != "DEGRADED" {
				t.Fatalf("instance = %s/%s, want REPAIR_REQUIRED/DEGRADED", got.RuntimeStatus, got.CompletionDimensions.ConsistencyState)
			}
			if status, _ := nodeStatus(t, f, instanceID, promotionexec.NodeExecutePromotion); status != string(runtime.NodeSucceeded) {
				t.Fatalf("core execution = %s, want SUCCEEDED: the core outcome must stand", status)
			}
			if status, class := nodeStatus(t, f, instanceID, nodeLeg); status != string(runtime.NodeFailed) || class != tc.class {
				t.Fatalf("leg execution = %s/%q, want FAILED/%s", status, class, tc.class)
			}
			if n := countWhere(t, f, `SELECT count(*) FROM workflow_node_execution WHERE tenant_id = $1 AND node_id = $2`, f.tenantID, promotionexec.NodeObservePayroll); n != 0 {
				t.Fatalf("payroll observation ran %d times after the leg failed", n)
			}
			if n := countWhere(t, f, `SELECT count(*) FROM outbox WHERE tenant_id = $1 AND effect_identity = $2`, f.tenantID, "wfrun037:"+promotionexec.NodeExecutePromotion); n != 1 {
				t.Fatalf("core outbox facts = %d, want 1: nothing may roll the core back", n)
			}
			if n := countWhere(t, f, `SELECT count(*) FROM outbox WHERE tenant_id = $1 AND effect_identity = $2`, f.tenantID, "wfrun037:"+nodeLeg); n != 0 {
				t.Fatalf("leg outbox facts = %d, want 0: the failed leg's own write must be discarded", n)
			}
			if n := countWhere(t, f, `SELECT count(*) FROM ledger_event WHERE tenant_id = $1`, f.tenantID); n != 1 {
				t.Fatalf("terminal ledger events = %d, want 1", n)
			}
			if repair.count() != 1 || countWhere(t, f, `SELECT count(*) FROM effect_reconciliation_job WHERE tenant_id = $1`, f.tenantID) != 1 {
				t.Fatalf("repair requests = %d, want the existing repair route to open one reconciliation job", repair.count())
			}

			settlements := listSettlements(t, f, instanceID)
			if len(settlements) != 1 {
				t.Fatalf("settlements = %+v, want exactly one", settlements)
			}
			s := settlements[0]
			wantCore := runtime.NodeExecutionID(f.tenantID, instanceID, promotionexec.NodeExecutePromotion, 1)
			if s.NodeID != nodeLeg || s.Attempt != 1 || s.Role != tc.role || s.Route != tc.route || s.ErrorClass != tc.class ||
				s.FailureRoute != promotionexec.NodeEndRepairPlan || s.PlanDigest != f.plan.Digest() || s.Verify() != nil ||
				len(s.Cores) != 1 || s.Cores[0].NodeID != promotionexec.NodeExecutePromotion || s.Cores[0].NodeExecutionID != wantCore || s.Cores[0].OutputArtifactRef == "" {
				t.Fatalf("settlement = %+v, want %s/%s against core %s", s, tc.role, tc.route, wantCore)
			}
			if _, err := f.db.Conn.Exec(ctx, `UPDATE workflow_effect_role_settlement SET error_class = 'EDITED' WHERE tenant_id = $1`, f.tenantID); err == nil {
				t.Fatal("a settlement row was mutable; it must be append-only")
			}
		})
	}
}

// outOfTxLegRunner runs every node before the advance transaction opens and
// fails the dependent leg there.
type outOfTxLegRunner struct{ promotionRunner }

func (r outOfTxLegRunner) Run(ctx context.Context, req execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	if req.Node.ID == nodeLeg {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, errLegDown
	}
	return r.promotionRunner.Run(ctx, req)
}

// TestTodo_WF_RUN_037_FaultOutsideTransaction proves the same settlement when
// the leg is not claimed by a transactional runner: its StepRunner error
// becomes the terminal failed outcome and the settlement names the core.
func TestTodo_WF_RUN_037_FaultOutsideTransaction(t *testing.T) {
	f := newLegFixture(t, "wfrun037-out-of-tx", workflow.RoleDownstreamEffect)
	registry, err := datalogger.NewLedgerEventDigestRegistry()
	if err != nil {
		t.Fatal(err)
	}
	driver, err := execute.New(execute.Options{
		DB: f.conn, Steps: outOfTxLegRunner{promotionRunner{payroll: "PASS", recon: "PASS"}},
		Terminal: &effects.LedgerTerminalWriter{Appender: datalogger.NewAppender(registry), ProjectionName: "wfrun037_outcome", SourceRef: "hcmnext:test:wfrun037"},
		Repair:   &repairRequester{}, Guard: idempotency.PostgresStore{}, Retention: idempotency.RetentionPolicy{Retention: 72 * time.Hour, RetryWindow: 6 * time.Hour},
		Clock: func() time.Time { return f.at }, EffectRoles: &execute.EffectRolePolicy{Settlements: runtime.EffectRoleSettlementStore{}},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := driver.Execute(context.Background(), execute.ExecuteRequest{Start: f.start})
	if err != nil || result.Status != execute.StatusComplete {
		t.Fatalf("Execute = %s, %v; want COMPLETE on the repair terminal", result.Status, err)
	}
	if got := instance(t, f, result.Start.InstanceID); got.RuntimeStatus != runtime.InstanceRepairRequired {
		t.Fatalf("instance = %s, want REPAIR_REQUIRED", got.RuntimeStatus)
	}
	settlements := listSettlements(t, f, result.Start.InstanceID)
	if len(settlements) != 1 || settlements[0].Route != runtime.RouteReconciliation || settlements[0].ErrorClass != execute.ErrorClassDownstreamEffectFailed ||
		len(settlements[0].Cores) != 1 || settlements[0].Cores[0].NodeID != promotionexec.NodeExecutePromotion {
		t.Fatalf("settlements = %+v, want one RECONCILIATION settlement against the core", settlements)
	}
}

// TestTodo_WF_RUN_037_FaultWithoutPolicyMasksTheCore records the RED
// behavior the policy removes: with no EffectRoles policy the leg's error
// aborts the run, the instance stays RUNNING at the leg with no settlement,
// and the committed core is visible only as a stuck instance.
func TestTodo_WF_RUN_037_FaultWithoutPolicyMasksTheCore(t *testing.T) {
	f := newLegFixture(t, "wfrun037-no-policy", workflow.RoleDownstreamEffect)
	_, err := legDriver(t, f, &repairRequester{}, nil).Execute(context.Background(), execute.ExecuteRequest{Start: f.start})
	if !errors.Is(err, errLegDown) {
		t.Fatalf("Execute = %v, want the leg error to abort the run without a policy", err)
	}
	got := instanceByTenant(t, f)
	if got.RuntimeStatus != runtime.InstanceRunning {
		t.Fatalf("instance = %s, want RUNNING (stuck) without the policy", got.RuntimeStatus)
	}
	if status, _ := nodeStatus(t, f, got.InstanceID, promotionexec.NodeExecutePromotion); status != string(runtime.NodeSucceeded) {
		t.Fatalf("core = %s, want SUCCEEDED: the core committed in its own advancement", status)
	}
	if n := countWhere(t, f, `SELECT count(*) FROM workflow_effect_role_settlement WHERE tenant_id = $1`, f.tenantID); n != 0 {
		t.Fatalf("settlements = %d without a policy, want 0", n)
	}
}

// TestEffectRolePolicyConfigurationIsValidated proves a policy without a
// recorder is refused at wiring time rather than at the first failed leg.
func TestEffectRolePolicyConfigurationIsValidated(t *testing.T) {
	if _, err := execute.New(execute.Options{DB: noopBeginner{}, Steps: promotionRunner{}, EffectRoles: &execute.EffectRolePolicy{}}); !errors.Is(err, execute.ErrInvalidConfiguration) {
		t.Fatalf("New with a recorder-less policy = %v, want ErrInvalidRequest", err)
	}
	if _, err := execute.New(execute.Options{DB: noopBeginner{}, Steps: promotionRunner{}, EffectRoles: &execute.EffectRolePolicy{Settlements: runtime.EffectRoleSettlementStore{}}}); err != nil {
		t.Fatalf("New with a complete policy: %v", err)
	}
}

type noopBeginner struct{}

func (noopBeginner) Begin(context.Context) (dbport.Tx, error) { return nil, errors.New("unused") }

func listSettlements(t *testing.T, f promotionFixture, instanceID uuid.UUID) []runtime.EffectRoleSettlement {
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
	out, err := (runtime.EffectRoleSettlementStore{}).ListForInstance(ctx, tx, f.tenantID, instanceID)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
