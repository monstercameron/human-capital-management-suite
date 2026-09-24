package workflow_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/transaction/idempotency"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/capabilityrunner"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute/effects"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

const (
	wfext005ServedWorkflowID = "hcmnext.workflows.test.wf_ext_005_served"
	wfext005ServedCapability = "hcmnext.test.wf_ext_005.served_read"
	wfext005ServedScope      = "scope:people.read"
	wfext005ServedNode       = "read_worker"
	wfext005ServedEnd        = "end"
	wfext005ServedFailureEnd = "end_failure"
	wfext005ServedTerminal   = "WF_EXT_005_SERVED"
	wfext005FailureTerminal  = "WF_EXT_005_FAILURE"
)

type wfext005ServedSteps struct{ runner *capabilityrunner.Runner }

func (s wfext005ServedSteps) Run(ctx context.Context, req execute.StepRequest) (frontier.NodeOutcome, runtime.GovernanceRefs, error) {
	if req.Node.Type == workflow.StepEnd {
		return frontier.NodeOutcome{NodeID: req.Node.ID}, runtime.GovernanceRefs{}, nil
	}
	return s.runner.Run(ctx, req)
}

type wfext005ServedEvidence struct{ calls int }

func (s *wfext005ServedEvidence) RecordInvocation(context.Context, capability.InvocationEvidence) (string, error) {
	s.calls++
	return fmt.Sprintf("wfext005-served-%d", s.calls), nil
}

func (s *wfext005ServedEvidence) RecordInvocationTx(ctx context.Context, evt capability.InvocationEvidence) (string, error) {
	if _, ok := dbport.TxFromContext(ctx); ok {
		return "", errors.New("workflow integration evidence sink cannot join a caller transaction")
	}
	return s.RecordInvocation(ctx, evt)
}

