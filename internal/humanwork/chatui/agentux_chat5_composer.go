package chatui

import "strings"

type composerKeyState struct {
	Draft                               string
	MentionOpen, MentionHighlighted     bool
	EmojiOpen, EmojiHighlighted         bool
	DocumentOpen, DocumentHighlighted   bool
	CommandOpen, CommandHighlighted     bool
	ChannelOpen, ChannelHighlighted     bool
	SuggestionVisible, Shift, Composing bool
	// MentionLoading is set while the "@" menu waits for the conversation's
	// members: it is open, has nothing to pick yet, and will have rows soon.
	MentionLoading bool
	// MentionAgentHighlighted is set when the highlighted mention is an agent.
	// Tab then opens that agent's details instead of inserting it.
	MentionAgentHighlighted bool
	// EnterNewline is the viewer's choice that Enter adds a line; Command is
	// Ctrl or Cmd held, which then sends (ISSUE-3934).
	EnterNewline, Command bool
}

type composerAction string

const (
	composerNative       composerAction = "native"
	composerPickMention  composerAction = "mention"
	composerPickEmoji    composerAction = "emoji"
	composerPickDocument composerAction = "document"
	composerPickCommand  composerAction = "command"
	composerPickChannel  composerAction = "channel"
	composerSend         composerAction = "send"
	composerEmpty        composerAction = "empty"
	// composerHold swallows Enter while the "@" menu is still loading: the
	// draft is not sent and no newline is typed.
	composerHold composerAction = "hold"
)

// The live menu state owns Enter only while it has a selectable item.
// Closing a menu never leaves a latch that consumes a subsequent send.
func composerKeyAction(state composerKeyState, key string) composerAction {
	if state.Composing || state.Shift || (key != "Enter" && key != "Tab") {
		return composerNative
	}
	if state.EnterNewline && key == "Enter" && state.Command {
		// The explicit send shortcut ignores the open lists: it sends the draft.
		if state.MentionOpen && state.MentionLoading && !state.MentionHighlighted {
			return composerHold
		}
		if !composerMessageReady(strings.TrimSpace(state.Draft)) {
			return composerEmpty
		}
		return composerSend
	}
	if state.MentionOpen && state.MentionHighlighted && !(key == "Tab" && state.MentionAgentHighlighted) {
		return composerPickMention
	}
	if state.EmojiOpen && state.EmojiHighlighted {
		return composerPickEmoji
	}
	if state.DocumentOpen && state.DocumentHighlighted {
		return composerPickDocument
	}
	if state.CommandOpen && state.CommandHighlighted {
		return composerPickCommand
	}
	if state.ChannelOpen && state.ChannelHighlighted {
		return composerPickChannel
	}
	if key != "Enter" || state.EnterNewline {
		// With Enter adding lines, a plain Enter that no list owns is a line.
		return composerNative
	}
	if state.MentionOpen && state.MentionLoading {
		return composerHold
	}
	if !composerMessageReady(strings.TrimSpace(state.Draft)) {
		return composerEmpty
	}
	return composerSend
}

func liveComposerMention(state mentionState, target, value string, caret int) mentionState {
	query, start, found := mentionTokenAt(value, caret)
	if !found || state.Target != target || state.Start != start || state.Query != query {
		return mentionState{}
	}
	return state
}
