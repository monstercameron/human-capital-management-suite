package chatui

import "sync"

// The preview's settings (anonymous votes, who may complete items, the closing
// time) are choices about a draft, so they live as long as the draft does: they
// come back when the person returns to the conversation, and after the chat
// page was replaced by another page and brought back (#3906). The draft's text
// is kept by the draft store; the settings are kept here, for one viewer and
// one conversation, and only for the kind of card they were chosen on. Cancel,
// a post and any text that is not a card line forget them.

type chatcmd003Keep struct {
	Kind     string
	Settings map[string]string
	Typed    bool
}

var chatcmd003Kept = struct {
	sync.Mutex
	by map[string]chatcmd003Keep
}{by: map[string]chatcmd003Keep{}}

func chatcmd003KeepKey(m Model) string { return m.CurrentUser + "\x00" + m.SelectedID }

// chatcmd003Remember keeps state's settings for the viewer's conversation.
func chatcmd003Remember(m Model, state chatcmd003Preview) {
	if state.Target != "chat-composer" || state.Conversation == "" || state.Conversation != m.SelectedID || state.Draft.Card.Kind == "" {
		return
	}
	settings := make(map[string]string, len(state.Settings))
	for name, value := range state.Settings {
		settings[name] = value
	}
	chatcmd003Kept.Lock()
	defer chatcmd003Kept.Unlock()
	chatcmd003Kept.by[chatcmd003KeepKey(m)] = chatcmd003Keep{Kind: state.Draft.Card.Kind, Settings: settings, Typed: state.Typed}
}

// chatcmd003Forget drops what was kept for the viewer's conversation.
func chatcmd003Forget(m Model) {
	chatcmd003Kept.Lock()
	defer chatcmd003Kept.Unlock()
	delete(chatcmd003Kept.by, chatcmd003KeepKey(m))
}

// chatcmd003Recall is the settings kept for a card of kind in the viewer's
// conversation.
func chatcmd003Recall(m Model, kind string) (map[string]string, bool, bool) {
	chatcmd003Kept.Lock()
	defer chatcmd003Kept.Unlock()
	keep, ok := chatcmd003Kept.by[chatcmd003KeepKey(m)]
	if !ok || keep.Kind != kind || len(keep.Settings) == 0 && !keep.Typed {
		return nil, false, false
	}
	settings := make(map[string]string, len(keep.Settings))
	for name, value := range keep.Settings {
		settings[name] = value
	}
	return settings, keep.Typed, true
}

// chatcmd003KeptReset forgets everything kept; tests start from nothing.
func chatcmd003KeptReset() {
	chatcmd003Kept.Lock()
	defer chatcmd003Kept.Unlock()
	clear(chatcmd003Kept.by)
}
