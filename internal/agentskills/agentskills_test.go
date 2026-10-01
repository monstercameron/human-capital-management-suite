package agentskills

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
)

func capabilityDefinition(id string, effect capability.EffectClass) capability.Definition {
	return capability.Definition{
		ID: id, Version: 1, OwnerDomain: "people",
		RequestSchema:  capability.SchemaRef{SchemaID: id + ".request", Version: 1, ProtobufFullName: "test.Request"},
		ResponseSchema: capability.SchemaRef{SchemaID: id + ".response", Version: 1, ProtobufFullName: "test.Response"},
		ErrorSchema:    capability.SchemaRef{SchemaID: id + ".error", Version: 1, ProtobufFullName: "test.Error"},
		EffectClass:    effect, RiskClass: "LOW", IdempotencyPolicyRef: "idem:test",
		AgentEligible: true, AuthZScopeRef: "scope:test", LegalBasisRef: "legal:test",
		EntitlementRef: "entitlement:test", SLOClassRef: "slo:test", TestRef: "test:capability",
	}
}

func capabilityRegistry(t *testing.T, defs ...capability.Definition) *capability.Registry {
	t.Helper()
	registry := capability.NewRegistry()
	for _, def := range defs {
		if err := registry.Register(def, func(context.Context, any) (any, error) { return nil, nil }); err != nil {
			t.Fatalf("register capability %s: %v", def.ID, err)
		}
	}
	return registry
}

func skillDefinition(capabilityID string, tier SideEffectTier) SkillDefinition {
	return SkillDefinition{
		ID: "hcmnext.skill.worker_state", Version: 1, Owner: "people",
		Description:    "Explain the current worker state.",
		InputSchema:    json.RawMessage(`{"type":"object","properties":{"workerId":{"type":"string"}},"required":["workerId"]}`),
		OutputSchema:   json.RawMessage(`{"type":"object","properties":{"state":{"type":"string"}},"required":["state"]}`),
		Operations:     []OperationRef{{Kind: OperationCapability, Capability: capability.Key{ID: capabilityID, Version: 1}}},
		SideEffectTier: tier, RequiredPurposes: []string{"workforce:explain"},
		DataClassesRead: []string{"WORKFORCE"}, IdempotencyRule: "read-only",
		CostClass: "LOW", EvalRefs: []string{"eval:worker-state"},
	}
}

// TestTodo_AGENT2_004 proves publication is typed, exact-versioned and
// capability-backed: unknown capabilities and under-declared effect tiers are
// refused, while a valid version produces a digest-backed task pin and MCP
// projection.
func TestTodo_AGENT2_004(t *testing.T) {
	read := capabilityDefinition("hcmnext.people.read_worker", capability.EffectReadOnly)
	write := capabilityDefinition("hcmnext.people.update_worker", capability.EffectInternalMutation)
	catalog := capabilityRegistry(t, read, write)
	registry := NewRegistry(catalog)

	if err := registry.Publish(skillDefinition(read.ID, TierRead)); err != nil {
		t.Fatalf("publish read skill: %v", err)
	}
	if _, err := registry.Pin(SkillKey{ID: "hcmnext.skill.worker_state", Version: 1}); err != nil {
		t.Fatalf("pin published skill: %v", err)
	}

	unknown := skillDefinition("hcmnext.people.missing", TierRead)
	if err := registry.Publish(unknown); !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("unknown capability error = %v, want ErrInvalidDefinition", err)
	}

	underDeclared := skillDefinition(write.ID, TierRead)
	underDeclared.ID = "hcmnext.skill.update_worker"
	if err := registry.Publish(underDeclared); !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("under-declared tier error = %v, want ErrInvalidDefinition", err)
	}

	validWrite := skillDefinition(write.ID, TierSubmitGoverned)
	validWrite.ID = "hcmnext.skill.update_worker"
	if err := registry.Publish(validWrite); err != nil {
		t.Fatalf("publish governed skill: %v", err)
	}
	record, ok := registry.Lookup(validWrite.Key())
	if !ok || record.HighestCapabilityTier != TierSubmitGoverned {
		t.Fatalf("resolved highest tier = %s, found=%v", record.HighestCapabilityTier, ok)
	}
	projection, err := registry.MCPToolsForPins([]SkillPin{{ID: "hcmnext.skill.worker_state", Version: 1, Digest: mustPin(t, registry, skillDefinition(read.ID, TierRead)).Digest}, {ID: validWrite.ID, Version: 1, Digest: record.Digest}})
	if err != nil {
		t.Fatalf("project pinned tools: %v", err)
	}
	if len(projection.Tools) != 2 || !projection.Tools[0].Annotations.DestructiveHint {
		t.Fatalf("projection = %+v", projection)
	}
}

