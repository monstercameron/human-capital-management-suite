package builders

import (
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

func TestBuildersMapExactSources(t *testing.T) {
	if got := FromInput("worker_id"); !reflect.DeepEqual(got, workflow.Source{Kind: workflow.SourceWorkflowInput, Path: "worker_id"}) {
		t.Fatalf("FromInput = %+v, want the workflow-input source", got)
	}
	if got := FromNode("read_facts", "worker_id"); !reflect.DeepEqual(got, workflow.Source{Kind: workflow.SourceNodeOutput, NodeID: "read_facts", Path: "worker_id"}) {
		t.Fatalf("FromNode = %+v, want the node-output source", got)
	}
	typ := workflow.ValueType{Kind: workflow.KindString}
	if got := Constant("COMMITTED", typ); !reflect.DeepEqual(got, workflow.Source{Kind: workflow.SourceConstant, Constant: "COMMITTED", Type: typ}) {
		t.Fatalf("Constant = %+v, want the literal source", got)
	}
}

func TestBuildersCompletionRecordsFiveDimensions(t *testing.T) {
	got := Completion("APPROVED", "COMMITTED", "COMPLETED", "CONSISTENT", "SATISFIED")
	want := map[string]string{
		"RequestState": "APPROVED", "ExecutionState": "COMMITTED", "BusinessState": "COMPLETED",
		"ConsistencyState": "CONSISTENT", "ObligationState": "SATISFIED",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Completion = %v, want %v", got, want)
	}
}

func TestBuildersTerminalInputsCarryFamilyIdentityAndCode(t *testing.T) {
	got := TerminalInputs("case_id", "CaseID")
	want := []workflow.Field{
		{Path: "case_id", Type: workflow.ValueType{Kind: workflow.KindString, Brand: "CaseID"}},
		{Path: "terminal_code", Type: workflow.ValueType{Kind: workflow.KindString}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TerminalInputs = %+v, want %+v", got, want)
	}
	extra := workflow.Field{Path: "plan_id", Type: workflow.ValueType{Kind: workflow.KindString}}
	withExtra := TerminalInputs("case_id", "CaseID", extra)
	if len(withExtra) != 3 || !reflect.DeepEqual(withExtra[2], extra) {
		t.Fatalf("TerminalInputs(extra) = %+v, want the extra field appended", withExtra)
	}
	if !reflect.DeepEqual(withExtra[:2], want) {
		t.Fatalf("TerminalInputs(extra) base = %+v, want %+v", withExtra[:2], want)
	}
}

func TestBuildersTerminalMappingsBindFamilyIdentityAndCode(t *testing.T) {
	plain := workflow.ValueType{Kind: workflow.KindString}
	got := TerminalMappings("case_id", "DISPOSED")
	want := []workflow.Mapping{
		{Target: "case_id", Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: "case_id"}},
		{Target: "terminal_code", Source: workflow.Source{Kind: workflow.SourceConstant, Constant: "DISPOSED", Type: plain}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("TerminalMappings = %+v, want %+v", got, want)
	}
	extra := workflow.Mapping{Target: "plan_id", Source: FromInput("plan_id")}
	withExtra := TerminalMappings("case_id", "DISPOSED", extra)
	if len(withExtra) != 3 || !reflect.DeepEqual(withExtra[2], extra) {
		t.Fatalf("TerminalMappings(extra) = %+v, want the extra mapping appended", withExtra)
	}
}
