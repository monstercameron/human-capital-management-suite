package chatui

import (
	"os"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// UXBLIND-057 addendum (inline unposted Poll/To-do preview): the preview
// advertises "Esc cancels", but its Escape lived only on the composer's own
// key handler while the preview is the composer's sibling. Escape from a
// focused close-time radio or completion-policy choice did nothing. The
// preview slot now carries the same cancellation the composer key offers,
// clearing only the active unposted draft and returning focus to its
// composer; ordinary radio keys are untouched.
func TestTodo_UXBLIND_057_PreviewCancel(t *testing.T) {
	for _, draft := range []string{
		`/poll "Where?" 1="Here" 2="There"`,
		`/todo "Launch" 1="Send invites" 2="Book the room"`,
	} {
		t.Run(draft[:5], func(t *testing.T) {
			m := chatcmd003UIFixture()
			posted := 0
			m.Chatcmd002.Post = func(string, string, chat.Chatcmd002Card, func(error)) { posted++ }
			emptied := "unset"
			m.Callbacks.DraftChanged = func(_ string, text string) { emptied = text }
			local := localStore{box: &localUI{}}
			if !chatcmd003Follow(m, local, "chat-composer", draft) {
				t.Fatalf("typing %q did not open its preview", draft)
			}
			// A descendant of another composer is not this preview's.
			if chatcmd003CancelPreview(ui.KeyboardEvent{}, m, local, "thread-composer") || !local.get().chatcmd003.Open {
				t.Fatal("another composer's Escape reached this preview")
			}
			if !chatcmd003CancelPreview(ui.KeyboardEvent{}, m, local, "chat-composer") {
				t.Fatal("Escape did not cancel the open preview")
			}
			if posted != 0 || local.get().chatcmd003.Open || emptied != "" {
				t.Fatalf("cancel posted=%d open=%v draft=%q; only the active draft clears, and the composer is left empty", posted, local.get().chatcmd003.Open, emptied)
			}
			// Nothing open, nothing happens.
			if chatcmd003CancelPreview(ui.KeyboardEvent{}, m, local, "chat-composer") {
				t.Fatal("Escape with no preview reported a cancellation")
			}
		})
	}
}

// TestTodo_UXBLIND_057_PreviewCancelRecovery pins the recovery path the
// addendum names: the preview's own Cancel control dispatches the same
// cancellation for both card kinds.
func TestTodo_UXBLIND_057_PreviewCancelRecovery(t *testing.T) {
	for _, draft := range []string{
		`/poll "Where?" 1="Here" 2="There"`,
		`/todo "Launch" 1="Send invites"`,
	} {
		m := chatcmd003UIFixture()
		m.Chatcmd002.Post = func(string, string, chat.Chatcmd002Card, func(error)) {}
		local := localStore{box: &localUI{}}
		if !chatcmd003Follow(m, local, "chat-composer", draft) {
			t.Fatalf("typing %q did not open its preview", draft)
		}
		markup := renderNode(t, chatcmd003PreviewView(m, local.get().chatcmd003))
		if !strings.Contains(markup, `data-action="chatcmd003-cancel"`) {
			t.Fatalf("%q preview lost its Cancel recovery: %s", draft[:5], markup)
		}
		chatcmd003Action(m, local, "chatcmd003-cancel", "", "")
		if local.get().chatcmd003.Open {
			t.Fatalf("%q Cancel did not clear the draft", draft[:5])
		}
	}
}

// TestTodo_UXBLIND_057_PreviewEscapeWiring pins the key path a native render
// cannot observe (handlers leave no markup): the preview slot carries a key
// handler on both composers, and that handler is exactly the shared
// cancellation, which answers only to Escape.
func TestTodo_UXBLIND_057_PreviewEscapeWiring(t *testing.T) {
	preview, err := os.ReadFile("chatcmd003_preview.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"onKey ui.Handler",
		"OnKeyDown: onKey",
		`if e.GetKey() != "Escape" || composerIsComposing(e) {`,
	} {
		if !strings.Contains(string(preview), want) {
			t.Errorf("preview lost Escape wiring %q", want)
		}
	}
	shell, err := os.ReadFile("render.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`chatcmd003Key(e, m, local, "chat-composer")`,
		`chatcmd003Key(e, m, local, "thread-composer")`,
		`chatcmd003PreviewFor(m, h.local.chatcmd003, id, h.previewKey)`,
		`chatcmd003PreviewFor(m, h.local.chatcmd003, "thread-composer", h.threadPreviewKey)`,
	} {
		if !strings.Contains(string(shell), want) {
			t.Errorf("composer lost preview Escape wiring %q", want)
		}
	}
}
