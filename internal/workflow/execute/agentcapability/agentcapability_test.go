package agentcapability

import (
	"context"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/conformance/triage"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/capabilityrunner"
)

func strType() workflow.ValueType { return workflow.ValueType{Kind: workflow.KindString} }

func testGovernance() Governance {
	return Governance{
		OwnerDomain:          "agents",
		RiskClass:            "AGENT_READ_DRAFT",
		IdempotencyPolicyRef: "policy:idempotency/cause-key/v1",
		AuthZScopeRef:        "scope:agents.invoke",
		LegalBasisRef:        "legal:legitimate-interest/v1",
		EntitlementRef:       "ent:agent-capability/v1",
		SLOClassRef:          "slo:interactive/v1",
		TestRef:              "TestTodo_WF_CTRL_005",
	}
}

func testAgent() PublishedAgent {
	return PublishedAgent{
		AgentID:   "case-triage",
		Version:   3,
		Status:    AgentStatusActive,
		Evaluated: true,
		InputSchema: capability.SchemaRef{
			SchemaID: "agent.case-triage.input", Version: 1,
			ProtobufFullName: "hcmnext.agents.casetriage.v1.Input",
		},
		OutputSchema: capability.SchemaRef{
			SchemaID: "agent.case-triage.output", Version: 1,
			ProtobufFullName: "hcmnext.agents.casetriage.v1.Output",
		},
		DataScope: capability.DataDomainFieldSet{
			DataDomains: []string{"hr.cases"},
			FieldPaths:  []string{"hr.cases.subject", "hr.cases.messages"},
		},
		Tools:            []string{"cases.read", "taxonomy.cite"},
		Budget:           Budget{MaxCostMinor: 500, Currency: "USD", MaxTokens: 8000},
		ServicePrincipal: "service:agent/case-triage",
		OutputFields: []workflow.Field{
			{Path: "route", Type: strType()},
			{Path: "summary", Type: strType()},
		},
	}
}

type stubSink struct{ n int }

func (s *stubSink) RecordInvocation(ctx context.Context, evt capability.InvocationEvidence) (string, error) {
	return s.RecordInvocationTx(ctx, evt)
}

func (s *stubSink) RecordInvocationTx(_ context.Context, _ capability.InvocationEvidence) (string, error) {
	s.n++
	return "ev-agent", nil
}

type fakeRunner struct {
	starts   int
	attaches int
	last     AgentRunRequest
	result   AgentRunResult
	startErr error
}

func (f *fakeRunner) Start(_ context.Context, req AgentRunRequest) (AgentRunResult, error) {
	f.starts++
	f.last = req
	if f.startErr != nil {
		return AgentRunResult{}, f.startErr
	}
	out := f.result
	if out.RunID == "" {
		out.RunID = "run-1"
	}
	return out, nil
}

func (f *fakeRunner) Attach(_ context.Context, runID string) (AgentRunResult, error) {
	f.attaches++
	out := f.result
	out.RunID = runID
	return out, nil
}

func succeededResult() AgentRunResult {
	return AgentRunResult{
		Verdict: VerdictSucceeded,
		Outputs: map[string]TypedText{
			"route":   {Type: strType(), Text: "GOVERNED_CREATE"},
			"summary": {Type: strType(), Text: "standard leave question"},
		},
		Reads: []string{"hr.cases:case-9"},
		Cost:  "USD 0.02",
	}
}

func testCall(agent PublishedAgent) capabilityrunner.CapabilityCall {
	return capabilityrunner.CapabilityCall{
		NodeID:     "triage_case",
		Capability: capability.Key{ID: CapabilityID(agent.AgentID), Version: agent.Version},
		Inputs: map[string]capabilityrunner.ResolvedValue{
			"case_id": {Type: strType(), Text: "case-9"},
		},
		Outputs: agent.OutputFields,
	}
}

