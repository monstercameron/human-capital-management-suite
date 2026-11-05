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

// BenefitsReadState is the server-composed result state for an authorized
// benefits read. The zero value means the governed benefits service has not
// published to this UI yet, which the pages render as unavailable.
type BenefitsReadState string

const (
	BenefitsReadReady    BenefitsReadState = "ready"
	BenefitsReadNoRecord BenefitsReadState = "no_record"
	BenefitsReadError    BenefitsReadState = "error"
)

// BenefitsProjection is the request-scoped, server-authorized benefits read.
// Options and Recon carry version-pinned domain facts that the pages
// evaluate with ResolveBenefitOptions and ResolveBenefitReconciliation; the
// UI never discovers plans or manufactures eligibility. Elect is the
// server-composed election adapter; nil renders enrollment read-only.
type BenefitsProjection struct {
	State   BenefitsReadState
	Options []BenefitOptionInput
	Recon   []BenefitReconInput
	Elect   func(BenefitElectionRequest, func(error))
}

// BenefitElectionRequest is the worker's election choice. It deliberately
// contains no tenant, principal, worker, rate, cost, dependent-roster, or
// head-revision fields: the authenticated election service derives them, so
// the UI can neither widen an election nor invent an identity.
type BenefitElectionRequest struct {
	PlanRef        string
	PlanRevision   string
	WindowID       string
	Tier           benefits.TierCode
	Waiver         bool
	WaiverEvidence string
}

// AuthorizedElectionContext is the server-derived remainder of a BEN-005
// election: identity, rate, exact costs, effective date, the authorized
// dependent roster, and the observed head. Identity, rate, costs, and date
// are always required; PriorDigest is empty only for a genesis revision,
// which RecordElection itself admits or refuses.
type AuthorizedElectionContext struct {
	Tenant        string
	WorkerRef     string
	RateVersion   string
	EmployeeCost  values.Decimal
	EmployerCost  values.Decimal
	EffectiveDate time.Time
	Dependents    []string
	PriorDigest   string
	Supersedes    string
}

// ToDomainInput binds the worker's choice to the server-authorized context
// as one BEN-005 ElectionInput. Unknown tiers and missing worker choices
// fail here; RecordElection enforces the window, rate, head, and replay
// rules on the mapped input.
func (r BenefitElectionRequest) ToDomainInput(ctx AuthorizedElectionContext) (benefits.ElectionInput, error) {
	if strings.TrimSpace(r.PlanRef) == "" || strings.TrimSpace(r.PlanRevision) == "" || strings.TrimSpace(r.WindowID) == "" {
		return benefits.ElectionInput{}, fmt.Errorf("benefits election: plan, plan revision, and window are required")
	}
	switch r.Tier {
	case benefits.TierEmployeeOnly, benefits.TierEmployeeSpouse, benefits.TierEmployeeChildren, benefits.TierFamily:
	default:
		return benefits.ElectionInput{}, fmt.Errorf("benefits election: unknown tier %q", r.Tier)
	}
	if r.Waiver && strings.TrimSpace(r.WaiverEvidence) == "" {
		return benefits.ElectionInput{}, fmt.Errorf("benefits election: a waiver requires waiver evidence")
	}
	if strings.TrimSpace(ctx.Tenant) == "" || strings.TrimSpace(ctx.WorkerRef) == "" || strings.TrimSpace(ctx.RateVersion) == "" {
		return benefits.ElectionInput{}, fmt.Errorf("benefits election: tenant, worker, and rate version are server-derived and required")
	}
	if err := ctx.EmployeeCost.Validate(); err != nil {
		return benefits.ElectionInput{}, fmt.Errorf("benefits election: employee cost: %v", err)
	}
	if err := ctx.EmployerCost.Validate(); err != nil {
		return benefits.ElectionInput{}, fmt.Errorf("benefits election: employer cost: %v", err)
	}
	if ctx.EffectiveDate.IsZero() {
		return benefits.ElectionInput{}, fmt.Errorf("benefits election: effective date is server-derived and required")
	}
	return benefits.ElectionInput{
		Tenant: ctx.Tenant, WorkerRef: ctx.WorkerRef,
		PlanRef: r.PlanRef, PlanRevision: r.PlanRevision, RateVersion: ctx.RateVersion,
		WindowID: r.WindowID, Tier: r.Tier, Dependents: append([]string(nil), ctx.Dependents...),
		EffectiveDate: ctx.EffectiveDate, EmployeeCost: ctx.EmployeeCost, EmployerCost: ctx.EmployerCost,
		Waiver: r.Waiver, WaiverEvidence: strings.TrimSpace(r.WaiverEvidence),
		PriorDigest: ctx.PriorDigest, Supersedes: ctx.Supersedes,
	}, nil
}

// submitBenefitElection validates the worker's choice against the resolved
// options before it reaches the server adapter: the plan must be present
// and electable, the window must be the resolved open window, and the tier
// must be domain vocabulary. Anything else is refused without calling Elect.
func submitBenefitElection(projection BenefitsProjection, options []BenefitOptionView, request BenefitElectionRequest, done func(error)) bool {
	if projection.Elect == nil || done == nil {
		return false
	}
	if request.Waiver && strings.TrimSpace(request.WaiverEvidence) == "" {
		return false
	}
	matched := false
	for _, option := range options {
		if !option.CanElect || option.PlanRef != request.PlanRef || option.PlanRevision != request.PlanRevision || option.Window.WindowID != request.WindowID {
			continue
		}
		matched = true
		break
	}
	if !matched || !validBenefitTier(request.Tier) {
		return false
	}
	projection.Elect(request, done)
	return true
}

func validBenefitTier(tier benefits.TierCode) bool {
	switch tier {
	case benefits.TierEmployeeOnly, benefits.TierEmployeeSpouse, benefits.TierEmployeeChildren, benefits.TierFamily:
		return true
	}
	return false
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