func TestTodo_WF_EXT_005_Integration(t *testing.T) {
	ctx := context.Background()
	at := time.Date(2026, 9, 23, 13, 0, 0, 0, time.UTC)
	key := "wfext005-served-" + uuid.NewString()
	db := pgtest.New(t)
	tenantID := insertTenant(t, db, key, at.Add(-time.Hour))
	proposal := newDemoProposal(t, values.TenantId(key), "intent:"+key, at)
	registry := capability.NewRegistry()
	definition := capability.Definition{
		ID: wfext005ServedCapability, Version: 1, OwnerDomain: "people",
		RequestSchema:  capability.SchemaRef{SchemaID: wfext005ServedCapability + ".request/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"},
		ResponseSchema: capability.SchemaRef{SchemaID: wfext005ServedCapability + ".response/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"},
		ErrorSchema:    capability.SchemaRef{SchemaID: wfext005ServedCapability + ".error/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"},
		EffectClass:    capability.EffectReadOnly, ReadData: capability.DataDomainFieldSet{DataDomains: []string{"people"}},
		RiskClass: "LOW", IdempotencyPolicyRef: "idempotency.read-safe.v1", AuthZScopeRef: wfext005ServedScope,
		LegalBasisRef: "legal.test/v1", EntitlementRef: "entitlement.test/v1", SLOClassRef: "slo.test/v1", TestRef: "conformance:wfext005-served/v1",
	}
	var handlerCalls int
	if err := registry.Register(definition, func(_ context.Context, payload any) (any, error) {
		call, ok := payload.(capabilityrunner.CapabilityCall)
		if !ok {
			return nil, fmt.Errorf("expected CapabilityCall, got %T", payload)
		}
		if call.Inputs["worker_id"].Text != "worker:wfext005" {
			return nil, fmt.Errorf("resolved worker_id = %q", call.Inputs["worker_id"].Text)
		}
		handlerCalls++
		return capabilityrunner.CapabilityAnswer{
			Outcome: workflow.OutcomeSucceeded,
			Outputs: map[string]capabilityrunner.ResolvedValue{"state": {Type: workflow.ValueType{Kind: workflow.KindString}, Text: "ACTIVE"}},
			Detail:  "read worker",
		}, nil
	}); err != nil {
		t.Fatalf("register capability: %v", err)
	}
	manifest, ok := registry.Lookup(capability.Key{ID: wfext005ServedCapability, Version: 1})
	if !ok {
		t.Fatal("registered capability is absent from the manifest registry")
	}
	plan := wfext005ServedPlan(t, registry)
	compiledNode, _ := plan.Node(wfext005ServedNode)
	if compiledNode.Capability == nil || compiledNode.Capability.Digest != manifest.Digest {
		t.Fatalf("compiled manifest = %+v, want registry digest %s", compiledNode.Capability, manifest.Digest)
	}
	versions := promotionVersionStore{plan: plan}
	resolver := effects.PolicyResolver{Entries: []effects.PolicyEntry{{WorkflowID: plan.WorkflowID, Pin: version.Pin{CompiledPlanDigest: plan.Digest()}, Plan: plan}}}
	start := runtime.StartRequest{
		TenantID: tenantID, CellID: "cell-local", StartIdempotencyKey: "start:" + key,
		Resolver: resolver, Versions: versions,
		Proposal:      runtime.ProposalBinding{Revision: proposal, ApprovalRef: "decision:" + key},
		ProposalFacts: runtime.MemoryProposalFacts{}, ApprovalFacts: approvedStartFacts(proposal),
		ExpectedIntentID: proposal.IntentID, ExpectedTenant: proposal.Tenant,
		BusinessSubjectRefs: []string{"employment:promotion-execute-demo-1"},
		ExecutionMode:       workflow.ModeExecute, CorrelationID: "corr:" + key, CreatedAt: at,
	}
	evidence := &wfext005ServedEvidence{}
	runner := &capabilityrunner.Runner{Gateway: capability.NewGateway(registry, evidence), SubjectRef: "test:workflow-runner", Tenant: key}
	terminal := &effects.LedgerTerminalWriter{Appender: newLedgerAppender(t), ProjectionName: "wfext005_served_outcome", SourceRef: "hcmnext:test:wfext005-served"}
	driver, err := execute.New(execute.Options{
		DB: appConn(t, db), Steps: wfext005ServedSteps{runner: runner}, Terminal: terminal,
		Guard: idempotency.PostgresStore{}, Retention: idempotency.RetentionPolicy{Retention: 72 * time.Hour, RetryWindow: 6 * time.Hour},
		Clock: func() time.Time { return at },
	})
	if err != nil {
		t.Fatalf("execute.New: %v", err)
	}
	result, err := driver.Execute(ctx, execute.ExecuteRequest{Start: start, Inputs: []workflow.TypedOutput{{Path: "worker_id", Value: workflow.TypedValue{Type: workflow.ValueType{Kind: workflow.KindString}, Text: "worker:wfext005"}}}})
	if err != nil {
		t.Fatalf("served capability Execute: %v", err)
	}
	if result.Status != execute.StatusComplete || handlerCalls != 1 || evidence.calls != 1 {
		t.Fatalf("result status=%s handler calls=%d evidence=%d; want COMPLETE and one governed call", result.Status, handlerCalls, evidence.calls)
	}
	unauthorizedPlan := wfext005ServedPlan(t, registry)
	unauthorizedNode, _ := unauthorizedPlan.Node(wfext005ServedNode)
	unauthorizedNode.Capability.AuthorityScopes = []string{"scope:people.write"}
	_, _, err = runner.Run(ctx, execute.StepRequest{
		TenantID: tenantID, InstanceID: uuid.New(), Attempt: 1, Node: unauthorizedNode, Plan: unauthorizedPlan, RecordedAt: at,
		Inputs: map[string]workflow.TypedValue{"worker_id": {Type: workflow.ValueType{Kind: workflow.KindString}, Text: "worker:wfext005"}},
	})
	var gatewayError *capability.GatewayError
	if !errors.As(err, &gatewayError) || gatewayError.Code != capability.CodeUnauthorized {
		t.Fatalf("unauthorized workflow node error = %v; want gateway scope denial", err)
	}
	if handlerCalls != 1 || evidence.calls != 2 {
		t.Fatalf("after scope denial handler calls=%d evidence=%d; want no extra effect and one refusal record", handlerCalls, evidence.calls)
	}

	var artifacts []runtime.NodeOutputArtifact
	inTenantTx(t, db, tenantID, func(tx dbport.Tx) error {
		var loadErr error
		artifacts, loadErr = runtime.LoadNodeOutputs(ctx, tx, tenantID, result.Start.InstanceID)
		return loadErr
	})
	for _, artifact := range artifacts {
		if artifact.NodeID != wfext005ServedNode {
			continue
		}
		value, found := artifact.Output("state")
		if !found || value.Type.Kind != workflow.KindString || value.Value != "ACTIVE" {
			t.Fatalf("persisted typed output = %+v, want state=ACTIVE STRING", value)
		}
		return
	}
	t.Fatalf("driver persisted no capability output artifact: %+v", artifacts)
}

