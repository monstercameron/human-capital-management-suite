package capabilityrunner

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/attest"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/promotionexec"
)

const testAttestationObligation = "obligation.time.attestation"

type fixedExecutionRequirement struct{ requirement attest.ExecutionRequirement }

func (r fixedExecutionRequirement) ResolveExecutionRequirement(context.Context, execute.StepRequest, workflow.CompiledNode, workflow.ObligationRequirement) (attest.ExecutionRequirement, error) {
	return r.requirement, nil
}

func TestTodo_REV_041_02(t *testing.T) {
	at := attest.FixedTrustedTime(time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC))
	store := attest.NewMemoryResponseStore()
	recorder, err := attest.NewRecorder(store, attest.TrustedClockFunc(func() (attest.TrustedTime, error) { return at, nil }))
	if err != nil {
		t.Fatal(err)
	}
	response, err := recorder.RecordResponse(context.Background(), attest.ResponseRequest{
		Tenant: "tenant-a", ResponseID: "response-1", StatementID: "time-punch-worker-attestation",
		StatementVersion: 1, StatementDigest: "sha256:statement", BindingDigest: "sha256:proposal",
		Status: attest.ResponseAccepted, Kind: attest.AssertionResponse, EvidenceReceipt: "sha256:evidence",
		IdempotencyKey: "response-1", TransactionID: "tx-1", AffectedObligations: []string{testAttestationObligation},
	})
	if err != nil {
		t.Fatal(err)
	}
	var handlerCalls int
	planForRevision := func(revision string) *workflow.CompiledWorkflow {
		t.Helper()
		definition := promotionexec.Definition()
		definition.Obligations = append(definition.Obligations, workflow.ObligationRequirement{
			ID: testAttestationObligation, Authority: "customer.policy.time.attestation",
			InsertionPoint: workflow.InsertSimulation, RequiredAction: "validate attestation before dependent effect",
			ResponsibleParty: "time.attestation", SatisfactionCondition: "exact response and transaction are revalidated before invocation",
			SourceVersion: "attest.execution.test/v1", ReevaluationPolicy: workflow.ReevalReevaluate, Mandatory: true,
		})
		for index := range definition.Nodes {
			if definition.Nodes[index].ID != promotionexec.NodeSnapshotWorker {
				continue
			}
			node := &definition.Nodes[index]
			node.Governance.ObligationRefs = append(node.Governance.ObligationRefs, testAttestationObligation)
			node.Metadata = map[string]string{
				workflow.AttestationExecutionMetadataPrefix + "response_id":       response.ResponseID,
				workflow.AttestationExecutionMetadataPrefix + "response_revision": revision,
				workflow.AttestationExecutionMetadataPrefix + "statement_id":      response.StatementID,
				workflow.AttestationExecutionMetadataPrefix + "statement_version": "1",
				workflow.AttestationExecutionMetadataPrefix + "statement_digest":  response.StatementDigest,
				workflow.AttestationExecutionMetadataPrefix + "binding_digest":    response.BindingDigest,
				workflow.AttestationExecutionMetadataPrefix + "transaction_id":    response.TransactionID,
			}
		}
		plan, err := promotionexec.Compile(definition)
		if err != nil {
			t.Fatalf("compile attestation-bound workflow: %v", err)
		}
		if err := plan.Verify(); err != nil {
			t.Fatalf("compiled plan does not verify: %v", err)
		}
		tampered := *plan
		tampered.Nodes = append([]workflow.CompiledNode(nil), plan.Nodes...)
		for index := range tampered.Nodes {
			if tampered.Nodes[index].ID == promotionexec.NodeSnapshotWorker {
				metadata := make(map[string]string, len(tampered.Nodes[index].Metadata))
				for key, value := range tampered.Nodes[index].Metadata {
					metadata[key] = value
				}
				tampered.Nodes[index].Metadata = metadata
				tampered.Nodes[index].Metadata[workflow.AttestationExecutionMetadataPrefix+"transaction_id"] = "tx-forged"
			}
		}
		if err := tampered.Verify(); err == nil {
			t.Fatal("edited transaction metadata still verifies against the compiled-plan digest")
		}
		return plan
	}
	plan := planForRevision("1")
	node, ok := plan.Node(promotionexec.NodeSnapshotWorker)
	if !ok {
		t.Fatal("compiled workflow lost attested capability node")
	}
	manifests, err := promotionexec.ManifestRegistry()
	if err != nil {
		t.Fatal(err)
	}
	manifest, ok := manifests.Lookup(capability.Key{ID: node.Capability.ID, Version: node.Capability.Version})
	if !ok {
		t.Fatalf("workflow capability %s is absent from promotion manifests", node.Capability.ID)
	}
	registry := capability.NewRegistry()
	if err := registry.Register(manifest.Definition, func(context.Context, any) (any, error) {
		handlerCalls++
		return CapabilityAnswer{Outcome: workflow.OutcomeSucceeded, Outputs: map[string]ResolvedValue{}}, nil
	}); err != nil {
		t.Fatal(err)
	}
	req := stepRequest(node)
	req.Plan = plan
	req.RecordedAt = at.At
	runner := &Runner{
		Gateway: capability.NewGateway(registry, &stubSink{}), SubjectRef: "test:runner", Tenant: "tenant-a",
		WorkflowInputs: map[string]ResolvedValue{
			"worker_id":      {Type: workflow.ValueType{Kind: workflow.KindString, Brand: "WorkerID"}, Text: "worker-1"},
			"effective_date": {Type: workflow.ValueType{Kind: workflow.KindLocalDate}, Text: "2026-09-23"},
		},
		AttestationResponses: store, AttestationClock: attest.TrustedClockFunc(func() (attest.TrustedTime, error) { return at, nil }),
	}
	_, _, err = runner.Run(context.Background(), req)
	if err != nil {
		t.Fatalf("accepted, exact attestation blocked dependent step: %v", err)
	}
	if handlerCalls != 1 {
		t.Fatalf("dependent effect calls = %d, want exactly one", handlerCalls)
	}

	stalePlan := planForRevision("2")
	staleNode, _ := stalePlan.Node(promotionexec.NodeSnapshotWorker)
	req.Plan, req.Node, req.Attempt = stalePlan, staleNode, 2
	_, _, err = runner.Run(context.Background(), req)
	if !errors.Is(err, attest.ErrRequiredAttestation) || handlerCalls != 1 {
		t.Fatalf("stale response err=%v handler calls=%d; want typed denial before dependent effect", err, handlerCalls)
	}
}

