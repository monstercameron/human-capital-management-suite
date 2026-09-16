// Package intervention defines the finite, governed workflow intervention
// vocabulary (planning/specs/workflow-runtime.md "Intervention Taxonomy").
//
// There is no generic force operation. Each [Kind] declares its own
// semantics, required authority, fields and preconditions. [Request.Validate]
// refuses a request that is malformed, lacks a reason, evidence or requester,
// or names an action with no executable path. [Evaluate] judges a valid
// request against the durable instance it targets and returns either an
// immutable [Plan] -- the exact runtime transition the executor may perform
// through internal/workflow/runtime -- or a typed denial: a no-op, a failed
// precondition, a stale version or an unsupported action. The package never
// performs a transition itself; it records the accepted decision
// ([DecisionStore]) in the transaction the executor performs it in.
package intervention

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

const contractVersion = 2

// Version reports this package's contract version.
func Version() int { return contractVersion }

// Kind is the closed set of workflow interventions. There is no custom,
// free-form or force kind.
type Kind string

// Intervention kinds.
const (
	// Retry runs the same failed node again under its idempotency contract.
	Retry Kind = "RETRY"
	// Resume continues a paused instance from its checkpoint.
	Resume Kind = "RESUME"
	// Skip declares an activated, non-effecting node unnecessary under an
	// authorized exception and takes its declared success route.
	Skip Kind = "SKIP"
	// Satisfy supplies the trusted result a waiting node required.
	Satisfy Kind = "SATISFY"
	// Override proceeds past a blocking approval or decision along a declared
	// route under exceptional authority.
	Override Kind = "OVERRIDE"
	// Rewind returns a paused instance to an earlier completed node without
	// deleting any history.
	Rewind Kind = "REWIND"
	// Compensate executes a declared compensation of a prior effect.
	Compensate Kind = "COMPENSATE"
	// Supersede ends an instance in favour of a linked replacement.
	Supersede Kind = "SUPERSEDE"
	// Reconcile records the observed outcome of an ambiguous effect.
	Reconcile Kind = "RECONCILE"
	// Cancel stops further execution at the instance's cancellation boundary.
	Cancel Kind = "CANCEL"
)

// Kinds lists every kind in a stable order.
func Kinds() []Kind {
	return []Kind{Retry, Resume, Skip, Satisfy, Override, Rewind, Compensate, Supersede, Reconcile, Cancel}
}

// Valid reports whether k is a declared kind.
func (k Kind) Valid() bool { return slices.Contains(Kinds(), k) }

// Capability is the governed capability (and so the authority family) the
// kind is exercised under. An approver is not a repair operator, and an
// override is its own family.
func (k Kind) Capability() string {
	switch k {
	case Retry, Resume, Cancel:
		return "workflow.instances." + strings.ToLower(string(k))
	case Override:
		return "workflow.override.decision"
	case Skip, Satisfy, Rewind, Compensate, Supersede, Reconcile:
		return "workflow.repair." + strings.ToLower(string(k))
	}
	return ""
}

// executable reports whether the kind has a runtime path in this build, and
// why not when it has none.
func (k Kind) executable() (bool, string) {
	if k == Compensate {
		return false, "no compensation runner is composed: internal/workflow/steps/compensate is not wired to the runtime, so a compensation cannot be executed or observed; nothing is claimed reversed"
	}
	return k.Valid(), ""
}

// Observation is what a reconciliation observed about an ambiguous effect.
type Observation string

// Observations.
const (
	ObservedApplied    Observation = "EFFECT_APPLIED"
	ObservedNotApplied Observation = "EFFECT_NOT_APPLIED"
)

// Request is one typed intervention. No caller names a target state: the
// transition follows from the kind, the durable instance and the plan.
type Request struct {
	Kind            Kind
	InstanceID      uuid.UUID
	ExpectedVersion int64

	// NodeID addresses RETRY, SKIP, SATISFY, OVERRIDE and RECONCILE.
	NodeID string
	// ExpectedAttempt fences RETRY on the failed attempt the operator saw.
	ExpectedAttempt int
	// Route is the declared route a SATISFY or OVERRIDE takes.
	Route string
	// TargetNodeID is the earlier node a REWIND returns control to.
	TargetNodeID string
	// Replacement is the instance a SUPERSEDE links to.
	Replacement uuid.UUID
	// Observation is what a RECONCILE observed.
	Observation Observation

	Reason       string
	EvidenceRefs []string
	RequestedBy  string
	RequestedAt  time.Time
}

