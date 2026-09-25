package main

import (
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// chatNavigationState is the small, non-authoritative part of a chat view that
// is useful to restore when the reader uses browser Back or Forward.
type chatNavigationState struct {
	OwnerTenantID  string
	OwnerSubject   string
	ConversationID string
	ShowDetails    bool
	ShowThread     bool
	ThreadParentID string
	PersonID       string
	FocusMessageID string
	FocusSequence  uint64
}

func chatNavigationStateFromModel(model chatui.Model) chatNavigationState {
	state := chatNavigationState{OwnerTenantID: model.CurrentTenantID, OwnerSubject: model.CurrentUser, ConversationID: model.SelectedID, ShowDetails: model.ShowDetails}
	if model.ShowThread && model.ThreadParentID != "" {
		state.ShowThread = true
		state.ThreadParentID = model.ThreadParentID
	}
	if model.ShowPerson && model.PersonDetails != nil {
		state.PersonID = model.PersonDetails.ID
	}
	if model.FocusMessageID != "" {
		state.FocusMessageID = model.FocusMessageID
	}
	return state
}

func validChatNavigationState(state chatNavigationState) bool {
	if len(state.OwnerTenantID) > 256 || len(state.OwnerSubject) > 256 || len(state.ConversationID) > 256 || len(state.ThreadParentID) > 256 || len(state.PersonID) > 256 || len(state.FocusMessageID) > 256 {
		return false
	}
	if state.OwnerTenantID == "" || state.OwnerSubject == "" {
		return false
	}
	if state.ShowThread && (state.ConversationID == "" || state.ThreadParentID == "") {
		return false
	}
	if state.FocusSequence > 0 && (state.ConversationID == "" || state.FocusMessageID == "") {
		return false
	}
	return true
}

// chatChannelFragmentValue is the value a room is addressed by in the
// "#channel=" fragment of the address bar. NAV-01: a channel is addressed by
// its readable name ("#channel=announcements") when that name identifies
// exactly one room the reader can see and cannot be mistaken for another
// room's id; every other room (direct messages, groups, ambiguous or unnamed
// channels, rooms outside the listing) keeps its stable id. Links pasted into
// messages still use ids (chatui.ChannelReferenceURL) because a name may be
// renamed later; the address bar only needs to survive this session's
// Back/Forward and reload, and resolveChatChannelFragment reads both forms.
func chatChannelFragmentValue(conversations []chatui.Conversation, id string) string {
	if id == "" {
		return ""
	}
	for _, conversation := range conversations {
		if conversation.ID != id {
			continue
		}
		if conversation.Kind != chatui.PublicChannel && conversation.Kind != chatui.PrivateChannel {
			return id
		}
		name := strings.TrimSpace(conversation.Name)
		if name == "" || name != conversation.Name {
			return id
		}
		if resolved, ok := resolveChatChannelFragment(conversations, name); !ok || resolved != id {
			return id
		}
		return name
	}
	return id
}

// resolveChatChannelFragment maps a "#channel=" fragment value back to a
// conversation id. An exact id always wins (backward compatibility with the
// id form and with pasted links); otherwise a channel whose name matches
// case-insensitively and uniquely is the answer. ok is false when the value
// names no listed room; the caller then treats it as an id the server may
// still authorize (a room beyond the first listing page).
func resolveChatChannelFragment(conversations []chatui.Conversation, value string) (string, bool) {
	if value == "" {
		return "", false
	}
	for _, conversation := range conversations {
		if conversation.ID == value {
			return value, true
		}
	}
	match := ""
	for _, conversation := range conversations {
		if conversation.Kind != chatui.PublicChannel && conversation.Kind != chatui.PrivateChannel {
			continue
		}
		if !strings.EqualFold(conversation.Name, value) {
			continue
		}
		if match != "" && match != conversation.ID {
			return value, false
		}
		match = conversation.ID
	}
	if match == "" {
		return value, false
	}
	return match, true
}
