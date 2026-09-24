package productui

import (
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/benefits"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// BenefitOptionInput is the server-authorized, version-pinned source for one
// plan option. Productui evaluates the supplied domain facts; it does not
// discover plans or manufacture worker eligibility.
type BenefitOptionInput struct {
	Revision         benefits.PlanRevision
	Worker           benefits.WorkerFacts
	EligibilityRules []benefits.EligibilityRule
	EnrollmentSet    benefits.EnrollmentWindowSet
	WindowKind       benefits.WindowKind
	ElectionDate     values.LocalDate
	EventDate        values.LocalDate
}

// BenefitOptionView is the safe worker-facing projection for a plan. The
// opaque rule payloads and carrier/sponsor references are intentionally
// omitted; elections are enabled only when both domain decisions are exact.
type BenefitOptionView struct {
	PlanRef       string
	PlanName      string
	PlanRevision  string
	PlanYear      int32
	Options       []string
	CoverageTiers []string
	Eligibility   benefits.EligibilityEvaluation
	Window        benefits.EnrollmentWindowResolution
	CanElect      bool
}

// ResolveBenefitOptions evaluates eligibility and the versioned enrollment
// window for each server-authorized plan. Any malformed source record is a
// read failure for the projection; callers should show their existing
// unavailable state instead of a partial or invented catalogue.
func ResolveBenefitOptions(inputs []BenefitOptionInput) ([]BenefitOptionView, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	out := make([]BenefitOptionView, 0, len(inputs))
	seen := make(map[string]struct{}, len(inputs))
	for i, input := range inputs {
		revision := input.Revision
		if err := benefits.ValidatePlanRevision(revision); err != nil {
			return nil, fmt.Errorf("benefits option %d revision: %w", i, err)
		}
		planRef := revision.PlanID.String()
		if _, duplicate := seen[planRef]; duplicate {
			return nil, fmt.Errorf("benefits option %d repeats plan %q", i, planRef)
		}
		seen[planRef] = struct{}{}
		facts := input.Worker
		if facts.Tenant != revision.PlanID.Tenant.String() || strings.TrimSpace(facts.PlanRef) == "" {
			return nil, fmt.Errorf("benefits option %d worker facts do not match plan tenant", i)
		}
		eligibility, err := benefits.EvaluateEligibility(facts, input.EligibilityRules)
		if err != nil {
			return nil, fmt.Errorf("benefits option %d eligibility: %w", i, err)
		}
		window, err := benefits.ResolveEnrollmentWindow(benefits.EnrollmentWindowRequest{
			Set: input.EnrollmentSet, PlanID: revision.PlanID, PlanRevision: revision.Revision.String(),
			Kind: input.WindowKind, ElectionDate: input.ElectionDate, EventDate: input.EventDate,
		})
		if err != nil {
			return nil, fmt.Errorf("benefits option %d enrollment window: %w", i, err)
		}
		out = append(out, BenefitOptionView{
			PlanRef: planRef, PlanName: revision.Name, PlanRevision: revision.Revision.String(),
			PlanYear: revision.PlanYear.Year, Options: append([]string(nil), revision.Options...),
			CoverageTiers: append([]string(nil), revision.CoverageTiers...),
			Eligibility:   eligibility, Window: window,
			CanElect: eligibility.Status == benefits.StatusEligible && window.Status == benefits.WindowOpen,
		})
	}
	return out, nil
}

// BenefitReconInput contains the expectation and provider observations that
// the authorized reconciliation read returned for one worker election.
type BenefitReconInput struct {
	Expectation benefits.CoverageExpectation
	Carrier     benefits.CarrierObservation
	Payroll     benefits.PayrollObservation
	AsOf        time.Time
}

// BenefitReconView carries only the real BEN-008 outcome and scoped repair
// details. Provider payloads are never rendered by the product layer.
type BenefitReconView struct {
	Outcome benefits.ReconOutcome
	Fields  []string
	Repair  *benefits.RepairCase
	Digest  string
}

// ResolveBenefitReconciliation delegates comparison and repair classification
// to BEN-008 for every authorized reconciliation record.
func ResolveBenefitReconciliation(inputs []BenefitReconInput) ([]BenefitReconView, error) {
	out := make([]BenefitReconView, 0, len(inputs))
	for i, input := range inputs {
		result, err := benefits.ReconcileCoverage(input.Expectation, input.Carrier, input.Payroll, input.AsOf)
		if err != nil {
			return nil, fmt.Errorf("benefits reconciliation %d: %w", i, err)
		}
		view := BenefitReconView{Outcome: result.Outcome, Fields: append([]string(nil), result.Fields...), Digest: result.Digest}
		if result.Repair != nil {
			repair := *result.Repair
			repair.Fields = append([]string(nil), result.Repair.Fields...)
			view.Repair = &repair
		}
		out = append(out, view)
	}
	return out, nil
}
