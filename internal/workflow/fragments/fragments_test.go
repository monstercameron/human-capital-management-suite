package fragments

import (
	"context"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/documents"
	"github.com/monstercameron/human-capital-management-suite/internal/documents/signing"
	"github.com/monstercameron/human-capital-management-suite/internal/documents/template"
	"github.com/monstercameron/human-capital-management-suite/internal/messaging"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/capabilityrunner"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
)

const (
	fxReadWorker = "test.read_worker"
	fxScopeRead  = "scope:test.read"
	fxSchemaReq  = "test.read_worker.request/v1"
	fxSchemaResp = "test.read_worker.response/v1"
)

func fixtureReadDef() capability.Definition {
	return capability.Definition{
		ID:                   fxReadWorker,
		Version:              1,
		OwnerDomain:          "test",
		RequestSchema:        capability.SchemaRef{SchemaID: fxSchemaReq, Version: 1, ProtobufFullName: "test.ReadRequest"},
		ResponseSchema:       capability.SchemaRef{SchemaID: fxSchemaResp, Version: 1, ProtobufFullName: "test.ReadResponse"},
		ErrorSchema:          capability.SchemaRef{SchemaID: "test.read_worker.error/v1", Version: 1, ProtobufFullName: "test.ReadError"},
		EffectClass:          capability.EffectReadOnly,
		RiskClass:            "LOW",
		IdempotencyPolicyRef: "idempotency.read-safe.v1",
		AgentEligible:        false,
		AuthZScopeRef:        fxScopeRead,
		LegalBasisRef:        "legal.test.v1",
		EntitlementRef:       "entitlement.test.v1",
		SLOClassRef:          "slo.interactive.p95-2s.v1",
		TestRef:              "conformance:test.read_worker/v1",
	}
}

func fragmentGoverned() workflow.NodeGovernance {
	return workflow.NodeGovernance{
		Purpose:        "EXT015_FIXTURE",
		Classification: "CONFIDENTIAL_HR",
		RequiredDecisions: []workflow.GovernanceKind{
			workflow.GovernanceAuthZ, workflow.GovernanceLegal,
			workflow.GovernancePurpose, workflow.GovernanceRisk,
		},
		RevalidationBoundary:  workflow.RevalidatePreEffect,
		DataAccessManifestRef: "dam/ext015",
	}
}

func fragmentRegistry(t *testing.T) (*capability.Registry, *stubSender, documents.TemplateMap, *documents.CeremonyRegistry) {
	t.Helper()
	reg := capability.NewRegistry()
	if err := reg.Register(fixtureReadDef(), func(_ context.Context, _ any) (any, error) { return nil, nil }); err != nil {
		t.Fatalf("register read fixture: %v", err)
	}
	sender := &stubSender{}
	store := documents.TemplateMap{}
	ceremonies := documents.NewCeremonyRegistry()
	if err := RegisterInto(reg, sender, store, ceremonies); err != nil {
		t.Fatalf("RegisterInto: %v", err)
	}
	return reg, sender, store, ceremonies
}

