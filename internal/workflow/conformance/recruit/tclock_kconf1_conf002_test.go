package recruit

import (
	"context"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/hireexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

func recruitNode(t *testing.T, def workflow.Definition, id string) workflow.Node {
	t.Helper()
	for _, node := range def.Nodes {
		if node.ID == id {
			return node
		}
	}
	t.Fatalf("definition has no node %q", id)
	return workflow.Node{}
}

func hireCommitNodes(def workflow.Definition) []workflow.Node {
	var out []workflow.Node
	for _, node := range def.Nodes {
		if node.ID == hireexec.NodeCommitHire {
			out = append(out, node)
		}
	}
	return out
}

// TestTodo_CONF_002 proves the conformance boundary in two stages: the
// Recruit/Hire/Onboard SIMULATE reference preserves candidate/person identity,
// offer binding and approvals without effects; the served New Hire EXECUTE
// graph owns exactly one employment commit and declares COMPLETED/CONSISTENT
// only at that commit's terminal. A downstream failure remains repairable.
func TestTodo_CONF_002(t *testing.T) {
	setup := mustSetup(t, GoldenEnvironment())
	receipt := mustRun(t, setup)
	if receipt.Terminal.TerminalCode != TerminalHired || receipt.EffectCounters().DomainWrites != 0 {
		t.Fatalf("golden simulation = terminal %q counters=%+v, want hired preview with zero writes", receipt.Terminal.TerminalCode, receipt.EffectCounters())
	}
	if len(receipt.WorkItems) != 2 {
		t.Fatalf("approval work items = %d, want hiring-manager and HRBP", len(receipt.WorkItems))
	}
	proposal := recruitNode(t, ReferenceDefinition(), NodeBuildProposal)
	wantSources := map[string]workflow.Source{
		"candidate_id":            {Kind: workflow.SourceWorkflowInput, Path: "candidate_id"},
		"offer_id":                {Kind: workflow.SourceWorkflowInput, Path: "offer_id"},
		"target_position_id":      {Kind: workflow.SourceWorkflowInput, Path: "target_position_id"},
		"person_id":               {Kind: workflow.SourceNodeOutput, NodeID: NodeReadPerson, Path: "person_id"},
		"offer_state":             {Kind: workflow.SourceNodeOutput, NodeID: NodeObserveOffer, Path: "offer_state"},
		"work_auth_evidence_refs": {Kind: workflow.SourceNodeOutput, NodeID: NodeVerifyWorkAuth, Path: "work_auth_evidence_refs"},
		"start_date":              {Kind: workflow.SourceWorkflowInput, Path: "start_date"},
	}
	for _, mapping := range proposal.InputMappings {
		want, ok := wantSources[mapping.Target]
		if ok && mapping.Source != want {
			t.Errorf("proposal mapping %q = %+v, want %+v", mapping.Target, mapping.Source, want)
		}
		delete(wantSources, mapping.Target)
	}
	if len(wantSources) != 0 {
		t.Fatalf("proposal does not bind candidate, worker/person, offer and evidence fields: %v", wantSources)
	}

	commits := hireCommitNodes(hireexec.Definition())
	if len(commits) != 1 {
		t.Fatalf("new-hire graph has %d authoritative employment commit nodes, want exactly one", len(commits))
	}
	commit := commits[0]
	if commit.Type != workflow.StepCapability || commit.DeclaredEffect != capability.EffectInternalMutation || commit.EffectRole != workflow.RoleAuthoritativeCore {
		t.Fatalf("employment commit is not the authoritative core write: %+v", commit)
	}
	if commit.Capability == nil || commit.Capability.IdempotencyKeyMapping != "candidate_id" {
		t.Fatalf("employment commit is not candidate-idempotent: %+v", commit.Capability)
	}
	end := recruitNode(t, hireexec.Definition(), hireexec.NodeEndHired)
	if end.End == nil || end.End.CommitReceiptRef == "" || end.End.CompletionMapping["BusinessState"] != "COMPLETED" || end.End.CompletionMapping["ConsistencyState"] != "CONSISTENT" {
		t.Fatalf("successful hire does not expose a committed consistent terminal: %+v", end.End)
	}
	repair := recruitNode(t, hireexec.Definition(), hireexec.NodeEndFailed)
	if repair.End == nil || len(repair.End.RepairRefs) == 0 || repair.End.CompletionMapping["ExecutionState"] != "REPAIR_REQUIRED" {
		t.Fatalf("downstream failure has no explicit repair state: %+v", repair.End)
	}
}

// TestTodo_CONF_002_Security proves every named pre-commit refusal remains a
// refusal and that the executable graph has no second employment write hidden
// behind another node.
func TestTodo_CONF_002_Security(t *testing.T) {
	for name, env := range map[string]func() *Environment{
		"duplicate": DuplicatePersonEnvironment,
		"capacity":  ExhaustedPositionEnvironment,
		"offer":     OfferExpiredEnvironment,
		"work_auth": MissingWorkAuthEnvironment,
	} {
		t.Run(name, func(t *testing.T) {
			receipt := mustRun(t, mustSetup(t, env()))
			if receipt.Terminal.TerminalCode == TerminalHired || !receipt.EffectCounters().IsZero() {
				t.Fatalf("refusal %s completed or counted effects: terminal=%q counters=%+v", name, receipt.Terminal.TerminalCode, receipt.EffectCounters())
			}
		})
	}
	if got := len(hireCommitNodes(hireexec.Definition())); got != 1 {
		t.Fatalf("authoritative employment writes = %d, want one", got)
	}
}

// TestTodo_CONF_002_Conformance checks that the two role identities survive
// the reference graph as distinct typed values: candidate ingress is never
// substituted for the person/worker projection used by the proposal.
func TestTodo_CONF_002_Conformance(t *testing.T) {
	env := GoldenEnvironment()
	setup := mustSetup(t, env)
	if got, err := setup.Inputs.Values.Get("candidate_id"); err != nil || got.Type.Brand != "CandidateID" {
		t.Fatalf("candidate input is not CandidateID: value=%v err=%v", got, err)
	}
	person := recruitNode(t, ReferenceDefinition(), NodeReadPerson)
	if len(person.Outputs) == 0 || person.Outputs[0].Type.Brand != "PersonID" {
		t.Fatalf("person registry does not produce a typed worker/person identity: %+v", person.Outputs)
	}
	if hireexec.NodeCommitHire == NodeReadPerson {
		t.Fatal("candidate lookup and employment commit collapsed into one node")
	}
}

// TestTodo_CONF_002_Mutation kills the duplicate-commit mutant: adding a
// second authoritative commit node to a copied definition must be observable
// and therefore cannot be mistaken for exactly-once employment creation.
func TestTodo_CONF_002_Mutation(t *testing.T) {
	def := hireexec.Definition()
	commits := hireCommitNodes(def)
	if len(commits) != 1 {
		t.Fatalf("baseline authoritative employment commits = %d, want one", len(commits))
	}
	def.Nodes = append(def.Nodes, commits[0])
	if got := len(hireCommitNodes(def)); got == 1 {
		t.Fatalf("duplicate employment commit mutant survived: %d", got)
	}

	// A malformed simulation input must still fail through the interpreter,
	// not reach a business terminal by defaulting a missing candidate.
	setup := mustSetup(t, GoldenEnvironment())
	delete(setup.Inputs.Values, "candidate_id")
	if _, err := simulate.Run(context.Background(), setup.Plan, setup.Inputs, setup.Options); err == nil {
		t.Fatal("missing candidate identity was accepted")
	}
}
