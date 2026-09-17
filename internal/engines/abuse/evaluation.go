package abuse

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrEvaluationRejected identifies a detector evaluation that cannot bind
// reviewed outcomes, sampling, rates and version, or a binding check that
// fails. A rejected evaluation authorizes nothing.
var ErrEvaluationRejected = errors.New("abuse: detector evaluation rejected")

// ReviewedVerdict is the closed human-review vocabulary for one detector
// signal. The zero value is deliberately invalid: an outcome without an
// explicit reviewed verdict can never count as reviewed.
type ReviewedVerdict string

const (
	ReviewedTruePositive  ReviewedVerdict = "TRUE_POSITIVE"
	ReviewedTrueNegative  ReviewedVerdict = "TRUE_NEGATIVE"
	ReviewedFalsePositive ReviewedVerdict = "FALSE_POSITIVE"
	ReviewedFalseNegative ReviewedVerdict = "FALSE_NEGATIVE"
	ReviewedUnknown       ReviewedVerdict = "UNKNOWN"
)

// Valid reports whether v is a declared reviewed verdict.
func (v ReviewedVerdict) Valid() bool {
	switch v {
	case ReviewedTruePositive, ReviewedTrueNegative,
		ReviewedFalsePositive, ReviewedFalseNegative, ReviewedUnknown:
		return true
	default:
		return false
	}
}

// ReviewedOutcome is one human-reviewed detector signal.
type ReviewedOutcome struct {
	SignalID string
	Verdict  ReviewedVerdict
	Reviewer string
	At       time.Time
}

// EvaluationInput binds one detector version to its reviewed outcomes and
// the sampled pool they were drawn from. SampledTotal carries the
// sampling-bias binding: coverage is reviewed over sampled, never assumed.
// All instants are caller-supplied; evaluation reads no clock.
type EvaluationInput struct {
	DetectorID      string
	DetectorVersion string
	DetectorDigest  string
	Outcomes        []ReviewedOutcome
	SampledTotal    int
	EvaluatedAt     time.Time
}

// EvaluationAction is the closed containment vocabulary. There is
// deliberately no broaden action: a degraded detector pauses or requires
// review, never silently widens containment.
type EvaluationAction string

const (
	ActionMaintain      EvaluationAction = "MAINTAIN"
	ActionRequireReview EvaluationAction = "REQUIRE_REVIEW"
	ActionPaused        EvaluationAction = "PAUSED"
)

// Valid reports whether a is a declared evaluation action.
func (a EvaluationAction) Valid() bool {
	switch a {
	case ActionMaintain, ActionRequireReview, ActionPaused:
		return true
	default:
		return false
	}
}

// Evaluation thresholds. A detector that misses or misfires at or above
// the pause bound is paused; one past the review bound requires a human
// before containment continues; low sampling coverage alone requires
// review because the rates cannot be trusted.
const (
	pauseFalsePositiveRate  = 0.5
	pauseFalseNegativeRate  = 0.5
	pauseUnknownRate        = 0.5
	reviewFalsePositiveRate = 0.1
	reviewFalseNegativeRate = 0.05
	reviewUnknownRate       = 0.1
	reviewMinCoverage       = 0.5
)

// Evaluation is the ABUSE-007 result: reviewed rates bound to one detector
// version with the containment verdict the rates compel.
type Evaluation struct {
	DetectorID        string
	DetectorVersion   string
	DetectorDigest    string
	Reviewed          int
	FalsePositives    int
	FalseNegatives    int
	Unknowns          int
	FalsePositiveRate float64
	FalseNegativeRate float64
	UnknownRate       float64
	Coverage          float64
	Action            EvaluationAction
	EvaluatedAt       time.Time
	Digest            string
}

func evaluationDigest(id, version, digest string, reviewed, fp, fn, unk int, action EvaluationAction, at time.Time) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{
		id, version, digest,
		fmt.Sprintf("%d", reviewed), fmt.Sprintf("%d", fp),
		fmt.Sprintf("%d", fn), fmt.Sprintf("%d", unk),
		string(action), at.UTC().Format(time.RFC3339Nano),
	}, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

