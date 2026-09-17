package legal

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// ErrClassificationInvalid is returned when a worker-classification request
// or result is malformed, and when a signed result fails verification. All
// classification outcomes for uncertain inputs use
// [ClassificationReviewRequired] instead: this error is for broken plumbing,
// never for an undecidable worker.
var ErrClassificationInvalid = errors.New("legal: worker classification is invalid")

// ClassificationProposal is the employer-proposed worker status the
// evaluation judges. It is a proposal, never a finding: the result says
// whether the pinned rule release and the observed factors support it.
type ClassificationProposal string

// Proposed worker statuses.
const (
	ClassificationExempt     ClassificationProposal = "EXEMPT"
	ClassificationNonexempt  ClassificationProposal = "NONEXEMPT"
	ClassificationContractor ClassificationProposal = "CONTRACTOR"
	ClassificationEmployee   ClassificationProposal = "EMPLOYEE"
)

// ClassificationOutcome is the closed verdict vocabulary. There are exactly
// three values: a worker is never "probably exempt".
const (
	ClassificationEligible       = "ELIGIBLE"
	ClassificationIneligible     = "INELIGIBLE"
	ClassificationReviewRequired = "REVIEW_REQUIRED"
)

// ClassificationFactors are the observed facts an evaluation may read. Every
// boolean is a pointer so that "observed false" and "never observed" stay
// distinct: an unknown factor routes to review, never to a default.
type ClassificationFactors struct {
	// SalaryBasisPaid records whether the worker is paid on a salary basis.
	SalaryBasisPaid *bool
	// AnnualSalary is meaningful only when SalaryStated is true.
	AnnualSalary values.Money
	SalaryStated bool
	// DutiesTestSatisfied records whether the worker's duties satisfy the
	// exemption test the pinned rule describes.
	DutiesTestSatisfied *bool
	// ContractorIndependenceMet records whether the worker meets the
	// independence test the pinned contractor rule describes.
	ContractorIndependenceMet *bool
	// WorkAuthorized records whether the worker is authorized to work.
	WorkAuthorized *bool
}

// ClassificationOverride is a reviewer's explicit decision replacing the
// computed outcome. All four fields are required: an anonymous, unreasoned
// or sourceless override is refused rather than recorded.
type ClassificationOverride struct {
	Reviewer  string
	Authority string
	Decision  string
	Rationale string
}

// ClassificationInput is one worker-classification question. Factors are
// facts about the worker, never conclusions; the evaluation draws the
// conclusion or refuses to.
type ClassificationInput struct {
	WorkerID string
	Proposed ClassificationProposal
	// WorkAuthorizationRequired marks that the role requires established
	// work authorization. When false, the authorization factor is not read.
	WorkAuthorizationRequired bool
	Factors                   ClassificationFactors
	// ObservedAsOf is the date the factors were last verified. Factors
	// postdating the transaction, or older than MaxFactorAgeDays, are stale
	// and route to review.
	ObservedAsOf  values.LocalDate
	EffectiveDate values.LocalDate
	KnownAt       values.KnownAt
	// MaxFactorAgeDays bounds factor staleness. Zero or negative means
	// same-day factors only: there is no platform-default freshness.
	MaxFactorAgeDays int
	// Override, when set, replaces the computed outcome with the reviewer's
	// decision and records the reviewer as evidence.
	Override *ClassificationOverride
}

// ClassificationResult is the immutable, signed verdict for one worker. It
// carries the factors it was decided under so a later reviewer can see what
// the computation saw, plus the pinned rule release that authorized the
// question.
type ClassificationResult struct {
	WorkerID          string
	Proposed          ClassificationProposal
	Outcome           string
	Factors           ClassificationFactors
	RuleRelease       RulePackRelease
	Jurisdiction      Jurisdiction
	ObservedAsOf      values.LocalDate
	EffectiveDate     values.LocalDate
	KnownAt           values.KnownAt
	EvaluatedAt       values.Instant
	Reviewer          string
	OverrideAuthority string
	Reasons           []string
	Digest            string
	Signature         Signature
}

