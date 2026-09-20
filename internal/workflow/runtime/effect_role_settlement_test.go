package runtime_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

const settlementLeg = "projection_leg"

// rolePlan compiles the executable promotion plan with one INTERNAL_MUTATION
// leg of role after the authoritative core.
func rolePlan(t *testing.T, role workflow.EffectRole) *workflow.CompiledWorkflow {
	t.Helper()
	// The frozen 1.0.0 graph, where the core routes straight to the payroll
	// observation the leg is inserted before.
	def := promotionexec.DefinitionV1_0()
	var core workflow.Node
	for _, n := range def.Nodes {
		if n.ID == promotionexec.NodeExecutePromotion {
			core = n
		}
	}
	ref := *core.Capability
	ref.EffectBinding = "promotion.projection_leg"
	def.Nodes = append(def.Nodes, workflow.Node{
		ID: settlementLeg, Type: workflow.StepCapability, InputSchema: core.InputSchema, OutputSchema: core.OutputSchema,
		Inputs: []workflow.Field{core.Inputs[0]}, Outputs: []workflow.Field{core.Outputs[0]},
		InputMappings: []workflow.Mapping{{Target: "worker_id", Source: workflow.Source{Kind: workflow.SourceNodeOutput, NodeID: promotionexec.NodeExecutePromotion, Path: "worker_id"}}},
		Capability:    &ref, DeclaredEffect: core.DeclaredEffect, EffectRole: role,
		FailureRoute: promotionexec.NodeEndRepairPlan, Governance: core.Governance,
	})
	for i := range def.Edges {
		if def.Edges[i].From == promotionexec.NodeExecutePromotion && def.Edges[i].RouteKey == "SUCCEEDED" {
			def.Edges[i].To = settlementLeg
		}
	}
	def.Edges = append(def.Edges,
		workflow.Edge{From: settlementLeg, To: promotionexec.NodeObservePayroll, RouteKey: "SUCCEEDED"},
		workflow.Edge{From: settlementLeg, To: promotionexec.NodeEndRepairPlan, RouteKey: "REJECTED"},
		workflow.Edge{From: settlementLeg, To: promotionexec.NodeEndRepairPlan, RouteKey: "UNKNOWN"},
		workflow.Edge{From: settlementLeg, To: promotionexec.NodeEndRepairPlan, RouteKey: "AMBIGUOUS"},
	)
	def.Limits.MaxNodes++
	plan, err := promotionexec.Compile(def)
	if err != nil {
		t.Fatalf("compile %s plan: %v", role, err)
	}
	return plan
}

func execution(tenant, instance uuid.UUID, node string, attempt int, status runtime.NodeStatus, output string) runtime.NodeExecution {
	e := runtime.NewNodeExecution(tenant, instance, node, attempt, workflow.StepCapability, status)
	e.OutputArtifactRef = output
	return e
}