func mustPin(t *testing.T, registry *Registry, def SkillDefinition) SkillPin {
	t.Helper()
	pin, err := registry.Pin(def.Key())
	if err != nil {
		t.Fatalf("pin %s: %v", def.Key(), err)
	}
	return pin
}

// TestTodo_AGENT2_004_Golden pins the MCP wire shape and versioned name. It
// protects the contract consumed by SchemaFlux and future MCP adapters.
func TestTodo_AGENT2_004_Golden(t *testing.T) {
	def := capabilityDefinition("hcmnext.people.read_worker", capability.EffectReadOnly)
	registry := NewRegistry(capabilityRegistry(t, def))
	skill := skillDefinition(def.ID, TierRead)
	if err := registry.Publish(skill); err != nil {
		t.Fatalf("publish: %v", err)
	}
	got, err := json.Marshal(registry.MCPToolsList())
	if err != nil {
		t.Fatalf("marshal projection: %v", err)
	}
	want := `{"tools":[{"name":"hcmnext.skill.worker_state/v1","description":"Explain the current worker state.","inputSchema":{"type":"object","properties":{"workerId":{"type":"string"}},"required":["workerId"]},"outputSchema":{"type":"object","properties":{"state":{"type":"string"}},"required":["state"]},"annotations":{"readOnlyHint":true,"destructiveHint":false}}]}`
	if string(got) != want {
		t.Fatalf("MCP projection = %s, want %s", got, want)
	}
}

// FuzzTodo_AGENT2_004 proves arbitrary schema bytes fail closed and never
// cause publication to panic. Valid object schemas remain publishable.
func FuzzTodo_AGENT2_004(f *testing.F) {
	f.Add(`{"type":"object"}`)
	f.Add(`{"type":"object","properties":{}}`)
	f.Add(`not-json`)
	f.Fuzz(func(t *testing.T, schema string) {
		def := capabilityDefinition("hcmnext.people.read_worker", capability.EffectReadOnly)
		registry := NewRegistry(capabilityRegistry(t, def))
		skill := skillDefinition(def.ID, TierRead)
		skill.InputSchema = json.RawMessage(schema)
		skill.OutputSchema = json.RawMessage(schema)
		err := registry.Publish(skill)
		var object map[string]json.RawMessage
		isObject := json.Unmarshal([]byte(schema), &object) == nil && object != nil
		if isObject && err != nil {
			t.Fatalf("object schema rejected: %v", err)
		}
		if !isObject && !errors.Is(err, ErrInvalidDefinition) {
			t.Fatalf("invalid schema error = %v, want ErrInvalidDefinition", err)
		}
	})
}

