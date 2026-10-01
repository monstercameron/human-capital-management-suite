package workspace

import (
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/fixtures"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/people"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/promotion"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/rewards"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/forms"
)

// ErrQueryInvalid is returned when a [Query] cannot be turned into the typed
// domain request the governed capabilities consume. It is a caller mistake,
// not a cell fault, and the handler projects it as 400.
var ErrQueryInvalid = errors.New("workspace: the promotion query is not answerable as stated")

// Field identifiers the workspace renders and the form submits.
//
// The five editable ones are exactly tools/uxqual/forms.RequiredFieldIDs, so
// a value read off this workspace's rendered form and a value handed straight
// to the governed capability call are the same typed shape by construction -
// that equality is FORM-004's "equivalent governed route", and this package
// reuses it rather than inventing a second field vocabulary.
const (
	FieldWorkerName           = "workerName"
	FieldCurrentJobTitle      = "currentJobTitle"
	FieldCurrentGrade         = "currentGrade"
	FieldCurrentBasePay       = "currentBasePay"
	FieldProposedJobTitle     = "proposedJobTitle"
	FieldProposedGrade        = "proposedGrade"
	FieldTargetPosition       = "targetPosition"
	FieldTargetOrgUnit        = "targetOrgUnit"
	FieldProposedComp         = "proposedCompensation"
	FieldEffectiveDate        = "effectiveDate"
	FieldBusinessReason       = "businessReason"
	FieldBandPosition         = "payBandPosition"
	FieldAnnualizedIncrease   = "annualizedIncrease"
	FieldCompensationWithheld = "compensationDisclosure"
)

// Query is the workspace question in the vocabulary a URL and an HTML form
// speak: strings. It is deliberately separate from [Request], which is the
// typed domain question, so that parsing failures are reported to the person
// who typed them rather than surfacing as a domain refusal.
type Query struct {
	// WorkerRef is the corpus key or entity id of the worker the promotion
	// is about.
	WorkerRef string

	// CurrentBase and ProposedBase are decimal text at the corpus money
	// contract; Currency applies to both. The current side is a declared
	// input rather than a governed read because P1A masters no compensation
	// projection: internal/domains/rewards is handed the baseline, it does
	// not fetch one.
	CurrentBase  string
	ProposedBase string
	Currency     string
	// BonusTargetPercent is the bonus target as a fraction, carried on both
	// snapshots so the annualized total-cash delta is computable.
	BonusTargetPercent string

	TargetJobCode    string
	TargetGrade      string
	TargetOrgUnit    string
	TargetPositionID string
	TargetPayZone    string

	EffectiveDate  string
	EvaluationDate string
	BusinessReason string

	BudgetAvailable string
}

// DefaultQuery returns the corpus's own declared promotion scenario: the
// worker, target placement, amounts, dates, business reason and budget that
// internal/domains/fixtures ports from the legacy compensation corpus.
//
// It is a seed, not a fact about whoever the caller asks for. The workspace
// is a form: the seed is what the fields are pre-filled with, and every one
// of them is editable and re-simulated on submit. Seeding from the corpus
// rather than from a hand-written literal here means the prototype's opening
// screen is the same scenario internal/domains/promotion itself certifies.
func DefaultQuery() (Query, error) {
	set, err := fixtures.LegacyScenarios()
	if err != nil {
		return Query{}, fmt.Errorf("workspace: read the promotion corpus: %w", err)
	}
	if len(set.Scenarios) == 0 {
		return Query{}, fmt.Errorf("%w: the promotion corpus declares no scenario", ErrQueryInvalid)
	}
	s := set.Scenarios[0]
	return Query{
		WorkerRef:          set.Worker,
		CurrentBase:        s.CurrentAmount,
		ProposedBase:       s.ProposedAmount,
		Currency:           s.ProposedCurrency,
		BonusTargetPercent: s.BonusTarget,
		TargetJobCode:      set.Target.JobCode,
		TargetGrade:        set.Target.Grade,
		TargetOrgUnit:      defaultOrgUnit,
		// TargetPositionID is deliberately left unset. PROMOUX-004 makes a
		// non-empty value here mean a specific, checkable thing -- a
		// server-issued position.RevisionRef the real Position domain can
		// verify -- and this page has no picker yet to issue one, and this
		// environment has no job_position row backing the legacy corpus
		// scenario (that corpus predates the Position domain). Seeding a
		// fabricated identifier here would either always fail existence
		// (dishonest theater) or require inventing backing data this
		// package has no authority to declare. The promotion this seed
		// describes still runs the full preflight on job/grade/org alone,
		// which internal/domains/promotion.checkPlacement already accepts.
		TargetPayZone:   set.Target.PayZone,
		EffectiveDate:   s.EffectiveAt,
		EvaluationDate:  s.EvaluationAt,
		BusinessReason:  s.BusinessReason,
		BudgetAvailable: set.BudgetAvailable,
	}, nil
}

