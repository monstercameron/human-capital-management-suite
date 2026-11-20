package chatui

import (
	"strings"
	"testing"
)

func issue3894Model(locale string) Model {
	return Model{Locale: locale, SelectedID: "room", CurrentUser: "walt",
		Conversations: []Conversation{{ID: "room", Name: "engineering", Kind: PublicChannel, Joined: true}},
		Messages:      []Message{{ID: "p1", AuthorID: "walt", Author: "Walt", Body: "Kickoff.", TimeLabel: "9:22 PM", Replies: 1}},
		ShowThread:    true, ThreadParentID: "p1",
		ThreadMessages: []Message{{ID: "r1", AuthorID: "ada", Author: "Ada", Body: "On it.", TimeLabel: "9:31 PM"}},
		Callbacks:      walkCallbacks()}
}

// TestIssue3894_PeekStateFollowsThread: View in channel on a narrow screen puts
// the thread aside for that one thread only; closing the thread or opening a
// different one never leaves the channel hidden behind a stale peek.
func TestIssue3894_PeekStateFollowsThread(t *testing.T) {
	m := issue3894Model("en-US")
	if !issue3894Peeking(m, localUI{threadPeek: "p1"}) {
		t.Fatal("thread p1 with a peek for p1 is not peeking")
	}
	if issue3894Peeking(m, localUI{}) {
		t.Fatal("no peek recorded but peeking")
	}
	if issue3894Peeking(m, localUI{threadPeek: "other"}) {
		t.Fatal("a peek for another thread applies to p1")
	}
	m.ShowThread = false
	if issue3894Peeking(m, localUI{threadPeek: "p1"}) {
		t.Fatal("peeking with the thread closed")
	}
}

// TestIssue3894_ReturnBarIsNamedInEveryLanguage: while the channel is showing
// there is a named, type=button way back to the thread, announced politely.
func TestIssue3894_ReturnBarIsNamedInEveryLanguage(t *testing.T) {
	if issue3894Return(issue3894Model("en-US"), localUI{}) != nil {
		t.Fatal("return bar rendered with no peek")
	}
	want := map[string]string{"en-US": "Back to thread", "de-DE": "Zurück zum Thread", "ar": "العودة إلى الموضوع"}
	for locale, label := range want {
		markup := renderNode(t, issue3894Return(issue3894Model(locale), localUI{threadPeek: "p1"}))
		for _, frag := range []string{`data-action="return-to-thread"`, `type="button"`, `aria-live="polite"`, label} {
			if !strings.Contains(markup, frag) {
				t.Errorf("%s return bar missing %q in %s", locale, frag, markup)
			}
		}
	}
}

// TestIssue3894_NarrowStylesHideOnlyTheOverlay: the thread column is set aside
// only inside the narrow rules, so wide layouts keep thread and channel side by side.
func TestIssue3894_NarrowStylesHideOnlyTheOverlay(t *testing.T) {
	for _, want := range []string{
		`@media(max-width:760px){.chat-workspace[data-thread-peek="true"] .chat-side{display:none}`,
		`@container chat (max-width:760px){.chat-workspace[data-thread-peek="true"] .chat-side{display:none}`,
		`.thread-peek-return{display:none}`,
		`min-height:44px`,
	} {
		if !strings.Contains(Stylesheet, want) {
			t.Errorf("stylesheet missing %q", want)
		}
	}
}

// TestIssue3894_WorkspaceCarriesPeekAttribute: the workspace and the thread's
// own View in channel button expose what the CSS and focus need.
func TestIssue3894_WorkspaceCarriesPeekAttribute(t *testing.T) {
	markup := renderNode(t, threadPane(issue3894Model("en-US"), handlers{}))
	if !strings.Contains(markup, `id="thread-view-in-channel"`) {
		t.Fatalf("View in channel has no id for focus return: %s", markup)
	}
}