// Validate refuses a request that is malformed or can never be accepted,
// before any state is read.
func (r Request) Validate() error {
	id := r.InstanceID.String()
	if !r.Kind.Valid() {
		return refuse(CodeNotSupported, string(r.Kind), "workflow intervention kind %q is not in the declared taxonomy; there is no generic force operation", r.Kind)
	}
	if ok, why := r.Kind.executable(); !ok {
		return refuse(CodeNotSupported, id, "%s: %s", r.Kind, why)
	}
	switch {
	case r.InstanceID == uuid.Nil:
		return refuse(CodeInvalidRequest, "", "instance id is required")
	case r.ExpectedVersion < 1:
		return refuse(CodeInvalidRequest, id, "expected instance version must be at least 1")
	case strings.TrimSpace(r.RequestedBy) == "":
		return refuse(CodeUnauthorized, id, "an intervention names the operator who requests it")
	case strings.TrimSpace(r.Reason) == "":
		return refuse(CodeReasonRequired, id, "an intervention declares its reason")
	case len(normalizedEvidence(r.EvidenceRefs)) == 0:
		return refuse(CodeEvidenceRequired, id, "an intervention cites at least one evidence reference")
	case r.RequestedAt.IsZero():
		return refuse(CodeInvalidRequest, id, "requested instant is required")
	}
	switch r.Kind {
	case Retry:
		if r.NodeID == "" || r.ExpectedAttempt < 1 {
			return refuse(CodeInvalidRequest, id, "retry names the node and the failed attempt it retries")
		}
	case Skip, Reconcile:
		if r.NodeID == "" {
			return refuse(CodeInvalidRequest, id, "%s names the node it acts on", r.Kind)
		}
	case Satisfy, Override:
		if r.NodeID == "" || r.Route == "" {
			return refuse(CodeInvalidRequest, id, "%s names the node and the declared route it takes", r.Kind)
		}
	case Rewind:
		if r.TargetNodeID == "" {
			return refuse(CodeInvalidRequest, id, "rewind names the earlier node control returns to")
		}
	case Supersede:
		if r.Replacement == uuid.Nil || r.Replacement == r.InstanceID {
			return refuse(CodeInvalidRequest, id, "supersede links a replacement instance other than itself")
		}
	}
	if r.Kind == Reconcile && r.Observation != ObservedApplied && r.Observation != ObservedNotApplied {
		return refuse(CodeInvalidRequest, id, "reconcile records EFFECT_APPLIED or EFFECT_NOT_APPLIED")
	}
	return nil
}

// Facts are the durable facts one evaluation reads, loaded by the executor in
// the transaction it will write in.
type Facts struct {
	Plan     *workflow.CompiledWorkflow
	Instance runtime.Instance
	Nodes    []runtime.NodeExecution
	// Replacement is the SUPERSEDE replacement instance, nil when the tenant
	// has none by that id.
	Replacement *runtime.Instance
}

// NodeRef names one node execution attempt.
type NodeRef struct {
	NodeID  string             `json:"node_id"`
	Attempt int                `json:"attempt"`
	Status  runtime.NodeStatus `json:"status"`
}

// Plan is the accepted, immutable transition an intervention may perform.
type Plan struct {
	Kind            Kind                   `json:"kind"`
	InstanceID      uuid.UUID              `json:"instance_id"`
	ExpectedVersion int64                  `json:"expected_version"`
	InstanceFrom    runtime.InstanceStatus `json:"instance_from"`
	InstanceTo      runtime.InstanceStatus `json:"instance_to"`
	// Node is the addressed node's latest attempt, NodeTo the status that
	// attempt (or, for RETRY and REWIND, the new attempt) is moved to.
	Node    NodeRef            `json:"node,omitzero"`
	NodeTo  runtime.NodeStatus `json:"node_to,omitempty"`
	Route   string             `json:"route,omitempty"`
	Settle  runtime.NodeStatus `json:"settle_as,omitempty"`
	Target  NodeRef            `json:"target,omitzero"`
	Cancels []NodeRef          `json:"cancels,omitempty"`
	// Replacement and Observation carry the SUPERSEDE and RECONCILE facts.
	Replacement uuid.UUID   `json:"replacement,omitzero"`
	Observation Observation `json:"observation,omitempty"`
	// OutputDigest is the digest of the cited evidence, recorded as the output
	// of a node a SKIP, SATISFY or OVERRIDE settles.
	OutputDigest string `json:"output_digest,omitempty"`
}

