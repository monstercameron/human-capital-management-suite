package workflow_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/attest"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/capabilityrunner"
)

const rev04102Obligation = "obligation.time.attestation"

type rev04102EvidenceSink struct{ count int }

func (s *rev04102EvidenceSink) RecordInvocation(context.Context, capability.InvocationEvidence) (string, error) {
	s.count++
	return "attested-capability-evidence", nil
}

func (s *rev04102EvidenceSink) RecordInvocationTx(ctx context.Context, evt capability.InvocationEvidence) (string, error) {
	if _, ok := dbport.TxFromContext(ctx); ok {
		return "", errors.New("attestation integration evidence sink cannot join a caller transaction")
	}
	return s.RecordInvocation(ctx, evt)
}

func TestTodo_REV_041_02_Integration(t *testing.T) {
	ctx := context.Background()
	at := attest.FixedTrustedTime(time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC))
	attestationStore := attest.NewMemoryResponseStore()
	recorder, err := attest.NewRecorder(attestationStore, attest.TrustedClockFunc(func() (attest.TrustedTime, error) { return at, nil }))
	if err != nil {
		t.Fatal(err)
	}
	response, err := recorder.RecordResponse(ctx, attest.ResponseRequest{
		Tenant: "tenant-a", ResponseID: "time-attestation-1", StatementID: "time-punch-proposal",
		StatementVersion: 4, StatementDigest: "sha256:statement", BindingDigest: "sha256:proposal",
		Status: attest.ResponseAccepted, Kind: attest.AssertionResponse,
		EvidenceReceipt: "sha256:evidence", IdempotencyKey: "attestation-response-1", TransactionID: "tx-1",
		AffectedObligations: []string{rev04102Obligation},
	})
	if err != nil {
		t.Fatal(err)
	}
	const capabilityID = "rev04102.integration.time-dependent-effect"
	definition := capability.Definition{
		ID: capabilityID, Version: 1, OwnerDomain: "time",
		RequestSchema:  capability.SchemaRef{SchemaID: capabilityID + ".request/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"},
		ResponseSchema: capability.SchemaRef{SchemaID: capabilityID + ".response/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"},
		ErrorSchema:    capability.SchemaRef{SchemaID: capabilityID + ".error/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"},
		EffectClass:    capability.EffectReadOnly, ReadData: capability.DataDomainFieldSet{DataDomains: []string{"time"}},
		RiskClass: "LOW", IdempotencyPolicyRef: "idempotency.read-safe.v1", AuthZScopeRef: "scope:time.read",
		LegalBasisRef: "legal.test/v1", EntitlementRef: "entitlement.test/v1", SLOClassRef: "slo.test/v1", TestRef: "conformance:rev04102/v1",
	}
	registry := capability.NewRegistry()
	handlerCalls := 0
	if err := registry.Register(definition, func(context.Context, any) (any, error) {
		handlerCalls++
		return capabilityrunner.CapabilityAnswer{Outcome: workflow.OutcomeSucceeded}, nil
	}); err != nil {
		t.Fatal(err)
	}
	manifest, ok := registry.Lookup(capability.Key{ID: capabilityID, Version: 1})
	if !ok {
		t.Fatal("dependent capability was not published")
	}
	node := workflow.CompiledNode{
		ID: "apply_time_proposal", Type: workflow.StepCapability, EffectClass: capability.EffectReadOnly,
		Capability: &workflow.CompiledCapability{
			ID: manifest.Definition.ID, Version: manifest.Definition.Version, Digest: manifest.Digest,
			Status: manifest.Status, OwnerDomain: manifest.Definition.OwnerDomain, EffectClass: manifest.Definition.EffectClass,
			AuthZScopeRef: manifest.Definition.AuthZScopeRef, OperationMode: workflow.ModeExecute, AuthorityScopes: []string{"scope:time.read"},
		},
		Governance: workflow.CompiledGovernance{ObligationRefs: []string{rev04102Obligation}},
	}
	prefix := workflow.AttestationExecutionMetadataPrefix
	node.Metadata = map[string]string{
		prefix + "response_id":       response.ResponseID,
		prefix + "response_revision": "1",
		prefix + "statement_id":      response.StatementID,
		prefix + "statement_version": "4",
		prefix + "statement_digest":  response.StatementDigest,
		prefix + "binding_digest":    response.BindingDigest,
		prefix + "transaction_id":    response.TransactionID,
	}
	plan := &workflow.CompiledWorkflow{Nodes: []workflow.CompiledNode{node}, Governance: workflow.GovernanceSummary{Obligations: []workflow.ObligationRequirement{{
		ID: rev04102Obligation, Mandatory: true, RequiredAction: "validate attestation before dependent effect",
	}}}}
	step := execute.StepRequest{Node: node, Plan: plan, TenantID: mustUUID(t, "d3f01f7e-398d-4f7c-935d-761c7d7f5478")}
	sink := &rev04102EvidenceSink{}
	runner := &capabilityrunner.Runner{
		Gateway: capability.NewGateway(registry, sink), Tenant: "tenant-a", SubjectRef: "worker:W-1",
		AttestationResponses: attestationStore,
		AttestationClock:     attest.TrustedClockFunc(func() (attest.TrustedTime, error) { return at, nil }),
	}
	got, refs, err := runner.Run(ctx, step)
	if err != nil {
		t.Fatalf("gated workflow step: %v", err)
	}
	if got.Outcome != workflow.OutcomeSucceeded || refs.CapabilityExecutionID != "attested-capability-evidence" {
		t.Fatalf("gated workflow result = %+v refs=%+v", got, refs)
	}
	if handlerCalls != 1 || sink.count != 1 {
		t.Fatalf("dependent capability calls=%d evidence records=%d; want one each", handlerCalls, sink.count)
	}

	// Refuse a stale revision and prove the gateway remains untouched.
	step.Attempt = 2
	step.Plan.Nodes[0].Metadata[prefix+"response_revision"] = "2"
	_, _, err = runner.Run(ctx, step)
	if !errors.Is(err, attest.ErrRequiredAttestation) {
		t.Fatalf("stale attestation error = %v, want ErrRequiredAttestation", err)
	}
	if handlerCalls != 1 || sink.count != 1 {
		t.Fatalf("stale response reached capability: calls=%d evidence=%d", handlerCalls, sink.count)
	}
}

func mustUUID(t *testing.T, value string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(value)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

var _ execute.StepRunner = (*capabilityrunner.Runner)(nil)