func fragmentDefinition() workflow.Definition {
	str := workflow.ValueType{Kind: workflow.KindString}
	branded := workflow.ValueType{Kind: workflow.KindString, Brand: "WorkerID"}
	return workflow.Definition{
		WorkflowID: "wf.ext015.demo", Version: 1, Name: "EXT-015 demo",
		InputSchema:     workflow.SchemaRef{SchemaID: "wf.ext015.demo.input", Version: 1, ProtobufFullName: "wf.ext015.demo.Input"},
		OutputSchema:    workflow.SchemaRef{SchemaID: "wf.ext015.demo.result", Version: 1, ProtobufFullName: "wf.ext015.demo.Result"},
		VariablesSchema: workflow.SchemaRef{SchemaID: "wf.ext015.demo.vars", Version: 1, ProtobufFullName: "wf.ext015.demo.Vars"},
		Inputs: []workflow.Field{
			{Path: "worker_id", Type: branded},
			{Path: "signer_id", Type: str},
			{Path: "bindings_json", Type: str},
		},
		Outputs:           []workflow.Field{{Path: "worker_id", Type: branded}},
		TenantScope:       "acme",
		OrganizationScope: "acme/eng",
		RiskClass:         "HIGH",
		DeclaredModes:     []workflow.ExecutionMode{workflow.ModeExecute},
		TerminalProfile:   workflow.TerminalProfileExecute,
		Limits:            workflow.Limits{MaxFanOut: 4, MaxDepth: 16, MaxNodes: 32},
		StartNodeID:       "start",
		Nodes: []workflow.Node{{
			ID:           "start",
			Type:         workflow.StepCapability,
			InputSchema:  workflow.SchemaRef{SchemaID: fxSchemaReq, Version: 1, ProtobufFullName: "test.ReadRequest"},
			OutputSchema: workflow.SchemaRef{SchemaID: fxSchemaResp, Version: 1, ProtobufFullName: "test.ReadResponse"},
			Inputs:       []workflow.Field{{Path: "worker_id", Type: branded}},
			Outputs:      []workflow.Field{{Path: "worker_id", Type: branded}},
			InputMappings: []workflow.Mapping{{
				Target: "worker_id",
				Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: "worker_id", Type: branded},
			}},
			Capability: &workflow.CapabilityRef{ID: fxReadWorker, Version: 1,
				OperationMode: workflow.ModeExecute, AuthorityScopes: []string{fxScopeRead}},
			DeclaredEffect: capability.EffectReadOnly,
			Governance:     fragmentGoverned(),
		}, {
			ID:           "end",
			Type:         workflow.StepEnd,
			InputSchema:  workflow.SchemaRef{SchemaID: fxSchemaReq, Version: 1, ProtobufFullName: "test.ReadRequest"},
			OutputSchema: workflow.SchemaRef{SchemaID: fxSchemaResp, Version: 1, ProtobufFullName: "test.ReadResponse"},
			Inputs: []workflow.Field{
				{Path: "worker_id", Type: branded},
				{Path: "terminal_code", Type: str},
			},
			InputMappings: []workflow.Mapping{
				{Target: "worker_id", Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: "worker_id", Type: branded}},
				{Target: "terminal_code", Source: workflow.Source{Kind: workflow.SourceConstant, Type: str, Constant: "DONE"}},
			},
			End: &workflow.EndSpec{
				TerminalCode: "DONE", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping: map[string]string{
					"RequestState": "APPROVED", "ExecutionState": "COMMITTED", "BusinessState": "COMPLETED",
					"ConsistencyState": "CONSISTENT", "ObligationState": "SATISFIED",
				},
				CommitReceiptRef: "receipt.ext015/v1",
			},
			Governance: fragmentGoverned(),
		}, {
			ID:           "end_exhausted",
			Type:         workflow.StepEnd,
			InputSchema:  workflow.SchemaRef{SchemaID: fxSchemaReq, Version: 1, ProtobufFullName: "test.ReadRequest"},
			OutputSchema: workflow.SchemaRef{SchemaID: fxSchemaResp, Version: 1, ProtobufFullName: "test.ReadResponse"},
			Inputs: []workflow.Field{
				{Path: "worker_id", Type: branded},
				{Path: "terminal_code", Type: str},
			},
			InputMappings: []workflow.Mapping{
				{Target: "worker_id", Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: "worker_id", Type: branded}},
				{Target: "terminal_code", Source: workflow.Source{Kind: workflow.SourceConstant, Type: str, Constant: "EXHAUSTED"}},
			},
			End: &workflow.EndSpec{
				TerminalCode: "EXHAUSTED", RuntimeStatus: workflow.RuntimeRepairRequired,
				CompletionMapping: map[string]string{
					"RequestState": "APPROVED", "ExecutionState": "REPAIR_REQUIRED", "BusinessState": "UNKNOWN",
					"ConsistencyState": "UNKNOWN", "ObligationState": "PENDING",
				},
				RepairRefs: []string{"repair.ext015/v1"},
			},
			Governance: fragmentGoverned(),
		}, {
			ID:           "end_degraded",
			Type:         workflow.StepEnd,
			InputSchema:  workflow.SchemaRef{SchemaID: fxSchemaReq, Version: 1, ProtobufFullName: "test.ReadRequest"},
			OutputSchema: workflow.SchemaRef{SchemaID: fxSchemaResp, Version: 1, ProtobufFullName: "test.ReadResponse"},
			Inputs: []workflow.Field{
				{Path: "worker_id", Type: branded},
				{Path: "terminal_code", Type: str},
			},
			InputMappings: []workflow.Mapping{
				{Target: "worker_id", Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: "worker_id", Type: branded}},
				{Target: "terminal_code", Source: workflow.Source{Kind: workflow.SourceConstant, Type: str, Constant: "DEGRADED"}},
			},
			End: &workflow.EndSpec{
				TerminalCode: "DEGRADED", RuntimeStatus: workflow.RuntimeCompleted,
				CompletionMapping: map[string]string{
					"RequestState": "APPROVED", "ExecutionState": "COMMITTED", "BusinessState": "COMPLETED",
					"ConsistencyState": "DEGRADED", "ObligationState": "SATISFIED",
				},
				CommitReceiptRef: "receipt.ext015/v1",
			},
			Governance: fragmentGoverned(),
		}, {
			ID:           "delivery_watch",
			Type:         workflow.StepObserve,
			InputSchema:  workflow.SchemaRef{SchemaID: fxSchemaReq, Version: 1, ProtobufFullName: "test.ReadRequest"},
			OutputSchema: workflow.SchemaRef{SchemaID: fxSchemaResp, Version: 1, ProtobufFullName: "test.ReadResponse"},
			Inputs: []workflow.Field{
				{Path: "worker_id", Type: branded},
				{Path: "delivery_receipt", Type: str},
			},
			Outputs: []workflow.Field{{Path: "observed", Type: str}},
			InputMappings: []workflow.Mapping{
				{
					Target: "worker_id",
					Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: "worker_id", Type: branded},
				},
				{
					Target: "delivery_receipt",
					Source: workflow.Source{Kind: workflow.SourceNodeOutput, NodeID: NotifySendID("team_notice"), Path: "digest", Type: str},
				},
			},
			Capability: &workflow.CapabilityRef{ID: fxReadWorker, Version: 1,
				OperationMode: workflow.ModeExecute, AuthorityScopes: []string{fxScopeRead}},
			Observe: &workflow.ObserveSpec{
				EvidenceKind: workflow.EvidenceAuthoritativeRead, SourceAuthority: "delivery.vendor",
				ExpectedStateFields: []string{"delivery_receipt"},
				RequiredWatermarks:  []string{"delivery.vendor.stream_head"},
				MaxAgeSeconds:       300, ComparisonProfile: "comparison.ext015.delivery/v1",
				RetryExhaustionRoute: "end_exhausted",
			},
			Retry:      &workflow.RetryPolicy{MaxAttempts: 2, BackoffRef: "policy.retry.observation.bounded/v1"},
			Governance: fragmentGoverned(),
		}},
	}
}