// Evaluate judges r against the durable facts. It is pure.
func Evaluate(r Request, f Facts) (Plan, error) {
	if err := r.Validate(); err != nil {
		return Plan{}, err
	}
	inst := f.Instance
	id := r.InstanceID.String()
	switch {
	case f.Plan == nil || inst.InstanceID != r.InstanceID:
		return Plan{}, refuse(CodeInvalidRequest, id, "facts do not describe the addressed instance")
	case f.Plan.Digest() != inst.CompiledPlanHash:
		return Plan{}, refuse(CodePreconditionFailed, id, "the instance is pinned to a plan other than the one supplied")
	case inst.InstanceVersion != r.ExpectedVersion:
		return Plan{}, refuse(CodeStaleVersion, id, "the operator saw instance version %d; the instance is at %d", r.ExpectedVersion, inst.InstanceVersion)
	}
	p := Plan{Kind: r.Kind, InstanceID: r.InstanceID, ExpectedVersion: r.ExpectedVersion,
		InstanceFrom: inst.RuntimeStatus, InstanceTo: inst.RuntimeStatus}
	if inst.RuntimeStatus.Terminal() && r.Kind != Reconcile {
		if r.Kind == Supersede && inst.RuntimeStatus == runtime.InstanceSuperseded {
			return Plan{}, refuse(CodeNoOp, id, "the instance is already superseded")
		}
		return Plan{}, refuse(CodePreconditionFailed, id, "the instance is %s; a terminal instance is never resurrected", inst.RuntimeStatus)
	}
	var node workflow.CompiledNode
	if r.NodeID != "" {
		var err error
		if node, p.Node, err = addressNode(f, r.NodeID, id); err != nil {
			return Plan{}, err
		}
	}
	var err error
	switch r.Kind {
	case Retry:
		err = evaluateRetry(r, &p, id)
	case Resume:
		err = evaluateResume(&p, id)
	case Skip:
		err = evaluateSkip(f, node, &p, id)
	case Satisfy, Override:
		err = evaluateRoute(r, f, node, &p, id)
	case Rewind:
		err = evaluateRewind(r, f, &p, id)
	case Supersede:
		err = evaluateSupersede(r, f, &p, id)
	case Reconcile:
		err = evaluateReconcile(r, node, &p, id)
	case Cancel:
		err = evaluateCancel(&p, id)
	}
	if err != nil {
		return Plan{}, err
	}
	if p.Route != "" {
		p.OutputDigest = EvidenceDigest(r.EvidenceRefs)
	}
	return p, nil
}

func addressNode(f Facts, nodeID, id string) (workflow.CompiledNode, NodeRef, error) {
	node, ok := f.Plan.Node(nodeID)
	if !ok {
		return node, NodeRef{}, refuse(CodePreconditionFailed, nodeID, "the pinned plan declares no such node")
	}
	if !f.Plan.InterventionEligible(nodeID) {
		return node, NodeRef{}, refuse(CodePreconditionFailed, nodeID, "the node is inside a compiled atomic region; intervention waits for its safe point")
	}
	latest, ok := latestAttempt(f.Nodes, nodeID)
	if !ok {
		return node, NodeRef{}, refuse(CodePreconditionFailed, nodeID, "the instance has never activated this node")
	}
	return node, NodeRef{NodeID: nodeID, Attempt: latest.Attempt, Status: latest.Status}, nil
}

