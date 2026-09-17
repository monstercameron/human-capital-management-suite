// Package jurisdiction proves selected-jurisdiction behavior across the
// Promotion slice (transactional) and the Medical Leave slice (long-running
// protected process) for CROSS-CONF-001. Both flows pin the same selected
// jurisdiction and composition release in snapshots and proposals, preserve
// historical evaluation across known-at/effective-at rule changes, and
// deterministically return BLOCKED|REVIEW_REQUIRED|REPLAN_REQUIRED with a
// successor proposal and zero stale effects for ambiguity or material rule
// change.
//
// The evaluator is kernel-pure: every instant, rule release and obligation
// fixture is caller-supplied, it keeps no state, and it creates no approval,
// work item, write or effect -- Effects is always zero. Neither workflow
// embeds legal rules: domain-specific obligations come only from the shared
// fixture the caller injects.
package jurisdiction

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
)

// FlowKind is the workflow slice under evaluation.
type FlowKind string

const (
	FlowPromotion    FlowKind = "PROMOTION"
	FlowMedicalLeave FlowKind = "MEDICAL_LEAVE"
)

// Verdict is the deterministic outcome of one evaluation.
type Verdict string

const (
	VerdictProceed        Verdict = "PROCEED"
	VerdictBlocked        Verdict = "BLOCKED"
	VerdictReviewRequired Verdict = "REVIEW_REQUIRED"
	VerdictReplanRequired Verdict = "REPLAN_REQUIRED"
)

var (
	// ErrLegalContextMissing refuses any flow that omits its LegalContext.
	// A promotion without legal context is the canonical RED case.
	ErrLegalContextMissing = errors.New("jurisdiction: legal context missing")
	// ErrEvaluationInvalid refuses resumes against unknown or stale
	// proposals and requests naming an unknown flow.
	ErrEvaluationInvalid = errors.New("jurisdiction: evaluation invalid")
)

// LegalContext is the trusted-boundary jurisdiction assertion for one
// evaluation. Selected/Release pin the chosen jurisdiction and its legal
// composition release; Candidates carries the unresolved multi-location set
// when the jurisdiction is ambiguous; Unknown marks an unresolvable
// selection. Present distinguishes an asserted context from an omitted one.
type LegalContext struct {
	Present    bool
	Selected   string
	Release    string
	Candidates []string
	Unknown    bool
}

// RuleRelease is one versioned legal rule pack with the bitemporal instants
// that govern it: EffectiveAt when it takes effect, KnownAt when it became
// known. A new release never rewrites a prior evaluation; it replans it.
type RuleRelease struct {
	Version     string
	EffectiveAt time.Time
	KnownAt     time.Time
}

// ObligationFixture maps a selected jurisdiction to the expected
// domain-specific obligations per flow. It is the shared legal fixture:
// callers inject it, workflows never hard-code it.
type ObligationFixture map[string]map[FlowKind][]string

// Snapshot is the pinned historical evaluation: the selected jurisdiction
// and composition release, the rule release it was evaluated under, and the
// obligations that evaluation produced.
type Snapshot struct {
	Flow            FlowKind
	Jurisdiction    string
	Release         string
	RuleVersion     string
	RuleEffectiveAt time.Time
	Obligations     []string
	ProposalID      string
}

// Proposal is a governable unit of work bound to one snapshot.
// SuccessorOf names the superseded proposal for replans and reviews;
// Candidates carries the unresolved set for ambiguity reviews.
type Proposal struct {
	ID          string
	Flow        FlowKind
	Snapshot    Snapshot
	SuccessorOf string
	Candidates  []string
}

// EvaluationRequest is one jurisdiction evaluation: an initial pin
// (ProposalID empty) or a resume of a pinned proposal under the current
// rule release.
type EvaluationRequest struct {
	Flow        FlowKind
	Legal       LegalContext
	Pinned      Snapshot
	ProposalID  string
	CurrentRule RuleRelease
	Fixtures    ObligationFixture
}

// EvaluationResult is the deterministic outcome. Pinned always preserves
// the historical evaluation on resume; Successor is set exactly for
// REVIEW_REQUIRED (ambiguity) and REPLAN_REQUIRED (material change).
// Effects is always zero: evaluation creates nothing.
type EvaluationResult struct {
	Verdict     Verdict
	Pinned      Snapshot
	Successor   *Proposal
	Obligations []string
	Effects     int
}

