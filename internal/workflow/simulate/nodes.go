package simulate

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// Evidence reference names the compiler declares per step type. The
// interpreter fills each one with a real identity rather than a placeholder,
// so a receipt's evidence list is a set of things that exist.
const (
	evCapabilityExecutionID = "capability_execution_id"
	evRequestDigest         = "request_digest"
	evOutputDigest          = "output_digest"
	evObservationID         = "observation_id"
	evSourceWatermark       = "source_watermark"
	evDecisionID            = "decision_id"
	evEvaluatedInputDigest  = "evaluated_input_digest"
	evEvaluationTraceRef    = "evaluation_trace_ref"
	evTransformExecutionID  = "transform_execution_id"
	evInputDigest           = "input_digest"
	evTaintManifestRef      = "taint_manifest_ref"
	evRecordedEffect        = "recorded_effect"
)

// dispatch executes one node according to its step type and returns the
// outcome it produced, the typed outputs, a short deterministic statement of
// what happened, and any evidence identities the execution itself minted.
func (r *run) dispatch(
	ctx context.Context,
	node workflow.CompiledNode,
	inputs Bag,
	items []WorkItem,
) (workflow.Outcome, Bag, string, map[string]string, error) {
	switch node.Type {
	case workflow.StepCapability:
		return r.runCapability(ctx, node, inputs)
	case workflow.StepDecision:
		return r.runDecision(ctx, node, inputs)
	case workflow.StepTransform:
		return r.runTransform(ctx, node, inputs)
	case workflow.StepObserve:
		return r.runObserve(ctx, node, inputs)
	case workflow.StepApproval, workflow.StepTask:
		return r.runHumanWork(node, items)
	case workflow.StepEnd:
		return r.runEnd(node, inputs)
	default:
		return "", nil, "", nil, refuse(CodeStepNotImplemented, node.ID,
			"%s is not executed by the P1A simulator", node.Type)
	}
}

// runCapability invokes a capability through the governed gateway. Every
// admission the gateway performs - version resolution, retired status,
// authorization decision, required scope and the P1A write-effect refusal -
// happens on this path; the interpreter never calls a handler directly.
func (r *run) runCapability(
	ctx context.Context,
	node workflow.CompiledNode,
	inputs Bag,
) (workflow.Outcome, Bag, string, map[string]string, error) {
	if node.Capability == nil {
		return "", nil, "", nil, refuse(CodeUnresolvedSource, node.ID,
			"CAPABILITY node binds no capability")
	}
	if node.EffectClass.IsWrite() {
		return r.recordCapability(node, inputs)
	}
	if r.gateway == nil {
		return "", nil, "", nil, refuse(CodeInvalidOptions, node.ID,
			"plan invokes %s/v%d but no capability registry was supplied",
			node.Capability.ID, node.Capability.Version)
	}
	key := capability.Key{ID: node.Capability.ID, Version: node.Capability.Version}
	request := capability.InvokeRequest{
		Capability: key,
		Payload: CapabilityRequest{
			NodeID:        node.ID,
			Capability:    key,
			OperationMode: node.Capability.OperationMode,
			Inputs:        inputs.clone(),
			Outputs:       node.Outputs,
			At:            r.now(),
		},
		Authorization: r.opts.authorizer()(node),
	}
	if r.opts.TestRun != nil {
		// A test run presents the governed envelope: its purpose stamps
		// every evidence record, and the key pins the call to this node in
		// this walk. The deadline rides the run clock, so a pinned clock
		// keeps the envelope — and the evidence — deterministic.
		request.Invocation = &capability.Invocation{
			Purpose:         TestPurpose,
			Deadline:        r.now().Add(time.Hour),
			IdempotencyKey:  fmt.Sprintf("test:%s:%s:%d", r.plan.Digest(), node.ID, len(r.trace)),
			DeclaredEffects: []capability.EffectClass{node.EffectClass},
		}
	}
	result, err := r.gateway.Invoke(ctx, request)
	if err != nil {
		return "", nil, "", nil, wrap(CodeHandlerFailed, node.ID, err,
			"capability %s refused or failed", key)
	}
	response, ok := result.Response.(CapabilityResponse)
	if !ok {
		return "", nil, "", nil, refuse(CodeHandlerFailed, node.ID,
			"capability %s returned %T, want simulate.CapabilityResponse", key, result.Response)
	}
	if response.Outcome == "" {
		return "", nil, "", nil, refuse(CodeHandlerFailed, node.ID,
			"capability %s returned no outcome", key)
	}
	extra := map[string]string{evCapabilityExecutionID: result.EvidenceID}
	detail := response.Detail
	if detail == "" {
		detail = "invoked " + key.String() + " in " + string(node.Capability.OperationMode)
	}
	return response.Outcome, response.Outputs, detail, extra, nil
}

