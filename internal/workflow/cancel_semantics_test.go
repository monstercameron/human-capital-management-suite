package workflow_test

import (
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

// TestTodo_WF_RUN_010_Semantics proves the REFACTOR clause: every compiled
// node declares its cancellation semantics from its effect class and its
// published compensation, and the promotion plan's one write
// (execute_promotion) is the only node that is not cancellation-free.
func TestTodo_WF_RUN_010_Semantics(t *testing.T) {
	plan, err := promotionexec.Compile()
	if err != nil {
		t.Fatal(err)
	}
	sems, err := plan.CancellationSemantics()
	if err != nil {
		t.Fatalf("CancellationSemantics: %v", err)
	}
	if len(sems) != len(plan.Nodes) {
		t.Fatalf("declared %d semantics for %d nodes", len(sems), len(plan.Nodes))
	}
	for _, sem := range sems {
		want := workflow.CancelFree
		if sem.NodeID == promotionexec.NodeExecutePromotion {
			want = workflow.CancelIrreversible
		}
		if sem.Class != want {
			t.Fatalf("node %s class = %s, want %s", sem.NodeID, sem.Class, want)
		}
	}

	compensable := workflow.CompiledNode{ID: "reserve", EffectClass: capability.EffectExternalMutation,
		CompensationRef: &workflow.ResolvedReference{ID: "release.stock", Version: "2"}}
	sem, err := workflow.CancellationSemanticsOf(compensable)
	if err != nil || sem.Class != workflow.CancelCompensable || sem.Compensation != "release.stock@2" {
		t.Fatalf("compensable semantics = %+v, %v", sem, err)
	}
	unversioned := compensable
	unversioned.CompensationRef = &workflow.ResolvedReference{ID: "release.stock"}
	if sem, _ := workflow.CancellationSemanticsOf(unversioned); sem.Compensation != "release.stock" {
		t.Fatalf("unversioned compensation = %q", sem.Compensation)
	}

	effect, ok := sem.Effect("reserve#1", true)
	if !ok || effect.Compensation != "release.stock@2" || effect.Ambiguous || effect.Reversible {
		t.Fatalf("settled compensable effect = %+v, %v", effect, ok)
	}
	inflight, ok := sem.Effect("reserve#2", false)
	if !ok || !inflight.Ambiguous {
		t.Fatalf("in-flight effect = %+v, %v", inflight, ok)
	}
	irreversible := workflow.CancellationSemantics{NodeID: "pay", Class: workflow.CancelIrreversible}
	if e, ok := irreversible.Effect("pay#1", true); !ok || e.Compensation != "" || e.Ambiguous {
		t.Fatalf("irreversible effect = %+v, %v", e, ok)
	}
	if _, ok := (workflow.CancellationSemantics{Class: workflow.CancelFree}).Effect("x", true); ok {
		t.Fatal("a cancellation-free node produced an effect record")
	}
}

// TestTodo_WF_RUN_010_SemanticsFault proves undeclarable semantics refuse:
// an unknown effect class and a nil plan never read as cancellation-free, and
// an ambiguous effect routes the decision to repair.
func TestTodo_WF_RUN_010_SemanticsFault(t *testing.T) {
	if _, err := workflow.CancellationSemanticsOf(workflow.CompiledNode{ID: "odd", EffectClass: "TELEPORT"}); err == nil || !strings.Contains(err.Error(), "odd") {
		t.Fatalf("unknown effect class error = %v", err)
	}
	bad := &workflow.CompiledWorkflow{Nodes: []workflow.CompiledNode{{ID: "odd", EffectClass: "TELEPORT"}}}
	if _, err := bad.CancellationSemantics(); err == nil {
		t.Fatal("a plan with an undeclarable node produced semantics")
	}
	var none *workflow.CompiledWorkflow
	if _, err := none.CancellationSemantics(); err == nil {
		t.Fatal("a nil plan produced semantics")
	}
	req := cancellationRequest()
	req.Effects = append(req.Effects, workflow.EffectRecord{ID: "promotion-commit", Ambiguous: true})
	outcome, err := workflow.DecideCancellation(req)
	if err != nil {
		t.Fatal(err)
	}
	if outcome.Decision != workflow.RepairRequired {
		t.Fatalf("ambiguous effect decision = %s, want REPAIR_REQUIRED", outcome.Decision)
	}
	last := outcome.Effects[len(outcome.Effects)-1]
	if last.ID != "promotion-commit" || last.Disposition != "AMBIGUOUS" {
		t.Fatalf("ambiguous disposition = %+v", last)
	}
}
