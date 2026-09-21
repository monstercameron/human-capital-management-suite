package draftcompile_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/draftcompile"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/prototype"
)

func TestTodo_WF_UI_004(t *testing.T) {
	document, err := json.Marshal(prototype.ApprovalDefinition())
	if err != nil {
		t.Fatal(err)
	}
	result := (draftcompile.Compiler{Options: workflow.Options{Phase: workflow.PhaseP1B}}).CompileDocument(
		context.Background(), values.TenantId("tenant-a"), document,
	)
	if !result.Valid || result.PlanDigest == "" || len(result.Diagnostics) != 0 {
		t.Fatalf("compile result = %+v", result)
	}
	if !result.Effects.ZeroEffect || !result.Unwind.Complete || len(result.Unwind.Steps) != 0 {
		t.Fatalf("derived summaries = effects %+v unwind %+v", result.Effects, result.Unwind)
	}

	broken := prototype.ApprovalDefinition()
	broken.Edges[0].To = "missing-node"
	document, err = json.Marshal(broken)
	if err != nil {
		t.Fatal(err)
	}
	result = (draftcompile.Compiler{Options: workflow.Options{Phase: workflow.PhaseP1B}}).CompileDocument(
		context.Background(), values.TenantId("tenant-a"), document,
	)
	if result.Valid || len(result.Diagnostics) == 0 {
		t.Fatalf("invalid graph compiled: %+v", result)
	}
	located := false
	for _, diagnostic := range result.Diagnostics {
		located = located || diagnostic.NodeID != "" || diagnostic.EdgeID != ""
	}
	if !located {
		t.Fatalf("diagnostics were not mapped to a node or edge: %+v", result.Diagnostics)
	}
}

func TestTodo_WF_UI_004_Security(t *testing.T) {
	definition := prototype.ApprovalDefinition()
	definition.Nodes[0].Type = workflow.StepCapability
	definition.Nodes[0].Capability = &workflow.CapabilityRef{ID: "tenant.forbidden.write", Version: 1, OperationMode: workflow.ModeExecute}
	document, err := json.Marshal(definition)
	if err != nil {
		t.Fatal(err)
	}
	registry := &countingResolver{}
	compiler := draftcompile.Compiler{
		Options: workflow.Options{Phase: workflow.PhaseP1B, Capabilities: registry},
		Policy: draftcompile.CapabilityPolicyFunc(func(context.Context, values.TenantId, capability.Key) bool {
			return false
		}),
	}
	result := compiler.CompileDocument(context.Background(), values.TenantId("tenant-a"), document)
	if result.Valid {
		t.Fatal("tenant-forbidden capability compiled")
	}
	if registry.lookups != 0 {
		t.Fatalf("global capability registry was consulted %d times before tenant allow-list refusal", registry.lookups)
	}
	found := false
	for _, diagnostic := range result.Diagnostics {
		if diagnostic.Code == workflow.CodeUnresolvedRef && diagnostic.NodeID == prototype.NodeApproval && strings.Contains(diagnostic.Ref, "tenant.forbidden.write") {
			found = true
		}
	}
	if !found {
		t.Fatalf("missing node-scoped tenant refusal: %+v", result.Diagnostics)
	}
}

func TestTodo_WF_UI_004_Golden(t *testing.T) {
	result := (draftcompile.Compiler{}).CompileDocument(context.Background(), values.TenantId("tenant-a"), json.RawMessage(`{"workflow_id":"x","unknown":true}`))
	got, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"valid":false,"diagnostics":[{"code":"INVALID_DEFINITION","field":"document","detail":"draft document is not valid workflow definition JSON"}],"effects":{"zero_effect":false,"nodes_by_class":null,"allowed_modes":null},"unwind":{"complete":false}}`
	if string(got) != want {
		t.Fatalf("compile diagnostic golden changed:\n got %s\nwant %s", got, want)
	}
}

type countingResolver struct{ lookups int }

func (r *countingResolver) Lookup(capability.Key) (capability.Record, bool) {
	r.lookups++
	return capability.Record{}, false
}