func TestTodo_REV_041_02_Security(t *testing.T) {
	at := attest.FixedTrustedTime(time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC))
	baseRequest := attest.ResponseRequest{
		Tenant: "tenant-a", ResponseID: "response-1", StatementID: "time-punch-worker-attestation",
		StatementVersion: 1, StatementDigest: "sha256:statement", BindingDigest: "sha256:proposal",
		Status: attest.ResponseAccepted, Kind: attest.AssertionResponse, EvidenceReceipt: "sha256:evidence",
		IdempotencyKey: "response-1", TransactionID: "tx-1", AffectedObligations: []string{testAttestationObligation},
	}
	for _, tc := range []struct {
		name              string
		configure         func(*Runner, *attest.ExecutionRequirement)
		configureResponse func(*attest.ResponseRequest)
		wantCalls         int
	}{
		{name: "missing gate configuration", configure: func(r *Runner, _ *attest.ExecutionRequirement) {
			r.ExecutionRequirements, r.AttestationResponses = nil, nil
		}},
		{name: "missing response", configure: func(*Runner, *attest.ExecutionRequirement) {}},
		{name: "refused response", configure: func(_ *Runner, req *attest.ExecutionRequirement) { req.ResponseID = "refused" }},
		{name: "wrong transaction", configure: func(_ *Runner, req *attest.ExecutionRequirement) { req.TransactionID = "tx-other" }},
		{name: "wrong tenant", configure: func(_ *Runner, req *attest.ExecutionRequirement) { req.Tenant = values.TenantId("other-tenant") }},
		{name: "missing obligation coverage", configure: func(*Runner, *attest.ExecutionRequirement) {}, configureResponse: func(req *attest.ResponseRequest) { req.AffectedObligations = nil }},
		{name: "wrong obligation coverage", configure: func(*Runner, *attest.ExecutionRequirement) {}, configureResponse: func(req *attest.ResponseRequest) { req.AffectedObligations = []string{"obligation.time.other"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := attest.NewMemoryResponseStore()
			recorder, err := attest.NewRecorder(store, attest.TrustedClockFunc(func() (attest.TrustedTime, error) { return at, nil }))
			if err != nil {
				t.Fatal(err)
			}
			responseReq := baseRequest
			if tc.configureResponse != nil {
				tc.configureResponse(&responseReq)
			}
			if tc.name == "refused response" {
				responseReq.ResponseID, responseReq.IdempotencyKey, responseReq.Status, responseReq.Reason = "refused", "refused", attest.ResponseRefused, "worker refused"
			}
			if tc.name != "missing response" {
				if _, err := recorder.RecordResponse(context.Background(), responseReq); err != nil {
					t.Fatal(err)
				}
			}
			requirement := attest.ExecutionRequirement{
				Tenant: baseRequest.Tenant, ObligationID: testAttestationObligation, StatementID: baseRequest.StatementID,
				StatementVersion: baseRequest.StatementVersion, StatementDigest: baseRequest.StatementDigest,
				BindingDigest: baseRequest.BindingDigest, ResponseID: baseRequest.ResponseID,
				ResponseRevision: 1, TransactionID: baseRequest.TransactionID,
			}
			registry := capability.NewRegistry()
			const capID = "rev04102.security.dependent_effect"
			calls := 0
			if err := registry.Register(testDefinition(capID, "scope:time.write"), func(context.Context, any) (any, error) {
				calls++
				return CapabilityAnswer{Outcome: workflow.OutcomeSucceeded}, nil
			}); err != nil {
				t.Fatal(err)
			}
			rec, _ := registry.Lookup(capability.Key{ID: capID, Version: 1})
			node := readNode(t, rec, []string{"scope:time.write"})
			node.Governance.ObligationRefs = []string{testAttestationObligation}
			plan := &workflow.CompiledWorkflow{Governance: workflow.GovernanceSummary{Obligations: []workflow.ObligationRequirement{{ID: testAttestationObligation, Mandatory: true}}}}
			req := stepRequest(node)
			req.Plan = plan
			tenant := "tenant-a"
			runner := &Runner{
				Gateway: capability.NewGateway(registry, &stubSink{}), SubjectRef: "test:runner", Tenant: tenant,
				WorkflowInputs:       map[string]ResolvedValue{"worker_id": {Type: strType(), Text: "worker-1"}},
				NodeOutputs:          map[string]map[string]ResolvedValue{"prev": {"band": {Type: strType(), Text: "B2"}}},
				AttestationResponses: store, AttestationClock: attest.TrustedClockFunc(func() (attest.TrustedTime, error) { return at, nil }),
				ExecutionRequirements: fixedExecutionRequirement{requirement: requirement},
			}
			tc.configure(runner, &requirement)
			if tc.name != "missing gate configuration" {
				runner.ExecutionRequirements = fixedExecutionRequirement{requirement: requirement}
			}
			_, _, err = runner.Run(context.Background(), req)
			if err == nil || !errors.Is(err, attest.ErrRequiredAttestation) {
				t.Fatalf("Run error = %v, want typed attestation denial", err)
			}
			if calls != tc.wantCalls {
				t.Fatalf("dependent effect calls = %d, want %d", calls, tc.wantCalls)
			}
		})
	}

	t.Run("unresolved requirement fails before gateway", func(t *testing.T) {
		registry := capability.NewRegistry()
		calls := 0
		const capID = "rev04102.security.unresolved_effect"
		if err := registry.Register(testDefinition(capID, "scope:time.write"), func(context.Context, any) (any, error) {
			calls++
			return nil, fmt.Errorf("must not execute")
		}); err != nil {
			t.Fatal(err)
		}
		rec, _ := registry.Lookup(capability.Key{ID: capID, Version: 1})
		node := readNode(t, rec, []string{"scope:time.write"})
		node.Governance.ObligationRefs = []string{testAttestationObligation}
		req := stepRequest(node)
		req.Plan = &workflow.CompiledWorkflow{Governance: workflow.GovernanceSummary{Obligations: []workflow.ObligationRequirement{{ID: testAttestationObligation, Mandatory: true}}}}
		runner := &Runner{Gateway: capability.NewGateway(registry, &stubSink{}), Tenant: "tenant-a", WorkflowInputs: map[string]ResolvedValue{"worker_id": {Type: strType(), Text: "worker-1"}}, NodeOutputs: map[string]map[string]ResolvedValue{"prev": {"band": {Type: strType(), Text: "B2"}}}}
		_, _, err := runner.Run(context.Background(), req)
		if !errors.Is(err, attest.ErrRequiredAttestation) || calls != 0 {
			t.Fatalf("error=%v calls=%d; want typed denial before invocation", err, calls)
		}
	})
}
