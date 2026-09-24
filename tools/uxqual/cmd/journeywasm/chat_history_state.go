package main

import "github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"

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
