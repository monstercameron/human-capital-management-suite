package capabilityrunner

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe/observetest"
)

// stubSink records gateway evidence with deterministic ids.
type stubSink struct {
	called int
}

func (s *stubSink) RecordInvocation(_ context.Context, _ capability.InvocationEvidence) (string, error) {
	s.called++
	return fmt.Sprintf("ev-%d", s.called), nil
}

func (s *stubSink) RecordInvocationTx(ctx context.Context, evt capability.InvocationEvidence) (string, error) {
	return s.RecordInvocation(ctx, evt)
}

func testSchema(id, slot string) capability.SchemaRef {
	return capability.SchemaRef{
		SchemaID:         id + "." + slot + "/v1",
		Version:          1,
		ProtobufFullName: "hcmnext.capabilities.v1.CapabilityDefinition",
	}
}

func testDefinition(id, scope string) capability.Definition {
	return capability.Definition{
		ID:                   id,
		Version:              1,
		OwnerDomain:          "people",
		RequestSchema:        testSchema(id, "request"),
		ResponseSchema:       testSchema(id, "response"),
		ErrorSchema:          testSchema(id, "error"),
		EffectClass:          capability.EffectReadOnly,
		ReadData:             capability.DataDomainFieldSet{DataDomains: []string{"people"}},
		RiskClass:            "LOW",
		IdempotencyPolicyRef: "idempotency.read-safe.v1",
		AuthZScopeRef:        scope,
		LegalBasisRef:        "legal.test/v1",
		EntitlementRef:       "entitlement.test/v1",
		SLOClassRef:          "slo.test/v1",
		TestRef:              "conformance:" + id + "/v1",
	}
}

func strType() workflow.ValueType { return workflow.ValueType{Kind: workflow.KindString} }

// readNode builds a compiled CAPABILITY node exercising every resolvable
// mapping kind: a workflow input, a predecessor output and a constant.
func readNode(t *testing.T, rec capability.Record, scopes []string) workflow.CompiledNode {
	t.Helper()
	return workflow.CompiledNode{
		ID:   "read_worker",
		Type: workflow.StepCapability,
		Mappings: []workflow.CompiledMapping{
			{Target: "worker_id", TargetType: strType(), SourceKind: workflow.SourceWorkflowInput, SourcePath: "worker_id", SourceType: strType(), PinnedInput: true},
			{Target: "band", TargetType: strType(), SourceKind: workflow.SourceNodeOutput, SourceNode: "prev", SourcePath: "band", SourceType: strType(), PinnedInput: true},
			{Target: "region", TargetType: strType(), SourceKind: workflow.SourceConstant, SourceType: strType(), Constant: "EMEA", PinnedInput: true},
		},
		Capability: &workflow.CompiledCapability{
			ID:              rec.Definition.ID,
			Version:         rec.Definition.Version,
			Digest:          rec.Digest,
			Status:          rec.Status,
			OwnerDomain:     rec.Definition.OwnerDomain,
			EffectClass:     rec.Definition.EffectClass,
			AuthZScopeRef:   rec.Definition.AuthZScopeRef,
			OperationMode:   workflow.ModeExecute,
			AuthorityScopes: append([]string(nil), scopes...),
		},
		EffectClass: capability.EffectReadOnly,
	}
}

func stepRequest(node workflow.CompiledNode) execute.StepRequest {
	return execute.StepRequest{
		TenantID:   uuid.New(),
		InstanceID: uuid.New(),
		Attempt:    1,
		Node:       node,
		RecordedAt: time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC),
	}
}

func TestCapabilityRunnerObservesFailureWithoutGateway(t *testing.T) {
	recorder := &observetest.Recorder{}
	req := stepRequest(workflow.CompiledNode{ID: "read_worker", Type: workflow.StepCapability})
	var runner *Runner
	_, _, err := runner.Run(recorder.Context(context.Background()), req)
	if err == nil {
		t.Fatal("missing gateway accepted")
	}
	ops := recorder.Named("workflow.execute.capability")
	if len(ops) != 1 || ops[0].Ended != 1 || ops[0].Outcome != observe.OutcomeFailure {
		t.Fatalf("configuration failure telemetry: %+v", ops)
	}
	if ops[0].Attrs[observe.KeyNode] != req.Node.ID || ops[0].Attrs[observe.KeyInstance] != req.InstanceID.String() {
		t.Fatalf("missing workflow binding: %+v", ops[0].Attrs)
	}
}