func evaluateRetry(r Request, p *Plan, id string) error {
	switch {
	case p.Node.Attempt != r.ExpectedAttempt:
		return refuse(CodeStaleVersion, id, "the operator saw attempt %d; the latest attempt is %d", r.ExpectedAttempt, p.Node.Attempt)
	case p.Node.Status == runtime.NodeFailed:
		p.NodeTo = runtime.NodeReady
		p.Target = NodeRef{NodeID: p.Node.NodeID, Attempt: p.Node.Attempt + 1, Status: runtime.NodeReady}
		return nil
	case activeStatus(p.Node.Status):
		return refuse(CodeNoOp, id, "attempt %d is already %s; a retry would change nothing", p.Node.Attempt, p.Node.Status)
	}
	return refuse(CodePreconditionFailed, id, "attempt %d is %s, not FAILED", p.Node.Attempt, p.Node.Status)
}

func evaluateResume(p *Plan, id string) error {
	switch p.InstanceFrom {
	case runtime.InstancePaused:
		p.InstanceTo = runtime.InstanceRunning
		return nil
	case runtime.InstanceCreated, runtime.InstanceRunning, runtime.InstanceWaiting:
		return refuse(CodeNoOp, id, "the instance is %s; there is nothing to resume", p.InstanceFrom)
	}
	return refuse(CodePreconditionFailed, id, "the instance is %s, not PAUSED", p.InstanceFrom)
}

// evaluateCancel plans the cancellation boundary. Whether the instance then
// reaches CANCELLED or is routed to repair because an effect is in flight is
// the cancellation engine's own decision, recorded as the observed transition.
func evaluateCancel(p *Plan, id string) error {
	if p.InstanceFrom == runtime.InstanceCancelling {
		return refuse(CodeNoOp, id, "the instance is already cancelling")
	}
	if !runtime.LegalInstanceTransition(p.InstanceFrom, runtime.InstanceCancelling) {
		return refuse(CodePreconditionFailed, id, "a %s instance cannot be cancelled", p.InstanceFrom)
	}
	p.InstanceTo = runtime.InstanceCancelling
	return nil
}

func evaluateSkip(f Facts, node workflow.CompiledNode, p *Plan, id string) error {
	if err := routable(f, node, p, id); err != nil {
		return err
	}
	if p.Node.Status != runtime.NodeReady && p.Node.Status != runtime.NodeWaiting {
		return refuse(CodePreconditionFailed, id, "only a READY or WAITING node that has not run can be skipped; %s is %s", node.ID, p.Node.Status)
	}
	switch node.Type {
	case workflow.StepApproval, workflow.StepDecision:
		return refuse(CodePreconditionFailed, id, "%s is a %s; proceeding past it is an OVERRIDE, not a skip", node.ID, node.Type)
	case workflow.StepEnd, workflow.StepCompensate, workflow.StepJoin, workflow.StepParallel, workflow.StepSubworkflow:
		return refuse(CodeNotSupported, id, "a %s node has no declared success route a skip can take", node.Type)
	}
	if node.EffectClass.IsWrite() {
		return refuse(CodePreconditionFailed, id, "%s declares a %s effect; skipping an effect is not a skip", node.ID, node.EffectClass)
	}
	route := string(workflow.OutcomeSucceeded)
	if node.Type == workflow.StepObserve {
		route = string(workflow.OutcomePass)
	}
	p.Route, p.Settle, p.NodeTo = route, runtime.NodeSkipped, runtime.NodeSkipped
	return successorMaterializable(f, node, route, id)
}

func evaluateRoute(r Request, f Facts, node workflow.CompiledNode, p *Plan, id string) error {
	if err := routable(f, node, p, id); err != nil {
		return err
	}
	decisionLike := node.Type == workflow.StepApproval || node.Type == workflow.StepDecision
	switch r.Kind {
	case Satisfy:
		if decisionLike {
			return refuse(CodePreconditionFailed, id, "%s is a %s; supplying its outcome is an OVERRIDE under exceptional authority", node.ID, node.Type)
		}
		if node.EffectClass.IsWrite() {
			return refuse(CodePreconditionFailed, id, "%s declares a %s effect; an effect's outcome is reconciled, not satisfied", node.ID, node.EffectClass)
		}
		if p.Node.Status != runtime.NodeWaiting {
			return refuse(CodePreconditionFailed, id, "%s is %s; only a WAITING node awaits a result", node.ID, p.Node.Status)
		}
		p.NodeTo = runtime.NodeSucceeded
	case Override:
		if !decisionLike {
			return refuse(CodePreconditionFailed, id, "%s is a %s; only an approval or decision outcome is overridden", node.ID, node.Type)
		}
		p.Settle, p.NodeTo = runtime.NodeOverridden, runtime.NodeOverridden
	}
	if !declaresEdge(f.Plan, node.ID, r.Route) {
		return refuse(CodePreconditionFailed, id, "%s declares no route %q", node.ID, r.Route)
	}
	p.Route = r.Route
	return successorMaterializable(f, node, r.Route, id)
}

