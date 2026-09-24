package simulate

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/lifecycle"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/timeauth"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/observe"
)

// Canonicalization profiles for the digests a run mints. Each is distinct, so
// bytes canonicalized as an input snapshot can never be read as a result.
const (
	inputsDigestProfile = "hcmnext.workflow.simulate.Inputs/v1"
	bagDigestProfile    = "hcmnext.workflow.simulate.Bag/v1"
	resultDigestProfile = "hcmnext.workflow.simulate.Result/v1"
	evidenceIDProfile   = "hcmnext.workflow.simulate.EvidenceID/v1"
)

// DefaultClockStart is the fixed instant a simulation starts at when no clock
// is supplied. It is a constant rather than time.Now for the reason CONF-001's
// disposition names: a receipt stamped with the wall clock is a receipt whose
// digest changes every second, which makes byte-stability unprovable.
//
// It coincides with the human-work promotion fixture's own scenario instant so
// a simulated run and the approval deadlines derived from it sit on one
// timeline instead of two that disagree by years.
var DefaultClockStart = time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)

// DefaultStepDuration is the virtual time one node costs. Virtual time is not
// a measurement: it is a declared, deterministic cost model, so "elapsed" in a
// receipt means the same thing on a slow machine and a fast one.
const DefaultStepDuration = 250 * time.Millisecond

// Inputs is everything a run is given that is not in the plan.
type Inputs struct {
	// Values are the workflow's declared input fields, keyed by path.
	Values Bag
	// Context supplies the pinned context snapshots nodes declare a
	// requirement on, keyed by context kind and then by field path. A kind a
	// node requires but this map does not carry is handled by the
	// requirement's own declared MissingBehavior, never defaulted.
	Context map[string]Bag
}

// Options configures one run. The zero value is usable for a plan with no
// capability, transform, observation or approval work; every port a plan
// actually needs must be supplied, because a silently defaulted port would
// produce a result nobody computed.
type Options struct {
	// Clock is the injected virtual clock. A nil clock starts a fresh
	// FakeClock at DefaultClockStart.
	Clock *timeauth.FakeClock
	// StepDuration is the virtual time each node costs. Zero means
	// DefaultStepDuration.
	StepDuration time.Duration

	// Capabilities resolves and invokes the capability versions the plan
	// binds. Handlers must be zero-effect; the gateway refuses any that is
	// not.
	Capabilities *capability.Registry
	// Authorization presents the already-made authorization decision for each
	// capability node. Nil means ScopeAuthorizer(SubjectRef).
	Authorization Authorizer
	// SubjectRef names the caller recorded in invocation evidence.
	SubjectRef string

	Decisions  DecisionPort
	Transforms TransformPort
	// RulePayloads resolves the exact table or expression digest carried by a
	// compiled DECISION node. A node without this binding refuses evaluation.
	RulePayloads PublishedRuleResolver
	Reads        ReadPort
	Approvals    ApprovalPort

	// Controls are extra pinned control versions to cite in the receipt,
	// beyond the compiler, interpreter and plan digest the run always pins.
	Controls []ControlVersion

	// MaxSteps bounds the walk. Zero means four visits per node plus a
	// margin, which is how a declared, guarded cycle stays bounded without a
	// timer or a lease.
	MaxSteps int
}

func (o Options) stepDuration() time.Duration {
	if o.StepDuration <= 0 {
		return DefaultStepDuration
	}
	return o.StepDuration
}

func (o Options) subjectRef() string {
	if o.SubjectRef == "" {
		return "subject:simulate"
	}
	return o.SubjectRef
}

func (o Options) authorizer() Authorizer {
	if o.Authorization != nil {
		return o.Authorization
	}
	return ScopeAuthorizer(o.subjectRef())
}

func (o Options) decisions() DecisionPort {
	if o.Decisions != nil {
		return o.Decisions
	}
	return PublishedRuleDecisions{Payloads: o.RulePayloads}
}

