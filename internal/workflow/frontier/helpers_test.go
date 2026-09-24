package frontier_test

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
)

var update = flag.Bool("update", false, "rewrite the checked-in golden vectors")

// echoHandler is the placeholder implementation every fixture capability is
// bound to. Nothing in this package invokes a handler; a capability registry
// simply refuses to publish a definition with no binding.
func echoHandler(_ context.Context, payload any) (any, error) { return payload, nil }

// fixtureSchema mirrors the BOOTSTRAP registry's schema naming so a fixture
// capability and the reference workflow agree on schema identity.
func fixtureSchema(id, slot string) capability.SchemaRef {
	return capability.SchemaRef{
		SchemaID:         id + "." + slot + "/v1",
		Version:          1,
		ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition",
	}
}

// fixtureCapability returns a publishable read-only capability definition.
func fixtureCapability(id, domain string) capability.Definition {
	return capability.Definition{
		ID:                   id,
		Version:              1,
		OwnerDomain:          domain,
		RequestSchema:        fixtureSchema(id, "request"),
		ResponseSchema:       fixtureSchema(id, "response"),
		ErrorSchema:          fixtureSchema(id, "error"),
		EffectClass:          capability.EffectReadOnly,
		ReadData:             capability.DataDomainFieldSet{DataDomains: []string{domain}},
		RiskClass:            "LOW",
		IdempotencyPolicyRef: "idempotency.read-safe.v1",
		AuthZScopeRef:        "scope:" + domain + ".read",
		LegalBasisRef:        "legal.p1a.observation-only.v1",
		EntitlementRef:       "entitlement.pilot.p1a.v1",
		SLOClassRef:          "slo.interactive.p95-2s.v1",
		TestRef:              "conformance:" + id + "/v1",
	}
}

// promotionRegistry publishes exactly the capability versions the promotion
// reference workflow binds. Compiling against this fixed table rather than the
// live bootstrap registry keeps a checked-in golden a statement about
// advancement, not about another package's capability catalogue.
func promotionRegistry(t *testing.T) *capability.Registry {
	t.Helper()
	r := capability.NewRegistry()
	domains := map[string]string{
		"hcmnext.people.explain_worker_state":        "people",
		"hcmnext.rewards.simulate_compensation":      "rewards",
		"hcmnext.rewards.evaluate_pay_band_position": "rewards",
		"hcmnext.operations.detect_drift":            "operations",
	}
	for _, key := range workflow.PromotionCapabilities() {
		domain, ok := domains[key.ID]
		if !ok {
			t.Fatalf("fixture registry has no domain for %s", key)
		}
		if err := r.Register(fixtureCapability(key.ID, domain), echoHandler); err != nil {
			t.Fatalf("publish fixture capability %s: %v", key, err)
		}
	}
	return r
}

// promotionPlan compiles the promotion reference workflow, which is the plan
// every test in this package advances through.
func promotionPlan(t *testing.T) *workflow.CompiledWorkflow {
	t.Helper()
	plan, err := workflow.Compile(workflow.PromotionReferenceDefinition(), workflow.Options{
		Phase: workflow.PhaseP1A, Capabilities: promotionRegistry(t), IRSchemaVersion: 1,
	})
	if err != nil {
		t.Fatalf("promotion reference must compile: %v", err)
	}
	return plan
}

// seed builds the initial state of one promotion instance.
func seed(t *testing.T, plan *workflow.CompiledWorkflow, decls ...frontier.JoinDeclaration) frontier.InstanceState {
	t.Helper()
	state, err := frontier.Seed(plan, "wf-instance-024", decls...)
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	return state
}

// step advances one node and fails on any refusal.
func step(t *testing.T, plan *workflow.CompiledWorkflow, state frontier.InstanceState, out frontier.NodeOutcome) frontier.Transition {
	t.Helper()
	tr, err := frontier.Advance(plan, state, out)
	if err != nil {
		t.Fatalf("Advance(%s): %v", out.NodeID, err)
	}
	return tr
}

// refuses advances one node, requires a refusal and asserts its code.
func refuses(t *testing.T, plan *workflow.CompiledWorkflow, state frontier.InstanceState, out frontier.NodeOutcome, code string) error {
	t.Helper()
	tr, err := frontier.Advance(plan, state, out)
	if err == nil {
		t.Fatalf("expected refusal %s, got transition %s", code, tr.Digest())
	}
	if got := frontier.CodeOf(err); got != code {
		t.Fatalf("refusal code = %q, want %q (%v)", got, code, err)
	}
	if tr.Digest() != "" {
		t.Fatalf("a refused advancement returned a transition digest %q", tr.Digest())
	}
	return err
}

// promotionRoutes is the exceeds-threshold walk of the reference workflow: the
// route each node takes when Jane's promotion crosses the finance threshold,
// which is the path promote-into-management.md describes.
var promotionRoutes = []frontier.NodeOutcome{
	{NodeID: workflow.PromotionNodeSnapshotWorker, Outcome: workflow.OutcomeSucceeded, OutputDigest: "sha256:snapshot"},
	{NodeID: workflow.PromotionNodeSimulateComp, Outcome: workflow.OutcomeSucceeded, OutputDigest: "sha256:comp"},
	{NodeID: workflow.PromotionNodeEvaluateBand, Outcome: workflow.OutcomeSucceeded, OutputDigest: "sha256:band"},
	{NodeID: workflow.PromotionNodeBuildProposal, Outcome: workflow.OutcomeSucceeded, OutputDigest: "sha256:proposal"},
	{NodeID: workflow.PromotionNodeRaiseThreshold, Outcome: "EXCEEDS_THRESHOLD", OutputDigest: "sha256:threshold"},
	{NodeID: workflow.PromotionNodeEndApproval, OutputDigest: "sha256:terminal"},
}