// routable holds the preconditions every routed intervention shares: a live,
// unpaused instance and an addressed node that is still active.
func routable(f Facts, node workflow.CompiledNode, p *Plan, id string) error {
	if settled(p.Node.Status) {
		return refuse(CodeNoOp, id, "%s already settled %s; the intervention would change nothing", node.ID, p.Node.Status)
	}
	if p.InstanceFrom != runtime.InstanceRunning && p.InstanceFrom != runtime.InstanceWaiting {
		return refuse(CodePreconditionFailed, id, "the instance is %s; a routed intervention needs a RUNNING or WAITING instance", p.InstanceFrom)
	}
	if !slices.Contains(f.Instance.CurrentNodeIDs, node.ID) {
		return refuse(CodePreconditionFailed, id, "%s is not on the instance frontier", node.ID)
	}
	return nil
}

// successorMaterializable refuses a route whose successor needs a work item,
// timer or signal subscription: the governed gateway composes only the
// durable continuation ledger and the ready-work queue, so such a successor
// would be recorded but never materialized.
func successorMaterializable(f Facts, node workflow.CompiledNode, route, id string) error {
	for _, e := range f.Plan.Edges {
		if e.From != node.ID || e.RouteKey != route {
			continue
		}
		succ, _ := f.Plan.Node(e.To)
		switch succ.Type {
		case workflow.StepApproval, workflow.StepTask, workflow.StepSignal, workflow.StepWait, workflow.StepJoin:
			return refuse(CodeNotSupported, id, "route %q activates %s, a %s whose work item, timer or subscription the operator gateway does not materialize", route, succ.ID, succ.Type)
		}
	}
	return nil
}

func evaluateRewind(r Request, f Facts, p *Plan, id string) error {
	if p.InstanceFrom != runtime.InstancePaused {
		return refuse(CodePreconditionFailed, id, "the instance is %s; rewind needs an instance PAUSED at a safe point", p.InstanceFrom)
	}
	target, ok := f.Plan.Node(r.TargetNodeID)
	if !ok {
		return refuse(CodePreconditionFailed, r.TargetNodeID, "the pinned plan declares no such node")
	}
	latest, ok := latestAttempt(f.Nodes, target.ID)
	if !ok {
		return refuse(CodePreconditionFailed, target.ID, "the instance never executed this node; rewind returns only to an earlier point")
	}
	if latest.Status == runtime.NodeReady && slices.Equal(f.Instance.CurrentNodeIDs, []string{target.ID}) {
		return refuse(CodeNoOp, id, "control is already at %s", target.ID)
	}
	if latest.Status != runtime.NodeSucceeded {
		return refuse(CodePreconditionFailed, target.ID, "rewind returns to a completed node; %s is %s", target.ID, latest.Status)
	}
	switch target.Type {
	case workflow.StepApproval, workflow.StepTask, workflow.StepSignal, workflow.StepWait, workflow.StepEnd, workflow.StepJoin:
		return refuse(CodeNotSupported, target.ID, "re-activating a %s needs a work item, timer or subscription the operator gateway does not materialize", target.Type)
	}
	if target.EffectClass.IsWrite() {
		return refuse(CodePreconditionFailed, target.ID, "%s declares a %s effect; re-running it is a retry or a repair, not a rewind", target.ID, target.EffectClass)
	}
	for _, n := range latestAttempts(f.Nodes) {
		cn, _ := f.Plan.Node(n.NodeID)
		if cn.EffectClass.IsWrite() && (n.Status == runtime.NodeSucceeded || activeStatus(n.Status)) {
			return refuse(CodePreconditionFailed, n.NodeID, "%s has a committed or in-flight %s effect; control cannot return before it without compensation", n.NodeID, cn.EffectClass)
		}
		if slices.Contains(f.Instance.CurrentNodeIDs, n.NodeID) {
			switch n.Status {
			case runtime.NodeReady, runtime.NodeRunning, runtime.NodeWaiting:
				p.Cancels = append(p.Cancels, NodeRef{NodeID: n.NodeID, Attempt: n.Attempt, Status: n.Status})
			case runtime.NodeRetrying:
				return refuse(CodePreconditionFailed, n.NodeID, "%s is RETRYING; let the retry settle before rewinding", n.NodeID)
			}
		}
	}
	p.Target = NodeRef{NodeID: target.ID, Attempt: latest.Attempt + 1, Status: runtime.NodeReady}
	p.NodeTo = runtime.NodeReady
	return nil
}