// TestDeriveEffectRoleSettlementNamesTheCommittedCore proves the pure
// derivation: a downstream effect routes to RECONCILIATION and a derived
// update to REBUILD_FROM_CORE, each naming only the core's latest SUCCEEDED
// attempt; a derived update with no committed core, a core or read node, an
// unknown node and an attempt without class or number are refused; the seal
// breaks when any field is edited.
func TestDeriveEffectRoleSettlementNamesTheCommittedCore(t *testing.T) {
	tenant, instance := uuid.New(), uuid.New()
	coreFailed := execution(tenant, instance, promotionexec.NodeExecutePromotion, 1, runtime.NodeRetrying, "")
	coreOK := execution(tenant, instance, promotionexec.NodeExecutePromotion, 2, runtime.NodeSucceeded, "sha256:core")
	foreign := execution(tenant, uuid.New(), promotionexec.NodeExecutePromotion, 9, runtime.NodeFailed, "")
	committed := []runtime.NodeExecution{coreOK, foreign, coreFailed}

	for _, tc := range []struct {
		role  workflow.EffectRole
		route runtime.EffectRoleRoute
	}{
		{workflow.RoleDownstreamEffect, runtime.RouteReconciliation},
		{workflow.RoleDerivedUpdate, runtime.RouteRebuildFromCore},
	} {
		plan := rolePlan(t, tc.role)
		s, err := runtime.DeriveEffectRoleSettlement(plan, instance, settlementLeg, 1, "LEG_FAILED", committed)
		if err != nil {
			t.Fatalf("%s: %v", tc.role, err)
		}
		if s.Route != tc.route || s.Role != tc.role || s.FailureRoute != promotionexec.NodeEndRepairPlan || s.PlanDigest != plan.Digest() ||
			s.EffectKey == "" || len(s.Cores) != 1 || s.Cores[0].NodeExecutionID != coreOK.NodeExecutionID || s.Cores[0].OutputArtifactRef != "sha256:core" ||
			!strings.HasPrefix(s.Digest, "sha256:") || s.Verify() != nil {
			t.Fatalf("%s settlement = %+v", tc.role, s)
		}
		edited := s
		edited.Cores = []runtime.CoreOutcome{{NodeID: "other", NodeExecutionID: coreOK.NodeExecutionID}}
		if runtimeCode(edited.Verify()) != runtime.CodeEffectRoleSealBroken {
			t.Fatalf("%s: an edited settlement verified", tc.role)
		}
	}

	downstream := rolePlan(t, workflow.RoleDownstreamEffect)
	uncommitted := []runtime.NodeExecution{coreFailed}
	if s, err := runtime.DeriveEffectRoleSettlement(downstream, instance, settlementLeg, 1, "LEG_FAILED", uncommitted); err != nil || len(s.Cores) != 0 {
		t.Fatalf("downstream with no committed core = %+v, %v; want an empty core list", s, err)
	}
	derived := rolePlan(t, workflow.RoleDerivedUpdate)
	for name, call := range map[string]func() error{
		"derived with no core": func() error {
			_, err := runtime.DeriveEffectRoleSettlement(derived, instance, settlementLeg, 1, "LEG_FAILED", uncommitted)
			return expectCode(err, runtime.CodeEffectRoleNoCore)
		},
		"core node": func() error {
			_, err := runtime.DeriveEffectRoleSettlement(derived, instance, promotionexec.NodeExecutePromotion, 1, "X", committed)
			return expectCode(err, runtime.CodeEffectRoleNotDependent)
		},
		"read node": func() error {
			_, err := runtime.DeriveEffectRoleSettlement(derived, instance, promotionexec.NodeObservePayroll, 1, "X", committed)
			return expectCode(err, runtime.CodeEffectRoleNotDependent)
		},
		"unknown node": func() error {
			_, err := runtime.DeriveEffectRoleSettlement(derived, instance, "ghost", 1, "X", committed)
			return expectCode(err, runtime.CodeInvalidRecord)
		},
		"no plan": func() error {
			_, err := runtime.DeriveEffectRoleSettlement(nil, instance, settlementLeg, 1, "X", committed)
			return expectCode(err, runtime.CodeInvalidRecord)
		},
		"zero attempt": func() error {
			_, err := runtime.DeriveEffectRoleSettlement(derived, instance, settlementLeg, 0, "X", committed)
			return expectCode(err, runtime.CodeInvalidRecord)
		},
		"blank class": func() error {
			_, err := runtime.DeriveEffectRoleSettlement(derived, instance, settlementLeg, 1, " ", committed)
			return expectCode(err, runtime.CodeInvalidRecord)
		},
	} {
		if err := call(); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func expectCode(err error, code string) error {
	if runtimeCode(err) != code {
		return errors.New("got " + errString(err) + ", want " + code)
	}
	return nil
}

func errString(err error) string {
	if err == nil {
		return "<nil>"
	}
	return err.Error()
}

// settlementRows creates an instance with the core and leg node executions
// the settlement's foreign key requires, and returns the derived settlement.
func settlementRows(t *testing.T, db *pgtest.DB, tenant uuid.UUID, role workflow.EffectRole) runtime.EffectRoleSettlement {
	t.Helper()
	ctx := context.Background()
	inst := newInstance(t, tenant, referencePlan(t))
	core := execution(tenant, inst.InstanceID, promotionexec.NodeExecutePromotion, 1, runtime.NodeSucceeded, "sha256:core-output")
	leg := execution(tenant, inst.InstanceID, settlementLeg, 1, runtime.NodeFailed, "")
	inTenantTx(t, appConn(t, db), tenant, func(tx dbport.Tx) error {
		created, err := (runtime.Store{}).CreateInstance(ctx, tx, inst)
		if err != nil {
			return err
		}
		_, v, err := (runtime.Store{}).RecordNodeExecution(ctx, tx, core, created.InstanceVersion)
		if err != nil {
			return err
		}
		_, _, err = (runtime.Store{}).RecordNodeExecution(ctx, tx, leg, v)
		return err
	})
	s, err := runtime.DeriveEffectRoleSettlement(rolePlan(t, role), inst.InstanceID, settlementLeg, 1, "LEG_FAILED", []runtime.NodeExecution{core, leg})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestEffectRoleSettlementStoreIsDurableIdempotentAndIsolated proves the
// PostgreSQL store: a settlement survives a fresh connection verbatim, a
// replay returns the stored row without duplicating it, a different
// settlement for the same attempt is refused, an unsealed or misrouted one is
// refused before any write, another tenant sees nothing, and the row is
// append-only.
func TestEffectRoleSettlementStoreIsDurableIdempotentAndIsolated(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	tenant := insertTenant(t, db, "wfrun037-settlement")
	other := insertTenant(t, db, "wfrun037-settlement-other")
	store := runtime.EffectRoleSettlementStore{}
	s := settlementRows(t, db, tenant, workflow.RoleDerivedUpdate)

	inTenantTx(t, appConn(t, db), tenant, func(tx dbport.Tx) error {
		stored, err := store.Record(ctx, tx, tenant, s, fixedInstant)
		if err != nil {
			return err
		}
		if stored.Digest != s.Digest || len(stored.Cores) != 1 || stored.Cores[0] != s.Cores[0] {
			return errors.New("stored settlement differs from the recorded one")
		}
		replay, err := store.Record(ctx, tx, tenant, s, fixedInstant.Add(time.Minute))
		if err != nil || replay.Digest != s.Digest {
			return errors.New("replay did not return the stored settlement: " + errString(err))
		}
		return nil
	})
	listed := func(conn uuid.UUID) []runtime.EffectRoleSettlement {
		var out []runtime.EffectRoleSettlement
		inTenantTx(t, appConn(t, db), conn, func(tx dbport.Tx) error {
			var err error
			out, err = store.ListForInstance(ctx, tx, conn, s.InstanceID)
			return err
		})
		return out
	}
	if got := listed(tenant); len(got) != 1 || got[0].Digest != s.Digest || got[0].Route != runtime.RouteRebuildFromCore || got[0].Verify() != nil {
		t.Fatalf("durable settlements = %+v, want the one sealed row", got)
	}
	if got := listed(other); len(got) != 0 {
		t.Fatalf("another tenant read %d settlements", len(got))
	}

	different := s
	different.ErrorClass = "ANOTHER_FAILURE"
	different = reseal(t, different)
	if err := inTenantTxErr(appConn(t, db), tenant, func(tx dbport.Tx) error {
		_, err := store.Record(ctx, tx, tenant, different, fixedInstant)
		return err
	}); runtimeCode(err) != runtime.CodeEffectRoleSealBroken {
		t.Fatalf("a second settlement of one attempt = %v, want %s", err, runtime.CodeEffectRoleSealBroken)
	}
	unsealed := s
	unsealed.FailureRoute = "somewhere_else"
	misrouted := s
	misrouted.Route = runtime.RouteReconciliation
	for name, tc := range map[string]struct {
		s      runtime.EffectRoleSettlement
		tenant uuid.UUID
		at     time.Time
		code   string
	}{
		"unsealed":    {unsealed, tenant, fixedInstant, runtime.CodeEffectRoleSealBroken},
		"misrouted":   {misrouted, tenant, fixedInstant, runtime.CodeEffectRoleNotDependent},
		"nil tenant":  {s, uuid.Nil, fixedInstant, runtime.CodeInvalidRecord},
		"no clock":    {s, tenant, time.Time{}, runtime.CodeInvalidRecord},
		"broken exec": {s, tenant, fixedInstant, runtime.CodeStorageFailed},
	} {
		var ex runtime.Executor = failingExecutor{err: errors.New("connection reset")}
		if name != "broken exec" {
			ex = failingExecutor{err: errors.New("must not be reached")}
		}
		if _, err := store.Record(ctx, ex, tc.tenant, tc.s, tc.at); runtimeCode(err) != tc.code {
			t.Fatalf("%s: Record = %v, want %s", name, err, tc.code)
		}
	}
	if _, err := store.ListForInstance(ctx, failingExecutor{err: errors.New("connection reset")}, tenant, s.InstanceID); runtimeCode(err) != runtime.CodeStorageFailed {
		t.Fatalf("list over a broken executor = %v, want %s", err, runtime.CodeStorageFailed)
	}
	if n := countRows(t, db, `SELECT count(*) FROM workflow_effect_role_settlement WHERE tenant_id = $1`, tenant); n != 1 {
		t.Fatalf("rows = %d, want 1", n)
	}
	for _, stmt := range []string{
		`UPDATE workflow_effect_role_settlement SET error_class = 'EDITED' WHERE tenant_id = $1`,
		`DELETE FROM workflow_effect_role_settlement WHERE tenant_id = $1`,
	} {
		if err := db.ExecErr(stmt, tenant); err == nil {
			t.Fatalf("superuser bypassed forbid_mutation with %q", stmt)
		}
	}
}

// reseal recomputes a settlement's seal by deriving it again with the edited
// error class, so the store sees a well-formed but different settlement.
func reseal(t *testing.T, s runtime.EffectRoleSettlement) runtime.EffectRoleSettlement {
	t.Helper()
	executions := []runtime.NodeExecution{}
	for _, c := range s.Cores {
		e := runtime.NewNodeExecution(uuid.Nil, s.InstanceID, c.NodeID, 1, workflow.StepCapability, runtime.NodeSucceeded)
		e.NodeExecutionID, e.OutputArtifactRef = c.NodeExecutionID, c.OutputArtifactRef
		executions = append(executions, e)
	}
	out, err := runtime.DeriveEffectRoleSettlement(rolePlan(t, s.Role), s.InstanceID, s.NodeID, s.Attempt, s.ErrorClass, executions)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
