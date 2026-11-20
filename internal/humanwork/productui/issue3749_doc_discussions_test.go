package productui

import (
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func issue3749Render(t *testing.T, props docsDiscussionsPanelProps) string {
	t.Helper()
	out, err := ui.RenderToString(ui.CreateElement(docsDiscussionsPanel, props))
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// The panel lists each conversation as a link to the latest message, with the
// number of messages, and a direct message is named by the page's own word.
func TestIssue3749_DocumentPanelListsTheConversationsAsLinks(t *testing.T) {
	out := issue3749Render(t, docsDiscussionsPanelProps{Locale: "en-US", Discussions: []DocumentDiscussion{
		{ConversationID: "general", Name: "general", Messages: 2, Href: "/workspace/app/chat#channel=general&message=m2&at=9"},
		{ConversationID: "dm", Direct: true, Messages: 1, Href: "/workspace/app/chat#channel=dm&message=m3&at=2"},
	}})
	for _, want := range []string{"Discussed in", `href="/workspace/app/chat#channel=general&amp;message=m2&amp;at=9"`, "general", "2 messages", "Direct message", "1 message", `aria-label="Open conversation general, 2 messages"`, `role="list"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in %s", want, out)
		}
	}
	if strings.Contains(out, "dana, lee") {
		t.Fatalf("a direct message was named by its members: %s", out)
	}
}

// A link that does not stay in Chat is never rendered as a link.
func TestIssue3749_DocumentPanelOnlyLinksIntoChat_Security(t *testing.T) {
	out := issue3749Render(t, docsDiscussionsPanelProps{Locale: "en-US", Discussions: []DocumentDiscussion{
		{ConversationID: "x", Name: "evil", Messages: 1, Href: "https://evil.example/workspace/app/chat"},
		{ConversationID: "y", Name: "also", Messages: 1, Href: "//evil.example/workspace/app/chat"},
		{ConversationID: "z", Name: "js", Messages: 1, Href: "javascript:alert(1)"},
	}})
	if strings.Contains(out, "evil.example") || strings.Contains(out, "javascript:") || strings.Contains(out, "<a ") {
		t.Fatalf("unsafe link rendered: %s", out)
	}
}

func TestIssue3749_DocumentPanelStates(t *testing.T) {
	loading := issue3749Render(t, docsDiscussionsPanelProps{Locale: "en-US", Loading: true})
	if !strings.Contains(loading, `role="status"`) || !strings.Contains(loading, "Loading conversations") {
		t.Fatalf("loading: %s", loading)
	}
	failed := issue3749Render(t, docsDiscussionsPanelProps{Locale: "en-US", Failed: true})
	if !strings.Contains(failed, "could not be loaded") || !strings.Contains(failed, "<button") {
		t.Fatalf("failed: %s", failed)
	}
	empty := issue3749Render(t, docsDiscussionsPanelProps{Locale: "en-US"})
	if !strings.Contains(empty, "No conversations mention this document") || strings.Contains(empty, "<ul") {
		t.Fatalf("empty: %s", empty)
	}
	incomplete := issue3749Render(t, docsDiscussionsPanelProps{Locale: "en-US", Incomplete: true, Discussions: []DocumentDiscussion{{ConversationID: "g", Name: "general", Messages: 1, Href: "/workspace/app/chat#channel=g"}}})
	if !strings.Contains(incomplete, "Some conversations could not be checked") {
		t.Fatalf("incomplete: %s", incomplete)
	}
}

func TestIssue3749_DocumentPanelCopyIsLocalizedAndRTL(t *testing.T) {
	for _, key := range []string{"discussions_heading", "discussions_loading", "discussions_unavailable", "discussions_empty", "discussions_incomplete", "discussions_direct", "discussions_open", "discussions_messages", "discussions_messages_one"} {
		en := docsLibraryCopy["en-US"][key]
		if en == "" {
			t.Fatalf("en-US %s missing", key)
		}
		for _, locale := range []string{"de-DE", "ar"} {
			if got := docsLibraryCopy[locale][key]; got == "" || got == en {
				t.Fatalf("%s %s = %q", locale, key, got)
			}
		}
	}
	out := issue3749Render(t, docsDiscussionsPanelProps{Locale: "ar", Discussions: []DocumentDiscussion{{ConversationID: "g", Name: "general", Messages: 3, Href: "/workspace/app/chat#channel=g"}}})
	if !strings.Contains(out, docsLibraryCopy["ar"]["discussions_heading"]) {
		t.Fatalf("arabic heading missing: %s", out)
	}
	de := issue3749Render(t, docsDiscussionsPanelProps{Locale: "de-DE", Discussions: []DocumentDiscussion{{ConversationID: "g", Name: "general", Messages: 3, Href: "/workspace/app/chat#channel=g"}}})
	if !strings.Contains(de, "3 Nachrichten") {
		t.Fatalf("german count: %s", de)
	}
	css := docsDiscussionsStylesheet()
	if strings.Contains(css, "margin-left") || strings.Contains(css, "padding-right") || !strings.Contains(css, "min-block-size:44px") {
		t.Fatalf("stylesheet must use logical properties and 44px targets: %s", css)
	}
}