func wfext005ServedPlan(t *testing.T, registry workflow.CapabilityResolver) *workflow.CompiledWorkflow {
	t.Helper()
	stringType := workflow.ValueType{Kind: workflow.KindString}
	authorityScopes := []string{wfext005ServedScope}
	input := workflow.Field{Path: "worker_id", Type: stringType}
	terminalInputs := []workflow.Field{{Path: "worker_id", Type: stringType}, {Path: "terminal_code", Type: stringType}}
	def := workflow.Definition{
		WorkflowID: wfext005ServedWorkflowID, Version: 1, Name: "WF-EXT-005 served integration",
		InputSchema:     workflow.SchemaRef{SchemaID: "wfext005.input/v1", Version: 1, ProtobufFullName: "hcmnext.test.WFEXT005Input"},
		OutputSchema:    workflow.SchemaRef{SchemaID: "wfext005.output/v1", Version: 1, ProtobufFullName: "hcmnext.test.WFEXT005Output"},
		VariablesSchema: workflow.SchemaRef{SchemaID: "wfext005.variables/v1", Version: 1, ProtobufFullName: "hcmnext.test.WFEXT005Variables"},
		Inputs:          []workflow.Field{input}, Outputs: terminalInputs,
		TenantScope: "test", OrganizationScope: "org:test/wfext005", RiskClass: "LOW",
		DeclaredModes: []workflow.ExecutionMode{workflow.ModeExecute}, TerminalProfile: workflow.TerminalProfileExecute,
		StartNodeID: wfext005ServedNode, Limits: workflow.Limits{MaxFanOut: 4, MaxDepth: 1, MaxNodes: 3},
		FailurePolicyRef: "policy.workflow.failure.test/v1", CancellationPolicyRef: "policy.workflow.cancel.test/v1",
		MigrationPolicyRef: "policy.workflow.migrate.test/v1", RetentionPolicyRef: "policy.workflow.retain.test/v1",
		Nodes: []workflow.Node{
			{ID: wfext005ServedNode, Type: workflow.StepCapability,
				InputSchema:  workflow.SchemaRef{SchemaID: wfext005ServedCapability + ".request/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"},
				OutputSchema: workflow.SchemaRef{SchemaID: wfext005ServedCapability + ".response/v1", Version: 1, ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition"},
				Inputs:       []workflow.Field{input}, Outputs: []workflow.Field{{Path: "state", Type: stringType}},
				InputMappings:  []workflow.Mapping{{Target: "worker_id", Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: "worker_id"}}},
				Capability:     &workflow.CapabilityRef{ID: wfext005ServedCapability, Version: 1, OperationMode: workflow.ModeExecute, AuthorityScopes: authorityScopes},
				DeclaredEffect: capability.EffectReadOnly, Governance: workflow.NodeGovernance{Purpose: "WF_EXT_005_TEST", Classification: "CONFIDENTIAL_HR", RequiredDecisions: []workflow.GovernanceKind{workflow.GovernanceAuthZ, workflow.GovernanceLegal, workflow.GovernancePurpose, workflow.GovernanceRisk}, RevalidationBoundary: workflow.RevalidateNone, DataAccessManifestRef: "data-access.test.wfext005/v1"}},
			{ID: wfext005ServedEnd, Type: workflow.StepEnd,
				InputSchema:  workflow.SchemaRef{SchemaID: "wfext005.end.input/v1", Version: 1, ProtobufFullName: "hcmnext.test.WFEXT005EndInput"},
				OutputSchema: workflow.SchemaRef{SchemaID: "wfext005.end.output/v1", Version: 1, ProtobufFullName: "hcmnext.test.WFEXT005EndOutput"},
				Inputs:       terminalInputs, InputMappings: []workflow.Mapping{
					{Target: "worker_id", Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: "worker_id"}},
					{Target: "terminal_code", Source: workflow.Source{Kind: workflow.SourceConstant, Constant: wfext005ServedTerminal, Type: stringType}},
				},
				DeclaredEffect: capability.EffectPure, Governance: workflow.NodeGovernance{Purpose: "WF_EXT_005_TEST", Classification: "CONFIDENTIAL_HR", DataAccessManifestRef: "data-access.test.wfext005/v1"},
				End: &workflow.EndSpec{TerminalCode: wfext005ServedTerminal, RuntimeStatus: workflow.RuntimeCompleted, CommitReceiptRef: "receipt.test.wfext005/v1",
					CompletionMapping: map[string]string{"RequestState": "CLOSED", "ExecutionState": "COMMITTED", "BusinessState": "COMPLETED", "ConsistencyState": "CONSISTENT", "ObligationState": "SATISFIED"}}},
			{ID: wfext005ServedFailureEnd, Type: workflow.StepEnd,
				InputSchema:  workflow.SchemaRef{SchemaID: "wfext005.failure.end.input/v1", Version: 1, ProtobufFullName: "hcmnext.test.WFEXT005FailureEndInput"},
				OutputSchema: workflow.SchemaRef{SchemaID: "wfext005.failure.end.output/v1", Version: 1, ProtobufFullName: "hcmnext.test.WFEXT005FailureEndOutput"},
				Inputs:       terminalInputs, InputMappings: []workflow.Mapping{
					{Target: "worker_id", Source: workflow.Source{Kind: workflow.SourceWorkflowInput, Path: "worker_id"}},
					{Target: "terminal_code", Source: workflow.Source{Kind: workflow.SourceConstant, Constant: wfext005FailureTerminal, Type: stringType}},
				},
				DeclaredEffect: capability.EffectPure, Governance: workflow.NodeGovernance{Purpose: "WF_EXT_005_TEST", Classification: "CONFIDENTIAL_HR", DataAccessManifestRef: "data-access.test.wfext005/v1"},
				End: &workflow.EndSpec{TerminalCode: wfext005FailureTerminal, RuntimeStatus: workflow.RuntimeBlocked, CommitReceiptRef: "receipt.test.wfext005/failure/v1",
					CompletionMapping: map[string]string{"RequestState": "APPROVED", "ExecutionState": "BLOCKED", "BusinessState": "UNKNOWN", "ConsistencyState": "UNKNOWN", "ObligationState": "PENDING"}}},
		},
		Edges: []workflow.Edge{{From: wfext005ServedNode, To: wfext005ServedEnd, RouteKey: "SUCCEEDED"}, {From: wfext005ServedNode, To: wfext005ServedFailureEnd, RouteKey: "AMBIGUOUS"}, {From: wfext005ServedNode, To: wfext005ServedFailureEnd, RouteKey: "REJECTED"}, {From: wfext005ServedNode, To: wfext005ServedFailureEnd, RouteKey: "UNKNOWN"}},
	}
	plan, err := workflow.Compile(def, workflow.Options{Phase: workflow.PhaseP1B, Capabilities: registry})
	if err != nil {
		t.Fatalf("compile served test workflow: %v", err)
	}
	return plan
}