// walk drives a whole outcome sequence and returns every transition it
// produced, stopping at the first refusal.
func walk(t *testing.T, plan *workflow.CompiledWorkflow, state frontier.InstanceState, outs []frontier.NodeOutcome) []frontier.Transition {
	t.Helper()
	out := make([]frontier.Transition, 0, len(outs))
	for _, o := range outs {
		tr := step(t, plan, state, o)
		out = append(out, tr)
		state = tr.Next
	}
	return out
}

// joinPlan is a hand-built compiled plan with a JOIN, used because the P1A
// compiler does not publish structural primitives
// (planning/specs/workflow-runtime.md, "P1 disposition": PARALLEL/JOIN are
// DESIGN and CONFORMANCE only). Advancement over a JOIN still has to be
// specified and tested now, because WF-RUN-024's RED case names it.
//
//	fan --TO_A--> branch_a --SUCCEEDED--> gate --SUCCEEDED--> end_join
//	    --TO_B--> branch_b --SUCCEEDED-->
func joinPlan() *workflow.CompiledWorkflow {
	terminal := workflow.Terminal{
		NodeID:        "end_join",
		TerminalCode:  "JOINED",
		RuntimeStatus: workflow.RuntimeBlocked,
		Dimensions: lifecycle.Dimensions{
			Request:     lifecycle.RequestSimulated,
			Execution:   lifecycle.ExecutionNotPlanned,
			Business:    lifecycle.BusinessNotStarted,
			Consistency: lifecycle.ConsistencyNotApplicable,
			Obligation:  lifecycle.ObligationNotApplicable,
		},
	}
	end := workflow.CompiledNode{ID: "end_join", Type: workflow.StepEnd, Terminal: &terminal}
	return &workflow.CompiledWorkflow{
		WorkflowID:  "hcmnext.workflows.join_fixture",
		Version:     1,
		StartNodeID: "fan",
		Nodes: []workflow.CompiledNode{
			{
				ID:     "fan",
				Type:   workflow.StepDecision,
				Routes: []string{"TO_A", "TO_B", "UNKNOWN"},
				Decision: &workflow.CompiledDecision{
					EvaluatorRef: "engines.decisiontable",
					Routes: []workflow.DecisionRoute{
						{Key: "TO_A", Predicate: "predicate.a"},
						{Key: "TO_B", Predicate: "predicate.b"},
					},
				},
			},
			{ID: "branch_a", Type: workflow.StepTransform, Routes: []string{"FAILED", "SUCCEEDED"}},
			{ID: "branch_b", Type: workflow.StepTransform, Routes: []string{"FAILED", "SUCCEEDED"}},
			{ID: "gate", Type: workflow.StepJoin, Routes: []string{"FAILED", "PARTIAL", "SUCCEEDED"}},
			end,
		},
		Edges: []workflow.Edge{
			{From: "fan", To: "branch_a", RouteKey: "TO_A"},
			{From: "fan", To: "branch_b", RouteKey: "TO_B"},
			{From: "fan", To: "end_join", RouteKey: "UNKNOWN"},
			{From: "branch_a", To: "gate", RouteKey: "SUCCEEDED"},
			{From: "branch_a", To: "end_join", RouteKey: "FAILED"},
			{From: "branch_b", To: "gate", RouteKey: "SUCCEEDED"},
			{From: "branch_b", To: "end_join", RouteKey: "FAILED"},
			{From: "gate", To: "end_join", RouteKey: "SUCCEEDED"},
			{From: "gate", To: "end_join", RouteKey: "PARTIAL"},
			{From: "gate", To: "end_join", RouteKey: "FAILED"},
		},
		Terminals: []workflow.Terminal{terminal},
	}
}

// bothBranchesReady is the state a PARALLEL runtime would hand a JOIN: two
// branches live at once. It is constructed rather than walked into, because
// the P1A compiler cannot publish a plan that fans out.
func bothBranchesReady(t *testing.T, plan *workflow.CompiledWorkflow, decls ...frontier.JoinDeclaration) frontier.InstanceState {
	t.Helper()
	state := seed(t, plan, decls...)
	state.Nodes = []frontier.NodeStatus{
		{NodeID: "branch_a", State: frontier.NodeReady},
		{NodeID: "branch_b", State: frontier.NodeReady},
		{NodeID: "fan", State: frontier.NodeSucceeded, RouteKey: "TO_A"},
	}
	state.Frontier = []string{"branch_a", "branch_b"}
	return state
}

// golden compares bytes against a checked-in vector, or rewrites it under
// -update. The checked-in bytes are the contract: a change to them is a change
// to every transition digest this package has ever minted.
func golden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s (regenerate with -update): %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("golden %s drifted\n got:\n%s\nwant:\n%s", path, got, want)
	}
}
