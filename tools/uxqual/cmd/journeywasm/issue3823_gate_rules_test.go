package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// issue3823Ruled is the saved gate: people are let in by rule, on their team.
func issue3823Ruled() chatgate.Definition {
	d := chatgateClientDefinition()
	return chatui.GateRuleDraft(d, chatui.GateRuleEdit{Subject: "team", Values: []string{"Payroll", "Legal"}, Reason: "Your team runs payroll", Else: "declined"})
}

// TestIssue3823_RulesSurviveHidingTheEditor: choosing another way in hides the
// rules editor; choosing By rule again shows the saved rule, not a blank one.
func TestIssue3823_RulesSurviveHidingTheEditor(t *testing.T) {
	saved := issue3823Ruled()
	values, checks := chatgateEditorValues() // the editor is not drawn: no rule fields
	values["gate-mode"] = "rule"
	draft, ok := chatgateDraftDefinition(saved, values, checks)
	if !ok || draft.Mode != "rule" {
		t.Fatalf("no draft for By rule: ok=%v draft=%+v", ok, draft)
	}
	if edit := chatui.GateRuleOf(draft); edit.Subject != "team" || len(edit.Values) != 2 || edit.Reason != "Your team runs payroll" {
		t.Fatalf("the saved rule was lost when By rule was chosen again: %+v", draft.Rules)
	}
}

// TestIssue3823_RuleEditsSurviveLeavingRuleMode: what was typed into the rules
// editor is kept in the draft when the mode is switched away from By rule, and
// publishing in the other mode still sends no rules.
func TestIssue3823_RuleEditsSurviveLeavingRuleMode(t *testing.T) {
	values, checks := chatgateEditorValues()
	values["gate-mode"] = "review"
	values["gate-rule-field"], values["gate-rule-reason"], values["gate-rule-else"] = "team", "Typed just now", "review"
	values["gate-rule-values"] = "Finance"
	draft, ok := chatgateDraftDefinition(chatgateClientDefinition(), values, checks)
	if !ok || draft.Mode != "review" {
		t.Fatalf("no draft: ok=%v %+v", ok, draft)
	}
	if edit := chatui.GateRuleOf(draft); edit.Subject != "team" || edit.Reason != "Typed just now" {
		t.Fatalf("the typed rule was dropped on leaving By rule: %+v", draft.Rules)
	}
	sent, err := chatgateEditedDefinition(chatgateClientDefinition(), values, checks)
	if err != nil || len(sent.Rules) != 0 {
		t.Fatalf("publishing in review mode sends rules: %+v %v", sent.Rules, err)
	}
}