// TestTodo_AGENT2_004_Conformance checks the pinned MCP revision and rejects
// malformed projections rather than letting an adapter publish ad hoc tools.
func TestTodo_AGENT2_004_Conformance(t *testing.T) {
	def := capabilityDefinition("hcmnext.people.read_worker", capability.EffectReadOnly)
	registry := NewRegistry(capabilityRegistry(t, def))
	if err := registry.Publish(skillDefinition(def.ID, TierRead)); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if err := registry.Conformance(MCPProtocolRevision); err != nil {
		t.Fatalf("conformance: %v", err)
	}
	if err := registry.Conformance("future-revision"); err == nil {
		t.Fatal("unsupported protocol revision unexpectedly conformed")
	}
	bad := MCPToolsList{Tools: []MCPTool{{Name: "bad", Description: "bad", InputSchema: json.RawMessage(`[]`), OutputSchema: json.RawMessage(`{}`)}}}
	if err := ValidateMCPProjection(MCPProtocolRevision, bad); err == nil {
		t.Fatal("array input schema unexpectedly conformed")
	}
	bad.Tools[0].InputSchema = json.RawMessage(`{"type":"string"}`)
	if err := ValidateMCPProjection(MCPProtocolRevision, bad); err == nil {
		t.Fatal("scalar input schema unexpectedly conformed")
	}
}

func TestRegistryRejectsAmbiguousOperationReferences(t *testing.T) {
	def := capabilityDefinition("hcmnext.people.read_worker", capability.EffectReadOnly)
	registry := NewRegistry(capabilityRegistry(t, def))
	capabilityWithConnection := skillDefinition(def.ID, TierRead)
	capabilityWithConnection.Operations[0].ConnectionID = "workday"
	if err := registry.Publish(capabilityWithConnection); !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("mixed capability reference error = %v, want ErrInvalidDefinition", err)
	}

	connectionWithCapability := skillDefinition(def.ID, TierRead)
	connectionWithCapability.Operations[0] = OperationRef{
		Kind: OperationConnection, ConnectionID: "workday", Operation: "read_worker",
		Capability: def.Key(),
	}
	if err := registry.Publish(connectionWithCapability); !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("mixed connection reference error = %v, want ErrInvalidDefinition", err)
	}
}

func TestRegistryRejectsDuplicatePinnedSkills(t *testing.T) {
	def := capabilityDefinition("hcmnext.people.read_worker", capability.EffectReadOnly)
	registry := NewRegistry(capabilityRegistry(t, def))
	skill := skillDefinition(def.ID, TierRead)
	if err := registry.Publish(skill); err != nil {
		t.Fatalf("publish: %v", err)
	}
	pin := mustPin(t, registry, skill)
	if _, err := registry.MCPToolsForPins([]SkillPin{pin, pin}); !errors.Is(err, ErrDuplicateSkillPin) {
		t.Fatalf("duplicate pin error = %v, want ErrDuplicateSkillPin", err)
	}
}

func TestMCPRejectsNonObjectSchema(t *testing.T) {
	projection := MCPToolsList{Tools: []MCPTool{{
		Name: "skill/v1", Description: "scalar schema", InputSchema: json.RawMessage("{\"type\":\"string\"}"), OutputSchema: json.RawMessage(`{"type":"object"}`),
	}}}
	if err := ValidateMCPProjection(MCPProtocolRevision, projection); err == nil {
		t.Fatal("scalar MCP schema unexpectedly conformed")
	}
}

func TestRegistryPublishedRecordIsImmutable(t *testing.T) {
	def := capabilityDefinition("hcmnext.people.read_worker", capability.EffectReadOnly)
	registry := NewRegistry(capabilityRegistry(t, def))
	skill := skillDefinition(def.ID, TierRead)
	if err := registry.Publish(skill); err != nil {
		t.Fatalf("publish: %v", err)
	}
	got, _ := registry.Lookup(skill.Key())
	got.Definition.Operations[0].Capability.ID = "tampered"
	got.Definition.InputSchema[0] = '['
	again, _ := registry.Lookup(skill.Key())
	if again.Definition.Operations[0].Capability.ID == "tampered" || !reflect.DeepEqual(again.Definition.InputSchema, skill.InputSchema) {
		t.Fatal("lookup exposed mutable registry state")
	}
}