func demoNotify() Notify {
	return Notify{
		ID:              "team_notice",
		AuthorityScopes: []string{"scope:messaging.send"},
		FailureNode:     "end",
		Governance:      fragmentGoverned(),
		WorkerInputPath: "worker_id",
		ReleaseGate:     &ReleaseGate{EventType: "team_notice.release", CorrelationKeyExpression: "worker_id", AcceptedSources: []string{"release-api"}},
		Notice: NoticeConfig{
			TenantID: "acme", OrganizationScope: "acme/eng",
			AudienceExpression: "team:eng", AudienceResolutionPolicy: "frozen",
			Purpose: "NOTICE", Classification: "INTERNAL", Urgency: "normal",
			DeliveryRequirement: "DELIVERED", ReplyMode: "NOT_ALLOWED",
			TemplateRef: "team_notice/v3", ParametersRef: "params:1",
			CorrelationID: "corr-1", ExpiresAt: 2000000000, NoticeKey: "n-1",
		},
	}
}

func demoDocument() Document {
	return Document{
		ID:                "offer_letter",
		AuthorityScopes:   []string{"scope:documents.read", "scope:documents.write"},
		Governance:        fragmentGoverned(),
		WorkerInputPath:   "worker_id",
		TemplateID:        "team_letter",
		TemplateVersion:   "3",
		BindingsInputPath: "bindings_json",
		SignerInputPath:   "signer_id",
		Assurance:         "ENHANCED",
		ExpiresInSeconds:  3600,
		CeremonyKey:       "k-1",
	}
}

