package toolbridge

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

const (
	testPurpose  = "agent.lookup"
	inputSchema  = `{"type":"object","properties":{"workerId":{"type":"string","minLength":1}},"required":["workerId"],"additionalProperties":false}`
	outputSchema = `{"type":"object","properties":{"name":{"type":"string"}},"required":["name"],"additionalProperties":false}`
)

var errGrantDenied = errors.New("grant revoked")

type bridgeFixture struct {
	bridge    *Bridge
	registry  *agentskills.Registry
	caps      *capability.Registry
	readPin   agentskills.SkillPin
	t3Pin     agentskills.SkillPin
	t4Pin     agentskills.SkillPin
	t2Pin     agentskills.SkillPin
	admission *fakeAdmission
	gate      *fakeGate
	cost      *fakeCost
	replay    *fakeReplay
	owner     *fakeOwner
	review    *fakeReview
}

func newBridgeFixture(t *testing.T) *bridgeFixture {
	t.Helper()
	caps := capability.NewRegistry()
	for _, effect := range []capability.EffectClass{capability.EffectReadOnly, capability.EffectInternalMutation, capability.EffectExternalMutation} {
		id := string(effect)
		def := capability.Definition{ID: id, Version: 1, OwnerDomain: "people", EffectClass: effect,
			RequestSchema:  capability.SchemaRef{SchemaID: id + ".request", Version: 1, ProtobufFullName: "test.Request"},
			ResponseSchema: capability.SchemaRef{SchemaID: id + ".response", Version: 1, ProtobufFullName: "test.Response"},
			ErrorSchema:    capability.SchemaRef{SchemaID: id + ".error", Version: 1, ProtobufFullName: "test.Error"},
			RiskClass:      "LOW", IdempotencyPolicyRef: "idem:required", AgentEligible: true,
			AuthZScopeRef: "scope:people", LegalBasisRef: "legal:test", EntitlementRef: "entitlement:test", SLOClassRef: "slo:test", TestRef: "test:owner"}
		if err := caps.Register(def, func(context.Context, any) (any, error) { return nil, nil }); err != nil {
			t.Fatal(err)
		}
	}
	registry := agentskills.NewRegistry(caps)
	definitions := []agentskills.SkillDefinition{
		skillDefinition("lookup", agentskills.TierRead, capability.EffectReadOnly),
		skillDefinition("announce", agentskills.TierCommunicate, capability.EffectReadOnly),
		skillDefinition("submit", agentskills.TierSubmitGoverned, capability.EffectInternalMutation),
		skillDefinition("external", agentskills.TierExternalWrite, capability.EffectExternalMutation),
	}
	for _, def := range definitions {
		if err := registry.Publish(def); err != nil {
			t.Fatal(err)
		}
	}
	pins := make([]agentskills.SkillPin, 0, len(definitions))
	for _, def := range definitions {
		pin, err := registry.Pin(def.Key())
		if err != nil {
			t.Fatal(err)
		}
		pins = append(pins, pin)
	}
	fixture := &bridgeFixture{registry: registry, caps: caps, admission: &fakeAdmission{pins: pins}, gate: &fakeGate{skills: registry}, cost: &fakeCost{}, replay: &fakeReplay{}, owner: &fakeOwner{response: map[string]any{"name": "Maya"}}, review: &fakeReview{}}
	fixture.readPin, fixture.t2Pin, fixture.t3Pin, fixture.t4Pin = pins[0], pins[1], pins[2], pins[3]
	bridge, err := New(Config{Admission: fixture.admission, Skills: registry, Capabilities: caps, Gate: fixture.gate, Costs: fixture.cost, Replay: fixture.replay, Owner: fixture.owner, Review: fixture.review})
	if err != nil {
		t.Fatal(err)
	}
	fixture.bridge = bridge
	return fixture
}

