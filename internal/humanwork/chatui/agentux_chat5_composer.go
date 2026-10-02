package chatui

import "strings"

type composerKeyState struct {
	Draft                               string
	MentionOpen, MentionHighlighted     bool
	EmojiOpen, EmojiHighlighted         bool
	DocumentOpen, DocumentHighlighted   bool
	CommandOpen, CommandHighlighted     bool
	SuggestionVisible, Shift, Composing bool
}

type composerAction string

const (
	composerNative       composerAction = "native"
	composerPickMention  composerAction = "mention"
	composerPickEmoji    composerAction = "emoji"
	composerPickDocument composerAction = "document"
	composerPickCommand  composerAction = "command"
	composerSend         composerAction = "send"
	composerEmpty        composerAction = "empty"
)

// The live menu state owns Enter only while it has a selectable item.
// Closing a menu never leaves a latch that consumes a subsequent send.
func composerKeyAction(state composerKeyState, key string) composerAction {
	if state.Composing || state.Shift || (key != "Enter" && key != "Tab") {
		return composerNative
	}
	if state.MentionOpen && state.MentionHighlighted {
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
	if key != "Enter" {
		return composerNative
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