// ClassifyWorker evaluates the proposed worker status under the
// CLASSIFICATION rules the context pinned. It is governed computation, not
// UI metadata: incomplete or stale factors return REVIEW_REQUIRED, a known
// disqualifier returns INELIGIBLE, and only complete supporting factors
// return ELIGIBLE. It never mutates the registry; the only write is the
// returned value's own signature.
func ClassifyWorker(ctx *LegalContext, input ClassificationInput, registry *Registry, signer *Signer, now values.Instant) (ClassificationResult, error) {
	if ctx == nil {
		return ClassificationResult{}, fmt.Errorf("%w: no legal context supplied", ErrClassificationInvalid)
	}
	if registry == nil {
		return ClassificationResult{}, fmt.Errorf("%w: no rule-pack registry supplied", ErrClassificationInvalid)
	}
	if signer == nil {
		return ClassificationResult{}, fmt.Errorf("%w: no signer supplied", ErrClassificationInvalid)
	}
	if err := now.Validate(); err != nil {
		return ClassificationResult{}, fmt.Errorf("%w: evaluation clock: %v", ErrClassificationInvalid, err)
	}
	if err := input.validate(); err != nil {
		return ClassificationResult{}, err
	}
	if err := ctx.Verify(); err != nil {
		return ClassificationResult{}, fmt.Errorf("%w: legal context authority: %v", ErrClassificationInvalid, err)
	}
	if input.Override != nil {
		if err := input.Override.validate(); err != nil {
			return ClassificationResult{}, err
		}
	}

	rules, release, err := classificationRules(ctx, registry, input.Proposed)
	if err != nil {
		return ClassificationResult{}, err
	}
	base := ClassificationResult{
		WorkerID:      input.WorkerID,
		Proposed:      input.Proposed,
		Factors:       input.Factors,
		RuleRelease:   release,
		Jurisdiction:  ctx.Jurisdiction(),
		ObservedAsOf:  input.ObservedAsOf,
		EffectiveDate: input.EffectiveDate,
		KnownAt:       input.KnownAt,
		EvaluatedAt:   now,
	}

	// Without a pinned rule for the question's dimension there is nothing
	// to decide under. The result still carries the first pinned release
	// so the receipt names which law set lacked the rule.
	if !rules.decisionAuthorized() {
		return signClassification(base.withReview("no-classification-rule-pinned"), signer)
	}
	// A reviewer override replaces computation, including its freshness
	// and completeness gates: the reviewer brings knowledge the factors
	// lack, and the authority fields record whose knowledge it was.
	if input.Override != nil {
		base.Outcome = input.Override.Decision
		base.Reviewer = input.Override.Reviewer
		base.OverrideAuthority = input.Override.Authority
		base.Reasons = []string{"reviewer-override:" + input.Override.Authority, input.Override.Rationale}
		return signClassification(base, signer)
	}
	if reason, stale := input.freshness(); stale {
		return signClassification(base.withReview(reason), signer)
	}
	if missing := input.completeness(rules.salaryThresholdStated()); missing != "" {
		return signClassification(base.withReview("incomplete-factors:"+missing), signer)
	}
	return signClassification(base.decide(rules, input.WorkAuthorizationRequired), signer)
}

// classificationRuleSet is the pinned CLASSIFICATION content one question
// may read: the highest stated exemption salary threshold across every
// pinned release (the contract's section 6.3 CLASSIFICATION comparator
// direction), and whether any contractor dimension rule was pinned.
type classificationRuleSet struct {
	threshold       values.Money
	thresholdStated bool
	contractorRule  bool
	exemptionRule   bool
}

// decisionAuthorized reports whether the pinned set carries a rule for the
// question's dimension at all.
func (s classificationRuleSet) decisionAuthorized() bool {
	return s.exemptionRule || s.contractorRule
}

