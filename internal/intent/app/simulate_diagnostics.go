package app

import (
	"context"
	"fmt"

	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/dataops"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/intelligence"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/repair"
	"github.com/monstercameron/human-capital-management-suite/internal/intent"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// simulateDrift answers a detect_drift intent.
func (s *IntentService) simulateDrift(
	ctx context.Context,
	principal *trust.Principal,
	purpose string,
	inst intent.Instance,
	def intent.Definition,
	call DomainCall,
) (*intentsv1.SimulationArtifact, *envelope.Error) {
	kernelResult, err := intent.Preflight(ctx, intent.PreflightRequest{
		Instance: inst, Definition: def, Baseline: call.Baseline,
	}, s.defs, nil)
	if err != nil {
		return nil, kernelRejection(err)
	}
	answer, _, ownedErr := s.invoke(ctx, principal, purpose, capabilityKeyFor(def.Ref), *call.Drift)
	if ownedErr != nil {
		return nil, ownedErr
	}
	report, ok := answer.(dataops.DriftReport)
	if !ok {
		return nil, unexpectedAnswer(dataops.DetectDriftIntentType, answer)
	}
	return &intentsv1.SimulationArtifact{
		IntentId:          inst.IntentID,
		Findings:          append(findingsProto(kernelResult, promotion.PreflightResult{}), driftFindings(report)...),
		Uncertainty:       driftUncertainty(report),
		ZeroEffectReceipt: receiptProto(report.Receipt, report.Effects),
	}, nil
}

// simulateRepair answers a create_repair_plan or a simulate_repair intent.
//
// Both go through the create_repair_plan capability first, because a
// simulation is about a plan and the plan is that capability's answer. Only
// simulate_repair goes on to the second invocation; create_repair_plan stops
// with the recommendation, which is the whole of its contract in this release.
func (s *IntentService) simulateRepair(
	ctx context.Context,
	principal *trust.Principal,
	purpose string,
	inst intent.Instance,
	def intent.Definition,
	call DomainCall,
) (*intentsv1.SimulationArtifact, *envelope.Error) {
	kernelResult, err := intent.Preflight(ctx, intent.PreflightRequest{
		Instance: inst, Definition: def, Baseline: call.Baseline,
	}, s.defs, nil)
	if err != nil {
		return nil, kernelRejection(err)
	}

	planKey := capabilityKeyFor(intent.Ref{TypeID: repair.CreateRepairPlanIntentType, Version: 1})
	answer, _, ownedErr := s.invoke(ctx, principal, purpose, planKey, *call.Repair)
	if ownedErr != nil {
		return nil, ownedErr
	}
	planned, ok := answer.(repairPlanAnswer)
	if !ok {
		return nil, unexpectedAnswer(repair.CreateRepairPlanIntentType, answer)
	}

	if def.Ref.TypeID == repair.CreateRepairPlanIntentType {
		return &intentsv1.SimulationArtifact{
			IntentId:          inst.IntentID,
			PlannedEffects:    repairStepsProto(planned.Plan),
			Findings:          append(findingsProto(kernelResult, promotion.PreflightResult{}), planFindings(planned)...),
			Uncertainty:       planUncertainty(planned),
			ZeroEffectReceipt: receiptProto(planned.Plan.Receipt, planned.Plan.Effects),
		}, nil
	}

	in := *call.Repair
	simulated, _, ownedErr := s.invoke(ctx, principal, purpose, capabilityKeyFor(def.Ref),
		repair.SimulateRepairRequest{
			Tenant:                 in.Tenant,
			Plan:                   planned.Plan,
			CurrentDiff:            planned.Diff,
			Canonical:              planned.Canonical,
			Observed:               planned.Observed,
			ObservedPresent:        planned.ObservedPresent,
			Observation:            planned.Observation,
			Fields:                 in.Fields,
			Authorization:          in.Authorization,
			Freshness:              in.Freshness,
			EvaluatedAt:            in.EvaluatedAt,
			AuthorityPolicyVersion: in.AuthorityPolicyVersion,
			LocalSystem:            in.LocalSystem,
			ExternalSystem:         in.ExternalSystem,
		})
	if ownedErr != nil {
		return nil, ownedErr
	}
	plannedSimulation, ok := simulated.(repairSimulationAnswer)
	if !ok {
		return nil, unexpectedAnswer(repair.SimulateRepairIntentType, simulated)
	}
	result := plannedSimulation.Simulation
	return &intentsv1.SimulationArtifact{
		IntentId:          inst.IntentID,
		PlannedEffects:    repairStepsProto(planned.Plan),
		Findings:          append(findingsProto(kernelResult, promotion.PreflightResult{}), simulationFindings(result)...),
		Uncertainty:       simulationUncertainty(result),
		ZeroEffectReceipt: receiptProto(result.Receipt, result.Effects),
	}, nil
}

// simulateTransaction answers an explain_transaction intent.
func (s *IntentService) simulateTransaction(
	ctx context.Context,
	principal *trust.Principal,
	purpose string,
	inst intent.Instance,
	def intent.Definition,
	call DomainCall,
) (*intentsv1.SimulationArtifact, *envelope.Error) {
	kernelResult, err := intent.Preflight(ctx, intent.PreflightRequest{
		Instance: inst, Definition: def, Baseline: call.Baseline,
	}, s.defs, nil)
	if err != nil {
		return nil, kernelRejection(err)
	}
	answer, _, ownedErr := s.invoke(ctx, principal, purpose, capabilityKeyFor(def.Ref), *call.Transaction)
	if ownedErr != nil {
		return nil, ownedErr
	}
	explanation, ok := answer.(intelligence.Explanation)
	if !ok {
		return nil, unexpectedAnswer(intelligence.ExplainTransactionIntentType, answer)
	}
	return &intentsv1.SimulationArtifact{
		IntentId:          inst.IntentID,
		Findings:          append(findingsProto(kernelResult, promotion.PreflightResult{}), explanationFindings(explanation)...),
		Uncertainty:       explanationUncertainty(explanation),
		ZeroEffectReceipt: receiptProto(explanation.Receipt, explanation.EffectCounters),
	}, nil
}

// unexpectedAnswer is the one refusal for a capability that answered with a
// type its contract does not declare. It is this cell's fault, not the
// caller's, and it says so.
func unexpectedAnswer(capabilityID string, answer any) *envelope.Error {
	return envelope.New(envelope.CodeUnavailable, reasonDomainUnavailable,
		"the operation could not be completed").
		WithDiagnostic(fmt.Errorf("app: %s returned %T", capabilityID, answer))
}

// ---------------------------------------------------------------------------
// Projection onto the wire artifact
// ---------------------------------------------------------------------------

// driftFindings reports one finding per examined subject, carrying the
// comparison's own verdict counts. The per-field values never travel: a
// drift finding names what disagreed, not what the two sides said.
func driftFindings(report dataops.DriftReport) []*intentsv1.Finding {
	out := make([]*intentsv1.Finding, 0, len(report.Subjects)+1)
	out = append(out, &intentsv1.Finding{
		Code: "DRIFT_POPULATION_EXAMINED",
		Message: fmt.Sprintf("examined %d of %d requested subject(s) over %d field(s) against %s",
			report.PopulationExamined, report.PopulationRequested, len(report.Fields), report.Source),
		Severity: report.Disclosure.String(),
	})
	for _, subject := range report.Subjects {
		out = append(out, &intentsv1.Finding{
			Code: "DRIFT_SUBJECT_COMPARED",
			Message: fmt.Sprintf("%s: %d match, %d mismatch, %d unknown, %d stale, %d not applicable",
				subject.Subject, subject.Diff.Verdicts.Match, subject.Diff.Verdicts.Mismatch,
				subject.Diff.Verdicts.Unknown, subject.Diff.Verdicts.Stale,
				subject.Diff.Verdicts.NotApplicable),
			Severity: verdictSeverity(subject.Diff.Verdicts.Mismatch),
		})
	}
	return out
}

// verdictSeverity renders a comparison outcome as the severity token the wire
// finding carries.
func verdictSeverity(mismatches int) string {
	if mismatches > 0 {
		return "MISMATCH"
	}
	return "MATCH"
}

// driftUncertainty reports what the run could not settle: the truncation it
// applied, and the provenance of every observation page it read.
func driftUncertainty(report dataops.DriftReport) []*intentsv1.UncertaintyNote {
	out := make([]*intentsv1.UncertaintyNote, 0, len(report.Watermarks)+1)
	if report.Truncated {
		out = append(out, &intentsv1.UncertaintyNote{
			Dimension: "population",
			Description: fmt.Sprintf("the run stopped at its ceiling of %d subject(s); %d were requested",
				report.PopulationCeiling, report.PopulationRequested),
		})
	}
	for _, w := range report.Watermarks {
		out = append(out, &intentsv1.UncertaintyNote{
			Dimension: "observation_watermark",
			Description: fmt.Sprintf("%s under %s retrieved %s carrying %d record(s), digest %s",
				w.Source, w.SchemaVersion, w.RetrievedAt.Instant().Time().UTC().Format("2006-01-02T15:04:05Z07:00"),
				w.Records, w.Digest),
		})
	}
	return out
}

// repairStepsProto reports what the plan says would happen if somebody were
// authorized to execute it. Nothing here is queued: the plan is marked
// non-executable by the repair package, and this renders that plan as the
// caller-visible "what would happen" list.
func repairStepsProto(plan repair.RepairPlan) []*intentsv1.PlannedEffect {
	out := make([]*intentsv1.PlannedEffect, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		out = append(out, &intentsv1.PlannedEffect{
			EffectKind: step.Action.String(),
			TargetRef:  step.Target.Subject.String() + "#" + string(step.Target.Field),
			Description: fmt.Sprintf("step %d on %s, risk %s, %s (plan %s is %s)",
				step.Ordinal, step.Target.System, step.Risk, step.Reason,
				plan.ID, plan.NotExecutableReason),
		})
	}
	return out
}

func planFindings(planned repairPlanAnswer) []*intentsv1.Finding {
	out := []*intentsv1.Finding{{
		Code: "REPAIR_DIAGNOSIS",
		Message: fmt.Sprintf("%s at %s confidence over comparison %s: %d actionable, %d blocked, %d unknown",
			planned.Diagnosis.Problem, planned.Diagnosis.Confidence, planned.Diff.Digest,
			len(planned.Diagnosis.Actionable), len(planned.Diagnosis.Blocked),
			len(planned.Diagnosis.Unknowns)),
		Severity: planned.Diagnosis.Problem.String(),
	}}
	for _, skipped := range planned.Plan.SkippedFields {
		out = append(out, &intentsv1.Finding{
			Code:     "REPAIR_FIELD_SKIPPED",
			Message:  string(skipped.Field) + ": " + skipped.Reason,
			Severity: "SKIPPED",
		})
	}
	return out
}

func planUncertainty(planned repairPlanAnswer) []*intentsv1.UncertaintyNote {
	out := make([]*intentsv1.UncertaintyNote, 0, len(planned.Diagnosis.Unknowns)+1)
	out = append(out, &intentsv1.UncertaintyNote{
		Dimension:   "executability",
		Description: planned.Plan.NotExecutableReason,
	})
	for _, unknown := range planned.Diagnosis.Unknowns {
		out = append(out, &intentsv1.UncertaintyNote{Dimension: "diagnosis", Description: unknown})
	}
	return out
}

func simulationFindings(result repair.RepairSimulation) []*intentsv1.Finding {
	out := []*intentsv1.Finding{{
		Code: "REPAIR_SIMULATION_STATUS",
		Message: fmt.Sprintf("plan %s is %s (%s); %d residual finding(s) would remain",
			result.PlanID, result.Status, result.StatusReason, len(result.ResidualFindings)),
		Severity: result.Status.String(),
	}}
	for _, risk := range result.Risks {
		out = append(out, &intentsv1.Finding{
			Code:     "REPAIR_SIMULATION_RISK",
			Message:  string(risk.Field) + ": " + risk.Token,
			Severity: risk.Class.String(),
		})
	}
	return out
}

func simulationUncertainty(result repair.RepairSimulation) []*intentsv1.UncertaintyNote {
	out := make([]*intentsv1.UncertaintyNote, 0, len(result.Caveats))
	for _, caveat := range result.Caveats {
		out = append(out, &intentsv1.UncertaintyNote{Dimension: "projection", Description: caveat})
	}
	return out
}

func explanationFindings(e intelligence.Explanation) []*intentsv1.Finding {
	out := make([]*intentsv1.Finding, 0, len(e.Sections)+1)
	out = append(out, &intentsv1.Finding{
		Code: "TRANSACTION_EXPLAINED",
		Message: fmt.Sprintf("%s, %d chronology entr(ies) over %d section(s); complete=%t",
			e.Presence, len(e.Chronology), len(e.Sections), e.Completeness.Complete),
		Severity: e.Disclosure.String(),
	})
	for _, section := range e.Sections {
		if section.Access == intelligence.AccessAuthorized {
			continue
		}
		out = append(out, &intentsv1.Finding{
			Code:     "TRANSACTION_SECTION_WITHHELD",
			Message:  section.Section.String() + ": " + section.DenialReason,
			Severity: section.Access.String(),
		})
	}
	return out
}

func explanationUncertainty(e intelligence.Explanation) []*intentsv1.UncertaintyNote {
	out := make([]*intentsv1.UncertaintyNote, 0, len(e.Completeness.Gaps)+1)
	for _, gap := range e.Completeness.Gaps {
		out = append(out, &intentsv1.UncertaintyNote{
			Dimension:   "section:" + gap.Section.String(),
			Description: gap.Reason,
		})
	}
	out = append(out, &intentsv1.UncertaintyNote{
		Dimension:   "evidence_precedence",
		Description: e.EvidencePrecedence,
	})
	return out
}