func skillDefinition(id string, tier agentskills.SideEffectTier, effect capability.EffectClass) agentskills.SkillDefinition {
	return agentskills.SkillDefinition{ID: id, Version: 1, Owner: "people", Description: "Read or update a worker",
		InputSchema: json.RawMessage(inputSchema), OutputSchema: json.RawMessage(outputSchema),
		Operations:     []agentskills.OperationRef{{Kind: agentskills.OperationCapability, Capability: capability.Key{ID: string(effect), Version: 1}}},
		SideEffectTier: tier, RequiredPurposes: []string{testPurpose}, IdempotencyRule: "exact call binding", CostClass: "LOW", EvalRefs: []string{"eval:tool"}}
}

func (f *bridgeFixture) call(pin agentskills.SkillPin, id string) ToolCall {
	return ToolCall{ID: id, Name: agentskills.MCPToolName(pin.Key()), Arguments: json.RawMessage(`{"workerId":"worker-42"}`)}
}

func (f *bridgeFixture) execute(call ToolCall) (Outcome, error) {
	return f.bridge.Execute(context.Background(), RunReference{RunID: "run-1", SourceKey: "source-1"}, call)
}

func TestTodo_AGENT_025(t *testing.T) {
	f := newBridgeFixture(t)
	out, err := f.execute(f.call(f.readPin, "call-1"))
	if err != nil {
		t.Fatal(err)
	}
	if out.ReviewPending || out.CallID != "call-1" || string(out.Output) != `{"name":"Maya"}` || f.owner.calls != 1 {
		t.Fatalf("outcome = %#v, owner calls = %d", out, f.owner.calls)
	}
	if len(f.gate.requests) != 1 || f.gate.requests[0].Skill != f.readPin || len(f.cost.skills) != 1 || len(f.replay.keys) != 1 {
		t.Fatalf("preflight ports gate=%d costs=%d replay=%d", len(f.gate.requests), len(f.cost.skills), len(f.replay.keys))
	}
	if f.owner.call.Capability.Definition.ID != string(capability.EffectReadOnly) || f.owner.call.Purpose != testPurpose || f.owner.call.IdempotencyKey == "" {
		t.Fatalf("owner call was not bound to the exact capability and purpose: %#v", f.owner.call)
	}
}

func TestTodo_AGENT_025_DiscoveryUsesPinnedVersionsAndCurrentGate(t *testing.T) {
	f := newBridgeFixture(t)
	tools, err := f.bridge.Tools(context.Background(), RunReference{RunID: "run-1", SourceKey: "source-1"})
	if err != nil || len(tools) != 4 {
		t.Fatalf("tools=%#v err=%v", tools, err)
	}
	if tools[0].Name != agentskills.MCPToolName(f.readPin.Key()) || string(tools[0].InputSchema) != inputSchema {
		t.Fatalf("first tool does not match exact skill pin: %#v", tools[0])
	}
	modelTools, err := f.bridge.ModelTools(context.Background(), RunReference{RunID: "run-1", SourceKey: "source-1"})
	if err != nil || len(modelTools) != len(tools) || modelTools[0].Name != tools[0].Name || string(modelTools[0].InputSchema) != inputSchema {
		t.Fatalf("model tool schema does not preserve pin projection: %#v err=%v", modelTools, err)
	}
	if err := f.caps.Retire(capability.Key{ID: string(capability.EffectReadOnly), Version: 1}); err != nil {
		t.Fatal(err)
	}
	tools, err = f.bridge.Tools(context.Background(), RunReference{RunID: "run-1", SourceKey: "source-1"})
	if err != nil || len(tools) != 2 {
		t.Fatalf("retired capability remained exposed: tools=%#v err=%v", tools, err)
	}
	f.gate.err = errGrantDenied
	tools, err = f.bridge.Tools(context.Background(), RunReference{RunID: "run-1", SourceKey: "source-1"})
	if err != nil || len(tools) != 0 {
		t.Fatalf("current grant denial should omit skills: tools=%#v err=%v", tools, err)
	}
}

