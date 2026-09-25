package workorder

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	orderdomain "github.com/monstercameron/human-capital-management-suite/internal/domains/workorder"
	templatedomain "github.com/monstercameron/human-capital-management-suite/internal/domains/workordertemplate"
)

var (
	ErrTemplateMismatch = errors.New("work order workflow: pinned template mismatch")
	ErrPhaseTransition  = errors.New("work order workflow: transition is not declared by the pinned template")
	ErrPhaseBinding     = errors.New("work order workflow: published phase has no compiled workflow binding")
	ErrStaleGateFacts   = errors.New("work order workflow: gate facts are stale or unbound")
)

// DecisionFact is an authoritative result from the workflow runtime or
// approval authority. Callers must resolve it for this order and revision.
type DecisionFact struct {
	ID        string
	Satisfied bool
}

// EvidenceFact is evidence verified against its owning authority. A string
// supplied by a client is not a verified evidence fact.
type EvidenceFact struct {
	ID        string
	SourceRef string
	Satisfied bool
}

// GateFact records one current governed gate result.
type GateFact struct {
	ID        string
	Kind      string
	PolicyRef string
	Satisfied bool
}

// WorkflowCompletionFact is a successful durable completion recorded by the
// pinned workflow runtime. The trusted reader must verify WorkOrderID and
// OrderRevision from the immutable workflow input/output binding; these fields
// are never copied directly from a phase-transition request.
type WorkflowCompletionFact struct {
	NodeID        string
	Outcome       string
	PlanVersion   string
	PlanDigest    string
	InstanceID    string
	WorkOrderID   string
	OrderRevision uint64
}

// GateFacts is produced by trusted workflow and evidence readers immediately
// before a transition. Its subject, revision and template digest bind every
// result to the exact current order state and published template.
type GateFacts struct {
	WorkOrderID         string
	OrderRevision       uint64
	TemplateDigest      string
	WorkflowVersion     string
	WorkflowDigest      string
	WorkflowInstanceID  string
	Decisions           []DecisionFact
	Evidence            []EvidenceFact
	Gates               []GateFact
	WorkflowCompletions []WorkflowCompletionFact
}

// GateBlocker is a stable, user-presentable reason the transition is pending.
type GateBlocker struct {
	Kind string
	ID   string
	Why  string
}

// GateDecision describes whether a transition may proceed and lists every
// unsatisfied requirement in deterministic order.
type GateDecision struct {
	Allowed  bool
	From     orderdomain.Phase
	To       orderdomain.Phase
	Pin      templatedomain.Pin
	Blockers []GateBlocker
}

// EvaluateTransition checks the current phase's exit requirements against an
// immutable published template, the current aggregate snapshot, and
// authoritative facts read for that exact snapshot. A caller must fail closed
// on an error or a decision with Allowed=false before invoking the aggregate
// phase transition.
func EvaluateTransition(
	published templatedomain.Published,
	order ordersnapshot,
	target orderdomain.Phase,
	facts GateFacts,
) (GateDecision, error) {
	if err := published.Verify(); err != nil {
		return GateDecision{}, fmt.Errorf("%w: %v", ErrTemplateMismatch, err)
	}
	pin := published.Pin()
	if order.TemplateID != pin.TemplateID || order.TemplateVersion != pin.Version {
		return GateDecision{}, fmt.Errorf("%w: order pins %s@%s; resolved %s@%s", ErrTemplateMismatch, order.TemplateID, order.TemplateVersion, pin.TemplateID, pin.Version)
	}
	if order.TemplateDigest != pin.Digest {
		return GateDecision{}, fmt.Errorf("%w: order pins digest %s; resolved %s", ErrTemplateMismatch, order.TemplateDigest, pin.Digest)
	}
	if order.WorkflowID != WorkflowIDForTemplate(pin.TemplateID) || strings.TrimSpace(order.WorkflowVersion) == "" || order.WorkflowDigest == "" || order.WorkflowInstanceID == "" ||
		facts.WorkflowVersion != order.WorkflowVersion || facts.WorkflowDigest != order.WorkflowDigest || facts.WorkflowInstanceID != order.WorkflowInstanceID {
		return GateDecision{}, ErrStaleGateFacts
	}
	if facts.WorkOrderID != order.ID || facts.OrderRevision != order.Revision || facts.TemplateDigest != pin.Digest {
		return GateDecision{}, ErrStaleGateFacts
	}
	if !snapshotAllows(order, target) {
		return GateDecision{}, ErrPhaseTransition
	}
	draft := published.Snapshot()
	if err := validatePhaseBindings(draft); err != nil {
		return GateDecision{}, err
	}
	var current *templatedomain.Phase
	var targetExists bool
	for i := range draft.Phases {
		phase := &draft.Phases[i]
		if orderdomain.Phase(phase.ID) == order.Phase {
			current = phase
		}
		if orderdomain.Phase(phase.ID) == target {
			targetExists = true
		}
	}
	if current == nil || !contains(current.AllowedExits, string(target)) || !targetExists {
		return GateDecision{}, ErrPhaseTransition
	}
	decision := GateDecision{Allowed: true, From: order.Phase, To: target, Pin: pin}
	checkWorkflowCompletion(&decision, order, facts)
	checkRequests(&decision, *current, draft, order)
	checkDecisions(&decision, *current, facts.Decisions)
	checkEvidence(&decision, *current, draft, facts.Evidence)
	checkGates(&decision, *current, facts.Gates)
	sort.Slice(decision.Blockers, func(i, j int) bool {
		a, b := decision.Blockers[i], decision.Blockers[j]
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.ID != b.ID {
			return a.ID < b.ID
		}
		return a.Why < b.Why
	})
	decision.Allowed = len(decision.Blockers) == 0
	return decision, nil
}