// Digest returns the canonical digest of the whole result. Two evaluations
// of the same logical inputs digest identically.
func (r EvaluationResult) Digest() string {
	w := canonicalbytes.New("workflow.conformance.jurisdiction.Evaluation", 1).
		String("verdict", string(r.Verdict)).
		String("flow", string(r.Pinned.Flow)).
		String("jurisdiction", r.Pinned.Jurisdiction).
		String("release", r.Pinned.Release).
		String("rule_version", r.Pinned.RuleVersion).
		String("rule_effective_at", r.Pinned.RuleEffectiveAt.UTC().Format(time.RFC3339)).
		String("proposal_id", r.Pinned.ProposalID).
		SortedStrings("pinned_obligations", r.Pinned.Obligations).
		SortedStrings("obligations", r.Obligations).
		Int("effects", int64(r.Effects))
	if r.Successor != nil {
		n := canonicalbytes.New("workflow.conformance.jurisdiction.Proposal", 1).
			String("id", r.Successor.ID).
			String("flow", string(r.Successor.Flow)).
			String("successor_of", r.Successor.SuccessorOf).
			String("jurisdiction", r.Successor.Snapshot.Jurisdiction).
			String("release", r.Successor.Snapshot.Release).
			String("rule_version", r.Successor.Snapshot.RuleVersion).
			String("rule_effective_at", r.Successor.Snapshot.RuleEffectiveAt.UTC().Format(time.RFC3339)).
			String("proposal_id", r.Successor.Snapshot.ProposalID).
			SortedStrings("obligations", r.Successor.Snapshot.Obligations).
			SortedStrings("candidates", r.Successor.Candidates)
		w.Nested("successor", n)
	} else {
		w.Bool("successor?", false)
	}
	digest, err := w.Digest()
	if err != nil {
		return ""
	}
	return digest
}

// EvaluateSelectedJurisdiction evaluates one flow's jurisdiction position.
// It never guesses: an ambiguous, empty or unknown selection returns
// REVIEW_REQUIRED or BLOCKED with zero effects. A resume under a materially
// changed rule or selection returns REPLAN_REQUIRED with a successor bound
// to the old proposal; the old proposal itself never resumes.
func EvaluateSelectedJurisdiction(req EvaluationRequest) (EvaluationResult, error) {
	if req.Flow != FlowPromotion && req.Flow != FlowMedicalLeave {
		return EvaluationResult{}, fmt.Errorf("%w: unknown flow %q", ErrEvaluationInvalid, req.Flow)
	}
	if !req.Legal.Present {
		return EvaluationResult{}, fmt.Errorf("%w: flow %s must pin a LegalContext", ErrLegalContextMissing, req.Flow)
	}
	if req.ProposalID == "" {
		return evaluateInitial(req)
	}
	return evaluateResume(req)
}

// gateSelection applies the never-guess rule to the asserted context.
// proceed is true only for a single pinned jurisdiction: an unresolvable
// selection blocks, while an unresolved multi-location set -- even a
// singleton that was never pinned -- requires review instead of guessing.
func gateSelection(legal LegalContext) (Verdict, bool) {
	if legal.Unknown {
		return VerdictBlocked, false
	}
	if len(legal.Candidates) > 0 {
		return VerdictReviewRequired, false
	}
	if strings.TrimSpace(legal.Selected) == "" {
		return VerdictBlocked, false
	}
	return VerdictProceed, true
}

func lookupObligations(fixtures ObligationFixture, jurisdiction string, flow FlowKind) ([]string, bool) {
	flows, ok := fixtures[jurisdiction]
	if !ok {
		return nil, false
	}
	obl, ok := flows[flow]
	if !ok {
		return nil, false
	}
	return append([]string(nil), obl...), true
}