func (o Options) transforms() TransformPort {
	if o.Transforms != nil {
		return o.Transforms
	}
	if o.RulePayloads != nil {
		return PublishedTransforms{Payloads: o.RulePayloads}
	}
	return unboundTransforms{}
}

func (o Options) reads() ReadPort {
	if o.Reads != nil {
		return o.Reads
	}
	return unboundReads{}
}

func (o Options) approvals() ApprovalPort {
	if o.Approvals != nil {
		return o.Approvals
	}
	return noApprovals{}
}

// Run walks a compiled plan in SIMULATE mode and returns the zero-effect
// receipt, or the typed refusal that stopped it.
//
// The walk is single-process, single-goroutine and in-memory by design. There
// is no instance row, no lease, no fence token and no timer: WF-RUN-000 gates
// every durable primitive behind the P1B re-evaluation, so a P1A simulation
// that died with its process is the honest artifact.
//
// Run refuses before it executes anything when the plan can mutate. That
// refusal is structural rather than a suppression flag: a write-class node has
// no path through this function, and the governed gateway underneath refuses
// it a second time.
func Run(ctx context.Context, plan *workflow.CompiledWorkflow, in Inputs, opts Options) (ret0 Receipt, retErr error) {
	ctx, obsOp := observe.Begin(ctx, "workflow.simulate.run", plan, in)
	defer func() { observe.DoneWith(obsOp, retErr, ret0) }()
	if plan == nil {
		return Receipt{}, refuse(CodeInvalidOptions, "", "no compiled plan")
	}
	if err := plan.Verify(); err != nil {
		return Receipt{}, wrap(CodePlanUnverified, "", err,
			"plan %s cannot be simulated", plan.WorkflowID)
	}
	if err := Admit(plan); err != nil {
		return Receipt{}, err
	}

	r := &run{
		plan:    plan,
		inputs:  in,
		opts:    opts,
		clock:   opts.Clock,
		step:    opts.stepDuration(),
		outputs: map[string]Bag{},
	}
	if r.clock == nil {
		r.clock = timeauth.NewFakeClock("simulate.virtual", DefaultClockStart)
	}
	r.maxSteps = opts.MaxSteps
	if r.maxSteps <= 0 {
		r.maxSteps = 4*len(plan.Nodes) + 8
	}
	if opts.Capabilities != nil {
		r.sink = &invocationSink{planDigest: plan.Digest()}
		r.gateway = capability.NewGateway(opts.Capabilities, r.sink, capability.WithClock(r.now))
	}
	return r.walk(ctx)
}

// Admit reports whether a plan may be simulated at all: every node zero-effect,
// every node admitting SIMULATE, every step type implemented, and the plan's
// own effect summary agreeing.
//
// It is exported because "this plan cannot execute in SIMULATE mode" is a
// question a caller should be able to ask before assembling ports and inputs,
// and because a structural test proves the refusal without having to construct
// a whole run.
func Admit(plan *workflow.CompiledWorkflow) error {
	if plan == nil {
		return refuse(CodeInvalidOptions, "", "no compiled plan")
	}
	// Nodes are checked before the plan summary so a refusal names the exact
	// node that cannot be simulated rather than the whole plan.
	for _, node := range plan.Nodes {
		if err := admitNode(node); err != nil {
			return err
		}
	}
	if !plan.Effects.ZeroEffect {
		return refuse(CodePlanNotZeroEffect, "",
			"plan %s declares effect classes %v; SIMULATE runs zero-effect plans only",
			plan.WorkflowID, sortedClasses(plan.Effects.NodesByClass))
	}
	return nil
}

// admitNode is the safe point in front of one node. Run calls it once over the
// whole plan before starting and again immediately before dispatching each
// node, so a plan that was admitted as a whole still cannot slip a write-class
// node past the interpreter.
func admitNode(node workflow.CompiledNode) error {
	if node.EffectClass.IsWrite() {
		return refuse(CodeWriteEffectInSimulate, node.ID,
			"%s declares %s; a write effect cannot be simulated, only refused",
			node.Type, node.EffectClass)
	}
	if !node.AdmitsMode(workflow.ModeSimulate) {
		return refuse(CodeModeNotAdmitted, node.ID,
			"%s admits modes %v, which does not include %s",
			node.Type, node.AllowedModes, workflow.ModeSimulate)
	}
	if !executable(node.Type) {
		return refuse(CodeStepNotImplemented, node.ID,
			"%s is not executed by the P1A simulator; durable primitives are gated behind WF-RUN-000",
			node.Type)
	}
	return nil
}