// edgeAll routes every outcome of a capability node along the happy path or
// to the degraded terminal: only the happy path claims a consistent outcome.
func edgeAll(from, happy string) []workflow.Edge {
	return []workflow.Edge{
		{From: from, To: happy, RouteKey: "SUCCEEDED"},
		{From: from, To: "end_degraded", RouteKey: "REJECTED"},
		{From: from, To: "end_degraded", RouteKey: "UNKNOWN"},
		{From: from, To: "end_degraded", RouteKey: "AMBIGUOUS"},
	}
}

func edgeObserve(from, happy string) []workflow.Edge {
	return []workflow.Edge{
		{From: from, To: happy, RouteKey: "PASS"},
		{From: from, To: "end_degraded", RouteKey: "PARTIAL"},
		{From: from, To: "end_degraded", RouteKey: "FAIL"},
		{From: from, To: "end_exhausted", RouteKey: "UNKNOWN"},
	}
}

func edgeSignal(from, happy string) []workflow.Edge {
	return []workflow.Edge{
		{From: from, To: happy, RouteKey: "SUCCEEDED"},
		{From: from, To: "end_degraded", RouteKey: "TIMED_OUT"},
		{From: from, To: "end_degraded", RouteKey: "CANCELLED"},
	}
}

// demoEdges wires the letter flow before the notice: the authoritative
// ceremony commits before the downstream notice depends on it.
func demoEdges() []workflow.Edge {
	var edges []workflow.Edge
	edges = append(edges, edgeAll("start", DocumentRenderID("offer_letter"))...)
	edges = append(edges, edgeAll(DocumentRenderID("offer_letter"), DocumentSignID("offer_letter"))...)
	edges = append(edges, edgeAll(DocumentSignID("offer_letter"), DocumentSignedID("offer_letter"))...)
	edges = append(edges, edgeSignal(DocumentSignedID("offer_letter"), NotifyReleaseID("team_notice"))...)
	edges = append(edges, edgeSignal(NotifyReleaseID("team_notice"), NotifySendID("team_notice"))...)
	edges = append(edges, edgeAll(NotifySendID("team_notice"), "delivery_watch")...)
	edges = append(edges, edgeObserve("delivery_watch", "end")...)
	return edges
}

