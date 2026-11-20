package workspace

import "context"

// JourneyDecisionEvent says that an approver's decision on a journey was
// accepted by the engine (issue 3728). It carries the outcome the engine
// reached and nothing a reader of a conversation must not learn: no reason, no
// worker, no pay figure. The decider is the trusted principal on the context.
type JourneyDecisionEvent struct {
	IntentID string
	// Approve is the decision the approver made.
	Approve bool
	// Stage is the stage the journey reached once the decision was applied.
	Stage JourneyStage
	// ProposalRevisionID is the proposal revision the decision applied to.
	ProposalRevisionID string
}

// JourneyDecisionAnnouncer is told, after the fact, that a decision was
// accepted. It is a courtesy: it returns nothing, the decision is already
// durable, and a failure in it never changes the answer the approver gets.
// Chat uses it to say, in each conversation the journey was shared into, that
// the request was decided.
type JourneyDecisionAnnouncer interface {
	AnnounceJourneyDecision(ctx context.Context, e JourneyDecisionEvent)
}