// recordCapability captures a write-class node as a typed record instead of
// invoking anything. The gateway is not consulted and no handler runs: the
// record carries the mapped inputs, the declared outputs and the preview of
// what the invocation would have carried. Admission guarantees a recording
// profile is active on this path; the check here fails closed if it is not.
func (r *run) recordCapability(
	node workflow.CompiledNode,
	inputs Bag,
) (workflow.Outcome, Bag, string, map[string]string, error) {
	if !r.recordingActive() {
		return "", nil, "", nil, refuse(CodeWriteEffectInSimulate, node.ID,
			"%s declares %s; a write effect cannot be simulated, only refused",
			node.Type, node.EffectClass)
	}
	key := capability.Key{ID: node.Capability.ID, Version: node.Capability.Version}
	record, err := recordEffect(node, key, inputs)
	if err != nil {
		return "", nil, "", nil, wrap(CodeRecordingRefused, node.ID, err,
			"would-be effect of %s cannot be recorded", key)
	}
	r.recording.Recorder.Record(record)
	r.recorded = append(r.recorded, record)
	detail := "recorded would-be " + string(record.Kind) + " " + key.String() + " (not invoked)"
	return workflow.OutcomeSucceeded, Bag{}, detail, map[string]string{evRecordedEffect: string(record.Kind)}, nil
}

// runDecision routes through the rules engine. The kernel decides nothing: it
// hands the pinned input snapshot to the declared evaluator and refuses any
// route key the node did not declare.
func (r *run) runDecision(
	ctx context.Context,
	node workflow.CompiledNode,
	inputs Bag,
) (workflow.Outcome, Bag, string, map[string]string, error) {
	if node.Decision == nil {
		return "", nil, "", nil, refuse(CodeUnresolvedSource, node.ID, "DECISION node binds no evaluator")
	}
	result, err := r.opts.decisions().Decide(ctx, DecisionRequest{
		NodeID:   node.ID,
		Decision: *node.Decision,
		Inputs:   inputs.clone(),
		At:       r.now(),
	})
	if err != nil {
		return "", nil, "", nil, wrap(CodeHandlerFailed, node.ID, err,
			"decision %s failed", node.Decision.EvaluatorRef)
	}
	if !declaresRoute(node, result.RouteKey) {
		return "", nil, "", nil, refuse(CodeMissingRoute, node.ID,
			"evaluator %s selected route %q, which the node does not declare",
			node.Decision.EvaluatorRef, result.RouteKey)
	}
	extra := map[string]string{evEvaluationTraceRef: result.TraceRef}
	detail := result.Detail
	if detail == "" {
		detail = "evaluated " + node.Decision.RuleRef
	}
	return workflow.Outcome(result.RouteKey), Bag{}, detail, extra, nil
}

// runTransform evaluates a registered pure transform.
func (r *run) runTransform(
	ctx context.Context,
	node workflow.CompiledNode,
	inputs Bag,
) (workflow.Outcome, Bag, string, map[string]string, error) {
	if node.Transform == nil {
		return "", nil, "", nil, refuse(CodeUnresolvedSource, node.ID, "TRANSFORM node binds no transform")
	}
	result, err := r.opts.transforms().Transform(ctx, TransformRequest{
		NodeID:    node.ID,
		Transform: *node.Transform,
		Inputs:    inputs.clone(),
		Outputs:   node.Outputs,
		At:        r.now(),
	})
	if err != nil {
		return "", nil, "", nil, wrap(CodeHandlerFailed, node.ID, err,
			"transform %s failed", node.Transform.TransformRef)
	}
	if result.Outcome == "" {
		result.Outcome = workflow.OutcomeSucceeded
	}
	extra := map[string]string{
		evTaintManifestRef: canonicalDigest(evidenceIDProfile+"#taint", node.Transform.Lineage),
	}
	detail := result.Detail
	if detail == "" {
		detail = "applied " + node.Transform.TransformRef
	}
	return result.Outcome, result.Outputs, detail, extra, nil
}