// TestTodo_WF_EXT_015 proves the messaging and document planes are reachable
// from workflow nodes: the three capabilities register with honest effects,
// both fragments expand to compiler-valid nodes at compile time, and
// SIMULATE suppresses the fragment writes through their declared overlays.
func TestTodo_WF_EXT_015(t *testing.T) {
	t.Run("CapabilitiesRegistered", func(t *testing.T) {
		reg, _, _, _ := fragmentRegistry(t)
		want := map[string]capability.EffectClass{
			messaging.CapSend:             capability.EffectExternalMutation,
			documents.CapRender:           capability.EffectReadOnly,
			documents.CapRequestSignature: capability.EffectInternalMutation,
		}
		for id, effect := range want {
			rec, ok := reg.Lookup(capability.Key{ID: id, Version: 1})
			if !ok {
				t.Fatalf("%s not resolvable", id)
			}
			if rec.Definition.EffectClass != effect {
				t.Fatalf("%s effect = %s, want %s", id, rec.Definition.EffectClass, effect)
			}
			if rec.Digest == "" || rec.Status != capability.StatusActive {
				t.Fatalf("%s record = %+v", id, rec)
			}
		}
	})

	t.Run("FragmentsExpandAndCompile", func(t *testing.T) {
		reg, _, _, _ := fragmentRegistry(t)
		def := fragmentDefinition()
		expanded, err := Expand(def, reg, []Notify{demoNotify()}, []Document{demoDocument()})
		if err != nil {
			t.Fatalf("Expand: %v", err)
		}
		if len(def.Nodes) != 5 {
			t.Fatal("Expand mutated the input definition")
		}
		if len(expanded.Nodes) != 10 {
			t.Fatalf("expanded nodes = %d, want 5 authored + 5 fragment", len(expanded.Nodes))
		}
		expanded.Edges = demoEdges()
		plan, err := workflow.Compile(expanded, workflow.Options{Phase: workflow.PhaseP1B, Capabilities: reg})
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		send := planNode(t, plan, NotifySendID("team_notice"))
		if send.EffectClass != capability.EffectExternalMutation {
			t.Fatalf("send effect = %s", send.EffectClass)
		}
		gate := planNode(t, plan, NotifyReleaseID("team_notice"))
		if gate.Type != workflow.StepSignal {
			t.Fatalf("release gate type = %s", gate.Type)
		}
	})

	t.Run("SimulateSuppressesFragments", func(t *testing.T) {
		reg, _, _, _ := fragmentRegistry(t)
		def := fragmentDefinition()
		expanded, err := Expand(def, reg, []Notify{demoNotify()}, []Document{demoDocument()})
		if err != nil {
			t.Fatalf("Expand: %v", err)
		}
		expanded.Edges = demoEdges()
		plan, err := workflow.Compile(expanded, workflow.Options{
			Phase: workflow.PhaseP1B, Capabilities: reg, SimulateProjection: true,
		})
		if err != nil {
			t.Fatalf("Compile SIMULATE: %v", err)
		}
		for _, id := range []string{NotifySendID("team_notice"), DocumentSignID("offer_letter")} {
			node := planNode(t, plan, id)
			if node.EffectClass.IsWrite() {
				t.Fatalf("%s still writes in SIMULATE: %s", id, node.EffectClass)
			}
		}
		render := planNode(t, plan, DocumentRenderID("offer_letter"))
		if render.EffectClass != capability.EffectReadOnly {
			t.Fatalf("render effect = %s", render.EffectClass)
		}
	})

	t.Run("UngatedNotifyExpandsSendOnly", func(t *testing.T) {
		reg, _, _, _ := fragmentRegistry(t)
		n := demoNotify()
		n.ID = "flash"
		n.ReleaseGate = nil
		expanded, err := Expand(fragmentDefinition(), reg, []Notify{n}, nil)
		if err != nil {
			t.Fatalf("Expand: %v", err)
		}
		if len(expanded.Nodes) != 6 {
			t.Fatalf("nodes = %d, want 5 authored + 1 send", len(expanded.Nodes))
		}
		send := expanded.Nodes[5]
		if send.ID != NotifySendID("flash") || send.Type != workflow.StepCapability {
			t.Fatalf("node = %+v", send)
		}
		if send.ModeOverlay == nil || send.ModeOverlay.SimulateEffect != capability.EffectReadOnly {
			t.Fatalf("send overlay = %+v", send.ModeOverlay)
		}
	})

	t.Run("ExpansionRefusesBadFragments", func(t *testing.T) {
		reg, _, _, _ := fragmentRegistry(t)
		good := demoNotify()
		cases := []struct {
			name string
			mut  func(*Notify)
			doc  func(*Document)
		}{
			{"EmptyID", func(n *Notify) { n.ID = "" }, nil},
			{"BadID", func(n *Notify) { n.ID = "has space" }, nil},
			{"UnknownCapabilityVersion", func(n *Notify) { n.CapabilityVersion = 99 }, nil},
			{"MissingGovernance", func(n *Notify) { n.Governance.Purpose = "" }, nil},
			{"MissingFailureNode", func(n *Notify) { n.FailureNode = "" }, nil},
			{"BadAssurance", nil, func(d *Document) { d.Assurance = "ULTRA" }},
			{"MissingTemplate", nil, func(d *Document) { d.TemplateID = "" }},
		}
		for _, c := range cases {
			n := good
			if c.mut != nil {
				c.mut(&n)
			}
			d := demoDocument()
			if c.doc != nil {
				c.doc(&d)
			}
			if _, err := Expand(fragmentDefinition(), reg, []Notify{n}, []Document{d}); err == nil {
				t.Errorf("%s accepted", c.name)
			}
		}
		dup := demoNotify()
		if _, err := Expand(fragmentDefinition(), reg, []Notify{demoNotify(), dup}, nil); err == nil {
			t.Error("duplicate fragment IDs accepted")
		}
		clash := demoNotify()
		clash.ID = "start"
		if _, err := Expand(fragmentDefinition(), reg, []Notify{clash}, nil); err == nil {
			t.Error("fragment colliding with an authored node accepted")
		}
	})
}

