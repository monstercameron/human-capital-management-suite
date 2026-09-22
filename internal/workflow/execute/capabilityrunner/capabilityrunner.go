package capabilityrunner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/frontier"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// stepCapabilityBudget bounds one gateway invocation the runner makes,
// measured from the step's recorded instant.
const stepCapabilityBudget = 60 * time.Second

// outputDigestProfile names the canonical profile the runner digests a
// capability's typed response under.
const outputDigestProfile = "hcmnext.workflow.capability_output/v1"

// GatewayInvoker is the governed capability invocation port. It is satisfied
// by *capability.Gateway; a test supplies a fake only when it must prove the
// runner's own refusal paths, never to re-prove the gateway.
type GatewayInvoker interface {
	Invoke(ctx context.Context, req capability.InvokeRequest) (capability.InvokeResult, error)
}

// ResolvedValue is one typed mapping result flowing into a capability call.
// The payload is canonical text rather than an `any`, matching
// internal/workflow/simulate's dataflow contract: the receipt digest stays
// stable without a bespoke encoder per Go type, and no implicit coercion can
// slip a value past the node's declared input fields.
type ResolvedValue struct {
	Type workflow.ValueType
	Text string
}

// CapabilityCall is the payload a CAPABILITY or OBSERVE node hands to the
// governed gateway. It is the runner's whole contract with a capability
// handler: the node that asked, the exact capability version compiled, the
// declared operation mode, the typed inputs the mappings produced and the
// instant the step was recorded at.
//
// The handler never receives the plan or the driver's state. A capability
// that could see the workflow around it would be able to make a decision the
// workflow author never reviewed.
type CapabilityCall struct {
	NodeID        string
	Capability    capability.Key
	OperationMode workflow.ExecutionMode
	Inputs        map[string]ResolvedValue
	Outputs       []workflow.Field
	At            time.Time
}

// CapabilityAnswer is a handler's typed answer: which of the step type's
// fixed outcomes it produced, the declared output fields, and a short,
// deterministic statement of what it did.
type CapabilityAnswer struct {
	Outcome workflow.Outcome
	Outputs map[string]ResolvedValue
	Detail  string
}

// Result is one generic invocation's full answer: the outcome the driver
// routes on, the typed outputs WF-EXT-004 persists, and the evidence and
// digest the advancement records.
type Result struct {
	Outcome      workflow.Outcome
	Outputs      map[string]ResolvedValue
	Detail       string
	EvidenceID   string
	OutputDigest string
}

// Runner executes compiled CAPABILITY and OBSERVE nodes through the
// capability gateway. It implements execute.StepRunner, so the execute driver
// can serve a node without a bespoke port: the node's compiled mappings are
// the request, the registry manifest the compiler pinned is the authority,
// and the gateway evidence is the governance reference.
type Runner struct {
	// Gateway is the governed invocation path. Required.
	Gateway GatewayInvoker
	// SubjectRef names the already-authorized subject the node's declared
	// authority scopes are presented under.
	SubjectRef string
	// Tenant is the tenant key the decision is made in. Empty falls back to
	// the step request's tenant.
	Tenant string
	// Purpose is the purpose of processing the invocation envelope carries.
	// Empty keeps the P1A interactive contract: no envelope, the request's
	// own zero-effect rule governs the call.
	Purpose string
	// Now supplies the instant a CapabilityCall carries. Nil reads the clock.
	Now func() time.Time
	// WorkflowInputs carries the workflow input values mappings resolve
	// from, keyed by workflow input path.
	WorkflowInputs map[string]ResolvedValue
	// NodeOutputs carries completed predecessor outputs mappings resolve
	// from, keyed first by producing node id, then by declared output path.
	// WF-EXT-004 replaces these maps with the durable artifact store; until
	// then the composition root supplies them per advancement.
	NodeOutputs map[string]map[string]ResolvedValue
	// OnOutputs, when set, receives the typed response after a successful
	// invocation. It is the seam WF-EXT-004's persistence binds to; the
	// runner itself records only the digest.
	OnOutputs func(nodeID string, outputs map[string]ResolvedValue)
}

var _ execute.StepRunner = (*Runner)(nil)

func (r *Runner) now() time.Time {
	if r != nil && r.Now != nil {
		return r.Now().UTC()
	}
	return time.Now().UTC()
}

