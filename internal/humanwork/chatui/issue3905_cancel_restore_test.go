package chatui

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// TestIssue3905_CancelRestoresDraft: Add, Poll (or To-do list) over a message
// the person had already typed turns it into a card draft; Cancel must give the
// message back instead of leaving the composer empty.
func TestIssue3905_CancelRestoresDraft(t *testing.T) {
	for _, kind := range []string{"poll", "todo"} {
		m := composerToolsModel("en-US", false)
		m.Chatcmd002.Post = func(_, _ string, _ chat.Chatcmd002Card, done func(error)) { done(nil) }
		draft := "unset"
		m.Callbacks.DraftChanged = func(_ string, text string) { draft = text }
		local := localStore{box: &localUI{}}
		const before = "Existing draft before poll"

		if !chatcmd003AddOpen(m, local, "chat-composer", kind, before) || !local.get().chatcmd003.Open {
			t.Fatalf("%s: Add did not open the preview", kind)
		}
		// Typing into the card draft must not lose the original either.
		chatcmd003Follow(m, local, "chat-composer", "/"+kind+" Existing draft before poll, more")
		chatcmd003Action(m, local, "chatcmd003-cancel", "", "")
		if state := local.get().chatcmd003; state.Open {
			t.Fatalf("%s: preview still open after Cancel", kind)
		}
		if draft != before {
			t.Fatalf("%s: after Cancel the draft is %q, want %q", kind, draft, before)
		}
	}

	// An empty composer still cancels to an empty composer.
	m := composerToolsModel("en-US", false)
	m.Chatcmd002.Post = func(_, _ string, _ chat.Chatcmd002Card, done func(error)) { done(nil) }
	draft := "unset"
	m.Callbacks.DraftChanged = func(_ string, text string) { draft = text }
	local := localStore{box: &localUI{}}
	chatcmd003AddOpen(m, local, "chat-composer", "poll", "")
	chatcmd003Action(m, local, "chatcmd003-cancel", "", "")
	if draft != "" {
		t.Fatalf("empty composer cancelled to %q", draft)
	}
}