// planNode finds one compiled node by ID.
func planNode(t *testing.T, plan *workflow.CompiledWorkflow, id string) workflow.CompiledNode {
	t.Helper()
	for _, n := range plan.Nodes {
		if n.ID == id {
			return n
		}
	}
	t.Fatalf("plan has no node %q", id)
	return workflow.CompiledNode{}
}

type stubSender struct {
	sent []messaging.MessageIntent
}

func (s *stubSender) Send(_ context.Context, intent messaging.MessageIntent) (messaging.Receipt, error) {
	s.sent = append(s.sent, intent)
	return messaging.Receipt{MessageID: "msg-" + intent.CorrelationID}, nil
}

// TestTodo_WF_EXT_015_Integration runs the served path end to end: the gated
// notice sends, the letter renders, and the signature is collected, all
// through compile, the capability runner and the real planes. The gateway is
// a P1B-mode stand-in — it enforces authorization, scopes, retirement and
// digests exactly like the served gateway, minus the P1A zero-effect ban,
// which the gateway's own suite proves separately.
func TestTodo_WF_EXT_015_Integration(t *testing.T) {
	reg, sender, store, ceremonies := fragmentRegistry(t)
	store["team_letter@3"] = publishedTeamLetter(t)

	def := fragmentDefinition()
	expanded, err := Expand(def, reg, []Notify{demoNotify()}, []Document{demoDocument()})
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	expanded.Edges = demoEdges()
	plan, err := workflow.Compile(expanded, workflow.Options{Phase: workflow.PhaseP1B, Capabilities: reg})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	// The release gate holds the notice: the only edge into the send node
	// leaves the gate on SUCCEEDED.
	held := false
	for _, e := range expanded.Edges {
		if e.To == NotifySendID("team_notice") {
			if e.From != NotifyReleaseID("team_notice") || e.RouteKey != "SUCCEEDED" {
				t.Fatalf("send is reachable without release: %+v", e)
			}
			held = true
		}
	}
	if !held {
		t.Fatal("send node has no incoming edge")
	}
	gate := planNode(t, plan, NotifyReleaseID("team_notice"))
	if gate.Signal == nil || gate.Signal.EventType != "team_notice.release" {
		t.Fatalf("release gate spec = %+v", gate.Signal)
	}

	clock := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	gateway := &p1bGateway{registry: reg, sink: &stubSink{}, handlers: Handlers(sender, store, ceremonies)}
	runner := &capabilityrunner.Runner{
		Gateway:    gateway,
		SubjectRef: "workflow-runner",
		Tenant:     "acme",
		Purpose:    "workflow_execution",
		Now:        func() time.Time { return clock },
		WorkflowInputs: map[string]capabilityrunner.ResolvedValue{
			"worker_id":     {Text: "worker-1"},
			"signer_id":     {Text: "worker-1"},
			"bindings_json": {Text: `{"name":"Jo"}`},
		},
		NodeOutputs: map[string]map[string]capabilityrunner.ResolvedValue{},
	}

	// Release arrives: the gated notice sends through the served runner.
	release := runNode(t, runner, plan, NotifySendID("team_notice"))
	if len(sender.sent) != 1 {
		t.Fatalf("sent = %d", len(sender.sent))
	}
	if sender.sent[0].TemplateRef != "team_notice/v3" {
		t.Fatalf("intent = %+v", sender.sent[0])
	}
	if release.Outcome != workflow.OutcomeSucceeded || outputTexts(release)["message_id"] == "" {
		t.Fatalf("send outcome = %+v", release)
	}

	// The letter renders through the served runner.
	rendered := runNode(t, runner, plan, DocumentRenderID("offer_letter"))
	renderTexts := outputTexts(rendered)
	runner.NodeOutputs[DocumentRenderID("offer_letter")] = map[string]capabilityrunner.ResolvedValue{
		"artifact_digest": {Text: renderTexts["artifact_digest"]},
		"content":         {Text: renderTexts["content"]},
	}
	if renderTexts["content"] != "Hello Jo!" {
		t.Fatalf("render outcome = %+v", rendered)
	}

	// Signature collection opens through the served runner, then completes
	// through the governed ceremony: provider delivery, signer
	// acknowledgement with the artifact hash, and the countersigned proof.
	signed := runNode(t, runner, plan, DocumentSignID("offer_letter"))
	signTexts := outputTexts(signed)
	ceremonyID := signTexts["ceremony_id"]
	if signed.Outcome != workflow.OutcomeSucceeded || signTexts["state"] != signing.StateRequested {
		t.Fatalf("sign outcome = %+v", signed)
	}
	sig, ok := ceremonies.Get(ceremonyID)
	if !ok {
		t.Fatalf("ceremony %q was not retained", ceremonyID)
	}
	delivered, err := sig.Ceremony.Deliver("receipt-1", "cb-1", clock.Unix()+10)
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
	acked, err := delivered.Acknowledge(renderTexts["artifact_digest"], clock.Unix()+20)
	if err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	sig.Ceremony = acked
	final, err := sig.Sign(
		signing.SignerProofFor("worker-1", renderTexts["artifact_digest"]),
		renderTexts["artifact_digest"], "cb-2", clock.Unix()+30)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if final.Ceremony.State != signing.StateSigned {
		t.Fatalf("ceremony state = %s", final.Ceremony.State)
	}

	// The correlated SIGNAL awaits exactly this completion.
	spec := planNode(t, plan, DocumentSignedID("offer_letter")).Signal
	if spec == nil || spec.EventType != documents.EventSignatureSigned {
		t.Fatalf("signed spec = %+v", spec)
	}
	if spec.CorrelationKeyExpression != "worker_id" {
		t.Fatalf("correlation = %q", spec.CorrelationKeyExpression)
	}
}