// TestTodo_WF_CTRL_005 proves the PRIMARY matrix of WF-CTRL-005: a published,
// evaluated agent version registers as an agent-eligible capability whose
// manifest is generated from its own bindings, a CAPABILITY node names it
// with typed input mappings, and the gateway starts one agent run tied to the
// workflow node whose schema-validated, grounded, taint-marked result maps to
// the capability outcomes.
func TestTodo_WF_CTRL_005(t *testing.T) {
	agent := testAgent()
	gov := testGovernance()

	def, err := ManifestDefinition(agent, gov)
	if err != nil {
		t.Fatalf("ManifestDefinition: %v", err)
	}
	if def.ID != "workflow.agent.case-triage" || def.Version != 3 {
		t.Fatalf("manifest identity = %s/v%d, want workflow.agent.case-triage/v3", def.ID, def.Version)
	}
	if !def.AgentEligible {
		t.Fatal("manifest is not agent eligible")
	}
	if def.RequestSchema != agent.InputSchema || def.ResponseSchema != agent.OutputSchema {
		t.Fatal("manifest schemas are not the agent's own bindings")
	}
	if def.EffectClass != capability.EffectReadOnly {
		t.Fatalf("default agent effect = %s, want READ_ONLY (read and draft only)", def.EffectClass)
	}
	if len(def.WriteData.DataDomains) != 0 || len(def.WriteData.FieldPaths) != 0 {
		t.Fatalf("default agent declares writes: %+v", def.WriteData)
	}

	registry := capability.NewRegistry()
	runner := &fakeRunner{result: succeededResult()}
	ledger := NewMemoryLedger()
	deps := Deps{
		Runner: runner,
		Ledger: ledger,
		Policy: RunAccessPolicy{
			AllowedDomains: []string{"hr.cases", "hr.payroll"},
			AllowedFields:  []string{"hr.cases.subject", "hr.cases.messages", "hr.payroll.net_pay"},
		},
	}
	if err := RegisterAgentCapability(registry, agent, gov, deps); err != nil {
		t.Fatalf("RegisterAgentCapability: %v", err)
	}

	gateway := capability.NewGateway(registry, &stubSink{})
	invoke := func() capabilityrunner.CapabilityAnswer {
		t.Helper()
		res, err := gateway.Invoke(context.Background(), capability.InvokeRequest{
			Capability: capability.Key{ID: def.ID, Version: def.Version},
			Payload:    testCall(agent),
			Authorization: capability.Authorization{
				Decision: capability.Allow, Scopes: []string{gov.AuthZScopeRef},
				Reason: "test", SubjectRef: "test:workflow", Tenant: "acme",
			},
		})
		if err != nil {
			t.Fatalf("gateway Invoke: %v", err)
		}
		answer, ok := res.Response.(capabilityrunner.CapabilityAnswer)
		if !ok {
			t.Fatalf("response is %T, want capabilityrunner.CapabilityAnswer", res.Response)
		}
		return answer
	}

	first := invoke()
	if first.Outcome != workflow.OutcomeSucceeded {
		t.Fatalf("outcome = %s, want SUCCEEDED", first.Outcome)
	}
	if first.Outputs["route"].Text != "GOVERNED_CREATE" {
		t.Fatalf("route = %q, want the agent's schema-validated output", first.Outputs["route"].Text)
	}
	for _, want := range []string{"taint=AGENT_PRODUCED", "hr.cases:case-9", "USD 0.02", "run-1"} {
		if !strings.Contains(first.Detail, want) {
			t.Fatalf("detail %q misses %q: timeline must show what the run read, produced and cost", first.Detail, want)
		}
	}
	if len(runner.last.Scope.DataDomains) != 1 || runner.last.Scope.DataDomains[0] != "hr.cases" {
		t.Fatalf("run scope = %+v, want the intersection (hr.cases only, not hr.payroll)", runner.last.Scope)
	}
	if runner.last.ServicePrincipal != agent.ServicePrincipal {
		t.Fatalf("run principal = %q, want the agent's service principal %q", runner.last.ServicePrincipal, agent.ServicePrincipal)
	}
	if runner.last.CauseID == "" {
		t.Fatal("run carries no cause identifier")
	}

	// A retry of the node is one run and one charge: the second identical
	// call attaches to the recorded run instead of starting another.
	second := invoke()
	if second.Outcome != workflow.OutcomeSucceeded {
		t.Fatalf("retry outcome = %s, want SUCCEEDED", second.Outcome)
	}
	if runner.starts != 1 {
		t.Fatalf("starts = %d, want 1: retries of the node are one run and one charge", runner.starts)
	}
	if runner.attaches != 1 {
		t.Fatalf("attaches = %d, want 1: the retry resumes the recorded run", runner.attaches)
	}
	if !strings.Contains(second.Detail, "run-1") {
		t.Fatalf("retry detail %q does not reference the one run", second.Detail)
	}
}

