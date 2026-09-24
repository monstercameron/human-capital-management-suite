package simulate

import (
	"fmt"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/rules"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/transformation/ir"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/rulepayload"
)

// PromotionSetup is a ready-to-run simulation of the promote-into-management
// reference workflow: the compiled plan, the declared inputs and the wired
// zero-effect environment behind every port.
//
// It exists so the golden is one call rather than fifty lines of assembly
// repeated in every test and every tool, and so there is exactly one place
// where the reference scenario's numbers live.
type PromotionSetup struct {
	Plan    *workflow.CompiledWorkflow
	Env     *Environment
	Inputs  Inputs
	Options Options
}

// Reference proposals for the raise-threshold decision. Jane's pinned baseline
// is 165,000.00 USD annual, so:
const (
	// PromotionWithinThresholdPay is a 9.09% raise, under the reference
	// configuration's 10% finance threshold.
	PromotionWithinThresholdPay = "180000.00"
	// PromotionExceedsThresholdPay is a 15.15% raise, over it.
	PromotionExceedsThresholdPay = "190000.00"
)

// NewPromotionSetup wires the reference run: Jane Doe promoted out of
// ENG-SWE3/P3 into the ENG-MGR1/M1 management band, effective 2026-10-01.
//
// This is a real management promotion, so it carries a grade change, and the
// reference threshold table escalates any grade change to FINANCE_REQUIRED.
// The walk therefore ends at the terminal that reports a pending Finance
// Partner approval - which is exactly what
// planning/reference-workflows/promote-into-management.md describes, and why
// P1A declares that approval as an outstanding obligation rather than
// executing an APPROVAL node that does not exist yet.
func NewPromotionSetup(proposedBasePay string) (*PromotionSetup, error) {
	env, err := NewPromotionEnvironment()
	if err != nil {
		return nil, err
	}
	return newSetup(env, proposedBasePay, true)
}

// NewInGradeSetup wires the same plan around an in-grade increase: Jane stays
// in ENG-SWE3/P3 and only her pay moves, under a policy version that permits a
// same-grade change.
//
// It exists because the management promotion always changes grade, and a grade
// change always escalates. Without this variant the plan's WITHIN_THRESHOLD
// branch - and with it the drift observation and the consistent terminal -
// would never be walked by anything, which would make an OBSERVE
// implementation nobody had executed.
func NewInGradeSetup(proposedBasePay string) (*PromotionSetup, error) {
	env, err := NewPromotionEnvironment()
	if err != nil {
		return nil, err
	}
	// PositionID is deliberately absent: PROMOUX-004 checks a non-empty
	// value against the real Position domain, and this reference workflow
	// scenario wires no PositionReader (it exercises workflow simulation
	// mechanics, not position selection). JobCode/Grade/OrgUnit alone
	// already satisfy checkPlacement's placement requirement.
	env.Target = promotion.TargetPlacement{
		JobCode: "ENG-SWE3",
		Grade:   "P3",
		OrgUnit: "eng-platform",
		PayZone: "US-WEST",
	}
	env.Policy.Version = "people.promotion.policy.in_grade/1.0.0"
	env.Policy.AllowSameGrade = true
	env.BusinessReason = "In-grade market adjustment for a senior engineer on Team Phoenix"
	return newSetup(env, proposedBasePay, false)
}

