package workflow_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/agentcapability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/agentgate"
)

// WF-CTRL-005-F1: the side-effecting agent gate, enforced by the real
// compiler. Matrix through workflow.Compile with the real adapter and the
// real CompileCheck: gated agents compile, ungated/inactive/unevaluated
// agents are refused with AGENT_GATE_REQUIRED, and non-agent definitions
// are unaffected.

const (
	gateAgentID      = "drafter"
	gateNodeApproval = "approve_gate"
	gateNodeDecision = "decide_gate"
)

func gateAgentCapID() string { return agentcapability.CapabilityID(gateAgentID) }

func gateTestAgent(sideEffecting bool) agentcapability.PublishedAgent {
	return agentcapability.PublishedAgent{
		AgentID:             gateAgentID,
		Version:             1,
		Status:              agentcapability.AgentStatusActive,
		Evaluated:           true,
		ServicePrincipal:    "service:agent/drafter",
		SideEffectingSkills: sideEffecting,
	}
}

// gateRegistry publishes the fixture capabilities plus the agent-backed
// capability the rebound node binds.
func gateRegistry(t *testing.T) *capability.Registry {
	t.Helper()
	r := effectsRegistry(t)
	// EXTERNAL mirrors the rebound node's original binding so unrelated
	// analyses stay quiet: the gate reads the published agent bindings,
	// not the capability effect class. AgentEligible stays false: it
	// permits agent initiators to invoke the capability, and output
	// validation is UNVALIDATED_AGENT_OUTPUT's territory, not the gate's.
	d := fixtureCapability(gateAgentCapID(), "intelligence", capability.EffectExternalMutation)
	if err := r.Register(d, echoHandler); err != nil {
		t.Fatalf("publish agent capability %s: %v", d.Key(), err)
	}
	return r
}

func gateOptions(t *testing.T, agents ...agentcapability.PublishedAgent) workflow.Options {
	t.Helper()
	return workflow.Options{
		Phase:        workflow.PhaseP1B,
		Capabilities: gateRegistry(t),
		AgentGate:    agentgate.NewPolicy(agents...),
	}
}

// agentGateBase rebinds the fixture's sync node to the agent-backed
// capability. The sync node keeps its retry budget, failure route and
// effect role; only the capability binding changes.
func agentGateBase(t *testing.T, def workflow.Definition) workflow.Definition {
	t.Helper()
	def.Limits.MaxFanOut = 8
	sync := nodeRef(t, &def, fxSync)
	sync.Capability.ID = gateAgentCapID()
	sync.Capability.Version = 1
	sync.Capability.AuthorityScopes = []string{"scope:intelligence.write"}
	sync.InputSchema = capSchema(gateAgentCapID(), "request")
	sync.OutputSchema = capSchema(gateAgentCapID(), "response")
	sync.Capability.EffectBinding = "intelligence.agent_draft"
	return def
}

func gateNodeInputs() ([]workflow.Field, []workflow.Mapping) {
	inputs := []workflow.Field{{Path: "worker_id", Type: brandedString("WorkerID")}}
	mappings := []workflow.Mapping{{Target: "worker_id", Source: nodeSource(fxRead, "worker_id")}}
	return inputs, mappings
}

// insertApprovalGate puts an APPROVAL node on the read->sync edge.
func insertApprovalGate(t *testing.T, def *workflow.Definition) {
	t.Helper()
	inputs, mappings := gateNodeInputs()
	def.Nodes = append(def.Nodes, workflow.Node{
		ID:             gateNodeApproval,
		Type:           workflow.StepApproval,
		DeclaredEffect: capability.EffectPure,
		InputSchema:    fixtureSchemaRef("AgentGateApprovalInput"),
		OutputSchema:   fixtureSchemaRef("AgentGateApprovalResult"),
		Inputs:         inputs,
		InputMappings:  mappings,
		Governance:     governedNode(workflow.RevalidatePreExecution),
	})
	edgeRef(t, def, fxRead, "SUCCEEDED").To = gateNodeApproval
	def.Edges = append(def.Edges,
		workflow.Edge{From: gateNodeApproval, To: fxSync, RouteKey: "APPROVED"},
		workflow.Edge{From: gateNodeApproval, To: fxEndRepair, RouteKey: "REJECTED"},
		workflow.Edge{From: gateNodeApproval, To: fxEndRepair, RouteKey: "INVALIDATED"},
		workflow.Edge{From: gateNodeApproval, To: fxEndRepair, RouteKey: "EXPIRED"},
		workflow.Edge{From: gateNodeApproval, To: fxEndRepair, RouteKey: "CANCELLED"},
	)
}

