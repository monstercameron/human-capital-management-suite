package main

import (
	"net/url"
	"strings"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// A committed mention already has a post ID and canonical agent attribution.
// Render its waiting state in the same update, before the invocation stream
// has completed a separate read. The stream replaces this provisional row.
func applyCommittedAgentPending(model *chatui.Model, post *chatv1.Post) {
	if model == nil || post == nil || post.GetId() == "" || post.GetAuthorId() != model.CurrentUser || post.GetConversationId() != model.SelectedID {
		return
	}
	for _, invocation := range model.PersonaInvocations {
		if invocation.PostID == post.GetId() {
			return
		}
	}
	for _, ref := range personaChatPostReferences(post) {
		if ref.Kind != "AGENT_MENTION" || ref.TenantID != model.CurrentTenantID || ref.ConversationID != model.SelectedID {
			continue
		}
		model.PersonaInvocations = append(model.PersonaInvocations, chatui.PersonaThreadInvocation{PostID: post.GetId(), ThreadID: post.GetParentId(), Projection: chatui.PersonaProgressProjection{InvocationID: "pending:" + post.GetId(), ViewerID: model.CurrentUser, InvokerID: model.CurrentUser, AgentName: ref.Display, Progress: agentPendingProgress(model.CurrentUser, ref.Display)}})
		break
	}
}

func reconcileAgentPending(previous, next []chatui.PersonaThreadInvocation) []chatui.PersonaThreadInvocation {
	for _, old := range previous {
		if !strings.HasPrefix(old.Projection.InvocationID, "pending:") {
			continue
		}
		found := false
		for _, current := range next {
			found = found || current.PostID == old.PostID
		}
		if !found {
			next = append(next, old)
		}
	}
	return preservePersonaElapsed(previous, next)
}

func preserveAgentConversationIdentity(previous chatui.Model, next *chatui.Model) {
	if next == nil || previous.CurrentTenantID != next.CurrentTenantID || previous.CurrentUser != next.CurrentUser {
		return
	}
	for _, room := range previous.Conversations {
		if room.Agent {
			applyAgentDirectConversation(next, room.ID, room.AgentID, room.Name, agentDirectIdentity{Icon: room.Icon, Revision: room.IconRevision, Purpose: room.AgentPurpose})
		}
	}
}

func bindAgentInvocationConversations(model *chatui.Model) {
	for _, invocation := range model.PersonaInvocations {
		projection := invocation.Projection
		if projection.ViewerID != model.CurrentUser || projection.InvokerID != model.CurrentUser {
			continue
		}
		link, err := url.Parse(projection.PrivateReplyHref)
		if err != nil || link.IsAbs() || link.Host != "" || link.Path != "/workspace/app/chat" || !strings.HasPrefix(link.Fragment, "channel=") {
			continue
		}
		room, err := url.QueryUnescape(strings.TrimPrefix(link.Fragment, "channel="))
		if err != nil {
			continue
		}
		for _, post := range append(append([]chatui.Message(nil), model.Messages...), model.ThreadMessages...) {
			if post.ID != invocation.PostID {
				continue
			}
			for _, ref := range post.PersonaReferences {
				if ref.Kind == "AGENT_MENTION" && ref.TenantID == model.CurrentTenantID && ref.ConversationID == model.SelectedID && ref.Display == projection.AgentName {
					applyAgentDirectConversation(model, room, ref.ID, ref.Display)
					break
				}
			}
		}
	}
}