// TestTodo_WF_CTRL_005_Property proves cause stability, scope intersection
// and verdict mapping behave as total functions over their inputs.
func TestTodo_WF_CTRL_005_Property(t *testing.T) {
	agent := testAgent()
	call := testCall(agent)

	if got := CauseOf(call); got != CauseOf(call) {
		t.Fatal("cause is not stable for identical calls")
	}
	other := call
	other.NodeID = "other_node"
	if CauseOf(call) == CauseOf(other) {
		t.Fatal("distinct nodes share a cause identifier")
	}
	other = call
	other.Inputs = map[string]capabilityrunner.ResolvedValue{"case_id": {Type: strType(), Text: "case-10"}}
	if CauseOf(call) == CauseOf(other) {
		t.Fatal("distinct inputs share a cause identifier")
	}

	agentScope := capability.DataDomainFieldSet{
		DataDomains: []string{"hr.cases", "hr.time"},
		FieldPaths:  []string{"hr.cases.subject", "hr.time.punches"},
	}
	policy := RunAccessPolicy{
		AllowedDomains: []string{"hr.cases", "hr.payroll"},
		AllowedFields:  []string{"hr.cases.subject", "hr.payroll.net_pay"},
	}
	got := IntersectScope(agentScope, policy)
	if len(got.DataDomains) != 1 || got.DataDomains[0] != "hr.cases" {
		t.Fatalf("domains = %v, want [hr.cases]", got.DataDomains)
	}
	if len(got.FieldPaths) != 1 || got.FieldPaths[0] != "hr.cases.subject" {
		t.Fatalf("fields = %v, want [hr.cases.subject]", got.FieldPaths)
	}
	// Intersection is symmetric in membership: swapping the inputs keeps the
	// same members.
	flipped := IntersectScope(capability.DataDomainFieldSet{
		DataDomains: policy.AllowedDomains, FieldPaths: policy.AllowedFields,
	}, RunAccessPolicy{AllowedDomains: agentScope.DataDomains, AllowedFields: agentScope.FieldPaths})
	if len(flipped.DataDomains) != 1 || len(flipped.FieldPaths) != 1 {
		t.Fatalf("flipped intersection = %+v, want the same members", flipped)
	}

	for verdict, want := range map[AgentVerdict]workflow.Outcome{
		VerdictSucceeded:      workflow.OutcomeSucceeded,
		VerdictRejected:       workflow.OutcomeRejected,
		VerdictUnknown:        workflow.OutcomeUnknown,
		VerdictAmbiguous:      workflow.OutcomeAmbiguous,
		AgentVerdict("bogus"): workflow.OutcomeUnknown,
	} {
		if got := MapVerdict(verdict); got != want {
			t.Fatalf("MapVerdict(%q) = %s, want %s", verdict, got, want)
		}
	}
}