func evaluateSupersede(r Request, f Facts, p *Plan, id string) error {
	switch rep := f.Replacement; {
	case rep == nil || rep.InstanceID != r.Replacement:
		return refuse(CodePreconditionFailed, id, "replacement instance %s does not exist in this tenant", r.Replacement)
	case rep.WorkflowID != f.Instance.WorkflowID:
		return refuse(CodePreconditionFailed, id, "replacement runs workflow %s, not %s", rep.WorkflowID, f.Instance.WorkflowID)
	case rep.RuntimeStatus.Terminal() && rep.RuntimeStatus != runtime.InstanceCompleted:
		return refuse(CodePreconditionFailed, id, "replacement instance is %s; a superseded transaction links to a live or completed replacement", rep.RuntimeStatus)
	}
	if !runtime.LegalInstanceTransition(p.InstanceFrom, runtime.InstanceSuperseded) {
		return refuse(CodePreconditionFailed, id, "a %s instance cannot be superseded", p.InstanceFrom)
	}
	for _, n := range latestAttempts(f.Nodes) {
		cn, _ := f.Plan.Node(n.NodeID)
		if cn.EffectClass.IsWrite() && (n.Status == runtime.NodeSucceeded || activeStatus(n.Status)) {
			return refuse(CodePreconditionFailed, n.NodeID, "%s has a committed or in-flight %s effect; supersession would leave it unaccounted for (reconcile or compensate first)", n.NodeID, cn.EffectClass)
		}
		switch n.Status {
		case runtime.NodeReady, runtime.NodeRunning, runtime.NodeWaiting:
			p.Cancels = append(p.Cancels, NodeRef{NodeID: n.NodeID, Attempt: n.Attempt, Status: n.Status})
		case runtime.NodeRetrying:
			return refuse(CodePreconditionFailed, n.NodeID, "%s is RETRYING; let the retry settle before superseding", n.NodeID)
		}
	}
	p.InstanceTo, p.Replacement = runtime.InstanceSuperseded, r.Replacement
	return nil
}

func evaluateReconcile(r Request, node workflow.CompiledNode, p *Plan, id string) error {
	if !node.EffectClass.IsWrite() {
		return refuse(CodePreconditionFailed, id, "%s declares no write effect; there is nothing ambiguous to reconcile", node.ID)
	}
	if p.Node.Status != runtime.NodeRunning && p.Node.Status != runtime.NodeWaiting {
		if settled(p.Node.Status) || p.Node.Status == runtime.NodeFailed {
			return refuse(CodeNoOp, id, "%s already settled %s; its effect is not ambiguous", node.ID, p.Node.Status)
		}
		return refuse(CodePreconditionFailed, id, "%s is %s; only an in-flight effect is reconciled", node.ID, p.Node.Status)
	}
	if p.InstanceFrom != runtime.InstanceRepairRequired {
		return refuse(CodePreconditionFailed, id, "the instance is %s; an ambiguous effect is reconciled once the instance is routed to REPAIR_REQUIRED", p.InstanceFrom)
	}
	p.Observation = r.Observation
	p.NodeTo = runtime.NodeFailed
	if r.Observation == ObservedApplied {
		p.NodeTo = runtime.NodeSucceeded
	}

	return nil
}

func declaresEdge(plan *workflow.CompiledWorkflow, nodeID, route string) bool {
	for _, e := range plan.Edges {
		if e.From == nodeID && e.RouteKey == route {
			return true
		}
	}
	return false
}

