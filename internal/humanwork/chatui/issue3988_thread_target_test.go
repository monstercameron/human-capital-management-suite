package chatui

import (
	"strings"
	"testing"
)

func issue3988Model(locale string) Model {
	root := Message{ID: "root", AuthorID: "alice", Author: "Alice", Body: "Launch plan", TimeLabel: "9:30"}
	m := Model{
		State: StateReady, Locale: locale, CurrentTenantID: "tenant", CurrentUser: "reader", SelectedID: "room",
		Conversations: []Conversation{{ID: "room", Name: "design", Kind: PublicChannel, Joined: true}},
		Messages:      []Message{root}, ShowThread: true, ThreadParentID: root.ID, ThreadParent: &root,
		ThreadMessages: []Message{
			{ID: "r1", AuthorID: "bob", Author: "Bob", Body: "Regions can go earlier", TimeLabel: "9:31"},
			{ID: "r2", AuthorID: "cy", Author: "Cy", Body: "Regions can go earlier too", TimeLabel: "9:32"},
		},
		FocusMessageID: "r2",
	}
	return m
}

// TestIssue3988_ThreadReplyTargetIsMarkedAndFocusable: the thread reply a search
// result opened is marked as the target, can take keyboard focus, and has an
// accessible name that says it is the search result; the other replies stay
// ordinary. Holds in every language.
func TestIssue3988_ThreadReplyTargetIsMarkedAndFocusable(t *testing.T) {
	for locale, want := range map[string]string{"en-US": "Search result", "de-DE": "Suchergebnis", "ar": "نتيجة البحث"} {
		m := issue3988Model(locale)
		got := renderNode(t, threadPane(m, handlers{}))
		start := strings.Index(got, `data-message-id="r2"`)
		if start < 0 {
			t.Fatalf("%s: the target reply is not rendered: %s", locale, got)
		}
		open := got[strings.LastIndex(got[:start], "<div"):]
		open = open[:strings.Index(open, ">")]
		for _, need := range []string{"search-target", `tabindex="0"`, `aria-label="` + want} {
			if !strings.Contains(open, need) {
				t.Fatalf("%s: the target reply lacks %q: %s", locale, need, open)
			}
		}
		if !strings.Contains(open, "Cy") {
			t.Fatalf("%s: the name does not say whose reply it is: %s", locale, open)
		}
		other := strings.Index(got, `data-message-id="r1"`)
		tag := got[strings.LastIndex(got[:other], "<div"):]
		tag = tag[:strings.Index(tag, ">")]
		if strings.Contains(tag, "search-target") || strings.Contains(tag, "tabindex") {
			t.Fatalf("%s: a reply that was not the result is marked: %s", locale, tag)
		}
	}
}

// TestIssue3988_ThreadReplyTargetHasVisibleOutline: the highlight rule that
// outlines a timeline hit also reaches a thread reply.
func TestIssue3988_ThreadReplyTargetHasVisibleOutline(t *testing.T) {
	if !strings.Contains(Stylesheet, ".thread-message.search-target") {
		t.Fatal("no outline rule for a targeted thread reply")
	}
}
