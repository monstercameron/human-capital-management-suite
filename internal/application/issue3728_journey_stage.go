package application

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/workspace"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// Issue 3728: when a workflow request that was shared into conversations is
// approved, rejected or blocked, each of those conversations gets one system
// line saying so, attributed to the person who decided. The line is written
// only where the journey was shared (the pointer #3725 records), only into a
// conversation that still exists and is not archived, and it holds no reason:
// the typed journey reference on it makes every reader's own access to the
// request decide what that reader sees, so a member who cannot open the request
// learns nothing beyond "a request shared here was updated".

// journeyStageLinks reads which conversations a journey was shared into.
type journeyStageLinks interface {
	ListWorkLinksBySource(ctx context.Context, tenant, kind, sourceID string, limit int) ([]chatstore.WorkLink, error)
}

// journeyStageChat writes the line; chat.Service is the served implementation.
type journeyStageChat interface {
	PostJourneyStage(context.Context, chat.JourneyStagePost) (chat.Post, error)
}

const journeyStageAnnounceMax = 25

// journeyStageAnnouncer implements workspace.JourneyDecisionAnnouncer.
type journeyStageAnnouncer struct {
	Chat  journeyStageChat
	Links journeyStageLinks
}

// journeyStageOutcome is the outcome a decision announces: a rejection is a
// rejection, an approval that left the request stopped on an exception reads
// as blocked, and any other approval as approved.
func journeyStageOutcome(e workspace.JourneyDecisionEvent) string {
	if !e.Approve || e.Stage == workspace.JourneyStageRejected {
		return chat.JourneyStageRejected
	}
	switch e.Stage {
	case workspace.JourneyStageBlocked, workspace.JourneyStageFailed, workspace.JourneyStageRepairRequired:
		return chat.JourneyStageBlocked
	}
	return chat.JourneyStageApproved
}

// AnnounceJourneyDecision implements workspace.JourneyDecisionAnnouncer. The
// context carries the decider's trusted identity; with none (or an agent) it
// does nothing. Every failure is swallowed after being logged: the decision is
// already durable and a conversation that cannot be told changes nothing about
// it, and one conversation's failure never stops the others.
func (a *journeyStageAnnouncer) AnnounceJourneyDecision(ctx context.Context, e workspace.JourneyDecisionEvent) {
	if a == nil || a.Chat == nil || a.Links == nil {
		return
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Warn("hcmnext.journey_stage_announce_panic")
		}
	}()
	p, ok := trust.FromContext(ctx)
	if !ok {
		return
	}
	decider, err := chatWorkPrincipal(p)
	if err != nil {
		return
	}
	journeyID := e.IntentID
	if strings.TrimSpace(journeyID) == "" || journeyID != strings.TrimSpace(journeyID) {
		return
	}
	links, err := a.Links.ListWorkLinksBySource(ctx, decider.TenantID, chatstore.WorkLinkJourneyShare, journeyID, journeyStageAnnounceMax)
	if err != nil {
		slog.Warn("hcmnext.journey_stage_announce_links_failed")
		return
	}
	outcome := journeyStageOutcome(e)
	key := "journey-stage:" + journeyID + ":" + outcome + ":" + string(e.Stage) + ":" + e.ProposalRevisionID
	for _, link := range links {
		if _, err := a.Chat.PostJourneyStage(ctx, chat.JourneyStagePost{
			TenantID: decider.TenantID, ConversationID: link.ConversationID, JourneyID: journeyID,
			Outcome: outcome, Decider: decider, IdempotencyKey: key,
		}); err != nil && !isQuietStageRefusal(err) {
			slog.Warn("hcmnext.journey_stage_announce_failed")
		}
	}
}

// isQuietStageRefusal is a conversation that is gone or archived: expected, so
// not worth a warning.
func isQuietStageRefusal(err error) bool {
	return errors.Is(err, chat.ErrNotFound)
}
