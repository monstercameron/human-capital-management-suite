package configuration

import (
	"errors"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

func wfpageK2ValidationRule(expression string) workflow.PageRule {
	return workflow.PageRule{Code: "MAX_STARTING_SALARY", Message: "Starting salary exceeds the grade limit.", Expression: expression, Inputs: map[string]workflow.ValueType{"grade": {Kind: workflow.KindString}, "salary": {Kind: workflow.KindMoney}}, Parameters: map[string]workflow.ValueType{"salary.max.g6": {Kind: workflow.KindMoney}}}
}

func TestTodo_WFPAGE_017(t *testing.T) {
	library := NewValidationLibrary()
	first, err := library.Publish("tenant-a", "max-starting-salary", wfpageK2ValidationRule(`salary <= param("salary.max.g6")`), []ValidationParameter{{Key: "salary.max.g6", Type: workflow.ValueType{Kind: workflow.KindMoney}}})
	if err != nil {
		t.Fatal(err)
	}
	if first.Version != 1 || first.Digest == "" {
		t.Fatalf("first version = %+v", first)
	}
	if err := library.BindPage("tenant-a", "new-hire", first.Name, first.Version); err != nil {
		t.Fatal(err)
	}
	second, err := library.Publish("tenant-a", "max-starting-salary", workflow.PageRule{Code: first.Rule.Code, Message: first.Rule.Message, Expression: `salary <= param("salary.max.g6") && grade == "G6"`, Inputs: first.Rule.Inputs, Parameters: first.Rule.Parameters}, first.Parameters)
	if err != nil {
		t.Fatal(err)
	}
	if second.Version != 2 || first.Digest == second.Digest {
		t.Fatalf("versions not immutable: first=%+v second=%+v", first, second)
	}
	uses := library.PageUses("tenant-a", first.Name)
	if len(uses) != 1 || uses[0].RuleVersion != 1 {
		t.Fatalf("page binding changed after publish: %+v", uses)
	}
	impact, err := library.ImpactBeforeChange("tenant-a", first.Name, 2)
	if err != nil || len(impact.PageUses) != 1 {
		t.Fatalf("impact = %+v, %v", impact, err)
	}
}

func TestTodo_WFPAGE_017_Browser(t *testing.T) {
	library := NewValidationLibrary()
	if _, err := library.Publish("tenant-a", "rule", workflow.PageRule{Code: "C", Message: "m", Expression: `amount > param("limit")`, Inputs: map[string]workflow.ValueType{"amount": {Kind: workflow.KindInteger}}, Parameters: map[string]workflow.ValueType{"limit": {Kind: workflow.KindInteger}}}, []ValidationParameter{{Key: "limit", Type: workflow.ValueType{Kind: workflow.KindInteger}}}); err != nil {
		t.Fatal(err)
	}
	if err := library.BindPage("tenant-a", "page-b", "rule", 1); err != nil {
		t.Fatal(err)
	}
	if err := library.BindPage("tenant-a", "page-a", "rule", 1); err != nil {
		t.Fatal(err)
	}
	uses := library.PageUses("tenant-a", "rule")
	if len(uses) != 2 || uses[0].PageID != "page-a" || uses[1].PageID != "page-b" {
		t.Fatalf("uses are not deterministic: %+v", uses)
	}
	if _, err := library.Get("tenant-b", "rule", 1); !errors.Is(err, ErrValidationTenant) {
		t.Fatalf("cross-tenant lookup = %v", err)
	}
	if _, err := library.Get("tenant-a", "rule", 2); !errors.Is(err, ErrValidationVersion) {
		t.Fatalf("unknown version = %v", err)
	}
}