// classificationRules re-fetches every release the context pinned and keeps
// the CLASSIFICATION rules for the proposed question's dimension. A pinned
// release the registry can no longer produce fails closed with
// [ErrRuleCoverageUnknown]: evaluation under a half-remembered law set is
// worse than no evaluation.
func classificationRules(ctx *LegalContext, registry *Registry, proposed ClassificationProposal) (classificationRuleSet, RulePackRelease, error) {
	var out classificationRuleSet
	refs := ctx.RulePackReleases()
	if len(refs) == 0 {
		return classificationRuleSet{}, RulePackRelease{}, fmt.Errorf("%w: context carries no rule-pack releases", ErrClassificationInvalid)
	}
	release := refs[0]
	for _, ref := range refs {
		pack, err := registry.GetExact(ref)
		if err != nil {
			return classificationRuleSet{}, RulePackRelease{}, fmt.Errorf("%w: %v", ErrRuleCoverageUnknown, err)
		}
		for _, rule := range pack.Classifications {
			switch {
			case proposed.exemption() && rule.Dimension == "EXEMPTION":
				out.exemptionRule = true
				if rule.SalaryThreshold.Validate() == nil {
					if !out.thresholdStated {
						out.threshold, out.thresholdStated = rule.SalaryThreshold, true
					} else if cmp, err := rule.SalaryThreshold.Cmp(out.threshold); err == nil && cmp > 0 {
						out.threshold = rule.SalaryThreshold
					} else if err != nil {
						return classificationRuleSet{}, RulePackRelease{}, fmt.Errorf("%w: classification thresholds are not comparable", ErrClassificationInvalid)
					}
				}
			case !proposed.exemption() && rule.Dimension == "CONTRACTOR":
				out.contractorRule = true
			}
		}
	}
	return out, release, nil
}

// exemption reports whether the proposal is judged under the exemption
// dimension (EXEMPT or NONEXEMPT) rather than the contractor dimension.
func (p ClassificationProposal) exemption() bool {
	return p == ClassificationExempt || p == ClassificationNonexempt
}

// salaryThresholdStated reports whether any pinned exemption rule states a salary
// threshold, which decides whether the salary factor is read at all.
func (s classificationRuleSet) salaryThresholdStated() bool { return s.thresholdStated }

// validate refuses a malformed question. Uncertainty in the factors is not
// malformation: it returns REVIEW_REQUIRED from [ClassifyWorker], never an
// error from here.
func (in ClassificationInput) validate() error {
	if in.WorkerID == "" {
		return fmt.Errorf("%w: worker id is required", ErrClassificationInvalid)
	}
	switch in.Proposed {
	case ClassificationExempt, ClassificationNonexempt, ClassificationContractor, ClassificationEmployee:
	default:
		return fmt.Errorf("%w: unknown proposed status %q", ErrClassificationInvalid, in.Proposed)
	}
	if err := in.ObservedAsOf.Validate(); err != nil {
		return fmt.Errorf("%w: factors-as-of date: %v", ErrClassificationInvalid, err)
	}
	if err := in.EffectiveDate.Validate(); err != nil {
		return fmt.Errorf("%w: effective date: %v", ErrClassificationInvalid, err)
	}
	if err := in.KnownAt.Instant().Validate(); err != nil {
		return fmt.Errorf("%w: known_at: %v", ErrClassificationInvalid, err)
	}
	return nil
}

// validate refuses an override that does not name its reviewer, authority,
// decision and rationale together.
func (o ClassificationOverride) validate() error {
	if o.Reviewer == "" || o.Authority == "" || o.Rationale == "" {
		return fmt.Errorf("%w: override needs reviewer, authority and rationale", ErrClassificationInvalid)
	}
	switch o.Decision {
	case ClassificationEligible, ClassificationIneligible, ClassificationReviewRequired:
	default:
		return fmt.Errorf("%w: override decision %q is unknown", ErrClassificationInvalid, o.Decision)
	}
	return nil
}

// freshness reports whether the factors are too old or too new to decide
// under. A non-positive MaxFactorAgeDays means same-day factors only.
func (in ClassificationInput) freshness() (string, bool) {
	if in.ObservedAsOf.Compare(in.EffectiveDate) > 0 {
		return "factors-postdate-transaction", true
	}
	maxAge := in.MaxFactorAgeDays
	if maxAge < 0 {
		maxAge = 0
	}
	if ageDays(in.ObservedAsOf, in.EffectiveDate) > maxAge {
		return "factors-stale", true
	}
	return "", false
}

// ageDays counts whole calendar days from a to b (b on or after a).
func ageDays(a, b values.LocalDate) int {
	from := time.Date(int(a.Year()), a.Month(), int(a.Day()), 0, 0, 0, 0, time.UTC)
	to := time.Date(int(b.Year()), b.Month(), int(b.Day()), 0, 0, 0, 0, time.UTC)
	return int(to.Sub(from).Hours() / 24)
}

