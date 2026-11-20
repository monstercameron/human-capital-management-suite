package chat

// Issue 3817: the notices a channel's join form sends to the people who decide
// its requests. They are moderation notices (the private notices a person reads
// on the Moderation page and is counted for in the sidebar). The notice names
// the channel and carries no applicant, no answer and no words.
const (
	// ModerationOutcomeGateReview: a request to join is waiting for a decision.
	ModerationOutcomeGateReview = "gate_review"
	// ModerationOutcomeGateOverdue: a request has waited past the first deadline.
	ModerationOutcomeGateOverdue = "gate_review_overdue"
	// ModerationOutcomeGateEscalated: a request has waited past the second
	// deadline; sent to the workspace administrators as well.
	ModerationOutcomeGateEscalated = "gate_review_escalated"
)

// IsGateReviewOutcome reports whether a notice is about a request waiting on a
// channel's join form.
func IsGateReviewOutcome(outcome string) bool {
	return outcome == ModerationOutcomeGateReview || outcome == ModerationOutcomeGateOverdue || outcome == ModerationOutcomeGateEscalated
}
