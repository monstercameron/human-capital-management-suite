// Evidence-bound pilot review: PILOT-001 decides go/no-go and value
// realization from current evidence, never from ambition.
//
// Decision rules, in order:
//  1. Every required criterion (safety, correctness, slo, adoption,
//     value, cost, support) must have at least one evidence item. A
//     wholly absent criterion means this pilot cannot be evaluated:
//     RESELECT.
//  2. Missing, stale, expired or failed-critical evidence blocks:
//     NO_GO. Staleness derives from observed time plus max age
//     against review time; expiry is explicit.
//  3. A live waiver names — but never passes — its evidence. Waivers
//     on critical failures (safety, correctness, slo) are void.
//  4. Failed non-critical evidence (adoption, value, cost, support)
//     downgrades to CONDITIONAL_GO with named blockers.
//  5. All pass clears GO.
//  6. Authority expansion is granted only for a GO verdict with an
//     approved expansion manifest and passing value evidence.
//
// Costs and support are required criteria, so a review that excludes
// implementation or support cost cannot pass. Every metric carries
// its denominator and a business-outcome statement: activity counts
// without outcomes refuse. Review is pure: inputs are never mutated
// and nothing is persisted.
package pilot

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Version reports this package's vocabulary version.
const Version = 1

// Evidence criteria under review.
const (
	CriterionSafety      = "safety"
	CriterionCorrectness = "correctness"
	CriterionSLO         = "slo"
	CriterionAdoption    = "adoption"
	CriterionValue       = "value"
	CriterionCost        = "cost"
	CriterionSupport     = "support"
)

// requiredCriteria are all evaluated; none may be absent.
var requiredCriteria = []string{
	CriterionSafety, CriterionCorrectness, CriterionSLO,
	CriterionAdoption, CriterionValue, CriterionCost, CriterionSupport,
}

// criticalCriteria fail the pilot outright; the rest downgrade it.
var criticalCriteria = map[string]bool{
	CriterionSafety: true, CriterionCorrectness: true, CriterionSLO: true,
}

// EvidenceStatus is the closed evidence vocabulary.
type EvidenceStatus string

// The evidence states.
const (
	EvidencePass    EvidenceStatus = "PASS"
	EvidenceFail    EvidenceStatus = "FAIL"
	EvidenceMissing EvidenceStatus = "MISSING"
	EvidenceWaived  EvidenceStatus = "WAIVED"
)

// RiskTier bounds expansion appetite.
type RiskTier string

// The risk tiers.
const (
	RiskLow    RiskTier = "LOW"
	RiskMedium RiskTier = "MEDIUM"
	RiskHigh   RiskTier = "HIGH"
)

// Waiver names who set aside an evidence item, why, and until when.
type Waiver struct {
	By               string
	Reason           string
	ExpiresUnixMilli int64
}

// Evidence is one measured review input.
type Evidence struct {
	Criterion         string
	Status            EvidenceStatus
	Numerator         int64
	Denominator       int64
	Digest            string
	Outcome           string
	ObservedUnixMilli int64
	MaxAgeMillis      int64
	ExpiresUnixMilli  int64
	Waiver            *Waiver
}

// Validate implements validation.
func (e Evidence) Validate() error {
	switch e.Criterion {
	case CriterionSafety, CriterionCorrectness, CriterionSLO,
		CriterionAdoption, CriterionValue, CriterionCost, CriterionSupport:
	default:
		return fmt.Errorf("pilot: criterion %q is not reviewed", e.Criterion)
	}
	switch e.Status {
	case EvidencePass, EvidenceFail, EvidenceMissing, EvidenceWaived:
	default:
		return fmt.Errorf("pilot: evidence status %q is not declared", e.Status)
	}
	if e.Numerator < 0 || e.Denominator <= 0 || e.Numerator > e.Denominator {
		return fmt.Errorf("pilot: %s metric needs 0 <= numerator <= denominator", e.Criterion)
	}
	if strings.TrimSpace(e.Digest) == "" || strings.TrimSpace(e.Outcome) == "" {
		return errors.New("pilot: evidence needs a digest and a business-outcome statement")
	}
	if e.ObservedUnixMilli < 0 || e.MaxAgeMillis <= 0 {
		return errors.New("pilot: evidence needs observation time and max age")
	}
	if e.Status == EvidenceWaived {
		if e.Waiver == nil || strings.TrimSpace(e.Waiver.By) == "" || strings.TrimSpace(e.Waiver.Reason) == "" || e.Waiver.ExpiresUnixMilli <= 0 {
			return fmt.Errorf("pilot: %s waiver needs by, reason and expiry", e.Criterion)
		}
	}
	return nil
}

