package main

import (
	"strings"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/agenticon"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// CHATUX-047: the list and the timeline answers name the people and agents they
// carry (Conversation.peer, Post.author). The client draws from those first and
// lets the directory reads that follow refine them, so the first frame after an
// answer has every name instead of an id, a "..." or an empty tile.

// chatux047Icon is the stored icon an answer carried for an agent, or the zero
// value (an agent with none stored draws its own fallback).
func chatux047Icon(icon *chatv1.AgentIcon) agenticon.Value {
	if icon == nil {
		return agenticon.Value{}
	}
	value := agenticon.Value{Glyph: icon.GetGlyph(), Shape: icon.GetShape(), Foreground: icon.GetForeground(), Background: icon.GetBackground(), Variation: icon.GetVariation()}
	if !value.Valid() {
		return agenticon.Value{}
	}
	return value
}

// chatux047Author is how a post's author reads: the session directory's name
// when it has one (the same rule the server applies, so it only refines), else
// the name the answer carried, else the humanized reference. The photograph is
// the one the answer carried; an agent's name, id and icon travel as AuthorAgent.
func chatux047Author(v *chatv1.Post, directory map[string]string) (name, photo string, agent *chatui.AuthorAgent) {
	id := v.GetAuthorId()
	identity := v.GetAuthor()
	carried := strings.TrimSpace(identity.GetDisplayName())
	if known, ok := directory[id]; ok && known != "" {
		name = known
	} else if carried != "" {
		name = carried
	} else {
		name = humanizeChatSubjectID(id)
	}
	photo = strings.TrimSpace(identity.GetAvatarRef())
	if identity.GetAgent() && carried != "" {
		agentID := strings.TrimSpace(identity.GetAgentId())
		if agentID == "" {
			agentID = id
		}
		name = carried
		agent = &chatui.AuthorAgent{AgentID: agentID, Icon: chatux047Icon(identity.GetAgentIcon()), IconRevision: identity.GetAgentIconRevision()}
	}
	return name, photo, agent
}

// chatux047Peer folds the peer an answer named into the row of a direct
// conversation: an agent's name, id and icon, or a person's name. The peer's
// subject and photograph are held beside the row by chatux047PeerIDs and
// chatux047PeerPhotos.
func chatux047Peer(room chatui.Conversation, v *chatv1.Conversation) chatui.Conversation {
	peer := v.GetPeer()
	if peer == nil || room.Kind != chatui.DirectMessage {
		return room
	}
	name := strings.TrimSpace(peer.GetDisplayName())
	subject := strings.TrimSpace(peer.GetSubjectId())
	if name != "" && name != room.ID && name != subject {
		room.Name = name
		if peer.GetAgent() {
			room.Agent = true
			room.AgentID = strings.TrimSpace(peer.GetAgentId())
			if room.AgentID == "" {
				room.AgentID = subject
			}
			room.Icon, room.IconRevision = chatux047Icon(peer.GetAgentIcon()), peer.GetAgentIconRevision()
		}
	}
	return room
}

// chatux047PeerIDs is the session's direct-conversation peers with the ones the
// list answer named. It is a copy: the session's own map is never written here.
func chatux047PeerIDs(held map[string]string, rows []*chatv1.Conversation) map[string]string {
	out := make(map[string]string, len(held)+len(rows))
	for id, peer := range held {
		out[id] = peer
	}
	for _, row := range rows {
		if row == nil || row.GetKind() != chatv1.ConversationKind_CONVERSATION_KIND_DIRECT {
			continue
		}
		if subject := strings.TrimSpace(row.GetPeer().GetSubjectId()); subject != "" {
			out[row.GetId()] = subject
		}
	}
	return out
}

// chatux047PeerPhotos is the photographs the list answer carried, by subject.
func chatux047PeerPhotos(rows []*chatv1.Conversation) map[string]string {
	photos := map[string]string{}
	for _, row := range rows {
		peer := row.GetPeer()
		if photo := strings.TrimSpace(peer.GetAvatarRef()); photo != "" && strings.TrimSpace(peer.GetSubjectId()) != "" {
			photos[strings.TrimSpace(peer.GetSubjectId())] = photo
		}
	}
	return photos
}

// chatux047AllDirectNamed reports whether the answer named the peer of every
// direct conversation the caller is in. When it did, the sidebar needs nothing
// from the agent list to draw its first frame, so the list does not wait for it.
func chatux047AllDirectNamed(rows []*chatv1.Conversation) bool {
	for _, row := range rows {
		if row == nil || !row.GetJoined() || row.GetKind() != chatv1.ConversationKind_CONVERSATION_KIND_DIRECT {
			continue
		}
		peer := row.GetPeer()
		if strings.TrimSpace(peer.GetDisplayName()) == "" {
			return false
		}
	}
	return true
}