// TestTodo_WF_EXT_015_Golden pins the compile-time expansion: the exact
// nodes both fragments contribute, so a silent change to generated schemas,
// effects or overlays breaks loudly.
func TestTodo_WF_EXT_015_Golden(t *testing.T) {
	reg, _, _, _ := fragmentRegistry(t)
	expanded, err := Expand(fragmentDefinition(), reg, []Notify{demoNotify()}, []Document{demoDocument()})
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	var generated []workflow.Node
	for _, n := range expanded.Nodes {
		if strings.Contains(n.ID, ".") {
			generated = append(generated, n)
		}
	}
	raw, err := os.ReadFile("testdata/fragments_golden.json")
	if err != nil {
		t.Fatalf("golden: %v", err)
	}
	var want []workflow.Node
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("golden unmarshal: %v", err)
	}
	if !reflect.DeepEqual(generated, want) {
		actual, _ := json.MarshalIndent(generated, "", "  ")
		t.Fatalf("expansion drifted:\n%s", string(actual))
	}
}

// outputTexts flattens typed node outputs by path.
func outputTexts(outcome frontier.NodeOutcome) map[string]string {
	texts := map[string]string{}
	if outcome.Outputs == nil {
		return texts
	}
	for _, v := range outcome.Outputs.Values {
		texts[v.Path] = v.Value.Text
	}
	return texts
}