// executable lists the step types this interpreter runs. CAPABILITY, DECISION,
// TRANSFORM, OBSERVE and END are P1A. APPROVAL and TASK are admitted only
// because a simulation of them is a statement about what would be awaited, not
// a durable human task; WAIT, SIGNAL, COMPENSATE and the three structural
// primitives are refused outright, because simulating them convincingly would
// require exactly the durable machinery WF-RUN-000 has not authorized.
func executable(t workflow.StepType) bool {
	switch t {
	case workflow.StepCapability, workflow.StepDecision, workflow.StepTransform,
		workflow.StepObserve, workflow.StepEnd, workflow.StepApproval, workflow.StepTask:
		return true
	default:
		return false
	}
}

func sortedClasses(byClass map[string][]string) []string {
	out := make([]string, 0, len(byClass))
	for class := range byClass {
		out = append(out, class)
	}
	sort.Strings(out)
	return out
}

// run is one walk's mutable state. It is deliberately not safe for concurrent
// use: a simulation is one deterministic sequence, and a concurrent one would
// have no stable trace order to digest.
type run struct {
	plan   *workflow.CompiledWorkflow
	inputs Inputs
	opts   Options

	clock    *timeauth.FakeClock
	step     time.Duration
	maxSteps int

	gateway *capability.Gateway
	sink    *invocationSink

	outputs   map[string]Bag
	trace     []NodeTrace
	workItems []WorkItem
}

func (r *run) now() time.Time { return r.clock.Sample().Wall.Time() }

func (r *run) walk(ctx context.Context) (Receipt, error) {
	startedAt := r.now()
	current := r.plan.StartNodeID
	var terminal *workflow.Terminal

	for steps := 0; ; steps++ {
		if err := ctx.Err(); err != nil {
			return Receipt{}, wrap(CodeHandlerFailed, current, err, "simulation cancelled")
		}
		if steps >= r.maxSteps {
			return Receipt{}, refuse(CodeStepBudgetExceeded, current,
				"walk exceeded its budget of %d node visits", r.maxSteps)
		}
		node, ok := r.plan.Node(current)
		if !ok {
			return Receipt{}, refuse(CodeUnresolvedSource, current, "plan declares no such node")
		}
		if err := admitNode(node); err != nil {
			return Receipt{}, err
		}
		next, done, err := r.step1(ctx, node)
		if err != nil {
			return Receipt{}, err
		}
		if done {
			terminal = node.Terminal
			break
		}
		current = next
	}

	if terminal == nil {
		return Receipt{}, refuse(CodeNoTerminal, current,
			"walk stopped without a compiled terminal artifact")
	}
	return r.mint(startedAt, *terminal)
}