func checkWorkflowCompletion(decision *GateDecision, order orderdomain.Snapshot, facts GateFacts) {
	var binding *PhaseBinding
	for _, candidate := range PhaseBindings() {
		if orderdomain.Phase(candidate.PhaseID) == order.Phase {
			copy := candidate
			binding = &copy
			break
		}
	}
	if binding == nil {
		decision.Blockers = append(decision.Blockers, GateBlocker{Kind: "WORKFLOW", ID: string(order.Phase), Why: "current phase has no compiled workflow binding"})
		return
	}
	for _, fact := range facts.WorkflowCompletions {
		if fact.NodeID == binding.CompletionNodeID && fact.Outcome == binding.CompletionOutcome &&
			fact.PlanVersion == order.WorkflowVersion && fact.PlanDigest == order.WorkflowDigest && fact.InstanceID == order.WorkflowInstanceID &&
			fact.WorkOrderID == order.ID && fact.OrderRevision == order.Revision {
			return
		}
	}
	decision.Blockers = append(decision.Blockers, GateBlocker{Kind: "WORKFLOW", ID: binding.CompletionNodeID, Why: "pinned workflow phase completion is missing or does not match the current order revision"})
}

// ordersnapshot is an internal alias that prevents the published template's
// similarly named concept from being confused with the business snapshot in
// this API.
type ordersnapshot = orderdomain.Snapshot

func validatePhaseBindings(draft templatedomain.Draft) error {
	bindings := make(map[string]PhaseBinding, len(PhaseBindings()))
	for _, binding := range PhaseBindings() {
		bindings[binding.PhaseID] = binding
	}
	phases := make(map[string]templatedomain.Phase, len(draft.Phases))
	for _, phase := range draft.Phases {
		if _, ok := bindings[phase.ID]; !ok {
			return fmt.Errorf("%w: %s", ErrPhaseBinding, phase.ID)
		}
		phases[phase.ID] = phase
	}
	expected := [][2]string{{"DRAFT", "AUTHORIZATION"}, {"AUTHORIZATION", "READY"}, {"READY", "EXECUTION"}, {"EXECUTION", "INSPECTION"}, {"INSPECTION", "ACCEPTED"}, {"ACCEPTED", "CLOSED"}}
	for _, edge := range expected {
		phase, ok := phases[edge[0]]
		if !ok || !contains(phase.AllowedExits, edge[1]) {
			return fmt.Errorf("%w: required transition %s -> %s is missing", ErrPhaseBinding, edge[0], edge[1])
		}
	}
	for _, edge := range expected {
		phase := phases[edge[0]]
		for _, exit := range phase.AllowedExits {
			if exit != edge[1] {
				return fmt.Errorf("%w: %s also permits bypass exit %s", ErrPhaseBinding, edge[0], exit)
			}
		}
	}
	for id := range bindings {
		if _, ok := phases[id]; !ok {
			return fmt.Errorf("%w: required phase %s is missing", ErrPhaseBinding, id)
		}
	}
	if len(phases["CLOSED"].AllowedExits) != 0 {
		return fmt.Errorf("%w: CLOSED cannot declare an exit", ErrPhaseBinding)
	}
	return nil
}

