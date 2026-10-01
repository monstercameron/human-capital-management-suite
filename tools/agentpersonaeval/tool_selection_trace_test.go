package agentpersonaeval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func toolTraceFixture(t *testing.T, events []ToolSelectionEvent) ToolSelectionTrace {
	t.Helper()
	trace, err := NewPersonaCaseToolSelectionTrace("synthetic-tenant-01", "persona-run-01", p21Persona(), p21Cases()[0], events)
	if err != nil {
		t.Fatal(err)
	}
	return trace
}

func digestForTrace(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func TestToolSelectionTraceSealsRuntimeObservationAndPolicy(t *testing.T) {
	trace := toolTraceFixture(t, []ToolSelectionEvent{
		{Sequence: 1, Kind: ToolSelectionProposal, ProposalName: "compensation.lookup"},
		{Sequence: 2, Kind: ToolSelectionAdmitted, Skill: "compensation.lookup", InvocationID: "invoke-1", ReceiptDigest: digestForTrace("admission")},
		{Sequence: 3, Kind: ToolSelectionExecuted, Skill: "compensation.lookup", InvocationID: "invoke-1", ReceiptDigest: digestForTrace("execution")},
	})
	if err := trace.Verify(); err != nil {
		t.Fatal(err)
	}
	if len(trace.Events) != 3 || trace.Events[2].Kind != ToolSelectionExecuted {
		t.Fatal("runtime event was not preserved in trace")
	}
	if trace.Binding.CaseID != "in-scope-comp" {
		t.Fatal("trace case binding was lost")
	}
	if !trace.MatchesPersonaCase(p21Persona(), p21Cases()[0]) || trace.MatchesPersonaCase(p21Persona(), p21Cases()[1]) {
		t.Fatal("trace did not bind to its exact persona case")
	}
	data, err := json.Marshal(trace)
	if err != nil {
		t.Fatal(err)
	}
	var restored ToolSelectionTrace
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	if err := restored.Verify(); err != nil || restored.Binding.RunID != "persona-run-01" {
		t.Fatalf("serialized trace did not verify: %v", err)
	}
}

func TestToolSelectionTraceProposalIsNotObservedSelection(t *testing.T) {
	trace := toolTraceFixture(t, []ToolSelectionEvent{{Sequence: 1, Kind: ToolSelectionProposal, ProposalName: "compensation.lookup"}})
	if len(trace.Events) != 1 || trace.Events[0].Kind != ToolSelectionProposal || trace.Events[0].Skill != "" {
		t.Fatal("model proposal was not kept separate from runtime event data")
	}
}

func TestToolSelectionTraceRetainsDeniedOutOfPolicyProposal(t *testing.T) {
	trace := toolTraceFixture(t, []ToolSelectionEvent{
		{Sequence: 1, Kind: ToolSelectionProposal, ProposalName: "payroll.run"},
		{Sequence: 2, Kind: ToolSelectionDenied, Skill: "payroll.run", InvocationID: "attempt-1", ReceiptDigest: digestForTrace("denial")},
	})
	if err := trace.Verify(); err != nil {
		t.Fatalf("server-side refusal of an unpinned skill was not recordable: %v", err)
	}
	if trace.Events[1].Kind != ToolSelectionDenied || trace.Events[1].Skill != "payroll.run" {
		t.Fatal("denied unpinned proposal was not retained as a denial record")
	}
}

func TestToolSelectionTraceRejectsUnboundOrForgedEvents(t *testing.T) {
	validBinding := ToolSelectionTraceBinding{SyntheticTenantID: "synthetic-tenant-01", RunID: "run-1", CaseID: "case-1", CaseDigest: digestForTrace("case"), PersonaDigest: digestForTrace("persona"), ModelDigest: digestForTrace("model")}
	for _, test := range []struct {
		name   string
		skills []string
		events []ToolSelectionEvent
	}{
		{name: "skill outside policy", skills: []string{"policy.lookup"}, events: []ToolSelectionEvent{{Sequence: 1, Kind: ToolSelectionExecuted, Skill: "compensation.lookup", InvocationID: "i", ReceiptDigest: digestForTrace("receipt")}}},
		{name: "execution without receipt", skills: []string{"compensation.lookup"}, events: []ToolSelectionEvent{{Sequence: 1, Kind: ToolSelectionExecuted, Skill: "compensation.lookup", InvocationID: "i"}}},
		{name: "execution without admission", skills: []string{"compensation.lookup"}, events: []ToolSelectionEvent{{Sequence: 1, Kind: ToolSelectionExecuted, Skill: "compensation.lookup", InvocationID: "i", ReceiptDigest: digestForTrace("receipt")}}},
		{name: "proposal carrying observed skill", skills: []string{"compensation.lookup"}, events: []ToolSelectionEvent{{Sequence: 1, Kind: ToolSelectionProposal, ProposalName: "compensation.lookup", Skill: "compensation.lookup"}}},
		{name: "duplicate policy skill", skills: []string{"compensation.lookup", "compensation.lookup"}},
		{name: "missing event evidence", skills: []string{"compensation.lookup"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := newToolSelectionTrace(validBinding, test.skills, test.events); err == nil {
				t.Fatal("invalid trace accepted")
			}
		})
	}
	if _, err := NewPersonaCaseToolSelectionTrace("production-tenant", "run-1", p21Persona(), p21Cases()[0], nil); err == nil {
		t.Fatal("non-synthetic tenant binding was accepted")
	}
	trace := toolTraceFixture(t, []ToolSelectionEvent{
		{Sequence: 1, Kind: ToolSelectionAdmitted, Skill: "compensation.lookup", InvocationID: "i", ReceiptDigest: digestForTrace("admission")},
		{Sequence: 2, Kind: ToolSelectionExecuted, Skill: "compensation.lookup", InvocationID: "i", ReceiptDigest: digestForTrace("receipt")},
	})
	trace.Binding.RunID = "other-run"
	if err := trace.Verify(); err == nil {
		t.Fatal("run-binding mutation retained a valid seal")
	}
}
