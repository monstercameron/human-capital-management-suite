package chat

// Issue 3818: the notices an applicant to a channel's join form receives when a
// person decides their request. They are private moderation notices, shown on
// the Moderation page and counted in the sidebar, addressed to the applicant
// alone. A declined notice carries the reason the reviewer gave to the applicant.
const (
	// ModerationOutcomeGateAdmitted: the request was accepted; the person is a member.
	ModerationOutcomeGateAdmitted = "gate_admitted"
	// ModerationOutcomeGateDeclined: the request was not accepted; Reason says why.
	ModerationOutcomeGateDeclined = "gate_declined"
)

// IsGateDecisionOutcome reports whether a notice tells an applicant the decision
// on their request to join.
func IsGateDecisionOutcome(outcome string) bool {
	return outcome == ModerationOutcomeGateAdmitted || outcome == ModerationOutcomeGateDeclined
}