// TestTodo_WF_CTRL_005_Security proves a workflow cannot give an agent
// authority its own published bindings lack, suspended or retired versions
// fail closed at compile time and at run time, and the agent never receives
// the workflow's credentials.
func TestTodo_WF_CTRL_005_Security(t *testing.T) {
	agent := testAgent()

	// A policy broader than the agent's bindings still runs inside the
	// agent's bindings.
	registry := capability.NewRegistry()
	runner := &fakeRunner{result: succeededResult()}
	deps := Deps{
		Runner: runner,
		Ledger: NewMemoryLedger(),
		Policy: RunAccessPolicy{
			AllowedDomains: []string{"hr.cases", "hr.payroll", "hr.compensation"},
			AllowedFields:  []string{"hr.cases.subject", "hr.cases.messages", "hr.payroll.net_pay"},
		},
	}
	if err := RegisterAgentCapability(registry, agent, testGovernance(), deps); err != nil {
		t.Fatalf("RegisterAgentCapability: %v", err)
	}
	if _, found := registry.Lookup(capability.Key{ID: CapabilityID(agent.AgentID), Version: agent.Version}); !found {
		t.Fatal("agent capability is not registered")
	}
	gateway := capability.NewGateway(registry, &stubSink{})
	if _, err := gateway.Invoke(context.Background(), capability.InvokeRequest{
		Capability: capability.Key{ID: CapabilityID(agent.AgentID), Version: agent.Version},
		Payload:    testCall(agent),
		Authorization: capability.Authorization{
			Decision: capability.Allow, Scopes: []string{"scope:agents.invoke"},
			Reason: "test", SubjectRef: "test:workflow", Tenant: "acme",
		},
	}); err != nil {
		t.Fatalf("gateway Invoke: %v", err)
	}
	for _, d := range runner.last.Scope.DataDomains {
		if d != "hr.cases" {
			t.Fatalf("run scope grants %q, which the agent's bindings lack", d)
		}
	}
	for _, f := range runner.last.Scope.FieldPaths {
		if f != "hr.cases.subject" && f != "hr.cases.messages" {
			t.Fatalf("run scope grants field %q, which the agent's bindings lack", f)
		}
	}
	// The run request carries the agent's service principal and the mapped
	// inputs only: there is no credential field for the workflow's
	// credentials to travel in.
	if runner.last.ServicePrincipal != agent.ServicePrincipal {
		t.Fatalf("run principal = %q, want %q", runner.last.ServicePrincipal, agent.ServicePrincipal)
	}
	if len(runner.last.Inputs) != 1 || runner.last.Inputs["case_id"].Text != "case-9" {
		t.Fatalf("run inputs = %+v, want only the node's typed mappings", runner.last.Inputs)
	}

	// Suspended or retired versions fail closed at compile time ...
	for _, status := range []AgentStatus{AgentStatusSuspended, AgentStatusRetired} {
		bad := agent
		bad.Status = status
		if err := CompileCheck(bad, true); err == nil {
			t.Fatalf("CompileCheck accepted a %s agent version", status)
		}
		if _, err := ManifestDefinition(bad, testGovernance()); err == nil {
			t.Fatalf("ManifestDefinition generated a manifest for a %s agent version", status)
		}
	}
	// ... and at run time, even when the suspension lands after registration.
	flipped := agent
	flippedRegistry := capability.NewRegistry()
	flippedRunner := &fakeRunner{result: succeededResult()}
	flippedDeps := Deps{
		Runner: flippedRunner, Ledger: NewMemoryLedger(),
		StatusOf: func(PublishedAgent) AgentStatus { return AgentStatusSuspended },
	}
	if err := RegisterAgentCapability(flippedRegistry, flipped, testGovernance(), flippedDeps); err != nil {
		t.Fatalf("RegisterAgentCapability: %v", err)
	}
	flippedGateway := capability.NewGateway(flippedRegistry, &stubSink{})
	if _, err := flippedGateway.Invoke(context.Background(), capability.InvokeRequest{
		Capability: capability.Key{ID: CapabilityID(agent.AgentID), Version: agent.Version},
		Payload:    testCall(agent),
		Authorization: capability.Authorization{
			Decision: capability.Allow, Scopes: []string{"scope:agents.invoke"},
			Reason: "test", SubjectRef: "test:workflow", Tenant: "acme",
		},
	}); err == nil {
		t.Fatal("runtime invoke accepted a suspended agent version")
	}
	if flippedRunner.starts != 0 {
		t.Fatal("suspended agent version started a run")
	}
}

