package forms

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

func TestTodo_WFPAGE_014_Security(t *testing.T) {
	field := ComputedField{Path: "total", Type: workflow.ValueType{Kind: workflow.KindInteger}, Expression: "amount", Inputs: map[string]workflow.ValueType{"amount": {Kind: workflow.KindInteger}}}
	value, err := field.Recompute(workflow.PageRuleInput{Fields: map[string]any{"amount": int64(42)}})
	if err != nil || value.(int64) != 42 {
		t.Fatalf("computed = %#v, %v", value, err)
	}
	expected := ComputedValue{Path: "total", Type: field.Type, Value: int64(42)}
	if err := VerifyComputedValue(expected, expected); err != nil {
		t.Fatal(err)
	}
	forged := expected
	forged.Value = int64(43)
	if !errors.Is(VerifyComputedValue(expected, forged), ErrComputedValueTampered) {
		t.Fatal("forged computed value accepted")
	}
}
