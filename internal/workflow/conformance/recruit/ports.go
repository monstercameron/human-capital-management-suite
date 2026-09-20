package recruit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/simulate"
)

// digest is a small, deterministic content identity for this fixture's own
// transform outputs. It is not the interpreter's canonical digest (that stays
// internal to package simulate); it only has to be stable across runs of the
// same inputs, which sha256 over a fixed field order gives for free.
func digest(profile string, parts ...string) string {
	h := sha256.New()
	h.Write([]byte(profile))
	h.Write([]byte{0})
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))[:32]
}

// Decisions evaluates the hire-routing DECISION node. Precedence is
// load-bearing and fixed: an already-employed person is refused before offer
// state is even considered, an unbound offer before capacity, an exhausted
// position or budget before documents, and missing work authorization before
// downstream readiness, so each blocked terminal proves exactly one gate.
type Decisions struct{}

func (Decisions) Decide(_ context.Context, req simulate.DecisionRequest) (simulate.DecisionResult, error) {
	if req.Decision.RuleRef != RuleHireRoute {
		return simulate.DecisionResult{}, fmt.Errorf("recruit: no evaluator is bound to rule %q", req.Decision.RuleRef)
	}
	get := func(name string) (bool, error) {
		v, err := req.Inputs.Get(name)
		if err != nil {
			return false, err
		}
		return v.Bool()
	}
	duplicate, err := get("duplicate_person")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	approved, err := get("offer_approved")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	accepted, err := get("offer_accepted")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	position, err := get("position_available")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	budget, err := get("budget_available")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	workAuth, err := get("work_auth_valid")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	allReady, err := get("all_ready")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	proposal, err := req.Inputs.Text("proposal_digest")
	if err != nil {
		return simulate.DecisionResult{}, err
	}

	route, detail := RouteHired, "every hire gate is clear against the bound proposal "+proposal
	switch {
	case duplicate:
		route, detail = RouteDuplicate, "person registry reports an existing employment; a second worker record is refused"
	case !approved || !accepted:
		route, detail = RouteOffer, "the offer is not both approved and accepted; nothing is bound"
	case !position || !budget:
		route, detail = RouteCapacity, "the position or its budget line is exhausted; the hold cannot be placed"
	case !workAuth:
		route, detail = RouteWorkAuth, "no valid work authorization documents; the document gate blocks"
	case !allReady:
		route, detail = RouteDegraded, "a downstream system is not ready; the start degrades to bounded repair"
	}
	return simulate.DecisionResult{
		RouteKey: route,
		TraceRef: RuleHireRoute + "#" + digest("trace", proposal, route)[:16],
		Detail:   detail,
	}, nil
}

// Transforms evaluates the employment-proposal TRANSFORM node: the proposal
// digest binds candidate, offer, position, person, the observed offer state,
// the document evidence refs and the start date into one checkable identity.
type Transforms struct{}

func (Transforms) Transform(_ context.Context, req simulate.TransformRequest) (simulate.TransformResult, error) {
	if req.Transform.TransformRef != TransformHireProposal {
		return simulate.TransformResult{}, fmt.Errorf("recruit: transform %s has no bound implementation", req.Transform.TransformRef)
	}
	text := func(name string) (string, error) { return req.Inputs.Text(name) }
	candidate, err := text("candidate_id")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	offer, err := text("offer_id")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	position, err := text("target_position_id")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	person, err := text("person_id")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	state, err := text("offer_state")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	refs, err := text("work_auth_evidence_refs")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	start, err := text("start_date")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	pd := digest("hcmnext.workflow.conformance.recruit.Proposal/v1", candidate, offer, position, person, state, refs, start)
	return simulate.TransformResult{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{"proposal_digest": simulate.NewString(pd)},
		Detail:  "bound employment proposal " + pd,
	}, nil
}

// Reads answers the offer-clock OBSERVE node from the environment's declared
// observation outcome. A caller controls it through
// [Environment.OfferClockOutcome] to walk the valid, expired or degraded
// offer path.
type Reads struct{ Env *Environment }

func (r Reads) Observe(_ context.Context, req simulate.ObservationRequest) (simulate.Observation, error) {
	outcome := r.Env.OfferClockOutcome
	if outcome == "" {
		outcome = workflow.OutcomeUnknown
	}
	state := OfferStateValid
	if outcome == workflow.OutcomeFail {
		state = OfferStateExpired
	}
	return simulate.Observation{
		Outcome:   outcome,
		Outputs:   simulate.Bag{"offer_state": simulate.NewString(state)},
		Watermark: r.Env.OfferWatermark,
		Detail:    "observed " + req.Observe.SourceAuthority + " for offer clock: " + string(outcome),
	}, nil
}

// Approvals derives the constant approval graph the reference workflow's
// hired terminal declares, without waiting for any of them. Every declared
// requirement ref gets exactly one work item; there is no silent narrowing.
type Approvals struct{}

func (Approvals) WouldAwait(_ context.Context, req simulate.ApprovalRequest) ([]simulate.WorkItem, error) {
	if len(req.RequirementRefs) == 0 {
		return nil, nil
	}
	refs := append([]string(nil), req.RequirementRefs...)
	sort.Strings(refs)
	items := make([]simulate.WorkItem, 0, len(refs))
	for i, ref := range refs {
		items = append(items, simulate.WorkItem{
			NodeID:            req.NodeID,
			Kind:              "APPROVAL",
			State:             simulate.WouldAwait,
			RequirementID:     ref,
			Stage:             uint32(i + 1),
			QuorumMin:         1,
			Outcome:           "RESOLVED",
			Candidates:        []string{"principal:" + ref + "-approver-1 via ROLE"},
			ExpressionDigest:  digest("hcmnext.workflow.conformance.recruit.ApprovalExpression/v1", ref),
			RequirementDigest: digest("hcmnext.workflow.conformance.recruit.ApprovalRequirement/v1", ref),
			Tier:              "STANDARD",
		})
	}
	return items, nil
}
