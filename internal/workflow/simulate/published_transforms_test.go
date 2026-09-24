package simulate

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation/ir"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/rulepayload"
)

func TestTodo_WF_EXT_006_PublishedTransform(t *testing.T) {
	program := ir.Program{
		IRVersion:      ir.IRVersion,
		DefinitionName: "copy_label",
		Instructions: []ir.Instruction{{
			Op:          ir.OpProject,
			Sources:     []transformation.Path{{Schema: "workflow", Field: "source", Type: transformation.TypeString}},
			Destination: transformation.Path{Schema: "workflow", Field: "target", Type: transformation.TypeString},
		}},
		Dependencies: []string{"workflow.source"},
		Limits:       ir.Limits{MaxSteps: 1, MaxFanOut: 1},
	}
	digest, err := program.Digest()
	if err != nil {
		t.Fatal(err)
	}
	store := rulepayload.New()
	ref := workflow.Reference{Kind: workflow.RefTransform, ID: "transform.copy_label", Version: "1"}
	pin, err := store.Publish(rulepayload.Payload{Ref: ref, Kind: rulepayload.KindTransform, Transform: &program})
	if err != nil {
		t.Fatal(err)
	}
	if pin.Digest != digest {
		t.Fatalf("pin digest = %q, want %q", pin.Digest, digest)
	}
	compiled := workflow.CompiledTransform{Program: &pin, Limits: workflow.TransformLimits{MaxSteps: 10, MaxInputBytes: 1024, MaxOutputBytes: 1024}}
	req := TransformRequest{
		NodeID:    "copy",
		Transform: compiled,
		Inputs:    Bag{"source": NewString("pinned")},
		Outputs:   []workflow.Field{{Path: "target", Type: workflow.ValueType{Kind: workflow.KindString}}},
	}
	evaluator := PublishedTransforms{Payloads: store}
	first, err := evaluator.Transform(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := evaluator.Transform(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if first.Outcome != workflow.OutcomeSucceeded || first.Outputs["target"].Text != "pinned" {
		t.Fatalf("transform result = %#v", first)
	}
	if first.Detail != second.Detail || first.Outputs["target"] != second.Outputs["target"] {
		t.Fatalf("pinned transform was not deterministic: %#v != %#v", first, second)
	}
}