// ReviewInput is the fully declared review.
type ReviewInput struct {
	PilotRef          string
	Evidence          []Evidence
	NowUnixMilli      int64
	RiskTier          RiskTier
	AllowExpansion    bool
	ExpansionManifest string
	ManifestApproved  bool
}

// Validate implements validation.
func (r ReviewInput) Validate() error {
	if strings.TrimSpace(r.PilotRef) == "" {
		return errors.New("pilot: pilot ref is required")
	}
	if len(r.Evidence) == 0 {
		return errors.New("pilot: review needs evidence")
	}
	seen := make(map[string]struct{}, len(r.Evidence))
	for _, evidence := range r.Evidence {
		if err := evidence.Validate(); err != nil {
			return err
		}
		key := evidence.Criterion + "\x00" + evidence.Digest
		if _, ok := seen[key]; ok {
			return fmt.Errorf("pilot: duplicate evidence for %s", evidence.Criterion)
		}
		seen[key] = struct{}{}
	}
	switch r.RiskTier {
	case RiskLow, RiskMedium, RiskHigh:
	default:
		return fmt.Errorf("pilot: risk %q is not declared", r.RiskTier)
	}
	if r.AllowExpansion && strings.TrimSpace(r.ExpansionManifest) == "" {
		return errors.New("pilot: expansion needs a manifest ref")
	}
	return nil
}

// ReviewDecision is the closed decision vocabulary.
type ReviewDecision string

// The decisions.
const (
	DecisionGo            ReviewDecision = "GO"
	DecisionConditionalGo ReviewDecision = "CONDITIONAL_GO"
	DecisionNoGo          ReviewDecision = "NO_GO"
	DecisionReselect      ReviewDecision = "RESELECT"
)

// MetricReport is one evaluated criterion.
type MetricReport struct {
	Criterion   string
	Status      EvidenceStatus
	Numerator   int64
	Denominator int64
	Digest      string
	Outcome     string
}

// ExpansionGrant is the authority-expansion verdict.
type ExpansionGrant struct {
	Permitted   bool
	ManifestRef string
	Reason      string
}

// DecisionPackage is the digested review account.
type DecisionPackage struct {
	PilotRef  string
	Decision  ReviewDecision
	Metrics   []MetricReport
	Blockers  []string
	Waivers   []string
	Expansion ExpansionGrant
	RiskTier  RiskTier
	Digest    string
}

// Review evaluates one evidence-bound pilot review.
func Review(input ReviewInput) (DecisionPackage, error) {
	if err := input.Validate(); err != nil {
		return DecisionPackage{}, err
	}
	byCriterion := make(map[string][]Evidence, len(input.Evidence))
	for _, evidence := range input.Evidence {
		byCriterion[evidence.Criterion] = append(byCriterion[evidence.Criterion], evidence)
	}
	verdict := DecisionPackage{PilotRef: input.PilotRef, RiskTier: input.RiskTier, Decision: DecisionGo}
	for _, criterion := range requiredCriteria {
		items, ok := byCriterion[criterion]
		if !ok || len(items) == 0 {
			verdict.Decision = DecisionReselect
			verdict.Blockers = append(verdict.Blockers, criterion+": unevaluated")
		}
	}
	for _, evidence := range input.Evidence {
		verdict.Metrics = append(verdict.Metrics, MetricReport{
			Criterion: evidence.Criterion, Status: effectiveStatus(evidence, input.NowUnixMilli),
			Numerator: evidence.Numerator, Denominator: evidence.Denominator,
			Digest: evidence.Digest, Outcome: evidence.Outcome,
		})
	}
	sort.Slice(verdict.Metrics, func(i, j int) bool {
		if verdict.Metrics[i].Criterion != verdict.Metrics[j].Criterion {
			return verdict.Metrics[i].Criterion < verdict.Metrics[j].Criterion
		}
		return verdict.Metrics[i].Digest < verdict.Metrics[j].Digest
	})
	if verdict.Decision == DecisionReselect {
		verdict.Expansion = ExpansionGrant{Reason: "reselect: pilot scope cannot be evaluated"}
		verdict.Digest = digestPackage(verdict)
		return verdict, nil
	}
	for _, metric := range verdict.Metrics {
		switch metric.Status {
		case EvidencePass:
		case EvidenceWaived:
			verdict.Waivers = append(verdict.Waivers, metric.Criterion+":"+metric.Digest)
		case EvidenceMissing:
			verdict.Decision = DecisionNoGo
			verdict.Blockers = append(verdict.Blockers, metric.Criterion+":"+metric.Digest)
		default:
			if criticalCriteria[metric.Criterion] {
				verdict.Decision = DecisionNoGo
			} else if verdict.Decision == DecisionGo {
				verdict.Decision = DecisionConditionalGo
			}
			verdict.Blockers = append(verdict.Blockers, metric.Criterion+":"+metric.Digest)
		}
	}
	verdict.Expansion = grantExpansion(input, verdict)
	verdict.Digest = digestPackage(verdict)
	return verdict, nil
}

