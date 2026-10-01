package forms

import (
	"errors"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

var ErrComputedValueTampered = errors.New("forms: computed value does not match server result")

// PageRule aliases the shared workflow rule contract so form producers and
// the server cannot accidentally grow separate expression languages.
type PageRule = workflow.PageRule
type CompiledPageRule = workflow.CompiledPageRule
type PageRuleInput = workflow.PageRuleInput
type PageRuleResult = workflow.PageRuleResult
type PageRuleFinding = workflow.PageRuleFinding

func CompilePageRule(rule PageRule) (CompiledPageRule, error) {
	return workflow.CompilePageRule(rule)
}

// ComputedField declares a read-only value whose expression is evaluated on
// the server from declared inputs. The submitted value is never used as an
// input to the calculation.
type ComputedField struct {
	Path          string
	Type          workflow.ValueType
	Expression    string
	Inputs        map[string]workflow.ValueType
	References    map[string]workflow.ValueType
	RepeatingCaps map[string]int
}

func (f ComputedField) Recompute(input workflow.PageRuleInput) (any, error) {
	inputs := f.Inputs
	if len(inputs) == 0 {
		inputs = inputTypes(input)
	}
	return workflow.EvaluatePageExpression(f.Expression, inputs, f.References, f.RepeatingCaps, f.Type, input)
}

// ComputedValue is the server-side scalar calculation primitive used by page
// submitters. It accepts only a trusted calculator and verifies the declared
// type before returning the result.
type ComputedValue struct {
	Path  string
	Type  workflow.ValueType
	Value any
}

func VerifyComputedValue(expected, submitted ComputedValue) error {
	if expected.Path == "" || expected.Path != submitted.Path || expected.Type.String() != submitted.Type.String() || fmt.Sprint(expected.Value) != fmt.Sprint(submitted.Value) {
		return ErrComputedValueTampered
	}
	return nil
}

func inputTypes(input workflow.PageRuleInput) map[string]workflow.ValueType {
	// Recompute is intentionally conservative: callers should use a declared
	// PageRule for real expressions. This helper only supplies useful types for
	// the small boolean computed guard above.
	types := make(map[string]workflow.ValueType, len(input.Fields))
	for key, value := range input.Fields {
		types[key] = workflow.ValueType{Kind: workflow.KindString}
		switch value.(type) {
		case bool:
			types[key] = workflow.ValueType{Kind: workflow.KindBool}
		case int, int32, int64:
			types[key] = workflow.ValueType{Kind: workflow.KindInteger}
		case string:
			types[key] = workflow.ValueType{Kind: workflow.KindString}
		}
	}
	return types
}