// runNode executes one compiled node through the served runner.
func runNode(t *testing.T, runner *capabilityrunner.Runner, plan *workflow.CompiledWorkflow, id string) frontier.NodeOutcome {
	t.Helper()
	outcome, _, err := runner.Run(context.Background(), execute.StepRequest{
		TenantID:   uuid.New(),
		InstanceID: uuid.New(),
		Attempt:    1,
		Node:       planNode(t, plan, id),
		Plan:       plan,
		RecordedAt: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("run %s: %v", id, err)
	}
	return outcome
}

// p1bGateway is the P1B-mode gateway stand-in: full authorization,
// retirement, digest and evidence discipline, minus the P1A zero-effect ban.
type p1bGateway struct {
	registry *capability.Registry
	sink     *stubSink
	handlers map[capability.Key]capability.Handler
}

func (g *p1bGateway) refuse(capabilityID string, version uint32, code, reason string) (capability.InvokeResult, error) {
	return capability.InvokeResult{}, &capability.GatewayError{
		Code: code, Capability: capabilityID, Version: version, Reason: reason, EvidenceID: "ev:test",
	}
}

func (g *p1bGateway) Invoke(ctx context.Context, req capability.InvokeRequest) (capability.InvokeResult, error) {
	if req.Authorization.Decision != capability.Allow {
		return g.refuse(req.Capability.ID, req.Capability.Version, "deny", "authorization decision is not allow")
	}
	rec, ok := g.registry.Lookup(req.Capability)
	if !ok {
		return g.refuse(req.Capability.ID, req.Capability.Version, "unknown", "capability not registered")
	}
	if rec.Status == capability.StatusRetired {
		return g.refuse(req.Capability.ID, req.Capability.Version, "retired", "capability retired")
	}
	scoped := false
	for _, s := range req.Authorization.Scopes {
		if s == rec.Definition.AuthZScopeRef {
			scoped = true
			break
		}
	}
	if !scoped {
		return g.refuse(req.Capability.ID, req.Capability.Version, "scope", "missing required scope")
	}
	handler, ok := g.handlers[req.Capability]
	if !ok {
		return g.refuse(req.Capability.ID, req.Capability.Version, "unbound", "no implementation bound")
	}
	resp, err := handler(ctx, req.Payload)
	if err != nil {
		return capability.InvokeResult{}, err
	}
	evidenceID, err := g.sink.RecordInvocation(ctx, capability.InvocationEvidence{})
	if err != nil {
		return capability.InvokeResult{}, err
	}
	return capability.InvokeResult{Response: resp, EvidenceID: evidenceID}, nil
}

type stubSink struct {
	records int
}

func (s *stubSink) RecordInvocation(_ context.Context, _ capability.InvocationEvidence) (string, error) {
	s.records++
	return "ev:test", nil
}

func publishedTeamLetter(t *testing.T) template.Template {
	t.Helper()
	tpl, err := template.New(template.Definition{
		ID: "team_letter", Version: "3", Type: "LETTER", Purpose: "team notice",
		SourceLocale: "en-US", Locale: "en-US", Jurisdiction: "US-CA",
		Classification: "INTERNAL", AccessibilityVersion: "wcag22-aa",
		Source:       "Hello {{.name}}!",
		Placeholders: []template.Placeholder{{Name: "name", Kind: template.String, Required: true}},
	})
	if err != nil {
		t.Fatalf("template.New: %v", err)
	}
	published, err := tpl.Publish(template.Publication{
		TemplateDigest: tpl.Digest(),
		Approval:       template.Approval{ApprovalID: "ap-1", ApprovedBy: "hr-1"},
		Fixtures: []template.Fixture{{
			Name:     "ann",
			Bindings: map[string]template.Binding{"name": {Kind: template.String, Value: "Ann"}},
		}},
		Applicability: template.Applicability{
			Locales: []string{"en-US"}, Jurisdictions: []string{"US-CA"}, Classifications: []string{"INTERNAL"},
		},
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
	return published
}