// step1 executes one node and returns the next node id, or reports that the
// walk reached a terminal.
func (r *run) step1(ctx context.Context, node workflow.CompiledNode) (string, bool, error) {
	enteredAt := r.now()

	inputs, err := r.resolveInputs(node)
	if err != nil {
		return "", false, err
	}
	missing, err := r.contextVerdict(node)
	if err != nil {
		return "", false, err
	}
	if err := inputs.assignableTo(node.Inputs); err != nil {
		return "", false, wrap(CodeUnresolvedSource, node.ID, err, "resolved inputs do not type-check")
	}
	inputDigest := digestBag(inputs)

	// Human work is derived before the node is dispatched, because an
	// APPROVAL or TASK node's simulated outcome is a statement about the
	// candidate set that was resolved, not a decision invented after the fact.
	items, err := r.raiseWorkItems(ctx, node, enteredAt)
	if err != nil {
		return "", false, err
	}
	r.workItems = append(r.workItems, items...)

	var (
		outcome workflow.Outcome
		outputs Bag
		detail  string
		extra   map[string]string
	)
	if missing != "" {
		// A required context snapshot the run was not given is never absent,
		// zero or false: the requirement's own declared behavior decides, and
		// it decides UNKNOWN or FAIL.
		outcome, outputs, detail = workflow.OutcomeUnknown, Bag{}, missing
	} else {
		outcome, outputs, detail, extra, err = r.dispatch(ctx, node, inputs, items)
		if err != nil {
			return "", false, err
		}
	}
	if outputs == nil {
		outputs = Bag{}
	}
	if err := outputs.assignableTo(node.Outputs); err != nil {
		return "", false, wrap(CodeOutputTypeMismatch, node.ID, err,
			"node produced an output its declaration does not admit")
	}
	r.outputs[node.ID] = outputs.clone()
	outputDigest := digestBag(outputs)

	r.clock.Advance(r.step)
	exitedAt := r.now()

	entry := NodeTrace{
		Order:        len(r.trace) + 1,
		NodeID:       node.ID,
		Type:         node.Type,
		Depth:        node.Depth,
		SafePoint:    node.SafePoint,
		EffectClass:  node.EffectClass,
		EnteredAt:    enteredAt,
		ExitedAt:     exitedAt,
		Elapsed:      exitedAt.Sub(enteredAt),
		Outcome:      outcome,
		InputDigest:  inputDigest,
		OutputDigest: outputDigest,
		Detail:       detail,
		Evidence:     r.evidenceFor(node, inputDigest, outputDigest, extra),
	}

	if node.Type == workflow.StepEnd {
		r.trace = append(r.trace, entry)
		return "", true, nil
	}

	next, err := r.route(node, outcome)
	if err != nil {
		return "", false, err
	}
	entry.RouteKey = string(outcome)
	entry.NextNodeID = next
	r.trace = append(r.trace, entry)
	return next, false, nil
}

// route selects the outgoing edge the outcome names. There is no implicit
// first edge at runtime any more than there is at compile time.
func (r *run) route(node workflow.CompiledNode, outcome workflow.Outcome) (string, error) {
	for _, e := range r.plan.Edges {
		if e.From == node.ID && e.RouteKey == string(outcome) {
			return e.To, nil
		}
	}
	return "", refuse(CodeMissingRoute, node.ID,
		"%s produced outcome %q, which has no outgoing edge", node.Type, outcome)
}

// contextVerdict applies each declared context requirement against the
// supplied snapshots and returns a non-empty explanation when the node must be
// routed to its degraded outcome instead of executed.
func (r *run) contextVerdict(node workflow.CompiledNode) (string, error) {
	for _, req := range node.RequiredContext {
		snapshot, ok := r.inputs.Context[req.Kind]
		missing := !ok
		if !missing {
			for _, path := range req.FieldPaths {
				if _, present := snapshot[path]; !present {
					missing = true
					break
				}
			}
		}
		if !missing {
			continue
		}
		switch req.MissingBehavior {
		case workflow.MissingFail:
			return "", refuse(CodeUnresolvedSource, node.ID,
				"required context %s is missing and its declared behavior is FAIL", req.Kind)
		default:
			return "required context " + req.Kind + " is missing; routed UNKNOWN as declared", nil
		}
	}
	return "", nil
}

// resolveInputs turns the node's compiled mappings into typed values, through
// the one resolver (WF-EXT-004) this package now shares with the durable
// EXECUTE path: [workflow.ResolveMappings] reads WORKFLOW_INPUT, NODE_OUTPUT
// and CONTEXT sources through [mappingSourceAdapter], which is nothing but a
// read-only view over this run's own inputs and recorded node outputs.
func (r *run) resolveInputs(node workflow.CompiledNode) (Bag, error) {
	resolved, err := workflow.ResolveMappings(node, mappingSourceAdapter{r: r})
	if err != nil {
		var merr *workflow.MappingError
		if errors.As(err, &merr) {
			return nil, wrap(CodeUnresolvedSource, node.ID, merr, "%s", merr.Detail)
		}
		return nil, wrap(CodeUnresolvedSource, node.ID, err, "resolve node inputs")
	}
	out := make(Bag, len(resolved))
	for target, v := range resolved {
		out[target] = Value{Type: v.Type, Text: v.Text}
	}
	return out, nil
}

