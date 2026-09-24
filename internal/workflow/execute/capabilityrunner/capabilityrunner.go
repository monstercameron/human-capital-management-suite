package capabilityrunner

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/attest"
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

// ExecutionRequirementResolver loads the authoritative attestation binding
// for one compiled obligation. Implementations must resolve stored evidence,
// never infer identity or digests from obligation prose.
type ExecutionRequirementResolver interface {
	ResolveExecutionRequirement(context.Context, execute.StepRequest, workflow.CompiledNode, workflow.ObligationRequirement) (attest.ExecutionRequirement, error)
}

// PinnedPlanExecutionRequirementResolver reads the attestation response and
// transaction binding from execution metadata committed into the compiled
// workflow digest. Runtime callers may provide a store-backed resolver when
// the binding is selected dynamically; this is the default for pinned plans.
type PinnedPlanExecutionRequirementResolver struct {
	Tenant string
}

func (r PinnedPlanExecutionRequirementResolver) ResolveExecutionRequirement(ctx context.Context, req execute.StepRequest, node workflow.CompiledNode, obligation workflow.ObligationRequirement) (result attest.ExecutionRequirement, retErr error) {
	ctx, op := observe.Begin(ctx, "workflow.execute.resolve_execution_requirement", req)
	defer func() { observe.Done(op, retErr) }()
	if req.Plan == nil {
		return attest.ExecutionRequirement{}, fmt.Errorf("compiled plan is required")
	}
	pinned, ok := req.Plan.Node(node.ID)
	if !ok {
		return attest.ExecutionRequirement{}, fmt.Errorf("node %s is not in the compiled plan", node.ID)
	}
	prefix := workflow.AttestationExecutionMetadataPrefix
	field := func(name string) (string, error) {
		key := prefix + name
		value := strings.TrimSpace(pinned.Metadata[key])
		if value == "" || value != pinned.Metadata[key] {
			return "", fmt.Errorf("compiled node %s is missing %s", node.ID, key)
		}
		return value, nil
	}
	responseID, err := field("response_id")
	if err != nil {
		return attest.ExecutionRequirement{}, err
	}
	responseRevisionText, err := field("response_revision")
	if err != nil {
		return attest.ExecutionRequirement{}, err
	}
	responseRevision, err := strconv.ParseUint(responseRevisionText, 10, 64)
	if err != nil || responseRevision == 0 {
		return attest.ExecutionRequirement{}, fmt.Errorf("compiled node %s has invalid response_revision", node.ID)
	}
	statementID, err := field("statement_id")
	if err != nil {
		return attest.ExecutionRequirement{}, err
	}
	statementVersionText, err := field("statement_version")
	if err != nil {
		return attest.ExecutionRequirement{}, err
	}
	statementVersion, err := strconv.ParseUint(statementVersionText, 10, 64)
	if err != nil || statementVersion == 0 {
		return attest.ExecutionRequirement{}, fmt.Errorf("compiled node %s has invalid statement_version", node.ID)
	}
	statementDigest, err := field("statement_digest")
	if err != nil {
		return attest.ExecutionRequirement{}, err
	}
	bindingDigest, err := field("binding_digest")
	if err != nil {
		return attest.ExecutionRequirement{}, err
	}
	transactionID, err := field("transaction_id")
	if err != nil {
		return attest.ExecutionRequirement{}, err
	}
	tenant := r.Tenant
	if strings.TrimSpace(tenant) == "" {
		tenant = req.TenantID.String()
	}
	return attest.ExecutionRequirement{
		Tenant: values.TenantId(tenant), ObligationID: obligation.ID,
		StatementID: statementID, StatementVersion: statementVersion,
		StatementDigest: statementDigest, BindingDigest: bindingDigest,
		ResponseID: responseID, ResponseRevision: responseRevision,
		TransactionID: transactionID,
	}, nil
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
	// AttestationResponses and AttestationClock are required when a node is
	// governed by a mandatory attestation obligation. Missing gate bindings
	// refuse execution.
	AttestationResponses attest.ResponseStore
	AttestationClock     attest.TrustedClock
	// ExecutionRequirements resolves the exact response and statement binding
	// from the authoritative workflow/attestation stores.
	ExecutionRequirements ExecutionRequirementResolver
	// OnOutputs, when set, observes a copy of the typed response after a
	// successful invocation. Durable persistence uses NodeOutcome.Outputs.
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
	outputs := typedOutputDocument(res.Outputs)
	return frontier.NodeOutcome{NodeID: node.ID, Outcome: res.Outcome, Outputs: outputs},
		runtime.GovernanceRefs{CapabilityExecutionID: res.EvidenceID}, nil
}

