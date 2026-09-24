package app

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/repair"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/snapshot"
	"github.com/monstercameron/human-capital-management-suite/internal/governance"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/definitions"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func compileDispatchPreflight(
	definition intent.Ref,
	proposalRevision string,
	snapshotDigest string,
	costUnits int64,
	costBasis string,
	risk intent.RiskAssessment,
	writes []intent.IntendedWrite,
	approvals []intent.ApprovalTask,
	obligations []string,
	revalidation string,
) (intent.PreflightPlan, error) {
	return intent.CompilePreflightPlan(intent.PreflightPlanRequest{
		Definition:       definition,
		ProposalRevision: proposalRevision,
		Snapshot:         snapshot.ReadSnapshot{Digest: snapshotDigest},
		Governance: governance.ComposeRequest{
			AuthZ:   governance.AuthZInput{Effect: "UNKNOWN", Reason: "dispatcher has no authorization receipt in its payload", Digest: snapshotDigest},
			Legal:   governance.LegalInput{Effect: "UNKNOWN", Reason: "dispatcher has no legal decision receipt in its payload", Digest: snapshotDigest},
			Purpose: governance.PurposeInput{Allowed: false, Reason: "purpose decision is not carried by the domain result"},
			Risk:    governance.RiskInput{Level: string(risk.Level)},
			Now:     time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		Cost:                 intent.CostBreakdown{Units: costUnits, Basis: costBasis},
		Risk:                 risk,
		Approvals:            approvals,
		Obligations:          obligations,
		Writes:               writes,
		RevalidationTriggers: []string{revalidation},
		Completion:           intent.WaitForChild,
		RepairExpectation:    "replan if a pinned source or control changes",
	})
}

func promotionPreflightPlan(result promotion.PreflightResult) (intent.PreflightPlan, error) {
	writes := make([]intent.IntendedWrite, 0)
	for _, binding := range definitions.Bindings() {
		if binding.Definition.TypeID != promotion.IntentType {
			continue
		}
		for _, path := range binding.WriteProperties {
			writes = append(writes, intent.IntendedWrite{Target: path, Effect: "UPDATE"})
		}
	}
	return compileDispatchPreflight(
		intent.Ref{TypeID: promotion.IntentType, Version: 1}, result.ResultDigest, result.InputsDigest, 0,
		"promotion preflight; cost not calculated at this stage",
		intent.RiskAssessment{Level: intent.RiskHigh, Factors: []string{"promotion preflight", result.RulePackVersion}},
		writes, nil, nil, "promotion_execution_revalidation/v1",
	)
}

func promotionCompositionPlan(call promotionCall, proposalRevision string) (intent.CompiledComposition, error) {
	registry, err := definitions.NewRegistry()
	if err != nil {
		return intent.CompiledComposition{}, err
	}
	parentRef := intent.Ref{TypeID: promotion.IntentType, Version: 1}
	parent, err := registry.Resolve(parentRef)
	if err != nil {
		return intent.CompiledComposition{}, err
	}
	var childRefs []intent.Ref
	for _, binding := range definitions.Bindings() {
		if binding.Definition == parentRef {
			childRefs = append(childRefs, binding.ChildDefinitions...)
			break
		}
	}
	if len(childRefs) < 2 {
		return intent.CompiledComposition{}, intent.ErrInvalidComposition
	}
	defs := make(map[intent.Ref]intent.Definition)
	for _, definition := range definitions.All() {
		defs[definition.Ref] = definition
	}
	tenant := string(call.Request.Tenant)
	orgScope := "tenant:" + tenant
	plan := intent.CompositionPlan{
		ParentDefinition: parentRef,
		ParentProposal:   proposalRevision,
		ParentTenant:     tenant,
		ParentOrg:        orgScope,
		ParentPurpose:    parent.Ref.TypeID,
		Boundary:         intent.BoundarySaga,
		Wait:             intent.WaitForChildren,
		Failure:          intent.FailParent,
		Correction:       intent.CorrectInPlace,
		Cancellation:     intent.PropagateCancellation,
		MaxCost:          1,
		MaxChildren:      len(childRefs),
	}
	for ordinal, ref := range childRefs {
		definition, ok := defs[ref]
		if !ok {
			return intent.CompiledComposition{}, intent.ErrUnknownDefinition
		}
		owner := strings.ToLower(definition.OwnerDomain)
		plan.Children = append(plan.Children, intent.ChildTemplate{
			Definition: ref,
			Ordinal:    uint32(ordinal),
			Owner:      owner,
			System:     "hcmnext." + owner,
			Tenant:     tenant,
			Org:        orgScope + "." + owner,
			Purpose:    parent.Ref.TypeID + "." + ref.TypeID,
			Material:   true,
		})
	}
	return intent.CompileComposition(plan)
}

func promotionSimulationPlan(result promotion.SimulationResult, call promotionCall) (intent.PreflightPlan, error) {
	writes := make([]intent.IntendedWrite, 0)
	for _, change := range result.Projected.Changes {
		if change.Changed {
			writes = append(writes, intent.IntendedWrite{Target: "assignment." + change.Field, Effect: "UPDATE"})
		}
	}
	if result.CompensationState == promotion.CompensationEvaluated {
		writes = append(writes, intent.IntendedWrite{Target: "compensation.base_amount", Effect: "UPDATE"})
	}
	units := int64(0)
	costBasis := "promotion simulation result " + result.ResultDigest + "; cost unavailable"
	if result.CompensationState == promotion.CompensationEvaluated {
		units = minorUnits(result.Compensation.Delta.AnnualizedBase)
		costBasis = moneyCostBasis("annualized compensation delta", result.Compensation.Delta.AnnualizedBase)
	}
	return compileDispatchPreflight(
		intent.Ref{TypeID: promotion.IntentType, Version: 1}, result.ResultDigest, result.InputsDigest, units,
		costBasis,
		intent.RiskAssessment{Level: intent.RiskHigh, Factors: []string{"promotion simulation", result.Preflight.RulePackVersion}},
		writes, nil, nil, "promotion_execution_revalidation/v1",
	)
}

func compensationPreflightPlan(result rewards.SimulateCompensationResult) (intent.PreflightPlan, error) {
	return compileDispatchPreflight(
		intent.Ref{TypeID: result.IntentType, Version: 1}, result.ResultDigest, result.InputsDigest, 0,
		moneyCostBasis("annualized compensation delta", result.Delta.AnnualizedBase),
		intent.RiskAssessment{Level: intent.RiskMedium, Factors: []string{result.RulePackVersion, result.AnnualizationVersion}},
		nil, nil, nil, "recalculate when compensation inputs change/v1",
	)
}

func minorUnits(amount values.Money) int64 {
	text := amount.Amount().String()
	if strings.HasPrefix(text, "-") {
		return 0
	}
	text = strings.ReplaceAll(text, ".", "")
	units, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0
	}
	return units
}

