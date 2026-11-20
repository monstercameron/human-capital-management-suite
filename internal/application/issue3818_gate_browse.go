package application

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
)

// Issue 3818: a person whose request to join waits for a decision is told, in
// Browse, who it waits for. The names come from the same read the join form
// uses for the applicant (the channel's managers), asked only for the channels
// where this person has a waiting request. A name that cannot be read is left
// out; the row then says the channel's managers decide.
func (a *ChatgateApplication) nameWaitingReviewers(ctx context.Context, actor chatgate.Actor, rows []chatstore.GateSummary) {
	directory, ok := a.Directory.(ChatgateReviewers)
	if !ok {
		return
	}
	for i := range rows {
		if rows[i].Request != "review" {
			continue
		}
		if names, err := directory.GateReviewers(ctx, actor, chatgate.Scope{Tenant: actor.Tenant, Conversation: rows[i].Conversation}); err == nil {
			rows[i].Reviewers = names
		}
	}
}