// EvaluateDetector binds reviewed outcomes to sampling, rates and version.
// Every outcome must carry an explicit reviewed verdict; the reviewed set
// can never exceed the sampled pool.
func EvaluateDetector(in EvaluationInput) (Evaluation, error) {
	if strings.TrimSpace(in.DetectorID) == "" {
		return Evaluation{}, fmt.Errorf("%w: detector id is required", ErrEvaluationRejected)
	}
	if !semverPattern.MatchString(in.DetectorVersion) {
		return Evaluation{}, fmt.Errorf("%w: detector version %q is not MAJOR.MINOR.PATCH", ErrEvaluationRejected, in.DetectorVersion)
	}
	if strings.TrimSpace(in.DetectorDigest) == "" {
		return Evaluation{}, fmt.Errorf("%w: detector version digest is required", ErrEvaluationRejected)
	}
	if len(in.Outcomes) == 0 {
		return Evaluation{}, fmt.Errorf("%w: at least one reviewed outcome is required", ErrEvaluationRejected)
	}
	if in.SampledTotal <= 0 {
		return Evaluation{}, fmt.Errorf("%w: sampled total is required", ErrEvaluationRejected)
	}
	if len(in.Outcomes) > in.SampledTotal {
		return Evaluation{}, fmt.Errorf("%w: %d reviewed outcomes exceed %d sampled", ErrEvaluationRejected, len(in.Outcomes), in.SampledTotal)
	}
	if in.EvaluatedAt.IsZero() {
		return Evaluation{}, fmt.Errorf("%w: evaluation time is required", ErrEvaluationRejected)
	}
	var fp, fn, unk int
	for i, o := range in.Outcomes {
		if strings.TrimSpace(o.SignalID) == "" || strings.TrimSpace(o.Reviewer) == "" || o.At.IsZero() {
			return Evaluation{}, fmt.Errorf("%w: outcome %d needs signal, reviewer and review time", ErrEvaluationRejected, i)
		}
		if !o.Verdict.Valid() {
			return Evaluation{}, fmt.Errorf("%w: outcome %d carries no reviewed verdict", ErrEvaluationRejected, i)
		}
		switch o.Verdict {
		case ReviewedFalsePositive:
			fp++
		case ReviewedFalseNegative:
			fn++
		case ReviewedUnknown:
			unk++
		}
	}
	reviewed := len(in.Outcomes)
	eval := Evaluation{
		DetectorID: in.DetectorID, DetectorVersion: in.DetectorVersion,
		DetectorDigest: in.DetectorDigest, Reviewed: reviewed,
		FalsePositives: fp, FalseNegatives: fn, Unknowns: unk,
		FalsePositiveRate: float64(fp) / float64(reviewed),
		FalseNegativeRate: float64(fn) / float64(reviewed),
		UnknownRate:       float64(unk) / float64(reviewed),
		Coverage:          float64(reviewed) / float64(in.SampledTotal),
		Action:            ActionMaintain,
		EvaluatedAt:       in.EvaluatedAt,
	}
	switch {
	case eval.FalsePositiveRate >= pauseFalsePositiveRate ||
		eval.FalseNegativeRate >= pauseFalseNegativeRate ||
		eval.UnknownRate >= pauseUnknownRate:
		eval.Action = ActionPaused
	case eval.FalsePositiveRate > reviewFalsePositiveRate ||
		eval.FalseNegativeRate > reviewFalseNegativeRate ||
		eval.UnknownRate > reviewUnknownRate ||
		eval.Coverage < reviewMinCoverage:
		eval.Action = ActionRequireReview
	}
	eval.Digest = evaluationDigest(
		eval.DetectorID, eval.DetectorVersion, eval.DetectorDigest,
		eval.Reviewed, eval.FalsePositives, eval.FalseNegatives,
		eval.Unknowns, eval.Action, eval.EvaluatedAt)
	return eval, nil
}

// VerifyEvaluationBinding proves eval still binds the named detector
// version: identity, version, digest and the evaluation digest itself must
// match, so an evaluation can never be replayed elsewhere or tampered with.
func VerifyEvaluationBinding(eval Evaluation, detectorID, version, digest string) error {
	if eval.DetectorID != detectorID || eval.DetectorVersion != version || eval.DetectorDigest != digest {
		return fmt.Errorf("%w: evaluation binds %s %s, not %s %s", ErrEvaluationRejected,
			eval.DetectorID, eval.DetectorVersion, detectorID, version)
	}
	if eval.Digest != evaluationDigest(
		eval.DetectorID, eval.DetectorVersion, eval.DetectorDigest,
		eval.Reviewed, eval.FalsePositives, eval.FalseNegatives,
		eval.Unknowns, eval.Action, eval.EvaluatedAt) {
		return fmt.Errorf("%w: evaluation digest does not match", ErrEvaluationRejected)
	}
	return nil
}