// TestTodo_WF_EXT_005 proves planning/todos.md WF-EXT-005: a generic runner
// invokes the capability gateway with the node's resolved mapping as the
// typed request and stores the typed response as the node's output digest.
func TestTodo_WF_EXT_005(t *testing.T) {
	const capID = "wfext005.read_worker"
	const scope = "scope:people.read"

	registry := capability.NewRegistry()
	var handlerCalls int
	var seenPayload CapabilityCall
	if err := registry.Register(testDefinition(capID, scope), func(_ context.Context, payload any) (any, error) {
		call, ok := payload.(CapabilityCall)
		if !ok {
			return nil, fmt.Errorf("runner must present CapabilityCall, got %T", payload)
		}
		seenPayload = call
		handlerCalls++
		return CapabilityAnswer{
			Outcome: workflow.OutcomeSucceeded,
			Outputs: map[string]ResolvedValue{"state": {Type: strType(), Text: "ACTIVE"}},
			Detail:  "read worker",
		}, nil
	}); err != nil {
		t.Fatalf("publish %s: %v", capID, err)
	}
	rec, found := registry.Lookup(capability.Key{ID: capID, Version: 1})
	if !found {
		t.Fatalf("registry has no %s/v1", capID)
	}

	t.Run("GREEN_resolved_mapping_invokes_gateway_and_stores_typed_response", func(t *testing.T) {
		node := readNode(t, rec, []string{scope})
		if node.Capability.Digest != rec.Digest {
			t.Fatalf("compiled manifest digest %q != registry digest %q: manifests must come from the registry", node.Capability.Digest, rec.Digest)
		}
		sink := &stubSink{}
		var stored map[string]ResolvedValue
		runner := &Runner{
			Gateway:        capability.NewGateway(registry, sink),
			SubjectRef:     "test:runner",
			Tenant:         "acme",
			WorkflowInputs: map[string]ResolvedValue{"worker_id": {Type: strType(), Text: "W-1"}},
			NodeOutputs:    map[string]map[string]ResolvedValue{"prev": {"band": {Type: strType(), Text: "B2"}}},
			OnOutputs:      func(_ string, outputs map[string]ResolvedValue) { stored = outputs },
		}
		request := stepRequest(node)
		request.Inputs = map[string]workflow.TypedValue{
			"worker_id": {Type: strType(), Text: "W-1"},
			"band":      {Type: strType(), Text: "B2"},
			"region":    {Type: strType(), Text: "EMEA"},
		}
		runner.WorkflowInputs["worker_id"] = ResolvedValue{Type: strType(), Text: "untrusted-side-map"}
		runner.NodeOutputs["prev"]["band"] = ResolvedValue{Type: strType(), Text: "untrusted-side-map"}
		outcome, refs, err := runner.Run(context.Background(), request)
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
		if handlerCalls != 1 {
			t.Fatalf("handler calls = %d, want 1: the runner must invoke the gateway exactly once", handlerCalls)
		}
		if seenPayload.NodeID != "read_worker" || seenPayload.Capability.ID != capID || seenPayload.OperationMode != workflow.ModeExecute {
			t.Fatalf("payload = %+v, want node read_worker capability %s mode EXECUTE", seenPayload, capID)
		}
		if got := seenPayload.Inputs["worker_id"].Text; got != "W-1" {
			t.Fatalf("worker_id = %q, want W-1 (workflow input)", got)
		}
		if got := seenPayload.Inputs["band"].Text; got != "B2" {
			t.Fatalf("band = %q, want B2 (predecessor output)", got)
		}
		if got := seenPayload.Inputs["region"].Text; got != "EMEA" {
			t.Fatalf("region = %q, want EMEA (constant)", got)
		}
		if outcome.NodeID != "read_worker" || outcome.Outcome != workflow.OutcomeSucceeded {
			t.Fatalf("outcome = %+v, want node read_worker SUCCEEDED", outcome)
		}
		if outcome.Outputs == nil || len(outcome.Outputs.Values) != 1 || outcome.Outputs.Values[0].Path != "state" || outcome.Outputs.Values[0].Value.Text != "ACTIVE" {
			t.Fatalf("typed outputs = %+v, want the handler's response for WF-EXT-004 persistence", outcome.Outputs)
		}
		if refs.CapabilityExecutionID != "ev-1" {
			t.Fatalf("capability execution = %q, want ev-1 (gateway evidence)", refs.CapabilityExecutionID)
		}
		if stored["state"].Text != "ACTIVE" {
			t.Fatalf("stored outputs = %v, want the typed response for WF-EXT-004 persistence", stored)
		}

		// Determinism: the same typed response digests identically.
		second, _, err := runner.Run(context.Background(), request)
		if err != nil {
			t.Fatalf("second Run: %v", err)
		}
		if second.Outputs == nil || len(second.Outputs.Values) != len(outcome.Outputs.Values) || second.Outputs.Values[0] != outcome.Outputs.Values[0] {
			t.Fatalf("typed outputs changed across identical invocations: %+v vs %+v", outcome.Outputs, second.Outputs)
		}
	})

	t.Run("GREEN_observe_nodes_invoke_through_the_same_gateway", func(t *testing.T) {
		const obsID = "wfext005.observe_worker"
		obsRegistry := capability.NewRegistry()
		var obsCalls int
		if err := obsRegistry.Register(testDefinition(obsID, scope), func(_ context.Context, payload any) (any, error) {
			call, ok := payload.(CapabilityCall)
			if !ok {
				return nil, fmt.Errorf("runner must present CapabilityCall, got %T", payload)
			}
			if call.Inputs["worker_id"].Text != "W-9" {
				return nil, fmt.Errorf("worker_id = %q, want W-9", call.Inputs["worker_id"].Text)
			}
			obsCalls++
			return CapabilityAnswer{
				Outcome: workflow.OutcomePass,
				Outputs: map[string]ResolvedValue{"observed": {Type: strType(), Text: "true"}},
				Detail:  "observed worker",
			}, nil
		}); err != nil {
			t.Fatalf("publish %s: %v", obsID, err)
		}
		obsRec, found := obsRegistry.Lookup(capability.Key{ID: obsID, Version: 1})
		if !found {
			t.Fatalf("registry has no %s/v1", obsID)
		}
		node := workflow.CompiledNode{
			ID:   "observe_worker",
			Type: workflow.StepObserve,
			Mappings: []workflow.CompiledMapping{
				{Target: "worker_id", TargetType: strType(), SourceKind: workflow.SourceWorkflowInput, SourcePath: "worker_id", SourceType: strType(), PinnedInput: true},
			},
			Capability: &workflow.CompiledCapability{
				ID:              obsRec.Definition.ID,
				Version:         obsRec.Definition.Version,
				Digest:          obsRec.Digest,
				Status:          obsRec.Status,
				OwnerDomain:     obsRec.Definition.OwnerDomain,
				EffectClass:     obsRec.Definition.EffectClass,
				AuthZScopeRef:   obsRec.Definition.AuthZScopeRef,
				OperationMode:   workflow.ModeExecute,
				AuthorityScopes: []string{scope},
			},
			EffectClass: capability.EffectReadOnly,
			Observe: &workflow.CompiledObserve{
				EvidenceKind:       workflow.EvidenceAuthoritativeRead,
				SourceAuthority:    "people",
				ComparisonProfile:  "comparison.test/v1",
				MaxAgeSeconds:      300,
				RequiredWatermarks: []string{"people.stream_head"},
			},
		}
		runner := &Runner{
			Gateway:        capability.NewGateway(obsRegistry, &stubSink{}),
			SubjectRef:     "test:runner",
			Tenant:         "acme",
			WorkflowInputs: map[string]ResolvedValue{"worker_id": {Type: strType(), Text: "W-9"}},
		}
		outcome, refs, err := runner.Run(context.Background(), stepRequest(node))
		if err != nil {
			t.Fatalf("OBSERVE Run: %v", err)
		}
		if obsCalls != 1 {
			t.Fatalf("handler calls = %d, want 1", obsCalls)
		}
		if outcome.Outcome != workflow.OutcomePass {
			t.Fatalf("outcome = %q, want PASS", outcome.Outcome)
		}
		if outcome.Outputs == nil || len(outcome.Outputs.Values) != 1 || outcome.Outputs.Values[0].Path != "observed" {
			t.Fatalf("typed outputs = %+v, want the OBSERVE handler response for durable persistence", outcome.Outputs)
		}
		if refs.CapabilityExecutionID == "" {
			t.Fatal("no gateway evidence recorded for the OBSERVE invocation")
		}
	})

	t.Run("GREEN_governed_envelope_carries_purpose_deadline_and_idempotency", func(t *testing.T) {
		node := readNode(t, rec, []string{scope})
		now := time.Now().UTC()
		runner := &Runner{
			Gateway:        capability.NewGateway(registry, &stubSink{}),
			SubjectRef:     "test:runner",
			Tenant:         "acme",
			Purpose:        "WF_EXT_005_TEST",
			Now:            func() time.Time { return now },
			WorkflowInputs: map[string]ResolvedValue{"worker_id": {Type: strType(), Text: "W-1"}},
			NodeOutputs:    map[string]map[string]ResolvedValue{"prev": {"band": {Type: strType(), Text: "B2"}}},
		}
		req := stepRequest(node)
		req.RecordedAt = now
		if _, _, err := runner.Run(context.Background(), req); err != nil {
			t.Fatalf("Run with governed envelope: %v", err)
		}
	})

	t.Run("RED_refusals_fail_closed_without_invoking_a_handler", func(t *testing.T) {
		cases := []struct {
			name string
			node workflow.CompiledNode
		}{
			{"non_capability_node", workflow.CompiledNode{ID: "decide", Type: workflow.StepDecision, Capability: &workflow.CompiledCapability{ID: capID, Version: 1}}},
			{"missing_capability_binding", workflow.CompiledNode{ID: "read_worker", Type: workflow.StepCapability}},
			{"missing_workflow_input", workflow.CompiledNode{ID: "read_worker", Type: workflow.StepCapability,
				Mappings:   []workflow.CompiledMapping{{Target: "worker_id", TargetType: strType(), SourceKind: workflow.SourceWorkflowInput, SourcePath: "absent", SourceType: strType()}},
				Capability: &workflow.CompiledCapability{ID: capID, Version: 1, AuthorityScopes: []string{scope}}}},
			{"missing_predecessor_output", workflow.CompiledNode{ID: "read_worker", Type: workflow.StepCapability,
				Mappings:   []workflow.CompiledMapping{{Target: "band", TargetType: strType(), SourceKind: workflow.SourceNodeOutput, SourceNode: "prev", SourcePath: "absent", SourceType: strType()}},
				Capability: &workflow.CompiledCapability{ID: capID, Version: 1, AuthorityScopes: []string{scope}}}},
			{"context_mapping_needs_wf_ext_004", workflow.CompiledNode{ID: "read_worker", Type: workflow.StepCapability,
				Mappings:   []workflow.CompiledMapping{{Target: "field", TargetType: strType(), SourceKind: workflow.SourceContext, SourceCtx: "hr", SourcePath: "field", SourceType: strType()}},
				Capability: &workflow.CompiledCapability{ID: capID, Version: 1, AuthorityScopes: []string{scope}}}},
			{"unknown_capability", workflow.CompiledNode{ID: "read_worker", Type: workflow.StepCapability,
				Capability: &workflow.CompiledCapability{ID: "wfext005.absent", Version: 1, AuthorityScopes: []string{scope}}}},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				before := handlerCalls
				runner := &Runner{
					Gateway:        capability.NewGateway(registry, &stubSink{}),
					SubjectRef:     "test:runner",
					Tenant:         "acme",
					WorkflowInputs: map[string]ResolvedValue{"worker_id": {Type: strType(), Text: "W-1"}},
					NodeOutputs:    map[string]map[string]ResolvedValue{"prev": {"band": {Type: strType(), Text: "B2"}}},
				}
				if _, _, err := runner.Run(context.Background(), stepRequest(tc.node)); err == nil {
					t.Fatalf("%s: expected an error", tc.name)
				}
				if handlerCalls != before {
					t.Fatalf("%s: handler ran on a refused node", tc.name)
				}
			})
		}
	})

	t.Run("RED_malformed_handler_answers_fail_closed", func(t *testing.T) {
		badRegistry := capability.NewRegistry()
		answers := []any{
			"not-a-typed-answer",
			CapabilityAnswer{},
			CapabilityAnswer{Outcome: "BOGUS", Outputs: map[string]ResolvedValue{}},
		}
		for i, answer := range answers {
			if err := badRegistry.Register(testDefinition(fmt.Sprintf("wfext005.bad_%d", i), scope), func(_ context.Context, _ any) (any, error) {
				return answer, nil
			}); err != nil {
				t.Fatalf("publish bad_%d: %v", i, err)
			}
		}
		for i := range answers {
			node := workflow.CompiledNode{ID: "read_worker", Type: workflow.StepCapability,
				Capability: &workflow.CompiledCapability{ID: fmt.Sprintf("wfext005.bad_%d", i), Version: 1, AuthorityScopes: []string{scope}}}
			runner := &Runner{Gateway: capability.NewGateway(badRegistry, &stubSink{}), SubjectRef: "test:runner", Tenant: "acme"}
			if _, _, err := runner.Run(context.Background(), stepRequest(node)); err == nil {
				t.Fatalf("bad answer %d: expected an error", i)
			}
		}
	})
}