func TestTodo_AGENT_025_Security(t *testing.T) {
	tests := []struct {
		name   string
		edit   func(*bridgeFixture, *ToolCall)
		want   error
		owner  int
		review int
	}{
		{name: "unknown name", edit: func(_ *bridgeFixture, c *ToolCall) { c.Name = "unregistered/v9" }, want: ErrUnknownTool},
		{name: "tampered pin digest", edit: func(f *bridgeFixture, _ *ToolCall) { f.admission.pins[0].Digest = "sha256:forged" }, want: ErrUnknownTool},
		{name: "admission run mismatch", edit: func(f *bridgeFixture, _ *ToolCall) { f.admission.badRun = true }, want: ErrAdmission},
		{name: "forged arguments", edit: func(_ *bridgeFixture, c *ToolCall) {
			c.Arguments = json.RawMessage(`{"workerId":"worker-42","scope":"*"}`)
		}, want: ErrArguments},
		{name: "missing required field", edit: func(_ *bridgeFixture, c *ToolCall) { c.Arguments = json.RawMessage(`{}`) }, want: ErrArguments},
		{name: "purpose not in pinned skill", edit: func(f *bridgeFixture, _ *ToolCall) { f.admission.purpose = "payroll.write" }, want: ErrAdmission},
		{name: "missing nonce", edit: func(f *bridgeFixture, _ *ToolCall) { f.admission.missingNonce = true }, want: ErrAdmission},
		{name: "budget denied", edit: func(f *bridgeFixture, _ *ToolCall) { f.cost.err = errors.New("budget exhausted") }, want: ErrCost},
		{name: "current grant denied", edit: func(f *bridgeFixture, _ *ToolCall) { f.gate.err = errGrantDenied }, want: errGrantDenied},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newBridgeFixture(t)
			call := f.call(f.readPin, "call-1")
			tc.edit(f, &call)
			_, err := f.execute(call)
			if err == nil || !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if f.owner.calls != tc.owner || f.review.calls != tc.review {
				t.Fatalf("owner/review calls = %d/%d", f.owner.calls, f.review.calls)
			}
		})
	}
	f := newBridgeFixture(t)
	calls := make([]ToolCall, MaxParallelCalls+1)
	for i := range calls {
		calls[i] = f.call(f.readPin, fmt.Sprintf("call-%d", i))
	}
	if _, err := f.bridge.ExecuteBatch(context.Background(), RunReference{RunID: "run-1", SourceKey: "source-1"}, calls); !errors.Is(err, ErrInvalid) || f.owner.calls != 0 {
		t.Fatalf("parallel burst err=%v owner calls=%d", err, f.owner.calls)
	}
}