// defaultOrgUnit is the organizational placement the corpus scenario implies
// but does not state. The legacy corpus carries a job code, a grade and a
// pay zone; the org unit is the P1A promotion context the same scenario runs
// inside (test/bootstrap drives the identical pair).
const defaultOrgUnit = "people-ops"

// revisionStreamPrefix names the compensation revision stream a declared
// baseline snapshot pins. The snapshot is an input, so the revision it cites
// is derived from the worker it is about rather than minted per request: two
// identical submissions must pin the same baseline, or the simulation digest
// would move for a reason that has nothing to do with the proposal.
const revisionStreamPrefix = "rewards.package."

// WithWorker returns a copy of q addressed at worker, when worker is
// non-empty.
func (q Query) WithWorker(worker string) Query {
	if strings.TrimSpace(worker) != "" {
		q.WorkerRef = strings.TrimSpace(worker)
	}
	return q
}

// WithFormAnswers overlays the answers a submitted workspace form carries,
// keyed by the same field ids both renderers emit. Only the fields the form
// actually collects are read: a read-only field the page displays is never
// taken back from the client, because a value the server rendered is not
// evidence when it returns.
func (q Query) WithFormAnswers(answers map[string]string) Query {
	set := func(dst *string, key string) {
		if v, ok := answers[key]; ok && strings.TrimSpace(v) != "" {
			*dst = strings.TrimSpace(v)
		}
	}
	set(&q.TargetJobCode, FieldProposedJobTitle)
	set(&q.TargetGrade, FieldProposedGrade)
	set(&q.ProposedBase, FieldProposedComp)
	set(&q.EffectiveDate, FieldEffectiveDate)
	set(&q.BusinessReason, FieldBusinessReason)
	return q
}

// Intent returns the canonical governed-intent identity of this query under
// tools/uxqual/forms' rules, so the workspace can state - and a test can
// check - that the request a human filled in and the request an API caller
// would have posted are the same intent.
func (q Query) Intent() (forms.IntentInstance, error) {
	return forms.FromCapabilityCall(forms.PromotionRequestInputs{
		WorkerID:             strings.TrimSpace(q.WorkerRef),
		ProposedJobTitle:     q.TargetJobCode,
		ProposedGrade:        q.TargetGrade,
		ProposedCompensation: q.ProposedBase,
		EffectiveDate:        q.EffectiveDate,
		BusinessReason:       q.BusinessReason,
	})
}

// Request is the typed, tenant-free promotion question the workspace asks a
// live cell.
//
// It carries no tenant, no subject and no principal: those are server-derived
// trusted context, and a request shape that could carry them would be a
// request shape a caller could use to select them. The [Cell] implementation
// resolves all three from the admitted credential.
type Request struct {
	// WorkerRef is the corpus key or entity id to resolve.
	WorkerRef string
	// Fields is the governed worker-state projection to read.
	Fields []people.FieldID

	Target         promotion.TargetPlacement
	Current        rewards.CompensationSnapshot
	Proposed       rewards.CompensationSnapshot
	Currency       string
	EffectiveDate  values.LocalDate
	EvaluationDate values.LocalDate
	BusinessReason string
	Budget         *promotion.BudgetAuthorityRef
	Policy         promotion.Policy
	Annualization  rewards.AnnualizationRule

	// TargetManagerSelection is PROMOUX-005's management-promotion
	// intention: the candidate manager and the affected direct-report
	// scope. nil means this request names no target manager -- the
	// job/grade/org-only preflight this page always ran is unaffected. The
	// query/form vocabulary (Query, Typed) does not populate this field yet;
	// a caller that wants it checked builds a Request directly.
	TargetManagerSelection *promotion.TargetManagerSelection
}

