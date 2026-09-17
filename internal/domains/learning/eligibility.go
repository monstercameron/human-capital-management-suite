// LEARN-002: resolve prerequisites and learning eligibility. Missing,
// expired or unknown prerequisites return conditional, blocked or unknown
// with an explanation naming the exact refs; equivalencies satisfy a
// prerequisite only through a registered rule citing exact evidence.
package learning

import (
	"fmt"
	"strings"
)

// Eligibility verdicts share the closed vocabulary.
const (
	VerdictEligible    = "ELIGIBLE"
	VerdictConditional = "CONDITIONAL"
	VerdictBlocked     = "BLOCKED"
	VerdictUnknown     = "UNKNOWN"
)

// LearnerEvidence is the prerequisite evidence presented for one learner.
type LearnerEvidence struct {
	LearnerID           string
	CompletedCourseRefs []string
	CredentialRefs      []string
	ExpiredRefs         []string
}

// EquivalencyRule declares that one evidence kind satisfies one
// prerequisite for one course version under one rule ref.
type EquivalencyRule struct {
	CourseID     string
	Version      uint64
	Satisfies    string
	RuleRef      string
	EvidenceKind string
}

// Equivalency is one applied rule-to-evidence binding in a verdict.
type Equivalency struct {
	RuleRef     string
	EvidenceRef string
}

// EligibilityResult is the explained verdict.
type EligibilityResult struct {
	Verdict       string
	Explanation   string
	Equivalencies []Equivalency
	BlockerRefs   []string
}

// RegisterEquivalency publishes one prerequisite-equivalency rule.
func (r *Registry) RegisterEquivalency(rule EquivalencyRule) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.equivalencies = append(r.equivalencies, rule)
}

// ResolveEligibility evaluates one learner against one course version.
// Unknown courses resolve UNKNOWN; empty learners never resolve ELIGIBLE.
func (r *Registry) ResolveEligibility(learner LearnerEvidence, courseID string, version uint64) (EligibilityResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.courses.LoadVersion(courseID, version)
	if !ok {
		return EligibilityResult{
			Verdict:     VerdictUnknown,
			Explanation: fmt.Sprintf("course %s version %d is not recorded", courseID, version),
		}, nil
	}
	if strings.TrimSpace(learner.LearnerID) == "" {
		return EligibilityResult{
			Verdict:     VerdictBlocked,
			Explanation: "learner identity is missing",
		}, nil
	}
	completed := map[string]bool{}
	for _, ref := range learner.CompletedCourseRefs {
		completed[ref] = true
	}
	expired := map[string]bool{}
	for _, ref := range learner.ExpiredRefs {
		expired[ref] = true
	}
	result := EligibilityResult{}
	for _, prereq := range v.PrerequisiteRefs {
		switch {
		case expired[prereq]:
			result.Verdict = VerdictBlocked
			result.BlockerRefs = append(result.BlockerRefs, prereq)
		case completed[prereq]:
			// Satisfied directly.
		default:
			applied := false
			for _, rule := range r.equivalencies {
				if rule.CourseID != courseID || rule.Version != version || rule.Satisfies != prereq {
					continue
				}
				for _, cred := range learner.CredentialRefs {
					if strings.HasPrefix(cred, rule.EvidenceKind) {
						result.Equivalencies = append(result.Equivalencies, Equivalency{
							RuleRef: rule.RuleRef, EvidenceRef: cred,
						})
						applied = true
						break
					}
				}
			}
			if !applied {
				if result.Verdict != VerdictBlocked {
					result.Verdict = VerdictConditional
				}
				result.BlockerRefs = append(result.BlockerRefs, prereq)
			}
		}
	}
	if result.Verdict == "" {
		result.Verdict = VerdictEligible
		result.Explanation = fmt.Sprintf("learner %s satisfies every prerequisite of %s",
			learner.LearnerID, versionKey(courseID, version))
		return result, nil
	}
	if result.Verdict == VerdictBlocked {
		result.Explanation = fmt.Sprintf("learner %s is blocked by expired prerequisite %s",
			learner.LearnerID, strings.Join(result.BlockerRefs, ","))
	} else {
		result.Explanation = fmt.Sprintf("learner %s is conditional on prerequisite %s",
			learner.LearnerID, strings.Join(result.BlockerRefs, ","))
	}
	return result, nil
}