// TestTodo_WF_CTRL_005_Integration proves the REFACTOR clause: the triage
// conformance fixture becomes one registered agent capability among others,
// and both serve through the same gateway.
func TestTodo_WF_CTRL_005_Integration(t *testing.T) {
	registry := capability.NewRegistry()
	telemetry := &triage.TelemetrySink{}
	triageAgent, triageDeps := TriageFixture(testProposer{}, telemetry, NewMemoryLedger())
	if err := RegisterAgentCapability(registry, triageAgent, testGovernance(), triageDeps); err != nil {
		t.Fatalf("register triage agent capability: %v", err)
	}
	other := testAgent()
	otherRunner := &fakeRunner{result: succeededResult()}
	otherDeps := Deps{Runner: otherRunner, Ledger: NewMemoryLedger()}
	if err := RegisterAgentCapability(registry, other, testGovernance(), otherDeps); err != nil {
		t.Fatalf("register second agent capability: %v", err)
	}
	if got := len(registry.List()); got != 2 {
		t.Fatalf("registry holds %d capabilities, want 2 (triage among others)", got)
	}

	gateway := capability.NewGateway(registry, &stubSink{})
	invoke := func(agent PublishedAgent, call capabilityrunner.CapabilityCall) capabilityrunner.CapabilityAnswer {
		t.Helper()
		res, err := gateway.Invoke(context.Background(), capability.InvokeRequest{
			Capability: capability.Key{ID: CapabilityID(agent.AgentID), Version: agent.Version},
			Payload:    call,
			Authorization: capability.Authorization{
				Decision: capability.Allow, Scopes: []string{"scope:agents.invoke"},
				Reason: "test", SubjectRef: "test:workflow", Tenant: "acme",
			},
		})
		if err != nil {
			t.Fatalf("gateway Invoke %s: %v", agent.AgentID, err)
		}
		answer, ok := res.Response.(capabilityrunner.CapabilityAnswer)
		if !ok {
			t.Fatalf("response is %T, want capabilityrunner.CapabilityAnswer", res.Response)
		}
		return answer
	}

	standard := capabilityrunner.CapabilityCall{
		NodeID:     "triage_standard",
		Capability: capability.Key{ID: CapabilityID(triageAgent.AgentID), Version: triageAgent.Version},
		Inputs: map[string]capabilityrunner.ResolvedValue{
			"subject":  {Type: strType(), Text: "leave balance question"},
			"taxonomy": {Type: strType(), Text: triage.TaxonomyStandardLeave},
			"message":  {Type: strType(), Text: "How much leave do I have?"},
		},
		Outputs: triageAgent.OutputFields,
	}
	answer := invoke(triageAgent, standard)
	if answer.Outcome != workflow.OutcomeSucceeded {
		t.Fatalf("triage standard outcome = %s, want SUCCEEDED", answer.Outcome)
	}
	if answer.Outputs["route"].Text != triage.RouteGovernedCreate {
		t.Fatalf("triage route = %q, want GOVERNED_CREATE", answer.Outputs["route"].Text)
	}
	if !strings.Contains(answer.Detail, "taint=AGENT_PRODUCED") {
		t.Fatalf("triage detail %q is not taint-marked", answer.Detail)
	}
	if len(telemetry.Events()) == 0 {
		t.Fatal("triage fixture recorded no telemetry")
	}

	specialist := standard
	specialist.NodeID = "triage_specialist"
	specialist.Inputs = map[string]capabilityrunner.ResolvedValue{
		"subject":  {Type: strType(), Text: "retaliation concern"},
		"taxonomy": {Type: strType(), Text: triage.TaxonomySpecialRetaliation},
		"message":  {Type: strType(), Text: "I was moved after raising a concern."},
	}
	routed := invoke(triageAgent, specialist)
	if routed.Outcome != workflow.OutcomeRejected {
		t.Fatalf("triage specialist outcome = %s, want REJECTED (routed to specialist)", routed.Outcome)
	}

	second := invoke(other, testCall(other))
	if second.Outcome != workflow.OutcomeSucceeded {
		t.Fatalf("second agent outcome = %s, want SUCCEEDED", second.Outcome)
	}
}