// Typed turns the string query into the typed domain request, or reports
// exactly which field could not be read.
func (q Query) Typed() (Request, error) {
	workerRef := strings.TrimSpace(q.WorkerRef)
	if workerRef == "" {
		return Request{}, fmt.Errorf("%w: no worker", ErrQueryInvalid)
	}
	effective, err := values.ParseLocalDate(q.EffectiveDate)
	if err != nil {
		return Request{}, fmt.Errorf("%w: effective date: %w", ErrQueryInvalid, err)
	}
	evaluation, err := values.ParseLocalDate(q.EvaluationDate)
	if err != nil {
		return Request{}, fmt.Errorf("%w: evaluation date: %w", ErrQueryInvalid, err)
	}
	watermark, err := values.NewSequenceRevision(revisionStreamPrefix+sanitizeStream(workerRef), 1)
	if err != nil {
		return Request{}, fmt.Errorf("%w: compensation revision: %w", ErrQueryInvalid, err)
	}
	current, err := q.snapshot(q.CurrentBase, effective, watermark)
	if err != nil {
		return Request{}, fmt.Errorf("%w: current base pay: %w", ErrQueryInvalid, err)
	}
	proposed, err := q.snapshot(q.ProposedBase, effective, watermark)
	if err != nil {
		return Request{}, fmt.Errorf("%w: proposed base pay: %w", ErrQueryInvalid, err)
	}
	budget, err := q.budget()
	if err != nil {
		return Request{}, err
	}
	if strings.TrimSpace(q.BusinessReason) == "" {
		return Request{}, fmt.Errorf("%w: business reason is required", ErrQueryInvalid)
	}
	return Request{
		WorkerRef: workerRef,
		Fields:    promotion.RequiredWorkerFields(),
		Target: promotion.TargetPlacement{
			JobCode:    q.TargetJobCode,
			Grade:      q.TargetGrade,
			OrgUnit:    q.TargetOrgUnit,
			PositionID: q.TargetPositionID,
			PayZone:    q.TargetPayZone,
		},
		Current:        current,
		Proposed:       proposed,
		Currency:       q.Currency,
		EffectiveDate:  effective,
		EvaluationDate: evaluation,
		BusinessReason: q.BusinessReason,
		Budget:         budget,
		Policy:         promotion.DefaultPolicy(),
		Annualization:  rewards.DefaultAnnualization(),
	}, nil
}

// snapshot builds one side of the declared compensation baseline.
func (q Query) snapshot(amount string, effective values.LocalDate, watermark values.RevisionToken) (rewards.CompensationSnapshot, error) {
	base, err := fixtures.Money(amount, q.Currency)
	if err != nil {
		return rewards.CompensationSnapshot{}, err
	}
	snapshot := rewards.CompensationSnapshot{
		Base:          values.Value(base),
		PayBasis:      rewards.PayBasisAnnualSalary,
		EffectiveDate: effective,
		Watermark:     watermark,
		Complete:      true,
	}
	if q.BonusTargetPercent != "" {
		bonus, bonusErr := fixtures.Percent(q.BonusTargetPercent)
		if bonusErr != nil {
			return rewards.CompensationSnapshot{}, fmt.Errorf("bonus target: %w", bonusErr)
		}
		snapshot.BonusTargetPercent = values.Value(bonus)
	}
	return snapshot, nil
}

// budget builds the observed budget authority the promotion is weighed
// against, or nil when the query declares none.
func (q Query) budget() (*promotion.BudgetAuthorityRef, error) {
	if strings.TrimSpace(q.BudgetAvailable) == "" {
		return nil, nil
	}
	available, err := fixtures.Money(q.BudgetAvailable, q.Currency)
	if err != nil {
		return nil, fmt.Errorf("%w: budget: %w", ErrQueryInvalid, err)
	}
	return &promotion.BudgetAuthorityRef{
		BudgetType:      promotion.BudgetTypeCompensationPool,
		OwnerSystem:     "finance.incumbent.erp",
		PolicyRef:       "finance.budget_authority/2026.1",
		Scope:           "cost-center:" + q.TargetOrgUnit,
		Period:          "FY2026",
		Currency:        q.Currency,
		Unit:            "MONEY",
		BaselineVersion: "finance.budget.baseline/2026.09",
		AvailableAmount: values.Value(available),
		ObservationID:   "obs_budget_" + sanitizeStream(q.TargetOrgUnit),
	}, nil
}

// sanitizeStream reduces a reference to the characters a revision stream and
// an observation identifier accept.
func sanitizeStream(ref string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(ref) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}