func activeStatus(s runtime.NodeStatus) bool {
	switch s {
	case runtime.NodeReady, runtime.NodeRunning, runtime.NodeWaiting, runtime.NodeRetrying:
		return true
	}
	return false
}

func settled(s runtime.NodeStatus) bool {
	switch s {
	case runtime.NodeSucceeded, runtime.NodeSkipped, runtime.NodeOverridden, runtime.NodeCompensated, runtime.NodeCancelled:
		return true
	}
	return false
}

func latestAttempt(nodes []runtime.NodeExecution, nodeID string) (runtime.NodeExecution, bool) {
	var out runtime.NodeExecution
	found := false
	for _, n := range nodes {
		if n.NodeID == nodeID && (!found || n.Attempt > out.Attempt) {
			out, found = n, true
		}
	}
	return out, found
}

// latestAttempts returns each node's latest attempt, sorted by node id.
func latestAttempts(nodes []runtime.NodeExecution) []runtime.NodeExecution {
	byNode := map[string]runtime.NodeExecution{}
	for _, n := range nodes {
		if cur, ok := byNode[n.NodeID]; !ok || n.Attempt > cur.Attempt {
			byNode[n.NodeID] = n
		}
	}
	out := make([]runtime.NodeExecution, 0, len(byNode))
	for _, n := range byNode {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].NodeID < out[j].NodeID })
	return out
}

func normalizedEvidence(refs []string) []string {
	out := make([]string, 0, len(refs))
	for _, ref := range refs {
		if ref = strings.TrimSpace(ref); ref != "" {
			out = append(out, ref)
		}
	}
	sort.Strings(out)
	return slices.Compact(out)
}

// EvidenceDigest is the canonical digest of a set of evidence references.
func EvidenceDigest(refs []string) string {
	return "sha256:" + digest("hcmnext.workflow.intervention.Evidence/v1", normalizedEvidence(refs))
}

func digest(profile string, value any) string {
	b, err := json.Marshal(value)
	if err != nil {
		b = []byte("unencodable:" + err.Error())
	}
	h := sha256.New()
	_, _ = h.Write([]byte(profile))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

// Denial codes. Every refusal this package raises carries exactly one.
const (
	CodeInvalidRequest     = "INTERVENTION_INVALID_REQUEST"
	CodeNotSupported       = "INTERVENTION_NOT_SUPPORTED"
	CodeNoOp               = "INTERVENTION_NO_OP"
	CodeUnauthorized       = "INTERVENTION_UNAUTHORIZED"
	CodePreconditionFailed = "INTERVENTION_PRECONDITION_FAILED"
	CodeReasonRequired     = "INTERVENTION_REASON_REQUIRED"
	CodeEvidenceRequired   = "INTERVENTION_EVIDENCE_REQUIRED"
	CodeStaleVersion       = "INTERVENTION_STALE_VERSION"
	CodeDecisionMutated    = "INTERVENTION_DECISION_MUTATED"
	CodeStorageFailed      = "INTERVENTION_STORAGE_FAILED"
)

// ErrIntervention is the sentinel every refusal unwraps to.
var ErrIntervention = errors.New("workflow/intervention: denied")

// Error is one typed denial.
type Error struct {
	Code, Ref, Detail string
	Err               error
}

func (e *Error) Error() string {
	msg := "workflow/intervention: " + e.Code
	if e.Ref != "" {
		msg += " [" + e.Ref + "]"
	}
	msg += ": " + e.Detail
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

// Unwrap exposes the package sentinel and any cause.
func (e *Error) Unwrap() []error {
	if e.Err != nil {
		return []error{ErrIntervention, e.Err}
	}
	return []error{ErrIntervention}
}

// ErrorCode reports the refusal's stable code for telemetry classification
// (internal/workflow/observe.ErrorCode); it never carries message text.
func (e *Error) ErrorCode() string {
	if e == nil {
		return ""
	}
	return e.Code
}

// CodeOf returns the denial code err carries, or "".
func CodeOf(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func refuse(code, ref, format string, args ...any) *Error {
	detail := format
	if len(args) > 0 {
		detail = fmt.Sprintf(format, args...)
	}
	return &Error{Code: code, Ref: ref, Detail: detail}
}