// effectiveStatus resolves freshness, expiry and waivers: stale,
// expired and void-waiver evidence never counts as a pass.
func effectiveStatus(evidence Evidence, now int64) EvidenceStatus {
	if evidence.ExpiresUnixMilli > 0 && now >= evidence.ExpiresUnixMilli {
		return EvidenceMissing
	}
	if now >= evidence.ObservedUnixMilli+evidence.MaxAgeMillis {
		return EvidenceMissing
	}
	if evidence.Status == EvidenceWaived {
		if evidence.Waiver == nil || now >= evidence.Waiver.ExpiresUnixMilli {
			return EvidenceMissing
		}
		// Waivers on critical criteria are void: safety,
		// correctness and slo never pass by excuse.
		if criticalCriteria[evidence.Criterion] {
			return EvidenceMissing
		}
		return EvidenceWaived
	}
	return evidence.Status
}

func grantExpansion(input ReviewInput, verdict DecisionPackage) ExpansionGrant {
	if !input.AllowExpansion {
		return ExpansionGrant{Reason: "expansion not requested"}
	}
	if verdict.Decision != DecisionGo {
		return ExpansionGrant{ManifestRef: input.ExpansionManifest, Reason: "expansion needs a GO verdict"}
	}
	if !input.ManifestApproved {
		return ExpansionGrant{ManifestRef: input.ExpansionManifest, Reason: "expansion needs an approved manifest"}
	}
	for _, metric := range verdict.Metrics {
		if metric.Criterion == CriterionValue && metric.Status != EvidencePass {
			return ExpansionGrant{ManifestRef: input.ExpansionManifest, Reason: "expansion needs passing value evidence"}
		}
	}
	return ExpansionGrant{Permitted: true, ManifestRef: input.ExpansionManifest, Reason: "go verdict with approved manifest and passing value"}
}

func digestPackage(verdict DecisionPackage) string {
	parts := []string{"pilot001-review", verdict.PilotRef, string(verdict.Decision), string(verdict.RiskTier)}
	for _, metric := range verdict.Metrics {
		parts = append(parts, strings.Join([]string{
			metric.Criterion, string(metric.Status),
			fmt.Sprint(metric.Numerator, metric.Denominator), metric.Digest, metric.Outcome,
		}, "\x00"))
	}
	blockers := append([]string(nil), verdict.Blockers...)
	sort.Strings(blockers)
	waivers := append([]string(nil), verdict.Waivers...)
	sort.Strings(waivers)
	parts = append(parts, "blockers:"+strings.Join(blockers, ","))
	parts = append(parts, "waivers:"+strings.Join(waivers, ","))
	parts = append(parts, fmt.Sprintf("expansion:%v:%s:%s", verdict.Expansion.Permitted, verdict.Expansion.ManifestRef, verdict.Expansion.Reason))
	sum := sha256.Sum256([]byte(strings.Join(parts, "\n")))
	return "sha256:" + hex.EncodeToString(sum[:])
}