// Run executes one READY CAPABILITY or OBSERVE node and returns only its
// typed outcome and governance references. It never schedules a successor:
// runtime.Advance derives continuations from the pinned plan.
func (r *Runner) Run(ctx context.Context, req execute.StepRequest) (out frontier.NodeOutcome, refs runtime.GovernanceRefs, retErr error) {
	ctx, op := observe.Begin(ctx, "workflow.execute.capability", req)
	op.Set(observe.KeyNode, req.Node.ID)
	defer func() { observe.DoneWith(op, retErr, out) }()
	if r == nil || r.Gateway == nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, invalid("capability runner: no gateway")
	}
	node := req.Node
	if node.Type != workflow.StepCapability && node.Type != workflow.StepObserve {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, invalid("capability runner: node %s is %s, not CAPABILITY or OBSERVE", node.ID, node.Type)
	}
	if node.Capability == nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, invalid("capability runner: node %s binds no capability", node.ID)
	}
	res, err := r.invoke(ctx, req, node)
	if err != nil {
		return frontier.NodeOutcome{}, runtime.GovernanceRefs{}, err
	}
	return frontier.NodeOutcome{NodeID: node.ID, Outcome: res.Outcome, OutputDigest: res.OutputDigest},
		runtime.GovernanceRefs{CapabilityExecutionID: res.EvidenceID}, nil
}

// invoke resolves the node's mappings, presents the node's declared
// authority scopes to the gateway, and digests the typed response.
func (r *Runner) invoke(ctx context.Context, req execute.StepRequest, node workflow.CompiledNode) (Result, error) {
	inputs, err := r.resolveInputs(node)
	if err != nil {
		return Result{}, err
	}
	key := capability.Key{ID: node.Capability.ID, Version: node.Capability.Version}
	result, err := r.Gateway.Invoke(ctx, capability.InvokeRequest{
		Capability: key,
		Payload: CapabilityCall{
			NodeID:        node.ID,
			Capability:    key,
			OperationMode: node.Capability.OperationMode,
			Inputs:        inputs,
			Outputs:       append([]workflow.Field(nil), req.Node.Outputs...),
			At:            r.now(),
		},
		Authorization: capability.Authorization{
			Decision:   capability.Allow,
			Scopes:     append([]string(nil), node.Capability.AuthorityScopes...),
			Reason:     "workflow node declared authority scopes",
			SubjectRef: r.SubjectRef,
			Tenant:     r.tenant(req),
		},
		Invocation: invocationFor(req, node, r.Purpose),
	})
	if err != nil {
		return Result{}, fmt.Errorf("workflow execute: capability runner: node %s: %w", node.ID, err)
	}
	answer, ok := result.Response.(CapabilityAnswer)
	if !ok {
		return Result{}, invalid("capability runner: node %s: capability %s returned %T, want capabilityrunner.CapabilityAnswer", node.ID, key, result.Response)
	}
	if answer.Outcome == "" {
		return Result{}, invalid("capability runner: node %s: capability %s returned no outcome", node.ID, key)
	}
	if err := checkOutcome(node, answer.Outcome); err != nil {
		return Result{}, err
	}
	digest := digestOutputs(key, answer.Outputs)
	if r.OnOutputs != nil {
		r.OnOutputs(node.ID, cloneOutputs(answer.Outputs))
	}
	return Result{
		Outcome:      answer.Outcome,
		Outputs:      cloneOutputs(answer.Outputs),
		Detail:       answer.Detail,
		EvidenceID:   result.EvidenceID,
		OutputDigest: digest,
	}, nil
}

// tenant resolves the tenant key the authorization decision is made in. It
// is never inferred from anything but the runner's binding or the step's own
// tenant: a durable sink refuses a record without one rather than guess.
func (r *Runner) tenant(req execute.StepRequest) string {
	if r != nil && strings.TrimSpace(r.Tenant) != "" {
		return r.Tenant
	}
	return req.TenantID.String()
}