// TestTodo_WF_EXT_005_Integration proves the registry half of WF-EXT-005:
// manifests come from the registry, never from a synthesized table, and a
// node outside the registry cannot invoke anything.
func TestTodo_WF_EXT_005_Integration(t *testing.T) {
	const capID = "wfext005.observe_payroll"
	const scope = "scope:observation.read"

	registry := capability.NewRegistry()
	var handlerCalls int
	if err := registry.Register(testDefinition(capID, scope), func(_ context.Context, payload any) (any, error) {
		handlerCalls++
		return CapabilityAnswer{
			Outcome: workflow.OutcomePass,
			Outputs: map[string]ResolvedValue{"payroll_state": {Type: strType(), Text: "APPLIED"}},
			Detail:  "observed payroll",
		}, nil
	}); err != nil {
		t.Fatalf("publish %s: %v", capID, err)
	}
	rec, found := registry.Lookup(capability.Key{ID: capID, Version: 1})
	if !found {
		t.Fatalf("registry has no %s/v1", capID)
	}

	node := workflow.CompiledNode{
		ID:   "observe_payroll",
		Type: workflow.StepObserve,
		Mappings: []workflow.CompiledMapping{
			{Target: "worker_id", TargetType: strType(), SourceKind: workflow.SourceWorkflowInput, SourcePath: "worker_id", SourceType: strType(), PinnedInput: true},
		},
		Capability: &workflow.CompiledCapability{
			ID:              rec.Definition.ID,
			Version:         rec.Definition.Version,
			Digest:          rec.Digest,
			Status:          rec.Status,
			OwnerDomain:     rec.Definition.OwnerDomain,
			EffectClass:     rec.Definition.EffectClass,
			AuthZScopeRef:   rec.Definition.AuthZScopeRef,
			OperationMode:   workflow.ModeExecute,
			AuthorityScopes: []string{scope},
		},
		EffectClass: capability.EffectReadOnly,
		Observe: &workflow.CompiledObserve{
			EvidenceKind:       workflow.EvidenceAuthoritativeRead,
			SourceAuthority:    "payroll",
			ComparisonProfile:  "comparison.test/v1",
			MaxAgeSeconds:      300,
			RequiredWatermarks: []string{"payroll.stream_head"},
		},
	}
	if node.Capability.Digest == "" || node.Capability.Digest != rec.Digest {
		t.Fatalf("compiled digest %q must pin registry digest %q", node.Capability.Digest, rec.Digest)
	}

	runner := &Runner{
		Gateway:        capability.NewGateway(registry, &stubSink{}),
		SubjectRef:     "test:runner",
		Tenant:         "acme",
		WorkflowInputs: map[string]ResolvedValue{"worker_id": {Type: strType(), Text: "W-7"}},
	}
	outcome, refs, err := runner.Run(context.Background(), stepRequest(node))
	if err != nil {
		t.Fatalf("OBSERVE Run: %v", err)
	}
	if outcome.Outcome != workflow.OutcomePass {
		t.Fatalf("outcome = %q, want PASS", outcome.Outcome)
	}
	if outcome.Outputs == nil || len(outcome.Outputs.Values) != 1 || outcome.Outputs.Values[0].Path != "payroll_state" {
		t.Fatalf("typed outputs = %+v, want the OBSERVE handler response for durable persistence", outcome.Outputs)
	}
	if refs.CapabilityExecutionID == "" {
		t.Fatal("no gateway evidence recorded for the OBSERVE invocation")
	}
	if handlerCalls != 1 {
		t.Fatalf("handler calls = %d, want 1", handlerCalls)
	}

	t.Run("retired_capability_refused", func(t *testing.T) {
		if err := registry.Retire(capability.Key{ID: capID, Version: 1}); err != nil {
			t.Fatalf("retire: %v", err)
		}
		before := handlerCalls
		if _, _, err := runner.Run(context.Background(), stepRequest(node)); err == nil {
			t.Fatal("expected a refusal for a retired capability")
		} else {
			var gwErr *capability.GatewayError
			if !errors.As(err, &gwErr) || gwErr.Code != capability.CodeCapabilityDisabled {
				t.Fatalf("error = %v, want %s", err, capability.CodeCapabilityDisabled)
			}
		}
		if handlerCalls != before {
			t.Fatal("handler ran for a retired capability")
		}
	})
}