// completeness names the first material fact the question reads that was
// never observed. The empty string means every fact the decision reads is
// present; only then may the decision be conclusive.
func (in ClassificationInput) completeness(thresholdStated bool) string {
	need := func(name string, v *bool) string {
		if v == nil {
			return name
		}
		return ""
	}
	if in.WorkAuthorizationRequired {
		if missing := need("work-authorized", in.Factors.WorkAuthorized); missing != "" {
			return missing
		}
	}
	if in.Proposed.exemption() {
		for _, missing := range []string{
			need("salary-basis", in.Factors.SalaryBasisPaid),
			need("duties-test", in.Factors.DutiesTestSatisfied),
		} {
			if missing != "" {
				return missing
			}
		}
		if thresholdStated && !in.Factors.SalaryStated {
			return "salary-amount"
		}
		return ""
	}
	return need("contractor-independence", in.Factors.ContractorIndependenceMet)
}

// withReview builds the zero-effect verdict: no eligibility is granted, no
// reviewer is named, and the reason records exactly what was missing.
func (r ClassificationResult) withReview(reason string) ClassificationResult {
	r.Outcome = ClassificationReviewRequired
	r.Reasons = []string{reason}
	return r
}

// decide runs on complete, fresh factors only. Every leg it reads is known,
// so every verdict below is conclusive rather than a guess. Authorization
// is only read when the input required it; the completeness gate already
// established the factor is present in that case.
func (r ClassificationResult) decide(rules classificationRuleSet, workAuthRequired bool) ClassificationResult {
	f := r.Factors
	legs := []string{}
	if workAuthRequired {
		if !*f.WorkAuthorized {
			r.Outcome = ClassificationIneligible
			r.Reasons = []string{"work-authorization-not-established"}
			return r
		}
		legs = append(legs, "work-authorized:true")
	}
	salaryLeg := "salary-threshold:none-stated"
	salaryOK := true
	if rules.thresholdStated {
		salaryOK = rules.salaryOK(f)
		salaryLeg = tri("salary-at-or-above-threshold", salaryOK)
	}
	switch r.Proposed {
	case ClassificationExempt:
		basis, duties := *f.SalaryBasisPaid, *f.DutiesTestSatisfied
		legs = append(legs, tri("salary-basis", basis), salaryLeg, tri("duties", duties))
		r.Reasons = legs
		if basis && salaryOK && duties {
			r.Outcome = ClassificationEligible
		} else {
			r.Outcome = ClassificationIneligible
		}
	case ClassificationNonexempt:
		basis, duties := *f.SalaryBasisPaid, *f.DutiesTestSatisfied
		legs = append(legs, tri("salary-basis", basis), salaryLeg, tri("duties", duties))
		r.Outcome = ClassificationEligible
		if basis && salaryOK && duties {
			r.Reasons = append(legs, "exempt-test-satisfied-nonexempt-treatment-elected")
		} else {
			r.Reasons = append(legs, "exempt-test-not-satisfied")
		}
	case ClassificationContractor:
		independent := *f.ContractorIndependenceMet
		r.Reasons = append(legs, tri("contractor-independence", independent))
		if independent {
			r.Outcome = ClassificationEligible
		} else {
			r.Outcome = ClassificationIneligible
		}
	case ClassificationEmployee:
		independent := *f.ContractorIndependenceMet
		if independent {
			r.Outcome = ClassificationReviewRequired
			r.Reasons = []string{"contractor-indicators-present"}
			return r
		}
		r.Outcome = ClassificationEligible
		r.Reasons = append(legs, tri("contractor-independence", independent))
	}
	return r
}

// salaryOK compares the stated salary against the highest pinned threshold.
// No stated threshold means the source states none, so the leg is vacuous.
// An incomparable salary is never rounded into compliance.
func (s classificationRuleSet) salaryOK(f ClassificationFactors) bool {
	if !s.salaryThresholdStated() {
		return true
	}
	if !f.SalaryStated {
		return false
	}
	cmp, err := f.AnnualSalary.Cmp(s.threshold)
	return err == nil && cmp >= 0
}

// tri renders one decided leg for the reason trace.
func tri(name string, v bool) string {
	if v {
		return name + ":true"
	}
	return name + ":false"
}

// signClassification digests and signs the verdict. An eligibility without
// a pinned release is never signed: it becomes review instead. Every other
// path already carries the context's first pinned release.
func signClassification(r ClassificationResult, signer *Signer) (ClassificationResult, error) {
	if r.RuleRelease.PackID == "" && r.Outcome != ClassificationReviewRequired {
		r = r.withReview("no-classification-rule-pinned")
	}
	digest, sig, err := signer.SignDigestChecked(r.CanonicalBytes())
	if err != nil {
		return ClassificationResult{}, fmt.Errorf("%w: signing classification: %v", ErrClassificationInvalid, err)
	}
	r.Digest, r.Signature = digest, sig
	return r, nil
}

