// Package punchpolicy owns TCLOCK-009 (versioned punch policy: rounding,
// grace, early clock-in lockout and auto-deducted breaks) and TCLOCK-010's
// pure part (versioned attestation question sets, tip declarations and
// job/cost-code transfer segment rules).
//
// The package is kernel-pure: no database, no time.Now (every instant is a
// caller-supplied argument), no network, no new dependencies and no
// package-level mutable state. Rules and thresholds are always data carried
// on a Policy or QuestionSet value, never a hard-coded constant, so a
// jurisdiction can configure or forbid a rule without a code change.
//
// A Policy never rewrites a raw punch: Evaluate takes the raw interval and
// returns a separate, policy-pinned EvaluatedInterval. The raw evidence
// recorded by internal/domains/clock is untouched.
package punchpolicy

import "errors"

const contractVersion = 1

// Version reports the punchpolicy contract version. A change to the meaning
// or shape of a Policy, an EvaluatedInterval, a Decision, a QuestionSet or a
// Consequence must advance this value.
func Version() int { return contractVersion }

// Sentinel errors. Every rejection wraps exactly one of these so a caller can
// fail closed with errors.Is instead of matching on message text.
var (
	// ErrInvalidPolicy identifies a Policy whose shape or thresholds cannot
	// be evaluated at all (missing id, negative threshold, undeclared mode).
	ErrInvalidPolicy = errors.New("punchpolicy: policy is invalid")
	// ErrPolicyRejected identifies a structurally valid Policy that a named
	// jurisdiction's rule pack refuses to accept (forbidden rounding, a
	// lockout window past the jurisdiction's cap, an undeclared override
	// role). The policy is never applied to a punch when this is returned.
	ErrPolicyRejected = errors.New("punchpolicy: jurisdiction rejects the policy")
	// ErrInvalidInterval identifies a raw or shift interval that cannot be
	// evaluated (zero, inverted, or otherwise malformed).
	ErrInvalidInterval = errors.New("punchpolicy: interval is invalid")
	// ErrInvalidQuestionSet identifies a QuestionSet that is not a well-formed
	// versioned vocabulary (duplicate id, undeclared kind, missing text key).
	ErrInvalidQuestionSet = errors.New("punchpolicy: question set is invalid")
	// ErrUnansweredRequired identifies a required question with no answer.
	ErrUnansweredRequired = errors.New("punchpolicy: a required question has no answer")
	// ErrInvalidTipDeclaration identifies a tip declaration or adjustment
	// that cannot be recorded.
	ErrInvalidTipDeclaration = errors.New("punchpolicy: tip declaration is invalid")
	// ErrInvalidTransfer identifies a job/cost-code transfer that cannot
	// close and open a labour segment.
	ErrInvalidTransfer = errors.New("punchpolicy: transfer is invalid")
	// ErrNoTransferChange identifies a transfer request whose job and cost
	// code exactly match the open segment: it changes nothing, so it is not
	// a transfer.
	ErrNoTransferChange = errors.New("punchpolicy: transfer names no change")
)