func evaluateInitial(req EvaluationRequest) (EvaluationResult, error) {
	if verdict, proceed := gateSelection(req.Legal); !proceed {
		res := EvaluationResult{Verdict: verdict}
		if verdict == VerdictReviewRequired {
			candidates := append([]string(nil), req.Legal.Candidates...)
			sort.Strings(candidates)
			sum := sha256.Sum256([]byte(strings.Join(candidates, ",")))
			res.Successor = &Proposal{
				ID:         fmt.Sprintf("review-%s-%.8x", req.Flow, sum[:4]),
				Flow:       req.Flow,
				Candidates: candidates,
			}
		}
		return res, nil
	}
	obligations, ok := lookupObligations(req.Fixtures, req.Legal.Selected, req.Flow)
	if !ok {
		return EvaluationResult{Verdict: VerdictReviewRequired}, nil
	}
	id := fmt.Sprintf("pin-%s-%s-%s", req.Flow, req.Legal.Selected, req.CurrentRule.Version)
	snap := Snapshot{
		Flow:            req.Flow,
		Jurisdiction:    req.Legal.Selected,
		Release:         req.Legal.Release,
		RuleVersion:     req.CurrentRule.Version,
		RuleEffectiveAt: req.CurrentRule.EffectiveAt,
		Obligations:     obligations,
		ProposalID:      id,
	}
	return EvaluationResult{
		Verdict:     VerdictProceed,
		Pinned:      snap,
		Obligations: append([]string(nil), obligations...),
	}, nil
}

func evaluateResume(req EvaluationRequest) (EvaluationResult, error) {
	if strings.TrimSpace(req.Pinned.ProposalID) == "" || req.Pinned.ProposalID != req.ProposalID {
		return EvaluationResult{}, fmt.Errorf("%w: cannot resume proposal %q", ErrEvaluationInvalid, req.ProposalID)
	}
	history := EvaluationResult{Pinned: req.Pinned, Obligations: append([]string(nil), req.Pinned.Obligations...)}
	if verdict, proceed := gateSelection(req.Legal); !proceed {
		history.Verdict = verdict
		history.Obligations = nil
		if verdict == VerdictReviewRequired {
			candidates := append([]string(nil), req.Legal.Candidates...)
			sort.Strings(candidates)
			sum := sha256.Sum256([]byte(strings.Join(candidates, ",")))
			history.Successor = &Proposal{
				ID:          fmt.Sprintf("review-%s-%.8x", req.Flow, sum[:4]),
				Flow:        req.Flow,
				SuccessorOf: req.ProposalID,
				Candidates:  candidates,
			}
		}
		return history, nil
	}
	obligations, ok := lookupObligations(req.Fixtures, req.Legal.Selected, req.Flow)
	if !ok {
		history.Verdict = VerdictReviewRequired
		history.Obligations = nil
		return history, nil
	}
	if req.Legal.Selected != req.Pinned.Jurisdiction || req.Legal.Release != req.Pinned.Release {
		return replan(req, obligations), nil
	}
	if req.CurrentRule.Version != req.Pinned.RuleVersion ||
		!req.CurrentRule.EffectiveAt.Equal(req.Pinned.RuleEffectiveAt) {
		return replan(req, obligations), nil
	}
	history.Verdict = VerdictProceed
	return history, nil
}

// replan binds a successor proposal to the current selection and rule while
// leaving the pinned historical evaluation untouched. Successor IDs are a
// pure function of flow, superseded proposal and rule version, so replays
// converge on one successor instead of duplicating proposals.
func replan(req EvaluationRequest, obligations []string) EvaluationResult {
	id := fmt.Sprintf("replan-%s-%s-%s", req.Flow, req.ProposalID, req.CurrentRule.Version)
	snap := Snapshot{
		Flow:            req.Flow,
		Jurisdiction:    req.Legal.Selected,
		Release:         req.Legal.Release,
		RuleVersion:     req.CurrentRule.Version,
		RuleEffectiveAt: req.CurrentRule.EffectiveAt,
		Obligations:     append([]string(nil), obligations...),
		ProposalID:      id,
	}
	return EvaluationResult{
		Verdict:     VerdictReplanRequired,
		Pinned:      req.Pinned,
		Obligations: append([]string(nil), obligations...),
		Successor: &Proposal{
			ID:          id,
			Flow:        req.Flow,
			Snapshot:    snap,
			SuccessorOf: req.ProposalID,
		},
	}
}