func moneyCostBasis(label string, amount values.Money) string {
	return label + "; currency=" + amount.Currency() + "; decimal_scale=" + strconv.Itoa(int(amount.Amount().Scale()))
}

func repairPreflightPlan(req repair.SimulateRepairRequest, result repair.RepairSimulation) (intent.PreflightPlan, error) {
	writes := make([]intent.IntendedWrite, 0)
	for _, step := range req.Plan.Steps {
		for _, target := range step.WriteSet {
			writes = append(writes, intent.IntendedWrite{Target: target, Effect: string(step.Action)})
		}
	}
	approvals := make([]intent.ApprovalTask, 0, len(req.Plan.ApprovalRoles))
	for _, role := range req.Plan.ApprovalRoles {
		approvals = append(approvals, intent.ApprovalTask{Kind: intent.KindApproval, Ref: role})
	}
	return compileDispatchPreflight(
		intent.Ref{TypeID: result.IntentType, Version: 1}, result.PlanDigest, result.InputsDigest, int64(result.Cost.Steps),
		fmt.Sprintf("repair simulation: %d steps, %d writes", result.Cost.Steps, result.Cost.LocalWrites+result.Cost.ExternalWrites),
		intent.RiskAssessment{Level: intent.RiskLevel(req.Plan.Risk.String()), Factors: []string{result.RulePackVersion, result.StatusReason}},
		writes, approvals, result.Caveats, "replan if repair observations change/v1",
	)
}
