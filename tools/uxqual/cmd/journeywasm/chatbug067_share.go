package main

import (
	"encoding/json"
	"net/http"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// agentShareOutcome reads the server's answer to "Share to channel"
// (CHATBUG-067). A refusal is something pressing again cannot change, and it
// comes with its reason: the agent answers privately, a source (named in
// detail when the server could name it) is not open to everyone in the channel,
// the answer is too old, or the person is not the one who asked. Anything else
// that is not success is a failure worth trying again.
func agentShareOutcome(status int, state, detail string) (chatui.AgentShareStatus, string, string) {
	switch status {
	case http.StatusOK:
		return chatui.AgentShareShared, "", ""
	case http.StatusConflict:
		switch state {
		case "agent", "channel", "expired":
			// "channel": the channel's administrator requires private answers.
			return chatui.AgentShareRefused, state, ""
		case "audience":
			return chatui.AgentShareRefused, state, detail
		}
	case http.StatusForbidden:
		return chatui.AgentShareRefused, "denied", ""
	}
	return chatui.AgentShareFailed, "", ""
}

// agentShareResult is where an answer stands after the server answered "Share
// to channel": shared, with the message it was shared as (CHATUX-026), refused
// with its reason, or failed.
func agentShareResult(status int, body []byte) chatui.AgentShareState {
	var answer struct {
		PostID string `json:"post_id"`
		State  string `json:"state"`
		Detail string `json:"detail"`
	}
	_ = json.Unmarshal(body, &answer)
	outcome, reason, source := agentShareOutcome(status, answer.State, answer.Detail)
	state := chatui.AgentShareState{Status: outcome, Reason: reason, Source: source}
	if outcome == chatui.AgentShareShared {
		state.PostID = answer.PostID
	}
	return state
}

// chatux026SharedOnOpen brings the page's share states in line with what the
// server said when the conversation's agent activity was opened: an answer the
// server names a shared copy for is shared, and one the page held as shared
// whose copy is gone (removed here or on another page) is private again. An
// answer with a request on its way is left alone. The map it returns is a new
// one when anything changed.
func chatux026SharedOnOpen(share map[string]chatui.AgentShareState, answers []personaChatAnswer) (map[string]chatui.AgentShareState, bool) {
	var next map[string]chatui.AgentShareState
	set := func(invocation string, state chatui.AgentShareState, keep bool) {
		if next == nil {
			next = make(map[string]chatui.AgentShareState, len(share)+1)
			for key, value := range share {
				next[key] = value
			}
		}
		if keep {
			next[invocation] = state
		} else {
			delete(next, invocation)
		}
	}
	for _, answer := range answers {
		if answer.InvocationID == "" {
			continue
		}
		state := share[answer.InvocationID]
		if next != nil {
			state = next[answer.InvocationID]
		}
		switch {
		case state.Status == chatui.AgentShareSharing || state.Status == chatui.AgentShareRemoving:
		case answer.SharedPostID != "" && (state.Status != chatui.AgentShareShared || state.PostID != answer.SharedPostID):
			set(answer.InvocationID, chatui.AgentShareState{Status: chatui.AgentShareShared, PostID: answer.SharedPostID}, true)
		case answer.SharedPostID == "" && state.Status == chatui.AgentShareShared:
			set(answer.InvocationID, chatui.AgentShareState{}, false)
		}
	}
	if next == nil {
		return share, false
	}
	return next, true
}

// chatux026CopyRevision is the revision of the shared copy as the page holds
// it, for the delete that removes it. A copy the page does not hold was never
// edited from this page and is taken to be at its first revision.
func chatux026CopyRevision(model chatui.Model, postID string) uint64 {
	for _, messages := range [][]chatui.Message{model.ThreadMessages, model.Messages} {
		for _, message := range messages {
			if message.ID == postID && message.Revision > 0 {
				return message.Revision
			}
		}
	}
	return 1
}
