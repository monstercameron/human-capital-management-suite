package leavereturn

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
// transform outputs. It only has to be stable across runs of the same
// inputs, which sha256 over a fixed field order gives for free.
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

// Decisions evaluates the evidence-review and readiness-return DECISION
// nodes. A manager determining legal eligibility, unsealed medical
// evidence, a not-ready return and an inactive employment are all refused
// rather than resolved.
type Decisions struct{}

func (Decisions) Decide(_ context.Context, req simulate.DecisionRequest) (simulate.DecisionResult, error) {
	switch req.Decision.RuleRef {
	case RuleEvidenceReview:
		return decideEvidenceReview(req)
	case RuleReadinessReturn:
		return decideReadinessReturn(req)
	default:
		return simulate.DecisionResult{}, fmt.Errorf("leavereturn: no evaluator is bound to rule %q", req.Decision.RuleRef)
	}
}

func decideEvidenceReview(req simulate.DecisionRequest) (simulate.DecisionResult, error) {
	reviewer, err := req.Inputs.Text("reviewer_role")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	sealed, err := boolInput(req.Inputs, "medical_sealed")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	managerDetermined, err := boolInput(req.Inputs, "manager_determined_eligibility")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	var route, detail string
	switch {
	case !sealed:
		route = RouteCompartmentBreachBlocked
		detail = "medical evidence left its restricted compartment; the review cannot proceed"
	case managerDetermined:
		route = RouteManagerEligibilityBlocked
		detail = "the manager determined legal eligibility; only the leave administrator's restricted review decides"
	case reviewer != "leave-administrator":
		route = RouteCompartmentBreachBlocked
		detail = "the restricted review belongs to the leave administrator, not " + reviewer
	default:
		route = RouteRoutineReviewRequired
		detail = "sealed evidence under the leave administrator; routine restricted review is required"
	}
	return simulate.DecisionResult{
		RouteKey: route,
		TraceRef: RuleEvidenceReview + "#" + digest("trace", reviewer, boolText(sealed), boolText(managerDetermined))[:16],
		Detail:   detail,
	}, nil
}

func decideReadinessReturn(req simulate.DecisionRequest) (simulate.DecisionResult, error) {
	state, err := req.Inputs.Text("readiness_state")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	active, err := boolInput(req.Inputs, "employment_active")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	requested, err := boolInput(req.Inputs, "return_requested")
	if err != nil {
		return simulate.DecisionResult{}, err
	}
	var route, detail string
	switch {
	case !active:
		route = RouteEmploymentInactiveInvalid
		detail = "the employment is not active; a return cannot commit against it"
	case state != "READY":
		route = RouteReturnBlockedNotReady
		detail = "readiness is " + state + "; the return waits for current evidence, restriction and readiness obligations"
	case !requested:
		route = RouteReturnBlockedNotReady
		detail = "no return was requested; leave continues under observation"
	default:
		route = RouteProceedToReturn
		detail = "ready with an active employment and a requested return"
	}
	return simulate.DecisionResult{
		RouteKey: route,
		TraceRef: RuleReadinessReturn + "#" + digest("trace", state, boolText(active))[:16],
		Detail:   detail,
	}, nil
}

func boolInput(b simulate.Bag, path string) (bool, error) {
	v, err := b.Get(path)
	if err != nil {
		return false, err
	}
	return v.Bool()
}

func boolText(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// Transforms evaluates the pure proposal TRANSFORM node.
type Transforms struct{}

func (Transforms) Transform(_ context.Context, req simulate.TransformRequest) (simulate.TransformResult, error) {
	if req.Transform.TransformRef != TransformBuildProposal {
		return simulate.TransformResult{}, fmt.Errorf("leavereturn: transform %s has no bound implementation", req.Transform.TransformRef)
	}
	worker, err := req.Inputs.Text("worker_id")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	employment, err := req.Inputs.Text("employment_id")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	programs, err := req.Inputs.Text("program_ids")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	jurisdiction, err := req.Inputs.Text("jurisdiction")
	if err != nil {
		return simulate.TransformResult{}, err
	}
	pd := digest("hcmnext.workflow.conformance.leavereturn.Proposal/v1", worker, employment, programs, jurisdiction)
	return simulate.TransformResult{
		Outcome: workflow.OutcomeSucceeded,
		Outputs: simulate.Bag{"proposal_digest": simulate.NewString(pd)},
		Detail:  "bound leave proposal " + pd,
	}, nil
}

// Reads answers the benefits-continuation OBSERVE node from the
// environment's declared observation outcome. [Environment.ObserveOutcome]
// controls it, so a caller can walk either the consistent
// pending-obligations path or the bounded repair path.
type Reads struct{ Env *Environment }

func (r Reads) Observe(_ context.Context, req simulate.ObservationRequest) (simulate.Observation, error) {
	outcome := r.Env.ObserveOutcome
	if outcome == "" {
		outcome = workflow.OutcomeUnknown
	}
	return simulate.Observation{
		Outcome:   outcome,
		Outputs:   simulate.Bag{"benefits_state": simulate.NewString(string(outcome))},
		Watermark: r.Env.ObserveWatermark,
		Detail:    "observed " + req.Observe.SourceAuthority + " for benefits continuation: " + string(outcome),
	}, nil
}

// Approvals derives the constant approval graph the reference workflow's
// terminals declare, without waiting for any of them. Every declared
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
			ExpressionDigest:  digest("hcmnext.workflow.conformance.leavereturn.ApprovalExpression/v1", ref),
			RequirementDigest: digest("hcmnext.workflow.conformance.leavereturn.ApprovalRequirement/v1", ref),
			Tier:              "STANDARD",
		})
	}
	return items, nil
}
