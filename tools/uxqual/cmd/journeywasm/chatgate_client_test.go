package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
)

func TestTodo_CHATGATE_005(t *testing.T) {
	d := chatgate.Definition{Fields: []chatgate.Field{{ID: "yes", Kind: "boolean"}, {ID: "choices", Kind: "multiple_choice"}, {ID: "text", Kind: "short_text", Required: true}}}
	v, e := chatgateValues(d, map[string][]string{"yes": {"false"}, "choices": {"A", "B"}, "text": {"typed on submit"}})
	if e != nil || string(v["yes"]) != "false" || string(v["choices"]) != `["A","B"]` || string(v["text"]) != `"typed on submit"` {
		t.Fatalf("DOM values %v %v", v, e)
	}
}
func TestTodo_CHATGATE_006(t *testing.T) {
	d := chatgate.Definition{Fields: []chatgate.Field{{ID: "team", KindVersion: "1.0.0", DataClass: "INTERNAL"}}}
	v, e := chatgateEditedDefinition(d, map[string]string{"gate-purpose": "Join", "gate-mode": "rule", "gate-editor-0-label": "Team", "gate-editor-0-kind": "single_choice", "gate-editor-0-purpose": "Routing", "gate-editor-0-days": "30", "gate-editor-0-options": "Payroll\nFinance", "gate-rule-field": "team", "gate-rule-values": "Payroll\nFinance", "gate-rule-reason": "Only these teams"}, map[string]bool{"gate-editor-0-required": true, "gate-editor-0-administrators": true})
	if e != nil || len(v.Rules) != 2 || len(v.Rules[0].When.Values) != 2 || v.Fields[0].RetentionDays != 30 {
		t.Fatalf("builder %+v %v", v, e)
	}
	if d.Fields[0].Label != "" {
		t.Fatal("builder mutated original")
	}
}