func TestTodo_AGENT_025_Integration(t *testing.T) {
	f := newBridgeFixture(t)
	for _, test := range []struct {
		name string
		pin  agentskills.SkillPin
		tier agentskills.SideEffectTier
	}{
		{name: "governed submission waits for exact approval", pin: f.t3Pin, tier: agentskills.TierSubmitGoverned},
		{name: "external write waits for exact approval", pin: f.t4Pin, tier: agentskills.TierExternalWrite},
		{name: "communication waits for user confirmation", pin: f.t2Pin, tier: agentskills.TierCommunicate},
	} {
		t.Run(test.name, func(t *testing.T) {
			out, err := f.execute(f.call(test.pin, "call-"+test.tier.String()))
			if err != nil {
				t.Fatal(err)
			}
			if !out.ReviewPending || out.ReviewID == "" || f.owner.calls != 0 {
				t.Fatalf("outcome=%#v owner calls=%d", out, f.owner.calls)
			}
			if got := f.review.requests[len(f.review.requests)-1].Tier; got != test.tier {
				t.Fatalf("review tier = %v, want %v", got, test.tier)
			}
		})
	}

	f = newBridgeFixture(t)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: "tenant-a", Subject: "user-1", SubjectKind: trust.SubjectKindHuman, OrganizationScopeID: "org-a",
		Roles: []string{"manager"}, Purposes: []string{testPurpose}, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceSubstantial, SessionRef: "session-1", IssuedAt: time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC),
		ExpiresAt: time.Date(2026, 9, 29, 13, 0, 0, 0, time.UTC), CredentialDigest: "verified-session-digest",
	})
	if err != nil {
		t.Fatal(err)
	}
	grants := &mutableSkillGrants{rows: []agentgate.SkillGrant{{ID: "current-grant", Tenant: values.TenantId("tenant-a"), Skill: f.readPin.Key(), Roles: []string{"manager"}, Population: "managers", OrganizationScopes: []string{"org-a"}, Purposes: []string{testPurpose}}}}
	gate, err := agentgate.New(agentgate.Config{Skills: f.registry, Grants: grants, PDP: allowRequestedFields{}, Now: func() time.Time { return time.Date(2026, 9, 29, 12, 30, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	f.admission.baseGate = agentgate.CallRequest{
		User:     agentgate.UserContext{Principal: principal, Population: "managers", Roles: []string{"manager"}, OrganizationScopes: []string{"org-a"}},
		Subjects: []agentgate.Subject{{Ref: values.EntityRef{Tenant: values.TenantId("tenant-a"), Kind: values.Kind("worker"), Id: "00000000-0000-4000-8000-000000000042"}, Organization: authz.OrgUnitRef{Tenant: values.TenantId("tenant-a"), ID: "org-a"}}},
		Fields:   []authz.FieldID{authz.FieldWorkerNumber}, At: time.Date(2026, 9, 29, 12, 30, 0, 0, time.UTC),
	}
	f.bridge, err = New(Config{Admission: f.admission, Skills: f.registry, Capabilities: f.caps, Gate: gate, Costs: f.cost, Replay: f.replay, Owner: f.owner, Review: f.review})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.execute(f.call(f.readPin, "call-current-grant")); err != nil {
		t.Fatalf("real current-grant gate refused eligible call: %v", err)
	}
	grants.rows[0].Roles = []string{"employee"}
	if _, err := f.execute(f.call(f.readPin, "call-revoked-grant")); err == nil || f.owner.calls != 1 {
		t.Fatalf("revoked grant was not denied before owner: err=%v calls=%d", err, f.owner.calls)
	}
}

func TestTodo_AGENT_025_Mutation(t *testing.T) {
	f := newBridgeFixture(t)
	f.replay.err = ErrReplay
	_, err := f.execute(f.call(f.readPin, "call-1"))
	if !errors.Is(err, ErrReplay) || f.owner.calls != 0 {
		t.Fatalf("replay err=%v owner calls=%d", err, f.owner.calls)
	}
	f = newBridgeFixture(t)
	f.owner.response = map[string]any{"name": 42}
	_, err = f.execute(f.call(f.readPin, "call-2"))
	if !errors.Is(err, ErrArguments) || f.owner.calls != 1 {
		t.Fatalf("invalid owner output err=%v calls=%d", err, f.owner.calls)
	}
	if err := validateTier(agentskills.SkillRecord{Definition: agentskills.SkillDefinition{SideEffectTier: agentskills.TierRead}, HighestCapabilityTier: agentskills.TierSubmitGoverned}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("understated tier error = %v", err)
	}
	f = newBridgeFixture(t)
	first := f.call(f.readPin, "stable-call-id")
	if _, err := f.execute(first); err != nil {
		t.Fatal(err)
	}
	second := f.call(f.readPin, "stable-call-id")
	second.Arguments = json.RawMessage(`{"workerId":"different-worker"}`)
	_, err = f.execute(second)
	if !errors.Is(err, ErrReplay) || f.owner.calls != 1 || idempotencyKey(f.replay.keys[0]) != idempotencyKey(f.replay.keys[1]) {
		t.Fatalf("changed retry escaped idempotency binding: err=%v calls=%d keys=%#v", err, f.owner.calls, f.replay.keys)
	}
}

func TestTodo_AGENT_025_RejectsPartialPortWiring(t *testing.T) {
	if _, err := New(Config{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("empty configuration error = %v", err)
	}
}