// TestTodo_WF_EXT_005_Security proves the SECURITY matrix of WF-EXT-005: a
// node cannot invoke a capability outside its compiled authorization scope.
func TestTodo_WF_EXT_005_Security(t *testing.T) {
	const capID = "wfext005.rewards_read"
	const required = "scope:rewards.read"

	registry := capability.NewRegistry()
	var handlerCalls int
	def := testDefinition(capID, required)
	def.OwnerDomain = "rewards"
	if err := registry.Register(def, func(_ context.Context, _ any) (any, error) {
		handlerCalls++
		return CapabilityAnswer{Outcome: workflow.OutcomeSucceeded, Outputs: map[string]ResolvedValue{}}, nil
	}); err != nil {
		t.Fatalf("publish %s: %v", capID, err)
	}

	nodeFor := func(scopes []string) workflow.CompiledNode {
		return workflow.CompiledNode{ID: "read_band", Type: workflow.StepCapability,
			Capability: &workflow.CompiledCapability{ID: capID, Version: 1, AuthZScopeRef: required, OperationMode: workflow.ModeExecute, AuthorityScopes: scopes}}
	}

	t.Run("outside_scope_refused_without_invoking_handler", func(t *testing.T) {
		for name, scopes := range map[string][]string{
			"wrong_scope": {"scope:people.read"},
			"no_scopes":   nil,
		} {
			t.Run(name, func(t *testing.T) {
				before := handlerCalls
				runner := &Runner{Gateway: capability.NewGateway(registry, &stubSink{}), SubjectRef: "test:runner", Tenant: "acme"}
				if _, _, err := runner.Run(context.Background(), stepRequest(nodeFor(scopes))); err == nil {
					t.Fatal("expected an authorization refusal")
				} else {
					var gwErr *capability.GatewayError
					if !errors.As(err, &gwErr) || gwErr.Code != capability.CodeUnauthorized {
						t.Fatalf("error = %v, want %s", err, capability.CodeUnauthorized)
					}
				}
				if handlerCalls != before {
					t.Fatal("handler ran outside the node's compiled authorization scope")
				}
			})
		}
	})

	t.Run("declared_scope_invokes", func(t *testing.T) {
		runner := &Runner{Gateway: capability.NewGateway(registry, &stubSink{}), SubjectRef: "test:runner", Tenant: "acme"}
		if _, _, err := runner.Run(context.Background(), stepRequest(nodeFor([]string{required}))); err != nil {
			t.Fatalf("declared scope must invoke: %v", err)
		}
		if handlerCalls != 1 {
			t.Fatalf("handler calls = %d, want 1", handlerCalls)
		}
	})
}
