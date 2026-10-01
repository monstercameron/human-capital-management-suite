package clockrepair

import (
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

type capabilities struct{}

func (capabilities) Lookup(key capability.Key) (capability.Record, bool) {
	var d capability.Definition
	switch key.ID {
	case CapabilityCommitRequest:
		d = capability.Definition{ID: CapabilityCommitRequest, Version: 1, OwnerDomain: "time", RequestSchema: capSchema(CapabilityCommitRequest, "request"), ResponseSchema: capSchema(CapabilityCommitRequest, "response"), ErrorSchema: capSchema(CapabilityCommitRequest, "error"), EffectClass: capability.EffectInternalMutation, RiskClass: "HIGH", IdempotencyPolicyRef: "idempotency.time.missing_punch_request/v1", AuthZScopeRef: "scope:time.punch.request", LegalBasisRef: "legal.time.recordkeeping/v1", EntitlementRef: "entitlement.time/v1", SLOClassRef: "slo.time.review.p95-300ms/v1", TestRef: "conformance:time.missing_punch.request_capture/v1"}
	case CapabilityValidate:
		d = capability.Definition{ID: CapabilityValidate, Version: 1, OwnerDomain: "time", RequestSchema: capSchema(CapabilityValidate, "request"), ResponseSchema: capSchema(CapabilityValidate, "response"), ErrorSchema: capSchema(CapabilityValidate, "error"), EffectClass: capability.EffectReadOnly, RiskClass: "HIGH", AuthZScopeRef: "scope:time.punch.review", LegalBasisRef: "legal.time.recordkeeping/v1", EntitlementRef: "entitlement.time/v1", SLOClassRef: "slo.time.review.p95-300ms/v1", TestRef: "conformance:time.missing_punch.validation/v1"}
	case CapabilityCorrection:
		d = capability.Definition{ID: CapabilityCorrection, Version: 1, OwnerDomain: "time", RequestSchema: capSchema(CapabilityCorrection, "request"), ResponseSchema: capSchema(CapabilityCorrection, "response"), ErrorSchema: capSchema(CapabilityCorrection, "error"), EffectClass: capability.EffectInternalMutation, RiskClass: "HIGH", IdempotencyPolicyRef: "idempotency.time.correction/v1", AuthZScopeRef: "scope:time.punch.correct", LegalBasisRef: "legal.time.recordkeeping/v1", EntitlementRef: "entitlement.time/v1", SLOClassRef: "slo.time.correction.p95-300ms/v1", TestRef: "conformance:time.append_only_correction/v1"}
	case CapabilityReopen:
		d = capability.Definition{ID: CapabilityReopen, Version: 1, OwnerDomain: "time", RequestSchema: capSchema(CapabilityReopen, "request"), ResponseSchema: capSchema(CapabilityReopen, "response"), ErrorSchema: capSchema(CapabilityReopen, "error"), EffectClass: capability.EffectInternalMutation, RiskClass: "HIGH", IdempotencyPolicyRef: "idempotency.time.period_reopen/v1", AuthZScopeRef: "scope:time.period.reopen", LegalBasisRef: "legal.time.recordkeeping/v1", EntitlementRef: "entitlement.time/v1", SLOClassRef: "slo.time.reopen.p95-300ms/v1", TestRef: "conformance:time.governed_reopen/v1"}
	default:
		return capability.Record{}, false
	}
	return capability.Record{Definition: d, Status: capability.StatusActive, Digest: "sha256:time:clock-repair-v1:" + key.ID}, true
}

func capSchema(id, slot string) capability.SchemaRef {
	return capability.SchemaRef{SchemaID: id + "." + slot + "/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"}
}

func capabilitySchema(id, slot string) workflow.SchemaRef {
	s := capSchema(id, slot)
	return workflow.SchemaRef{SchemaID: s.SchemaID, Version: s.Version, ProtobufFullName: s.ProtobufFullName}
}

// CompileOptions pins the workflow phase and capability contracts.
func CompileOptions() workflow.Options {
	return workflow.Options{Phase: workflow.PhaseP1B, IRSchemaVersion: 2, Capabilities: capabilities{}}
}

// Compile compiles the hand-authored missing-punch correction graph.
func Compile() (*workflow.CompiledWorkflow, error) {
	return workflow.Compile(Definition(), CompileOptions())
}
