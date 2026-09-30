package threatregister

import (
	"testing"
)

func mustLoadAgentExtension(t *testing.T) AgentExtension {
	t.Helper()
	r, err := LoadAgentExtension("../../../definitions/architecture/agent-threat-model.yaml")
	if err != nil {
		t.Fatalf("LoadAgentExtension: %v", err)
	}
	return *r
}

func TestTodo_AGENT2_002(t *testing.T) {
	r := mustLoadAgentExtension(t)
	if violations := r.Validate(); len(violations) != 0 {
		t.Fatalf("agent threat extension has %d violations: %v", len(violations), violations)
	}
	if len(r.Threats) != len(AllRegisteredAgentAttackClasses()) {
		t.Fatalf("got %d threats, want one per combined required class (%d)", len(r.Threats), len(AllRegisteredAgentAttackClasses()))
	}
	for _, th := range r.ThreatsForClasses(AllAgentAttackClasses()) {
		if len(th.ControlTodos) == 0 || th.RedTeamCase != "AGENT2-023" {
			t.Errorf("threat %s must map to a control and AGENT2-023, got controls=%v case=%q", th.ID, th.ControlTodos, th.RedTeamCase)
		}
	}
	for _, th := range r.ThreatsForClasses(AllPersonaAttackClasses()) {
		if len(th.ControlTodos) == 0 || th.RedTeamCase != "AGENTP-022" || th.RedTeamCaseID == "" {
			t.Errorf("persona threat %s must map to controls and an AGENTP-022 case, got controls=%v case=%q/%q", th.ID, th.ControlTodos, th.RedTeamCase, th.RedTeamCaseID)
		}
	}
}

func TestTodo_AGENT2_002_Golden(t *testing.T) {
	r := mustLoadAgentExtension(t)
	digest, err := r.Agent2CanonicalDigest()
	if err != nil {
		t.Fatalf("CanonicalDigest: %v", err)
	}
	const want = "46e39c8f9e88a13ca0338c4d65cecd0584196355c3908423498fbe71f498e9c4"
	if digest != want {
		t.Fatalf("canonical digest = %s, want %s", digest, want)
	}
}

func TestTodo_AGENT2_002_Security(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*AgentExtension)
	}{
		{"missing control", func(r *AgentExtension) { r.Threats[0].ControlTodos = nil }},
		{"missing red-team case", func(r *AgentExtension) { r.Threats[0].RedTeamCase = "" }},
		{"unknown control", func(r *AgentExtension) { r.Threats[0].ControlTodos = []string{"AGENT2-999"} }},
		{"missing attack class", func(r *AgentExtension) { r.Threats[0].AttackClass = "UNKNOWN" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := mustLoadAgentExtension(t)
			tc.mutate(&r)
			if got := r.Validate(); len(got) == 0 {
				t.Fatal("Validate accepted an extension missing a required security mapping")
			}
		})
	}
}