// mappingSourceAdapter satisfies [workflow.MappingSource] over one run's
// in-memory inputs and recorded node outputs. [simulate.Value] and
// [workflow.TypedValue] hold the identical (Type, Text) shape by design, so
// the adapter only ever relabels a lookup -- it never converts a value.
type mappingSourceAdapter struct{ r *run }

func (a mappingSourceAdapter) WorkflowInput(path string) (workflow.TypedValue, bool) {
	v, ok := a.r.inputs.Values[path]
	if !ok {
		return workflow.TypedValue{}, false
	}
	return workflow.TypedValue{Type: v.Type, Text: v.Text}, true
}

func (a mappingSourceAdapter) NodeOutput(nodeID, path string) (workflow.TypedValue, bool, bool) {
	produced, nodeProduced := a.r.outputs[nodeID]
	if !nodeProduced {
		return workflow.TypedValue{}, false, false
	}
	v, ok := produced[path]
	if !ok {
		return workflow.TypedValue{}, true, false
	}
	return workflow.TypedValue{Type: v.Type, Text: v.Text}, true, true
}

func (a mappingSourceAdapter) Context(kind, path string) (workflow.TypedValue, bool) {
	snapshot, ok := a.r.inputs.Context[kind]
	if !ok {
		return workflow.TypedValue{}, false
	}
	v, ok := snapshot[path]
	if !ok {
		return workflow.TypedValue{}, false
	}
	return workflow.TypedValue{Type: v.Type, Text: v.Text}, true
}

// mint assembles the receipt once the walk has reached a terminal, checking
// the terminal's five-dimension tuple against the intent lifecycle rules on
// the way out.
func (r *run) mint(startedAt time.Time, terminal workflow.Terminal) (Receipt, error) {
	if err := r.checkTerminal(terminal); err != nil {
		return Receipt{}, err
	}
	endedAt := r.now()

	counters := evidence.ZeroEffects()
	receipt := Receipt{
		InterpreterVersion: InterpreterVersion,
		CompilerVersion:    r.plan.CompilerVersion,
		WorkflowID:         r.plan.WorkflowID,
		WorkflowVersion:    r.plan.Version,
		PlanDigest:         r.plan.Digest(),
		Mode:               workflow.ModeSimulate,
		TerminalProfile:    r.plan.TerminalProfile,
		InputsDigest:       r.inputsDigest(),
		StartedAt:          startedAt,
		EndedAt:            endedAt,
		Elapsed:            endedAt.Sub(startedAt),
		Trace:              r.trace,
		WorkItems:          r.workItems,
		Terminal: Terminal{
			NodeID:                    terminal.NodeID,
			TerminalCode:              terminal.TerminalCode,
			RuntimeStatus:             terminal.RuntimeStatus,
			OutstandingObligationRefs: append([]string(nil), terminal.OutstandingObligationRefs...),
		},
		Lifecycle: lifecycleStateOf(terminal.Dimensions),
		Counters:  countersOf(counters),
		Controls:  r.controls(),
	}
	if receipt.WorkItems == nil {
		receipt.WorkItems = []WorkItem{}
	}
	if receipt.Trace == nil {
		receipt.Trace = []NodeTrace{}
	}

	resultDigest := canonicalDigest(resultDigestProfile, struct {
		Trace     []NodeTrace    `json:"trace"`
		WorkItems []WorkItem     `json:"work_items"`
		Terminal  Terminal       `json:"terminal"`
		Lifecycle LifecycleState `json:"lifecycle"`
	}{receipt.Trace, receipt.WorkItems, receipt.Terminal, receipt.Lifecycle})

	controls := make([]evidence.ControlVersion, 0, len(receipt.Controls))
	for _, c := range receipt.Controls {
		controls = append(controls, evidence.ControlVersion{Name: c.Name, Version: c.Version})
	}
	zero, err := evidence.NewZeroEffectReceipt(
		r.plan.WorkflowID,
		planVersionString(r.plan.Version),
		evidence.ModeSimulate,
		receipt.Lifecycle.RequestState,
		controls,
		receipt.InputsDigest,
		resultDigest,
		counters,
	)
	if err != nil {
		return Receipt{}, wrap(CodeReceiptInvalid, terminal.NodeID, err,
			"zero-effect receipt refused")
	}
	receipt.ZeroEffect = zero
	receipt.ZeroEffectDigest = canonicalDigest(receiptDigestProfile+"#zero", string(zero.Canonical()))
	receipt.digest = computeReceiptDigest(receipt)
	return receipt, nil
}