// Verify recomputes the verdict's digest and checks the embedded signature,
// refusing any result that is semantically incomplete even when re-signed.
func (r ClassificationResult) Verify() error { return r.verify(nil) }

// VerifyWithKey behaves like [ClassificationResult.Verify] and additionally
// requires the embedded public key to equal the trusted key.
func (r ClassificationResult) VerifyWithKey(key []byte) error { return r.verify(key) }

func (r ClassificationResult) verify(key []byte) error {
	if r.WorkerID == "" || !r.Proposed.known() || !knownOutcome(r.Outcome) || r.RuleRelease.PackID == "" || r.RuleRelease.Version == 0 || r.RuleRelease.Jurisdiction.Validate() != nil || r.ObservedAsOf.Validate() != nil || r.EffectiveDate.Validate() != nil || r.KnownAt.Instant().Validate() != nil || r.EvaluatedAt.Validate() != nil || len(r.Reasons) == 0 {
		return ErrClassificationInvalid
	}
	if (r.Reviewer == "") != (r.OverrideAuthority == "") {
		return ErrClassificationInvalid
	}
	if key != nil && (len(key) != len(r.Signature.PublicKey) || !slices.Equal(key, r.Signature.PublicKey)) {
		return ErrClassificationInvalid
	}
	if err := VerifySignature(r.CanonicalBytes(), r.Digest, r.Signature); err != nil {
		return fmt.Errorf("%w: %v", ErrClassificationInvalid, err)
	}
	return nil
}

// known reports whether the proposal names a decidable status.
func (p ClassificationProposal) known() bool {
	switch p {
	case ClassificationExempt, ClassificationNonexempt, ClassificationContractor, ClassificationEmployee:
		return true
	}
	return false
}

// knownOutcome reports whether the outcome is in the closed vocabulary.
func knownOutcome(o string) bool {
	return o == ClassificationEligible || o == ClassificationIneligible || o == ClassificationReviewRequired
}

// CanonicalBytes is the deterministic encoding the verdict's digest and
// signature cover. Tri-state factors encode explicitly so that "observed
// false" and "never observed" never digest identically.
func (r ClassificationResult) CanonicalBytes() []byte {
	var out = []byte{'L', 'C', '5', '1'}
	out = appendField(out, "worker_id", r.WorkerID)
	out = appendField(out, "proposed", string(r.Proposed))
	out = appendField(out, "outcome", r.Outcome)
	out = appendField(out, "salary_basis", triState(r.Factors.SalaryBasisPaid))
	salary := "unstated"
	if r.Factors.SalaryStated {
		salary = r.Factors.AnnualSalary.String()
	}
	out = appendField(out, "salary", salary)
	out = appendField(out, "duties", triState(r.Factors.DutiesTestSatisfied))
	out = appendField(out, "contractor_independence", triState(r.Factors.ContractorIndependenceMet))
	out = appendField(out, "work_authorized", triState(r.Factors.WorkAuthorized))
	out = appendField(out, "observed_as_of", r.ObservedAsOf.String())
	out = appendField(out, "rule_release_pack_id", r.RuleRelease.PackID)
	out = appendUint32Field(out, "rule_release_version", r.RuleRelease.Version)
	out = appendUint32Field(out, "rule_release_minor", r.RuleRelease.MinorVersion)
	out = r.RuleRelease.Jurisdiction.canonicalBytes(appendField(out, "rule_release_jurisdiction", ""))
	out = r.Jurisdiction.canonicalBytes(appendField(out, "jurisdiction", ""))
	out = appendField(out, "effective_date", r.EffectiveDate.String())
	out = appendField(out, "known_at", r.KnownAt.String())
	out = appendField(out, "evaluated_at", r.EvaluatedAt.String())
	out = appendField(out, "reviewer", r.Reviewer)
	out = appendField(out, "override_authority", r.OverrideAuthority)
	for _, reason := range r.Reasons {
		out = appendField(out, "reason", reason)
	}
	return out
}

// triState renders an observed-or-unknown factor for the digest.
func triState(v *bool) string {
	if v == nil {
		return "unknown"
	}
	if *v {
		return "true"
	}
	return "false"
}
