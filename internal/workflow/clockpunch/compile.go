package clockpunch

import (
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

type capabilities struct{}

func (capabilities) Lookup(key capability.Key) (capability.Record, bool) {
	if key.ID != CapabilityCommit || key.Version != 1 {
		return capability.Record{}, false
	}
	return capability.Record{Definition: capability.Definition{ID: CapabilityCommit, Version: 1, OwnerDomain: "time",
		RequestSchema: capSchema("request"), ResponseSchema: capSchema("response"), ErrorSchema: capSchema("error"),
		EffectClass: capability.EffectInternalMutation, RiskClass: "HIGH", IdempotencyPolicyRef: "idempotency.time/v1", AuthZScopeRef: "scope:time.punch.write",
		LegalBasisRef: "legal.time.recordkeeping/v1", EntitlementRef: "entitlement.time/v1", SLOClassRef: "slo.time.punch.p95-300ms/v1", TestRef: "conformance:time.clock_in_out/v1"}, Status: capability.StatusActive, Digest: "sha256:time:clock-punch-v1"}, true
}

func capSchema(slot string) capability.SchemaRef {
	return capability.SchemaRef{SchemaID: CapabilityCommit + "." + slot + "/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"}
}
func capabilitySchema(slot string) workflow.SchemaRef {
	s := capSchema(slot)
	return workflow.SchemaRef{SchemaID: s.SchemaID, Version: s.Version, ProtobufFullName: s.ProtobufFullName}
}

// CompileOptions pins the graph's executable phase, IR and capability contract.
func CompileOptions() workflow.Options {
	return workflow.Options{Phase: workflow.PhaseP1B, IRSchemaVersion: 2, Capabilities: capabilities{}}
}

// Compile compiles the hand-authored graph using the shared compiler.
func Compile() (*workflow.CompiledWorkflow, error) {
	return workflow.Compile(Definition(), CompileOptions())
}
