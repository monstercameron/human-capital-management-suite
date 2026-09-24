package simulate

import (
	"context"
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
)

// Route keys declared by the Promotion reference workflow.
const (
	RouteWithinThreshold  = "WITHIN_THRESHOLD"
	RouteExceedsThreshold = "EXCEEDS_THRESHOLD"
)

// PromotionThresholdRuleRef is the stable alias the published Promotion
// threshold body resolves through. Its version is pinned separately as v3.
const PromotionThresholdRuleRef = "rules.compensation.raise_threshold/v3"

const (
	FieldRaiseRatio   = "raise_ratio"
	FieldBandPosition = "band_position"
)

// RulesDecisions evaluates every DECISION through the same published payload
// resolver. It also resolves the exact Promotion table for approval derivation.
type RulesDecisions struct {
	Payloads      PublishedRuleResolver
	PromotionRule *workflow.ResolvedReference
}

func (d RulesDecisions) Decide(ctx context.Context, req DecisionRequest) (DecisionResult, error) {
	return (PublishedRuleDecisions{Payloads: d.Payloads}).Decide(ctx, req)
}

// Tier recomputes the Promotion approval tier from the exact table body the
// workflow's compiled DECISION pinned.
func (d RulesDecisions) Tier(input rules.PromotionApprovalInput) (rules.PromotionApprovalDecision, error) {
	if err := input.Validate(); err != nil {
		return rules.PromotionApprovalDecision{}, fmt.Errorf("simulate: invalid promotion approval inputs: %w", err)
	}
	if d.Payloads == nil || d.PromotionRule == nil || d.PromotionRule.Digest == "" || d.PromotionRule.Kind != workflow.RefRule {
		return rules.PromotionApprovalDecision{}, fmt.Errorf("simulate: promotion rule payload is not pinned")
	}
	ref := workflow.Reference{Kind: d.PromotionRule.Kind, ID: d.PromotionRule.ID, Version: d.PromotionRule.Version}
	payload, err := d.Payloads.Resolve(ref, d.PromotionRule.Digest)
	if err != nil {
		return rules.PromotionApprovalDecision{}, fmt.Errorf("simulate: resolve promotion rule payload: %w", err)
	}
	if payload.Kind != "DECISION_TABLE" || payload.Table == nil {
		return rules.PromotionApprovalDecision{}, fmt.Errorf("simulate: promotion rule payload is not a decision table")
	}
	inputs := map[string]rules.Value{
		rules.ColumnIncreasePercent: rules.DecimalValue(input.IncreasePercent),
		rules.ColumnBandPosition:    rules.StringValue(string(input.BandPosition)),
		rules.ColumnBudgetAuthority: rules.StringValue(string(input.BudgetAuthority)),
		rules.ColumnGradeChange:     rules.BoolValue(input.GradeChange),
	}
	result, err := rules.Evaluate(*payload.Table, inputs)
	if err != nil {
		return rules.PromotionApprovalDecision{}, fmt.Errorf("simulate: evaluate pinned promotion table: %w", err)
	}
	decision := rules.PromotionApprovalDecision{TableID: result.TableID, TableVersion: result.TableVersion, TableDigest: result.TableDigest, Trace: result.Trace}
	switch result.Status {
	case rules.StatusMatched:
		if len(result.Matches) != 1 {
			return rules.PromotionApprovalDecision{}, fmt.Errorf("simulate: pinned promotion table returned %d matches", len(result.Matches))
		}
		decision.MatchedRowID = result.Matches[0].RowID
		for i, output := range payload.Table.Outputs {
			if output.Name == "approval_tier" {
				if i >= len(result.Matches[0].Outputs) || result.Matches[0].Outputs[i].Kind() != rules.KindString {
					return rules.PromotionApprovalDecision{}, fmt.Errorf("simulate: pinned promotion table has an invalid approval_tier output")
				}
				decision.Tier = rules.ApprovalTier(result.Matches[0].Outputs[i].String())
				break
			}
		}
		if !validApprovalTier(decision.Tier) {
			return rules.PromotionApprovalDecision{}, fmt.Errorf("simulate: pinned promotion table did not produce a valid approval_tier")
		}
	case rules.StatusUnknown:
		decision.Tier = rules.ApprovalTierUnknownBlocked
	case rules.StatusConflict:
		return rules.PromotionApprovalDecision{}, fmt.Errorf("simulate: pinned promotion table reported conflict")
	default:
		return rules.PromotionApprovalDecision{}, fmt.Errorf("simulate: pinned promotion table returned status %s", result.Status)
	}
	return decision, nil
}

func validApprovalTier(tier rules.ApprovalTier) bool {
	switch tier {
	case rules.ApprovalTierStandard, rules.ApprovalTierFinanceRequired, rules.ApprovalTierExecutiveRequired, rules.ApprovalTierUnknownBlocked:
		return true
	default:
		return false
	}
}

func increaseDecimal(text string) (values.Decimal, error) {
	return values.NewDecimal(text, rules.IncreasePercentScale, values.RoundingHalfEven)
}