// newSetup compiles the reference workflow against the environment's own
// capability registry and wires every port.
func newSetup(env *Environment, proposedBasePay string, gradeChange bool) (*PromotionSetup, error) {
	registry, err := env.Registry()
	if err != nil {
		return nil, err
	}
	ruleStore := rulepayload.New()
	thresholdTable := rules.PromotionWorkflowThresholdTable()
	ruleRef := workflow.Reference{Kind: workflow.RefRule, ID: PromotionThresholdRuleRef, Version: "v3"}
	rulePin, err := ruleStore.Publish(rulepayload.Payload{
		Ref: ruleRef, BodyRef: &workflow.VersionedRef{ID: thresholdTable.ID, Version: thresholdTable.Version},
		Kind: rulepayload.KindDecisionTable, Table: &thresholdTable,
	})
	if err != nil {
		return nil, fmt.Errorf("simulate: publish promotion threshold: %w", err)
	}
	transformProgram := promotionDecisionFactsProgram()
	transformRef := workflow.Reference{Kind: workflow.RefTransform, ID: workflow.PromotionDecisionFactsIRID, Version: workflow.PromotionDecisionFactsIRVer}
	if _, err := ruleStore.Publish(rulepayload.Payload{Ref: transformRef, Kind: rulepayload.KindTransform, Transform: &transformProgram}); err != nil {
		return nil, fmt.Errorf("simulate: publish promotion decision-facts transform: %w", err)
	}
	plan, err := workflow.CompilePromotionReferenceWithRules(registry, ruleStore)
	if err != nil {
		return nil, fmt.Errorf("simulate: promotion reference must compile: %w", err)
	}
	base, err := fixtures.Money(proposedBasePay, "USD")
	if err != nil {
		return nil, fmt.Errorf("simulate: proposed base pay: %w", err)
	}

	// The grade change is stated rather than inferred inside a rule: whether a
	// move is a grade change is a fact about the proposal, and the threshold
	// table's job is to price it, not to work out what it is.
	decisionNode, ok := plan.Node(workflow.PromotionNodeRaiseThreshold)
	if !ok || decisionNode.Decision == nil || decisionNode.Decision.Rule == nil || decisionNode.Decision.Rule.Digest != rulePin.Digest {
		return nil, fmt.Errorf("simulate: compiled promotion plan did not retain its exact threshold rule pin")
	}
	decisions := RulesDecisions{Payloads: ruleStore, PromotionRule: decisionNode.Decision.Rule}

	return &PromotionSetup{
		Plan: plan,
		Env:  env,
		Inputs: Inputs{
			Values: Bag{
				"worker_id":         NewBranded("WorkerID", env.Worker.Id),
				"target_job_id":     NewBranded("JobID", env.Target.JobCode),
				"proposed_base_pay": NewMoney(base),
				"effective_date":    NewLocalDate(env.EffectiveDate),
				"budget_authority":  NewString(string(env.budgetAuthority())),
				"grade_change":      NewBool(gradeChange),
			},
			Context: map[string]Bag{
				// The snapshot node declares a pinned LegalContext requirement
				// whose declared missing behavior is UNKNOWN. Supplying it is
				// what lets the walk proceed past the snapshot rather than
				// routing to the unknown terminal.
				"LegalContext": {
					"jurisdiction":             NewString("US-CA"),
					"applicable_rule_versions": NewString("legal.promotion.us-ca/2026.1"),
				},
			},
		},
		Options: Options{
			Capabilities: registry,
			SubjectRef:   "principal:hr-partner-7",
			Decisions:    decisions,
			RulePayloads: ruleStore,
			Transforms:   PromotionTransforms{Env: env, Payloads: ruleStore},
			Reads:        PromotionReads(env),
			Approvals:    HumanWorkApprovals{Decisions: decisions, ProposalNodeID: workflow.PromotionNodeBuildProposal},
			Controls: []ControlVersion{
				{Name: "hcmnext.domains.fixtures.corpus", Version: string(fixtures.Tenant)},
				{Name: "hcmnext.domains.promotion.policy", Version: env.Policy.Version},
				{Name: "hcmnext.domains.rewards.annualization", Version: env.Annualization.Version},
			},
		},
	}, nil
}

func promotionDecisionFactsProgram() ir.Program {
	return ir.Program{
		IRVersion: ir.IRVersion, DefinitionName: "promotion.project_decision_facts",
		Instructions: []ir.Instruction{
			{Op: ir.OpProject, Sources: []transformation.Path{{Schema: "workflow", Field: FieldRaiseRatio, Type: transformation.TypeDecimal}}, Destination: transformation.Path{Schema: "workflow", Field: FieldRaiseRatio, Type: transformation.TypeDecimal}},
			{Op: ir.OpProject, Sources: []transformation.Path{{Schema: "workflow", Field: FieldBandPosition, Type: transformation.TypeString}}, Destination: transformation.Path{Schema: "workflow", Field: FieldBandPosition, Type: transformation.TypeString}},
		},
		Dependencies: []string{"workflow.band_position", "workflow.raise_ratio"},
		Limits:       ir.Limits{MaxSteps: 2, MaxFanOut: 1},
	}
}

// ProposedBasePay returns the money value the setup was built with.
func (s *PromotionSetup) ProposedBasePay() (values.Money, error) {
	v, err := s.Inputs.Values.Get("proposed_base_pay")
	if err != nil {
		return values.Money{}, err
	}
	return v.Money()
}
