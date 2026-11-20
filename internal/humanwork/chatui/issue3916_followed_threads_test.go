package chatui

import (
	"strings"
	"testing"
)

func issue3916Model(locale string) Model {
	m := chatux007Model(locale)
	m.FollowedLoaded = true
	m.FollowedThreads = []FollowedThreadRow{
		{ConversationID: "general", RootPostID: "r1", Channel: "general", InChannel: true, Author: "Ana", Body: "Plans for the offsite", Replies: 3, ActivityLabel: "Today, 3:04 PM", Sequence: 7},
		{ConversationID: "unread", RootPostID: "r2", Channel: "Ben", Author: "Ben", Replies: 0, Removed: true},
	}
	return m
}

func TestIssue3916_ControlIsADisclosureWithTheCount(t *testing.T) {
	m := issue3916Model("en-US")
	markup := renderNode(t, rail(m, handlers{}))
	for _, want := range []string{`data-action="followed-toggle"`, `aria-expanded="false"`, `aria-controls="issue3916-panel"`, "Followed threads, 2 followed"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("missing %q in %s", want, markup)
		}
	}
	// Closed, the region the control names is in the page as an empty hidden
	// element, so aria-controls always resolves; its content is not drawn.
	if !strings.Contains(markup, `<section id="issue3916-panel" hidden`) && !strings.Contains(markup, `hidden id="issue3916-panel"`) {
		t.Fatalf("closed panel is not an empty hidden region: %s", markup)
	}
	if strings.Contains(markup, `class="issue3916-panel"`) {
		t.Fatal("panel content drawn while closed")
	}
}

func TestIssue3916_PanelListsEachThreadWithContextAndControls(t *testing.T) {
	m := issue3916Model("en-US")
	markup := renderNode(t, rail(m, handlers{local: localUI{followedOpen: true}}))
	for _, want := range []string{`aria-expanded="true"`, `id="issue3916-panel"`, "#general", "Plans for the offsite", "3 replies", "Today, 3:04 PM",
		`data-action="followed-open"`, `data-id="r1"`, `data-extra="general"`, `data-action="followed-stop"`, "Stop following",
		"Stop following the thread by Ana in #general", "Open the thread by Ana in #general: Plans for the offsite. 3 replies. Latest activity Today, 3:04 PM",
		"The original message was deleted", "No replies yet", "Followed threads: 2"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("missing %q in %s", want, markup)
		}
	}
	// A deleted root keeps the row but never shows text.
	if strings.Contains(markup, "Plans for the offsite</span><span class=\"issue3916-meta\">Ben") {
		t.Fatalf("unexpected preview")
	}
}

func TestIssue3916_EmptyLoadingAndUnavailableStates(t *testing.T) {
	open := handlers{local: localUI{followedOpen: true}}
	m := chatux007Model("en-US")
	m.FollowedLoaded = true
	markup := renderNode(t, rail(m, open))
	if !strings.Contains(markup, "You are not following any threads yet. To follow one, open a thread and choose Notify me about replies.") || strings.Contains(markup, "issue3916-count") {
		t.Fatalf("empty state: %s", markup)
	}
	m = chatux007Model("en-US")
	m.FollowedLoading = true
	if markup = renderNode(t, rail(m, open)); !strings.Contains(markup, "Loading the threads you follow") || strings.Contains(markup, "not following any") {
		t.Fatalf("loading must not read as empty: %s", markup)
	}
	m.FollowedLoading, m.FollowedError = false, true
	markup = renderNode(t, rail(m, open))
	if !strings.Contains(markup, "could not be loaded") || !strings.Contains(markup, `data-action="followed-retry"`) || strings.Contains(markup, "not following any") || strings.Contains(markup, "followed</") {
		t.Fatalf("unavailable state must offer a retry and no count: %s", markup)
	}
	m = issue3916Model("en-US")
	m.FollowedNote = "unfollow_failed"
	if markup = renderNode(t, rail(m, open)); !strings.Contains(markup, "That thread could not be unfollowed. Try again.") || !strings.Contains(markup, "Plans for the offsite") {
		t.Fatalf("a refused unfollow must keep the row and say so: %s", markup)
	}
}

// The panel's rules must reach the served stylesheet, not just exist as a
// constant: without them the list is unstyled and the phone targets are lost.
func TestIssue3916_StylesAreInTheServedStylesheet(t *testing.T) {
	if !strings.Contains(Stylesheet, Issue3916Styles) {
		t.Fatalf("Issue3916Styles is not part of Stylesheet")
	}
}

func TestIssue3916_CopyIsTranslatedAndInRightToLeftOrder(t *testing.T) {
	for locale, want := range map[string]string{"de-DE": "Nicht mehr verfolgen", "ar": "إيقاف المتابعة"} {
		m := issue3916Model(locale)
		markup := renderNode(t, rail(m, handlers{local: localUI{followedOpen: true}}))
		if !strings.Contains(markup, want) || strings.Contains(markup, "Stop following") || strings.Contains(markup, "chat.issue3916") {
			t.Fatalf("%s copy: %s", locale, markup)
		}
	}
	for key := range issue3916Copy["en-US"] {
		for _, locale := range []string{"de-DE", "ar"} {
			if strings.TrimSpace(issue3916Copy[locale][key]) == "" {
				t.Fatalf("%s lacks %s", locale, key)
			}
		}
	}
	if !strings.Contains(Issue3916Styles, "min-height:44px") || strings.Contains(Issue3916Styles, "margin-left") || strings.Contains(Issue3916Styles, "text-align:left") {
		t.Fatalf("styles must hold 44px targets and use logical properties")
	}
}