func snapshotAllows(order orderdomain.Snapshot, target orderdomain.Phase) bool {
	for _, edge := range order.Transitions {
		if edge.From == order.Phase && edge.To == target {
			return true
		}
	}
	return false
}

func checkRequests(decision *GateDecision, phase templatedomain.Phase, draft templatedomain.Draft, order orderdomain.Snapshot) {
	definitions := make(map[string]templatedomain.RequestDefinition, len(draft.Requests))
	for _, definition := range draft.Requests {
		definitions[definition.ID] = definition
	}
	for _, requiredID := range phase.RequiredRequests {
		definition, exists := definitions[requiredID]
		if !exists {
			decision.Blockers = append(decision.Blockers, GateBlocker{Kind: "REQUEST", ID: requiredID, Why: "template request definition is unavailable"})
			continue
		}
		found := false
		for _, request := range order.Requests {
			if request.DefinitionID == requiredID && string(request.Kind) == string(definition.Kind) && request.Status == orderdomain.RequestApproved {
				found = true
				break
			}
		}
		if !found {
			decision.Blockers = append(decision.Blockers, GateBlocker{Kind: "REQUEST", ID: requiredID, Why: "required request is missing or not approved"})
		}
	}
}

func checkDecisions(decision *GateDecision, phase templatedomain.Phase, facts []DecisionFact) {
	for _, requiredID := range phase.RequiredDecisions {
		if !hasDecision(facts, requiredID) {
			decision.Blockers = append(decision.Blockers, GateBlocker{Kind: "DECISION", ID: requiredID, Why: "required workflow decision is missing or unsatisfied"})
		}
	}
}

func checkEvidence(decision *GateDecision, phase templatedomain.Phase, draft templatedomain.Draft, facts []EvidenceFact) {
	definitions := make(map[string]templatedomain.EvidenceRequirement, len(draft.Evidence))
	for _, definition := range draft.Evidence {
		definitions[definition.ID] = definition
	}
	for _, requiredID := range phase.RequiredEvidence {
		definition, exists := definitions[requiredID]
		if !exists || !definition.Required {
			decision.Blockers = append(decision.Blockers, GateBlocker{Kind: "EVIDENCE", ID: requiredID, Why: "required evidence definition is unavailable"})
			continue
		}
		if !hasEvidence(facts, requiredID) {
			decision.Blockers = append(decision.Blockers, GateBlocker{Kind: "EVIDENCE", ID: requiredID, Why: "required evidence is missing or unverified"})
		}
	}
}

func checkGates(decision *GateDecision, phase templatedomain.Phase, facts []GateFact) {
	for _, required := range phase.Gates {
		if !required.Required {
			continue
		}
		if !hasGate(facts, required) {
			decision.Blockers = append(decision.Blockers, GateBlocker{Kind: "GATE", ID: required.ID, Why: "required gate is missing, stale, mismatched or unsatisfied"})
		}
	}
}

func hasDecision(facts []DecisionFact, id string) bool {
	for _, fact := range facts {
		if fact.ID == id && fact.Satisfied {
			return true
		}
	}
	return false
}
func hasEvidence(facts []EvidenceFact, id string) bool {
	for _, fact := range facts {
		if fact.ID == id && fact.SourceRef != "" && fact.Satisfied {
			return true
		}
	}
	return false
}
func hasGate(facts []GateFact, want templatedomain.Gate) bool {
	for _, fact := range facts {
		if fact.ID == want.ID && fact.Kind == want.Kind && fact.PolicyRef == want.PolicyRef && fact.Satisfied {
			return true
		}
	}
	return false
}
func contains(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}
