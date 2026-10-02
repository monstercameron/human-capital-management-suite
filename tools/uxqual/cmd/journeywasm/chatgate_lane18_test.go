package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

func chatgateClientDefinition() chatgate.Definition {
	return chatgate.Definition{Mode: "rule", Purpose: "Payroll help", Fields: []chatgate.Field{
		{ID: "team", Kind: "single_choice", KindVersion: "1.0.0", Label: "Which team?", Purpose: "Routing", DataClass: "INTERNAL", Required: true, RetentionDays: 30, Options: []string{"Payroll", "Finance", "Legal"}, Visibility: chatgate.Visibility{Administrators: true}},
		{ID: "why", Kind: "short_text", KindVersion: "1.0.0", Label: "Why?", Purpose: "Context", DataClass: "INTERNAL", Required: true, RetentionDays: 30, Visibility: chatgate.Visibility{Administrators: true}},
	}}
}

// chatgateEditorValues is what the builder's fields hold for the definition,
// as the page reads them from the document.
func chatgateEditorValues() (map[string]string, map[string]bool) {
	values := map[string]string{"gate-purpose": "Payroll help", "gate-mode": "rule",
		"gate-editor-0-label": "Which team?", "gate-editor-0-purpose": "Routing", "gate-editor-0-kind": "single_choice", "gate-editor-0-days": "30", "gate-editor-0-options": "Payroll\nFinance\nLegal",
		"gate-editor-1-label": "Why?", "gate-editor-1-purpose": "Context", "gate-editor-1-kind": "short_text", "gate-editor-1-days": "30"}
	checks := map[string]bool{"gate-editor-0-required": true, "gate-editor-0-administrators": true, "gate-editor-1-required": true, "gate-editor-1-administrators": true}
	return values, checks
}

// TestTodo_CHATGATE_006_Rule: the builder's rule fields become the rule the
// service takes: the ticked answers and the typed ones, a question or a fact
// of the directory, and the choice of what happens to everyone else.
func TestTodo_CHATGATE_006_Rule(t *testing.T) {
	values, checks := chatgateEditorValues()
	values["gate-rule-field"], values["gate-rule-reason"], values["gate-rule-else"] = "team", "Your team runs payroll", "declined"
	values["gate-rule-choice-0"], values["gate-rule-choice-1"], values["gate-rule-choice-2"] = "Payroll", "Finance", "Legal"
	checks["gate-rule-choice-0"], checks["gate-rule-choice-1"], checks["gate-rule-choice-2"] = true, false, true
	values["gate-rule-values"] = "Treasury\n\n Payroll "
	d, err := chatgateEditedDefinition(chatgateClientDefinition(), values, checks)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Rules) != 2 || d.Rules[0].When.Field != "team" || strings.Join(d.Rules[0].When.Values, ",") != "Payroll,Legal,Treasury" || d.Rules[1].Outcome != "declined" {
		t.Fatalf("rules=%+v", d.Rules)
	}
	if err = chatgate.NewRegistry().ValidateDefinition(d, chatgate.Policy{Ceiling: "INTERNAL"}); err != nil {
		t.Fatalf("the service refuses the definition the builder sends: %v", err)
	}
	// A fact of the directory, everyone else reviewed.
	values["gate-rule-field"], values["gate-rule-else"] = chatui.GateRuleFactPrefix+"team", "review"
	if d, err = chatgateEditedDefinition(chatgateClientDefinition(), values, checks); err != nil || len(d.Rules) != 1 || d.Rules[0].When.Fact != "team" || d.Rules[0].When.Field != "" {
		t.Fatalf("a rule on a fact: %+v %v", d.Rules, err)
	}
	if got := chatui.GateRulePreview("en-US", d); got != "Admit people whose team is Payroll, Legal or Treasury. Everyone else waits for an administrator." {
		t.Fatalf("preview %q", got)
	}
	// An unfinished rule is not sent, and still draws: choosing the subject
	// first must redraw the builder for it.
	unfinished, _ := chatgateEditorValues()
	unfinished["gate-rule-field"] = chatui.GateRuleFactPrefix + "location"
	if _, err = chatgateEditedDefinition(chatgateClientDefinition(), unfinished, checks); !errors.Is(err, chatgate.ErrInvalid) {
		t.Fatalf("an unfinished rule was accepted for sending: %v", err)
	}
	draft, ok := chatgateDraftDefinition(chatgateClientDefinition(), unfinished, map[string]bool{"gate-editor-0-required": true, "gate-editor-0-administrators": true, "gate-editor-1-administrators": true})
	if !ok || draft.Mode != "rule" || chatui.GateRuleOf(draft).Subject != chatui.GateRuleFactPrefix+"location" || draft.Fields[0].Label != "Which team?" {
		t.Fatalf("draft=%+v ok=%v", draft, ok)
	}
	// A question whose fields cannot be read at all is no draft.
	broken, _ := chatgateEditorValues()
	broken["gate-editor-0-days"] = "soon"
	if _, ok = chatgateDraftDefinition(chatgateClientDefinition(), broken, checks); ok {
		t.Fatal("a draft was built from a question that cannot be read")
	}
	// Leaving rule mode drops the rule.
	values["gate-mode"] = "review"
	if d, err = chatgateEditedDefinition(chatgateClientDefinition(), values, checks); err != nil || len(d.Rules) != 0 {
		t.Fatalf("review mode kept rules: %+v %v", d.Rules, err)
	}
}