// runObserve asks the injected read port for an authoritative read.
//
// A missing or unreadable source is UNKNOWN, never PASS: the whole point of an
// OBSERVE node is that a transport acknowledgement is not evidence of business
// state, and neither is the absence of one.
func (r *run) runObserve(
	ctx context.Context,
	node workflow.CompiledNode,
	inputs Bag,
) (workflow.Outcome, Bag, string, map[string]string, error) {
	if node.Observe == nil {
		return "", nil, "", nil, refuse(CodeUnresolvedSource, node.ID, "OBSERVE node binds no observation")
	}
	obs, err := r.opts.reads().Observe(ctx, ObservationRequest{
		NodeID:  node.ID,
		Observe: *node.Observe,
		Inputs:  inputs.clone(),
		Outputs: node.Outputs,
		At:      r.now(),
	})
	if err != nil {
		return "", nil, "", nil, wrap(CodeHandlerFailed, node.ID, err,
			"observation against %s failed", node.Observe.SourceAuthority)
	}
	if obs.Outcome == "" {
		obs.Outcome = workflow.OutcomeUnknown
	}
	if obs.Outcome == workflow.OutcomePass && obs.Watermark == "" {
		return "", nil, "", nil, refuse(CodeHandlerFailed, node.ID,
			"observation claims PASS without citing a source watermark")
	}
	extra := map[string]string{evSourceWatermark: obs.Watermark}
	detail := obs.Detail
	if detail == "" {
		detail = "observed " + node.Observe.SourceAuthority
	}
	return obs.Outcome, obs.Outputs, detail, extra, nil
}

// runHumanWork executes an APPROVAL or TASK node without waiting.
//
// The outcome names the branch the simulation explored, not a decision anyone
// made: the receipt's work items carry WOULD_AWAIT, and no approval binding is
// recorded anywhere. A resolution that found nobody authorized routes to
// CANCELLED rather than claiming an approval that could never be given.
func (r *run) runHumanWork(
	node workflow.CompiledNode,
	items []WorkItem,
) (workflow.Outcome, Bag, string, map[string]string, error) {
	resolved := len(items) > 0
	for _, item := range items {
		if item.Outcome != "RESOLVED" {
			resolved = false
			break
		}
	}
	approved := workflow.Outcome("APPROVED")
	if node.Type == workflow.StepTask {
		approved = workflow.OutcomeSucceeded
	}
	outcome := approved
	detail := "would await " + itoa(uint64(len(items))) + " work item(s); no decision was recorded"
	if !resolved {
		outcome = workflow.Outcome("CANCELLED")
		detail = "no authorized approver resolved; simulation explored the cancelled branch"
	}
	if !declaresRoute(node, string(outcome)) {
		return "", nil, "", nil, refuse(CodeMissingRoute, node.ID,
			"%s would take route %q, which the node does not declare", node.Type, outcome)
	}
	return outcome, Bag{}, detail, nil, nil
}

// runEnd closes the walk at a terminal.
//
// It produces no outputs of its own: a terminal binds the workflow's declared
// result fields as inputs, and those are already covered by the trace entry's
// input digest. The five-dimension legality check happens once, on the way
// out, in run.mint.
func (r *run) runEnd(
	node workflow.CompiledNode,
	inputs Bag,
) (workflow.Outcome, Bag, string, map[string]string, error) {
	if node.Terminal == nil {
		return "", nil, "", nil, refuse(CodeIllegalTerminal, node.ID,
			"END node carries no compiled terminal artifact")
	}
	outputs := make(Bag, len(node.Outputs))
	for _, field := range node.Outputs {
		if v, ok := inputs[field.Path]; ok {
			outputs[field.Path] = v
		}
	}
	return workflow.Outcome(node.Terminal.TerminalCode), outputs,
		"reached terminal " + node.Terminal.TerminalCode +
			" with runtime status " + string(node.Terminal.RuntimeStatus), nil, nil
}

// declaresRoute reports whether the node declares an outgoing edge for a route
// key, which for an END node is vacuously false and never asked.
func declaresRoute(node workflow.CompiledNode, key string) bool {
	for _, r := range node.Routes {
		if r == key {
			return true
		}
	}
	return false
}

