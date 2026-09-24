package simulate

import (
	"context"
	"sort"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork"
)

// TierBlocked is the work-item outcome recorded when the approval tier itself
// could not be resolved. It is deliberately not an empty requirement set: an
// unresolved tier resolving to "nobody has to approve" is exactly the failure
// the rules engine refuses to make.
const TierBlocked = "TIER_BLOCKED"

// HumanWorkApprovals derives the promotion approval graph and resolves its
// candidate approvers, without waiting for any of them.
//
// It re-evaluates the threshold table rather than reading a tier the DECISION
// node remembered. That is deliberate: deriving approvals from a remembered
// answer lets the routing decision and the approval graph disagree the moment
// the table is republished, and an approval graph that disagrees with the
// decision that produced it is worse than no graph.
//
// The requirements it returns are the whole graph the tier produces - the
// reference workflow's constant baseline of current manager, HRBP and
// compensation partner, plus whatever the tier adds - not only the ids the
// node's governance surface happened to name.
type HumanWorkApprovals struct {
	// Decisions re-evaluates the same published rule body the DECISION node used.
	Decisions RulesDecisions
	// ProposalNodeID names the node whose outputs carry the raise and band
	// position. Empty means "search every executed node", in sorted node order.
	ProposalNodeID string
}

// WouldAwait implements ApprovalPort.
func (h HumanWorkApprovals) WouldAwait(_ context.Context, req ApprovalRequest) ([]WorkItem, error) {
	ratio, position, ok := findProposalFacts(req, h.ProposalNodeID)
	if !ok {
		return nil, refuse(CodeHandlerFailed, req.NodeID,
			"no executed node produced %s and %s, so no approval graph can be derived",
			FieldRaiseRatio, FieldBandPosition)
	}

	increase, err := increaseDecimal(ratio)
	if err != nil {
		return nil, wrap(CodeHandlerFailed, req.NodeID, err, "approval tier")
	}
	budgetText, err := req.WorkflowInputs.Text("budget_authority")
	if err != nil {
		return nil, wrap(CodeHandlerFailed, req.NodeID, err, "approval tier inputs")
	}
	gradeValue, err := req.WorkflowInputs.Get("grade_change")
	if err != nil {
		return nil, wrap(CodeHandlerFailed, req.NodeID, err, "approval tier inputs")
	}
	gradeChange, err := gradeValue.Bool()
	if err != nil {
		return nil, wrap(CodeHandlerFailed, req.NodeID, err, "approval tier inputs")
	}
	input := rules.PromotionApprovalInput{
		IncreasePercent: increase,
		BandPosition:    bandPositionOf(position),
		BudgetAuthority: rules.BudgetAuthority(budgetText),
		GradeChange:     gradeChange,
	}
	tier, err := h.Decisions.Tier(input)
	if err != nil {
		return nil, wrap(CodeHandlerFailed, req.NodeID, err, "approval tier")
	}
	if tier.Tier == rules.ApprovalTierUnknownBlocked {
		return blockedItems(req, tier), nil
	}

	scenario, err := humanwork.NewPromotionScenario(input)
	if err != nil {
		return nil, wrap(CodeHandlerFailed, req.NodeID, err, "approval requirement derivation")
	}
	resolutions, err := scenario.ResolveAll()
	if err != nil {
		return nil, wrap(CodeHandlerFailed, req.NodeID, err, "approver resolution")
	}

	items := make([]WorkItem, 0, len(resolutions))
	for _, res := range resolutions {
		requirement, found := scenario.Requirements.Find(res.RequirementID)
		item := WorkItem{
			NodeID:            req.NodeID,
			Kind:              "APPROVAL",
			State:             WouldAwait,
			RequirementID:     res.RequirementID,
			QuorumMin:         res.QuorumRequired,
			Outcome:           string(res.Outcome),
			FallbackUsed:      res.FallbackUsed,
			ExpressionDigest:  res.ExpressionDigest,
			RequirementDigest: res.RequirementDigest,
			Tier:              string(scenario.Requirements.Tier),
		}
		if found {
			item.Stage = requirement.Stage
			item.DecideBy = requirement.Deadline.DecideBy.Time()
			item.Expiry = requirement.Deadline.Expiry.Time()
		}
		for _, c := range res.Candidates {
			item.Candidates = append(item.Candidates, c.PrincipalID+" via "+string(c.Via))
		}
		sort.Strings(item.Candidates)
		for _, e := range res.Excluded {
			item.Excluded = append(item.Excluded, e.PrincipalID+" ["+e.RuleID+"] "+e.Reason)
		}
		sort.Strings(item.Excluded)
		items = append(items, item)
	}
	return items, nil
}

// blockedItems states one work item per requirement reference the node
// declared, marked TIER_BLOCKED with no candidates.
func blockedItems(req ApprovalRequest, tier rules.PromotionApprovalDecision) []WorkItem {
	refs := req.RequirementRefs
	if len(refs) == 0 {
		refs = []string{"approval.unresolved"}
	}
	items := make([]WorkItem, 0, len(refs))
	for _, ref := range refs {
		items = append(items, WorkItem{
			NodeID:        req.NodeID,
			Kind:          "APPROVAL",
			State:         WouldAwait,
			RequirementID: ref,
			Outcome:       TierBlocked,
			Tier:          string(tier.Tier),
		})
	}
	return items
}

// findProposalFacts locates the raise and band position among the outputs the
// run has produced so far.
func findProposalFacts(req ApprovalRequest, preferred string) (string, string, bool) {
	read := func(bag Bag) (string, string, bool) {
		ratio, okRatio := bag[FieldRaiseRatio]
		position, okPosition := bag[FieldBandPosition]
		if !okRatio || !okPosition {
			return "", "", false
		}
		return ratio.Text, position.Text, true
	}
	if preferred != "" {
		if bag, ok := req.NodeOutputs[preferred]; ok {
			return read(bag)
		}
		return "", "", false
	}
	ids := make([]string, 0, len(req.NodeOutputs))
	for id := range req.NodeOutputs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if ratio, position, ok := read(req.NodeOutputs[id]); ok {
			return ratio, position, true
		}
	}
	return "", "", false
}

func bandPositionOf(text string) rules.BandPosition {
	band := rules.BandPosition(text)
	if !band.Valid() {
		return rules.BandPositionUnknown
	}
	return band
}
