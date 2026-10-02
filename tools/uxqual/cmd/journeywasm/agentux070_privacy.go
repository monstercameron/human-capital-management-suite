package main

import "github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"

// agentux070ChannelPrivacyState is the channel's requirement on agent answers as
// the directory read just reported it. A directory that carries none (a
// conversation with no agents, or a server without the setting) leaves the page
// with no setting to show. A save still on its way is kept, so a read that lands
// while it goes through does not flicker the switch back.
func agentux070ChannelPrivacyState(payload personaChatDirectory, conversation string, held chatui.ChannelAgentPrivacyState) chatui.ChannelAgentPrivacyState {
	if payload.ChannelPrivacy == nil {
		return chatui.ChannelAgentPrivacyState{}
	}
	state := chatui.ChannelAgentPrivacyState{ConversationID: conversation, Known: true, Private: payload.ChannelPrivacy.Private, CanChange: payload.ChannelPrivacy.CanChange}
	if held.ConversationID == conversation && held.Saving {
		state.Private, state.Saving = held.Private, true
	}
	return state
}

// agentux070ChannelPrivacySaving is the state while a change goes through: the
// switch shows what was asked for and cannot be pressed again.
func agentux070ChannelPrivacySaving(held chatui.ChannelAgentPrivacyState, conversation string, private bool) chatui.ChannelAgentPrivacyState {
	if held.ConversationID != conversation || !held.Known {
		return held
	}
	held.Private, held.Saving, held.Failed = private, true, false
	return held
}

// agentux070ChannelPrivacySaved is the state once the server answered: what it
// said the requirement is, or the state before the change with the failure said.
func agentux070ChannelPrivacySaved(held chatui.ChannelAgentPrivacyState, conversation string, saved *bool, before bool) chatui.ChannelAgentPrivacyState {
	if held.ConversationID != conversation || !held.Known {
		return held
	}
	held.Saving = false
	if saved == nil {
		held.Private, held.Failed = before, true
		return held
	}
	held.Private, held.Failed = *saved, false
	return held
}