type testProposer struct{}

func (testProposer) Propose(prompt string) (triage.Proposal, error) {
	if strings.Contains(prompt, "specialist/retaliation") {
		return triage.Proposal{Route: triage.RouteSpecialist, Taxonomy: triage.TaxonomySpecialRetaliation, Cited: true}, nil
	}
	return triage.Proposal{Route: triage.RouteGovernedCreate, Taxonomy: triage.TaxonomyStandardLeave, Cited: true}, nil
}

// TestTodo_WF_CTRL_005_Fault proves the crash points between node start, run
// start, run completion and node completion resume onto the one recorded run,
// and that kill switches on the agent, the capability and the tenant each stop
// new runs and refuse in-flight nodes.
func TestTodo_WF_CTRL_005_Fault(t *testing.T) {
	agent := testAgent()

	t.Run("crash_between_run_start_and_node_completion_resumes", func(t *testing.T) {
		ledger := NewMemoryLedger()
		runner := &fakeRunner{result: succeededResult()}
		deps := Deps{Runner: runner, Ledger: ledger}
		registry := capability.NewRegistry()
		if err := RegisterAgentCapability(registry, agent, testGovernance(), deps); err != nil {
			t.Fatalf("RegisterAgentCapability: %v", err)
		}
		// The first attempt started the run and crashed before the node
		// completed: only the ledger entry survives.
		call := testCall(agent)
		ledger.Record(CauseOf(call), "run-crashed")
		gateway := capability.NewGateway(registry, &stubSink{})
		res, err := gateway.Invoke(context.Background(), capability.InvokeRequest{
			Capability: capability.Key{ID: CapabilityID(agent.AgentID), Version: agent.Version},
			Payload:    call,
			Authorization: capability.Authorization{
				Decision: capability.Allow, Scopes: []string{"scope:agents.invoke"},
				Reason: "test", SubjectRef: "test:workflow", Tenant: "acme",
			},
		})
		if err != nil {
			t.Fatalf("resume Invoke: %v", err)
		}
		answer := res.Response.(capabilityrunner.CapabilityAnswer)
		if answer.Outcome != workflow.OutcomeSucceeded {
			t.Fatalf("resume outcome = %s, want SUCCEEDED", answer.Outcome)
		}
		if runner.starts != 0 || runner.attaches != 1 {
			t.Fatalf("starts=%d attaches=%d, want 0/1: resume must not start a second run", runner.starts, runner.attaches)
		}
		if !strings.Contains(answer.Detail, "run-crashed") {
			t.Fatalf("resume detail %q does not reference the recorded run", answer.Detail)
		}
	})

	t.Run("kill_switches_stop_new_runs_and_refuse_in_flight", func(t *testing.T) {
		killed := agent
		killed.KillSwitch = true
		registry := capability.NewRegistry()
		runner := &fakeRunner{result: succeededResult()}
		if err := RegisterAgentCapability(registry, killed, testGovernance(), Deps{Runner: runner, Ledger: NewMemoryLedger()}); err != nil {
			t.Fatalf("RegisterAgentCapability: %v", err)
		}
		gateway := capability.NewGateway(registry, &stubSink{})
		call := testCall(killed)
		invoke := func() error {
			_, err := gateway.Invoke(context.Background(), capability.InvokeRequest{
				Capability: capability.Key{ID: CapabilityID(killed.AgentID), Version: killed.Version},
				Payload:    call,
				Authorization: capability.Authorization{
					Decision: capability.Allow, Scopes: []string{"scope:agents.invoke"},
					Reason: "test", SubjectRef: "test:workflow", Tenant: "acme",
				},
			})
			return err
		}
		if err := invoke(); err == nil || !strings.Contains(err.Error(), "kill") {
			t.Fatalf("agent kill switch invoke err = %v, want a kill-switch refusal", err)
		}
		if runner.starts != 0 {
			t.Fatal("killed agent started a run")
		}

		capKilled := capability.NewRegistry()
		capRunner := &fakeRunner{result: succeededResult()}
		capDeps := Deps{
			Runner: capRunner, Ledger: NewMemoryLedger(),
			CapabilityKilled: func(capability.Key) bool { return true },
		}
		if err := RegisterAgentCapability(capKilled, agent, testGovernance(), capDeps); err != nil {
			t.Fatalf("RegisterAgentCapability: %v", err)
		}
		capGateway := capability.NewGateway(capKilled, &stubSink{})
		if _, err := capGateway.Invoke(context.Background(), capability.InvokeRequest{
			Capability: capability.Key{ID: CapabilityID(agent.AgentID), Version: agent.Version},
			Payload:    testCall(agent),
			Authorization: capability.Authorization{
				Decision: capability.Allow, Scopes: []string{"scope:agents.invoke"},
				Reason: "test", SubjectRef: "test:workflow", Tenant: "acme",
			},
		}); err == nil || !strings.Contains(err.Error(), "kill") {
			t.Fatalf("capability kill switch invoke err = %v, want a kill-switch refusal", err)
		}

		tenantKilled := capability.NewRegistry()
		tenantRunner := &fakeRunner{result: succeededResult()}
		tenantDeps := Deps{
			Runner: tenantRunner, Ledger: NewMemoryLedger(), Tenant: "acme",
			TenantKilled: func(string) bool { return true },
		}
		if err := RegisterAgentCapability(tenantKilled, agent, testGovernance(), tenantDeps); err != nil {
			t.Fatalf("RegisterAgentCapability: %v", err)
		}
		tenantGateway := capability.NewGateway(tenantKilled, &stubSink{})
		if _, err := tenantGateway.Invoke(context.Background(), capability.InvokeRequest{
			Capability: capability.Key{ID: CapabilityID(agent.AgentID), Version: agent.Version},
			Payload:    testCall(agent),
			Authorization: capability.Authorization{
				Decision: capability.Allow, Scopes: []string{"scope:agents.invoke"},
				Reason: "test", SubjectRef: "test:workflow", Tenant: "acme",
			},
		}); err == nil || !strings.Contains(err.Error(), "kill") {
			t.Fatalf("tenant kill switch invoke err = %v, want a kill-switch refusal", err)
		}
	})
}