// invoke resolves the node's mappings, presents the node's declared
// authority scopes to the gateway, and digests the typed response.
func (r *Runner) invoke(ctx context.Context, req execute.StepRequest, node workflow.CompiledNode) (Result, error) {
	inputs, err := r.resolveInputs(req, node)
	if err != nil {
		return Result{}, err
	}
	requirements, err := r.executionRequirements(ctx, req, node)
	if err != nil {
		return Result{}, err
	}
	var result capability.InvokeResult
	invoke := func(effectCtx context.Context) error {
		var invokeErr error
		result, invokeErr = r.Gateway.Invoke(effectCtx, capability.InvokeRequest{
			Capability: capability.Key{ID: node.Capability.ID, Version: node.Capability.Version},
			Payload: CapabilityCall{
				NodeID:        node.ID,
				Capability:    capability.Key{ID: node.Capability.ID, Version: node.Capability.Version},
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
		return invokeErr
	}
	// Nest the effect in every applicable gate. Each exact requirement is
	// revalidated immediately before the gateway is allowed to invoke.
	var gated func(int, context.Context) error
	gated = func(index int, effectCtx context.Context) error {
		if index == len(requirements) {
			return invoke(effectCtx)
		}
		_, gateErr := attest.EnforceBeforeEffect(effectCtx, r.AttestationResponses, r.AttestationClock, requirements[index], func(gateCtx context.Context, _ attest.ExecutionDecision) error {
			return gated(index+1, gateCtx)
		})
		return gateErr
	}
	if err := gated(0, ctx); err != nil {
		return Result{}, fmt.Errorf("workflow execute: capability runner: node %s: %w", node.ID, err)
	}
	key := capability.Key{ID: node.Capability.ID, Version: node.Capability.Version}
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

func (r *Runner) executionRequirements(ctx context.Context, req execute.StepRequest, node workflow.CompiledNode) ([]attest.ExecutionRequirement, error) {
	if len(node.Governance.ObligationRefs) == 0 && !node.EffectClass.IsWrite() {
		return nil, nil
	}
	if req.Plan == nil {
		for _, id := range node.Governance.ObligationRefs {
			if strings.Contains(strings.ToLower(id), "attest") {
				return nil, fmt.Errorf("%w: node %s references attestation obligation %s without its compiled plan", attest.ErrRequiredAttestation, node.ID, id)
			}
		}
		if node.EffectClass.IsWrite() {
			return nil, fmt.Errorf("%w: node %s is a write effect without its compiled plan", attest.ErrRequiredAttestation, node.ID)
		}
		return nil, nil
	}
	byID := make(map[string]workflow.ObligationRequirement, len(req.Plan.Governance.Obligations))
	for _, obligation := range req.Plan.Governance.Obligations {
		byID[obligation.ID] = obligation
	}
	selected := append([]string(nil), node.Governance.ObligationRefs...)
	// A plan-level mandatory attestation obligation protects every write
	// capability in that plan, even when the author did not repeat the ref on
	// the effect node itself.
	if node.EffectClass.IsWrite() {
		for _, obligation := range req.Plan.Governance.Obligations {
			if obligation.Mandatory && isAttestationObligation(obligation) {
				selected = append(selected, obligation.ID)
			}
		}
	}
	seen := make(map[string]struct{}, len(selected))
	var requirements []attest.ExecutionRequirement
	for _, id := range selected {
		if _, duplicate := seen[id]; duplicate {
			continue
		}
		seen[id] = struct{}{}
		obligation, ok := byID[id]
		if !ok {
			if strings.Contains(strings.ToLower(id), "attest") {
				return nil, fmt.Errorf("%w: node %s references unresolved attestation obligation %s", attest.ErrRequiredAttestation, node.ID, id)
			}
			continue
		}
		if !isAttestationObligation(obligation) {
			continue
		}
		if !obligation.Mandatory {
			continue
		}
		if r.AttestationResponses == nil || r.AttestationClock == nil {
			return nil, fmt.Errorf("%w: node %s has mandatory attestation obligation %s but no execution gate is configured", attest.ErrRequiredAttestation, node.ID, id)
		}
		resolver := r.ExecutionRequirements
		if resolver == nil {
			resolver = PinnedPlanExecutionRequirementResolver{Tenant: r.tenant(req)}
		}
		requirement, err := resolver.ResolveExecutionRequirement(ctx, req, node, obligation)
		if err != nil {
			return nil, fmt.Errorf("%w: resolve obligation %s: %v", attest.ErrRequiredAttestation, id, err)
		}
		if requirement.ObligationID != id || string(requirement.Tenant) != r.tenant(req) {
			return nil, fmt.Errorf("%w: resolved obligation or tenant does not match node %s", attest.ErrRequiredAttestation, node.ID)
		}
		requirements = append(requirements, requirement)
	}
	return requirements, nil
}

func isAttestationObligation(obligation workflow.ObligationRequirement) bool {
	for _, text := range []string{obligation.ID, obligation.Authority, obligation.RequiredAction, obligation.ResponsibleParty, obligation.SatisfactionCondition, obligation.SourceVersion} {
		if strings.Contains(strings.ToLower(text), "attest") {
			return true
		}
	}
	return false
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
func (r *Runner) resolveInputs(req execute.StepRequest, node workflow.CompiledNode) (map[string]ResolvedValue, error) {
	// WF-EXT-004 resolves compiled mappings in the driver from the durable
	// input/output artifacts. Prefer that exact result whenever present; the
	// compatibility maps below remain for callers that intentionally run a
	// node without a persisted workflow input document.
	if req.Inputs != nil {
		out := make(map[string]ResolvedValue, len(req.Inputs))
		for path, value := range req.Inputs {
			out[path] = ResolvedValue{Type: value.Type, Text: value.Text}
		}
		return out, nil
	}
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

// typedOutputDocument converts the capability response into the WF-EXT-004
// artifact shape. The driver validates every path and type against the
// compiled node and persists the artifact atomically with advancement.
func typedOutputDocument(outputs map[string]ResolvedValue) *workflow.OutputDocument {
	if outputs == nil {
		return nil
	}
	paths := make([]string, 0, len(outputs))
	for path := range outputs {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	doc := &workflow.OutputDocument{Values: make([]workflow.TypedOutput, 0, len(paths))}
	for _, path := range paths {
		value := outputs[path]
		doc.Values = append(doc.Values, workflow.TypedOutput{Path: path, Value: workflow.TypedValue{Type: value.Type, Text: value.Text}})
	}
	return doc
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