// TestTodo_CHATGATE_005_Validation: the form checks its answers before they
// are sent and says, per question, what is wrong.
func TestTodo_CHATGATE_005_Validation(t *testing.T) {
	d := chatgateClientDefinition()
	problems := chatgateSubmitProblems("en-US", d, map[string][]string{"team": {"Marketing"}, "why": {"  "}})
	if problems["team"] != "Choose a valid answer to this question." || problems["why"] != "Answer this question to continue." || len(problems) != 2 {
		t.Fatalf("problems=%v", problems)
	}
	// A question the document did not hold at all is a missing answer.
	if problems = chatgateSubmitProblems("en-US", d, map[string][]string{"team": {"Payroll"}}); problems["why"] == "" || len(problems) != 1 {
		t.Fatalf("a missing required answer: %v", problems)
	}
	if problems = chatgateSubmitProblems("en-US", d, map[string][]string{"team": {"Payroll"}, "why": {"A payslip question"}}); len(problems) != 0 {
		t.Fatalf("good answers are refused: %v", problems)
	}
	for _, locale := range []string{"de-DE", "ar"} {
		if got := chatgateSubmitProblems(locale, d, map[string][]string{"team": {""}, "why": {""}}); len(got) != 2 || got["why"] == "Answer this question to continue." || got["why"] == "" {
			t.Errorf("%s: %v", locale, got)
		}
	}
}

// TestTodo_CHATGATE_005_Joins: the list of gates the server sends becomes what
// Browse and the banner show, and a channel the list does not hold is known to
// have no gate.
func TestTodo_CHATGATE_005_Joins(t *testing.T) {
	by := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	raw, _ := json.Marshal([]map[string]any{
		{"conversation": "gated", "questions": 3, "purpose": "Routing", "channel_purpose": "Payroll questions", "mode": "review"},
		{"conversation": "mine", "questions": 2, "mode": "rule", "member": true, "answer_again": true, "answer_by": by},
		{"conversation": "", "questions": 4},
		{"conversation": "empty", "questions": 0},
	})
	joins, ok := chatgateJoins(raw)
	if !ok || len(joins) != 2 {
		t.Fatalf("joins=%+v ok=%v", joins, ok)
	}
	if got := joins["gated"]; got.Questions != 3 || got.Purpose != "Routing" || got.ChannelPurpose != "Payroll questions" || got.Mode != "review" || got.AnswerAgain {
		t.Fatalf("gated=%+v", got)
	}
	if got := joins["mine"]; !got.AnswerAgain || !got.AnswerBy.Equal(by) {
		t.Fatalf("mine=%+v", got)
	}
	for _, bad := range []string{"", "null-ish", `{"conversation":"x"}`} {
		if _, ok := chatgateJoins(json.RawMessage(bad)); ok {
			t.Errorf("a list that is not a list was read: %q", bad)
		}
	}
	if empty, ok := chatgateJoins(json.RawMessage(`[]`)); !ok || empty == nil || len(empty) != 0 {
		t.Fatalf("an empty list is not a list that was read: %v %v", empty, ok)
	}
	// Known to be ungated only once the list was read.
	if chatgateKnownUngated(nil, "open") || !chatgateKnownUngated(joins, "open") || chatgateKnownUngated(joins, "gated") {
		t.Fatal("what the page knows about ungated channels is wrong")
	}
	if chatgateListAction == "get" || chatgateListAction == "" {
		t.Fatal("the list read is not its own action")
	}
}