// TestTodo_WF_CTRL_005_Conformance proves the compiler half: an agent with
// side-effecting skills states that in its manifest and is refused unless the
// path to it passes a gate, while the read-and-draft default compiles
// anywhere.
func TestTodo_WF_CTRL_005_Conformance(t *testing.T) {
	plain := testAgent()
	if err := CompileCheck(plain, false); err != nil {
		t.Fatalf("read-and-draft agent without a gate: %v", err)
	}
	def, err := ManifestDefinition(plain, testGovernance())
	if err != nil {
		t.Fatalf("ManifestDefinition: %v", err)
	}
	if def.EffectClass != capability.EffectReadOnly {
		t.Fatalf("default effect = %s, want READ_ONLY", def.EffectClass)
	}

	writer := testAgent()
	writer.AgentID = "case-writer"
	writer.SideEffectingSkills = true
	if err := CompileCheck(writer, false); err == nil {
		t.Fatal("side-effecting agent compiled without a gate on its path")
	}
	if err := CompileCheck(writer, true); err != nil {
		t.Fatalf("gated side-effecting agent refused: %v", err)
	}
	wdef, err := ManifestDefinition(writer, testGovernance())
	if err != nil {
		t.Fatalf("ManifestDefinition: %v", err)
	}
	if wdef.EffectClass != capability.EffectInternalMutation {
		t.Fatalf("side-effecting effect = %s, want INTERNAL_MUTATION stated in the manifest", wdef.EffectClass)
	}
	if len(wdef.WriteData.DataDomains) == 0 {
		t.Fatal("side-effecting manifest declares no write scope")
	}

	unevaluated := testAgent()
	unevaluated.Evaluated = false
	if err := CompileCheck(unevaluated, true); err == nil {
		t.Fatal("unevaluated agent compiled")
	}
}