// insertDecisionGate puts a rule-carrying DECISION node on the read->sync
// edge. RULE is not a step type: the rule gate is a DECISION carrying the
// rule reference.
func insertDecisionGate(t *testing.T, def *workflow.Definition) {
	t.Helper()
	inputs, mappings := gateNodeInputs()
	def.Nodes = append(def.Nodes, workflow.Node{
		ID:             gateNodeDecision,
		Type:           workflow.StepDecision,
		DeclaredEffect: capability.EffectPure,
		InputSchema:    fixtureSchemaRef("AgentGateDecisionInput"),
		OutputSchema:   fixtureSchemaRef("AgentGateDecisionResult"),
		Inputs:         inputs,
		InputMappings:  mappings,
		Decision: &workflow.DecisionSpec{
			EvaluatorRef:       "evaluator.fixture.agentgate/v1",
			EvaluatorVersion:   1,
			RuleRef:            "rule.fixture.agentgate/v1",
			InputDigestProfile: "hcmnext.canonical.fixture.agentgate/v1",
			Routes: []workflow.DecisionRoute{
				{Key: "ALLOW", Predicate: "request_is_allowed", Precedence: 10},
				{Key: "DENY", Predicate: "request_is_denied", Precedence: 20},
				{Key: string(workflow.OutcomeUnknown), Predicate: "evaluation_unknown", Precedence: 30},
			},
			DefaultRoute: "DENY",
		},
		Governance: governedNode(workflow.RevalidatePreExecution),
	})
	edgeRef(t, def, fxRead, "SUCCEEDED").To = gateNodeDecision
	def.Edges = append(def.Edges,
		workflow.Edge{From: gateNodeDecision, To: fxSync, RouteKey: "ALLOW"},
		workflow.Edge{From: gateNodeDecision, To: fxEndRepair, RouteKey: "DENY"},
		workflow.Edge{From: gateNodeDecision, To: fxEndRepair, RouteKey: string(workflow.OutcomeUnknown)},
	)
}

func mustAccept(t *testing.T, def workflow.Definition, opts workflow.Options) {
	t.Helper()
	if _, err := workflow.Compile(def, opts); err != nil {
		t.Fatalf("expected compilation to be accepted, got %v", err)
	}
}

func TestTodo_WF_CTRL_005_F1(t *testing.T) {
	sideEffecting := gateTestAgent(true)
	readOnly := gateTestAgent(false)

	t.Run("accept approval gate on path", func(t *testing.T) {
		def := agentGateBase(t, effectsDefinition())
		insertApprovalGate(t, &def)
		mustAccept(t, def, gateOptions(t, sideEffecting))
	})

	t.Run("accept rule decision gate on path", func(t *testing.T) {
		def := agentGateBase(t, effectsDefinition())
		insertDecisionGate(t, &def)
		mustAccept(t, def, gateOptions(t, sideEffecting))
	})

	t.Run("accept read-only agent without a gate", func(t *testing.T) {
		def := agentGateBase(t, effectsDefinition())
		mustAccept(t, def, gateOptions(t, readOnly))
	})

	t.Run("accept non-agent definition unaffected", func(t *testing.T) {
		mustAccept(t, effectsDefinition(), gateOptions(t, sideEffecting))
	})

	t.Run("accept nil policy skips the check", func(t *testing.T) {
		def := agentGateBase(t, effectsDefinition())
		opts := gateOptions(t, sideEffecting)
		opts.AgentGate = nil
		mustAccept(t, def, opts)
	})

	t.Run("refuse ungated side-effecting agent", func(t *testing.T) {
		def := agentGateBase(t, effectsDefinition())
		mustReject(t, def, gateOptions(t, sideEffecting), workflow.CodeAgentGateRequired)
	})

	t.Run("refuse gate downstream of the agent", func(t *testing.T) {
		def := agentGateBase(t, effectsDefinition())
		inputs, mappings := gateNodeInputs()
		def.Nodes = append(def.Nodes, workflow.Node{
			ID:             gateNodeApproval,
			Type:           workflow.StepApproval,
			DeclaredEffect: capability.EffectPure,
			InputSchema:    fixtureSchemaRef("AgentGateApprovalInput"),
			OutputSchema:   fixtureSchemaRef("AgentGateApprovalResult"),
			Inputs:         inputs,
			InputMappings:  mappings,
			Governance:     governedNode(workflow.RevalidatePreExecution),
		})
		edgeRef(t, &def, fxSync, "SUCCEEDED").To = gateNodeApproval
		def.Edges = append(def.Edges,
			workflow.Edge{From: gateNodeApproval, To: fxObserve, RouteKey: "APPROVED"},
			workflow.Edge{From: gateNodeApproval, To: fxEndRepair, RouteKey: "REJECTED"},
			workflow.Edge{From: gateNodeApproval, To: fxEndRepair, RouteKey: "INVALIDATED"},
			workflow.Edge{From: gateNodeApproval, To: fxEndRepair, RouteKey: "EXPIRED"},
			workflow.Edge{From: gateNodeApproval, To: fxEndRepair, RouteKey: "CANCELLED"},
		)
		mustReject(t, def, gateOptions(t, sideEffecting), workflow.CodeAgentGateRequired)
	})

	t.Run("refuse bypass outcome path around the gate", func(t *testing.T) {
		def := agentGateBase(t, effectsDefinition())
		insertApprovalGate(t, &def)
		// A second outcome path reaches the agent without passing the
		// gate, so the gate no longer dominates it.
		edgeRef(t, &def, fxRead, "REJECTED").To = fxSync
		mustReject(t, def, gateOptions(t, sideEffecting), workflow.CodeAgentGateRequired)
	})

	t.Run("refuse inactive agent behind a gate", func(t *testing.T) {
		def := agentGateBase(t, effectsDefinition())
		insertApprovalGate(t, &def)
		suspended := sideEffecting
		suspended.Status = agentcapability.AgentStatusSuspended
		mustReject(t, def, gateOptions(t, suspended), workflow.CodeAgentGateRequired)
	})

	t.Run("refuse unevaluated agent behind a gate", func(t *testing.T) {
		def := agentGateBase(t, effectsDefinition())
		insertApprovalGate(t, &def)
		unevaluated := sideEffecting
		unevaluated.Evaluated = false
		mustReject(t, def, gateOptions(t, unevaluated), workflow.CodeAgentGateRequired)
	})
}