// checkTerminal runs the intent kernel's own legality rules over the terminal
// the walk reached. The compiler already proved every declared terminal legal;
// re-checking the one actually reached is what makes END an executed step
// rather than a label.
func (r *run) checkTerminal(t workflow.Terminal) error {
	ctx := lifecycle.Context{Persisted: true}
	if len(t.RepairRefs) > 0 {
		ctx.RepairRef = t.RepairRefs[0]
	} else if len(t.IncidentRefs) > 0 {
		ctx.RepairRef = t.IncidentRefs[0]
	}
	if err := lifecycle.Check(t.Dimensions, ctx); err != nil {
		var vs *lifecycle.ViolationSet
		if errors.As(err, &vs) {
			broken := make([]string, 0, len(vs.Violations))
			for _, v := range vs.Violations {
				broken = append(broken, string(v.Rule)+"["+string(v.Dimension)+"]")
			}
			return wrap(CodeIllegalTerminal, t.NodeID, err,
				"terminal %s violates %s", t.TerminalCode, strings.Join(broken, ", "))
		}
		return wrap(CodeIllegalTerminal, t.NodeID, err, "terminal %s", t.TerminalCode)
	}
	return nil
}

func (r *run) controls() []ControlVersion {
	out := []ControlVersion{
		{Name: "hcmnext.workflow.compiler", Version: r.plan.CompilerVersion},
		{Name: "hcmnext.workflow.plan", Version: r.plan.Digest()},
		{Name: "hcmnext.workflow.simulate", Version: InterpreterVersion},
	}
	out = append(out, r.opts.Controls...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Version < out[j].Version
	})
	return out
}

func (r *run) inputsDigest() string {
	type contextSnapshot struct {
		Kind   string  `json:"kind"`
		Values []entry `json:"values"`
	}
	kinds := make([]string, 0, len(r.inputs.Context))
	for kind := range r.inputs.Context {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)
	snapshots := make([]contextSnapshot, 0, len(kinds))
	for _, kind := range kinds {
		snapshots = append(snapshots, contextSnapshot{Kind: kind, Values: entriesOf(r.inputs.Context[kind])})
	}
	return canonicalDigest(inputsDigestProfile, struct {
		Plan    string            `json:"plan_digest"`
		Values  []entry           `json:"values"`
		Context []contextSnapshot `json:"context"`
	}{r.plan.Digest(), entriesOf(r.inputs.Values), snapshots})
}

// entry is one path/value pair in a canonicalized bag.
type entry struct {
	Path  string `json:"path"`
	Value Value  `json:"value"`
}

func entriesOf(b Bag) []entry {
	out := make([]entry, 0, len(b))
	for _, path := range b.Paths() {
		out = append(out, entry{Path: path, Value: b[path]})
	}
	return out
}

// digestBag is the content identity of one node's input or output set.
func digestBag(b Bag) string { return canonicalDigest(bagDigestProfile, entriesOf(b)) }

func planVersionString(v uint32) string {
	return "v" + itoa(uint64(v))
}

// itoa renders an unsigned integer for the deterministic detail strings and
// evidence identities a receipt carries.
func itoa(v uint64) string { return strconv.FormatUint(v, 10) }