// raiseWorkItems derives the human work a node would have raised. It is called
// for every APPROVAL or TASK node, and for any node - typically a terminal -
// whose governance surface declares approval requirements.
func (r *run) raiseWorkItems(ctx context.Context, node workflow.CompiledNode, at time.Time) ([]WorkItem, error) {
	refs := append([]string(nil), node.Governance.ApprovalRequirements...)
	sort.Strings(refs)
	if len(refs) == 0 && node.Type != workflow.StepApproval && node.Type != workflow.StepTask {
		return nil, nil
	}
	snapshot := make(map[string]Bag, len(r.outputs))
	for id, bag := range r.outputs {
		snapshot[id] = bag.clone()
	}
	items, err := r.opts.approvals().WouldAwait(ctx, ApprovalRequest{
		NodeID:          node.ID,
		RequirementRefs: refs,
		WorkflowInputs:  r.inputs.Values.clone(),
		NodeOutputs:     snapshot,
		At:              at,
	})
	if err != nil {
		return nil, wrap(CodeHandlerFailed, node.ID, err, "approval derivation failed")
	}
	for i := range items {
		if items[i].NodeID == "" {
			items[i].NodeID = node.ID
		}
		items[i].State = WouldAwait
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Stage != items[j].Stage {
			return items[i].Stage < items[j].Stage
		}
		return items[i].RequirementID < items[j].RequirementID
	})
	return items, nil
}

// evidenceFor fills every evidence reference the compiled node declares.
//
// A digest reference is the digest the run actually computed, an invocation
// reference is the identity the gateway minted, and everything else is derived
// from the plan digest, the node id and the reference name - never from a
// counter or a clock, so two runs of one plan mint the same evidence ids.
func (r *run) evidenceFor(node workflow.CompiledNode, inputDigest, outputDigest string, extra map[string]string) []Evidence {
	out := make([]Evidence, 0, len(node.EvidenceRefs))
	for _, ref := range node.EvidenceRefs {
		id := extra[ref]
		if id == "" {
			switch ref {
			case evRequestDigest, evInputDigest, evEvaluatedInputDigest:
				id = inputDigest
			case evOutputDigest:
				id = outputDigest
			default:
				id = mintEvidenceID(r.plan.Digest(), node.ID, ref)
			}
		}
		out = append(out, Evidence{Ref: ref, ID: id})
	}
	return out
}

// mintEvidenceID derives a stable evidence identity.
func mintEvidenceID(planDigest, nodeID, ref string) string {
	full := canonicalDigest(evidenceIDProfile, []string{planDigest, nodeID, ref})
	return "ev:" + full[:32]
}

// invocationSink is the gateway's evidence sink for one run. It mints
// deterministic invocation identities and keeps every decision, refusal
// included, so a receipt can be checked against what the gateway actually did.
type invocationSink struct {
	planDigest string
	seq        int
	// store, when set by a WF-TEST-002 test run, receives every invocation
	// decision under the expiring test-evidence class. A nil store keeps
	// today's behavior: the evidence id is minted and nothing is kept.
	store  *ExpiringEvidenceStore
	tenant string
}

// RecordInvocation implements capability.EvidenceSink.
func (s *invocationSink) RecordInvocation(_ context.Context, evt capability.InvocationEvidence) (string, error) {
	s.seq++
	full := canonicalDigest(evidenceIDProfile+"#invocation", []string{
		s.planDigest,
		evt.CapabilityID,
		itoa(uint64(evt.CapabilityVersion)),
		evt.Decision,
		evt.ReasonCode,
		itoa(uint64(s.seq)),
	})
	if s.store != nil {
		if _, err := s.store.Put(EvidenceRecord{
			Purpose:           evt.Purpose,
			Principal:         evt.SubjectRef,
			Tenant:            s.tenant,
			CapabilityID:      evt.CapabilityID,
			CapabilityVersion: evt.CapabilityVersion,
			Decision:          evt.Decision,
			ReasonCode:        evt.ReasonCode,
		}); err != nil {
			return "", err
		}
	}
	return "ev:" + full[:32], nil
}

func (s *invocationSink) RecordInvocationTx(ctx context.Context, evt capability.InvocationEvidence) (string, error) {
	return s.RecordInvocation(ctx, evt)
}