// resolveInputs turns the node's compiled mappings into typed values. A
// CONTEXT source fails closed naming WF-EXT-004: durable context artifacts
// do not exist yet, and an ambient read would be exactly the hidden
// current-state read the compiler refuses to declare.
func (r *Runner) resolveInputs(node workflow.CompiledNode) (map[string]ResolvedValue, error) {
	out := make(map[string]ResolvedValue, len(node.Mappings))
	for _, m := range node.Mappings {
		switch m.SourceKind {
		case workflow.SourceConstant:
			out[m.Target] = ResolvedValue{Type: m.TargetType, Text: m.Constant}
		case workflow.SourceWorkflowInput:
			v, ok := r.WorkflowInputs[m.SourcePath]
			if !ok {
				return nil, invalid("capability runner: node %s input %q reads workflow input %q, which was not supplied", node.ID, m.Target, m.SourcePath)
			}
			out[m.Target] = v
		case workflow.SourceNodeOutput:
			produced, ok := r.NodeOutputs[m.SourceNode]
			if !ok {
				return nil, invalid("capability runner: node %s input %q reads node %q, which has no recorded output", node.ID, m.Target, m.SourceNode)
			}
			v, ok := produced[m.SourcePath]
			if !ok {
				return nil, invalid("capability runner: node %s input %q reads %s.%s, which that node did not produce", node.ID, m.Target, m.SourceNode, m.SourcePath)
			}
			out[m.Target] = v
		case workflow.SourceContext:
			return nil, invalid("capability runner: node %s input %q reads context %s: durable context artifacts land with WF-EXT-004", node.ID, m.Target, m.SourceCtx)
		default:
			return nil, invalid("capability runner: node %s input %q declares source kind %q", node.ID, m.Target, m.SourceKind)
		}
	}
	return out, nil
}

// invocationFor builds the governed envelope one call presents. A blank
// purpose keeps the P1A interactive contract with no envelope.
func invocationFor(req execute.StepRequest, node workflow.CompiledNode, purpose string) *capability.Invocation {
	if strings.TrimSpace(purpose) == "" {
		return nil
	}
	recorded := req.RecordedAt.UTC()
	if recorded.IsZero() {
		recorded = time.Now().UTC()
	}
	return &capability.Invocation{
		Purpose:         purpose,
		Deadline:        recorded.Add(stepCapabilityBudget),
		IdempotencyKey:  fmt.Sprintf("workflow:%s:%s:%s:%d", req.TenantID, req.InstanceID, node.ID, req.Attempt),
		DeclaredEffects: declaredEffects(node),
	}
}

// declaredEffects restates the node's effect class as the permitted set the
// governed envelope carries: every class up to and including the node's own.
func declaredEffects(node workflow.CompiledNode) []capability.EffectClass {
	ladder := []capability.EffectClass{
		capability.EffectPure,
		capability.EffectReadOnly,
		capability.EffectInternalMutation,
		capability.EffectExternalMutation,
		capability.EffectIrreversibleExternalMutation,
	}
	for i, class := range ladder {
		if class == node.EffectClass {
			return append([]capability.EffectClass(nil), ladder[:i+1]...)
		}
	}
	return []capability.EffectClass{capability.EffectPure}
}

// checkOutcome refuses a handler answer the node's step type never declares.
// A capability that could invent a route would be able to steer the workflow
// down an edge the author never reviewed.
func checkOutcome(node workflow.CompiledNode, outcome workflow.Outcome) error {
	conformance, ok := workflow.ConformanceFor(node.Type)
	if !ok {
		return invalid("capability runner: node %s has unknown step type %s", node.ID, node.Type)
	}
	for _, declared := range conformance.Outcomes {
		if declared == outcome {
			return nil
		}
	}
	return invalid("capability runner: node %s selected route %q, which %s does not declare", node.ID, outcome, node.Type)
}

// digestOutputs digests the typed response deterministically: the profile,
// the exact capability version, then every output in sorted path order with
// its declared type and canonical text. Evidence ids never enter the digest,
// so two identical answers digest identically across attempts.
func digestOutputs(key capability.Key, outputs map[string]ResolvedValue) string {
	paths := make([]string, 0, len(outputs))
	for path := range outputs {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	h := sha256.New()
	write := func(s string) {
		h.Write([]byte(s))
		h.Write([]byte{0})
	}
	write(outputDigestProfile)
	write(key.ID)
	write(fmt.Sprintf("%d", key.Version))
	for _, path := range paths {
		v := outputs[path]
		write(path)
		write(v.Type.String())
		write(v.Text)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// cloneOutputs deep-copies a response so a handler can never reach back into
// runner state through the map it was handed, and callers cannot mutate the
// recorded answer through the result.
func cloneOutputs(outputs map[string]ResolvedValue) map[string]ResolvedValue {
	if outputs == nil {
		return nil
	}
	out := make(map[string]ResolvedValue, len(outputs))
	for k, v := range outputs {
		out[k] = v
	}
	return out
}

// invalid reports a runner misconfiguration or refusal. It wraps the same
// sentinel the execute driver classifies, so a runner refusal reads as an
// invalid configuration rather than a downstream failure the run could route
// around.
func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", execute.ErrInvalidConfiguration, fmt.Sprintf(format, args...))
}
