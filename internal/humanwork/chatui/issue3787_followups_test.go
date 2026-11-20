package chatui

import (
	"strings"
	"testing"
)

func issue3787Model(locale string) Model {
	m := chatux007Model(locale)
	m.FollowUpsLoaded = true
	m.FollowUps = []FollowUpRow{
		{ConversationID: "general", PostID: "p1", Direction: "owed", Channel: "general", InChannel: true, Who: "Dana", Body: "Can you confirm the shift swap?", AgeHours: 30, Sequence: 7},
		{ConversationID: "general", PostID: "p2", Direction: "owed", Channel: "general", InChannel: true, Who: "Eli", Body: "Did you see the rota?", AgeHours: 3, Evidence: "reacted", Sequence: 9},
		{ConversationID: "unread", PostID: "p3", Direction: "waiting", Channel: "Priya", Who: "Priya", Body: "Can you confirm the swap?", AgeHours: 100, Evidence: "posted_later", Sequence: 4, ThreadRootID: "root-1"},
		{ConversationID: "general", PostID: "p4", Direction: "waiting", Channel: "general", InChannel: true, Body: "Who has the key?", AgeHours: 2, Kept: true, Sequence: 12},
	}
	return m
}

func TestIssue3787_ControlIsADisclosureWithTheCount(t *testing.T) {
	m := issue3787Model("en-US")
	markup := renderNode(t, rail(m, handlers{}))
	for _, want := range []string{`data-action="followups-toggle"`, `aria-expanded="false"`, `aria-controls="issue3787-panel"`, "Follow-ups, 4 open"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("missing %q in %s", want, markup)
		}
	}
	// Closed, the region the control names is in the page as an empty hidden
	// element, so aria-controls always resolves; its content is not drawn.
	if !strings.Contains(markup, `<section id="issue3787-panel" hidden`) && !strings.Contains(markup, `hidden id="issue3787-panel"`) {
		t.Fatalf("closed panel is not an empty hidden region: %s", markup)
	}
	if strings.Contains(markup, `class="issue3787-panel"`) {
		t.Fatal("panel content drawn while closed")
	}
}

func TestIssue3787_PanelShowsBothDirectionsWithAgeAndEvidence(t *testing.T) {
	m := issue3787Model("en-US")
	markup := renderNode(t, rail(m, handlers{local: localUI{followUpsOpen: true}}))
	for _, want := range []string{`aria-expanded="true"`, `id="issue3787-panel"`,
		"Waiting on you", "You are waiting on",
		"Dana asked you", "You asked Priya", "You asked a question",
		"Can you confirm the shift swap?", "#general",
		"Waiting 1 d", "Waiting 3 h", "Waiting 4 d",
		// Evidence is a hint the person reviews, never a reason a row vanished.
		"Possible reply", "You reacted, but did not reply.", "Someone else posted next in this conversation.", "Kept open",
		"Nothing is sent to anyone from here",
		`data-action="followups-open"`, `data-action="followups-snooze"`, `data-action="followups-done"`, `data-action="followups-dismiss"`, `data-action="followups-keep"`,
		`data-id="p1"`, `data-extra="owed:general"`, `data-extra="waiting:unread"`,
		"Snooze a day: Can you confirm the shift swap?", "Mark done: Can you confirm the shift swap?",
		"Follow-ups: 2 waiting on you, 2 waiting on others"} {
		if !strings.Contains(markup, want) {
			t.Fatalf("missing %q in %s", want, markup)
		}
	}
	// Keep open is offered only where there is evidence to review and the row is not already kept.
	if got := strings.Count(markup, `data-action="followups-keep"`); got != 2 {
		t.Fatalf("keep open offered on %d rows, want the 2 with a possible reply", got)
	}
	// The row without evidence shows no "Possible reply" chip of its own.
	if got := strings.Count(markup, "Possible reply"); got != 2 {
		t.Fatalf("possible-reply labels = %d", got)
	}
}

func TestIssue3787_EmptyLoadingAndUnavailableStates(t *testing.T) {
	open := handlers{local: localUI{followUpsOpen: true}}
	m := chatux007Model("en-US")
	m.FollowUpsLoaded = true
	markup := renderNode(t, rail(m, open))
	if !strings.Contains(markup, "Nothing needs a follow-up right now") || strings.Contains(markup, "issue3787-count") {
		t.Fatalf("empty state: %s", markup)
	}
	m = chatux007Model("en-US")
	m.FollowUpsLoading = true
	if markup = renderNode(t, rail(m, open)); !strings.Contains(markup, "Loading your follow-ups") || strings.Contains(markup, "Nothing needs a follow-up") {
		t.Fatalf("loading must not read as empty: %s", markup)
	}
	m.FollowUpsLoading, m.FollowUpsError = false, true
	markup = renderNode(t, rail(m, open))
	if !strings.Contains(markup, "could not be loaded") || !strings.Contains(markup, `data-action="followups-retry"`) || strings.Contains(markup, "Nothing needs a follow-up") || strings.Contains(markup, " open</") {
		t.Fatalf("unavailable state must offer a retry and no count: %s", markup)
	}
	// One direction empty still says so.
	m = issue3787Model("en-US")
	m.FollowUps = m.FollowUps[:2]
	if markup = renderNode(t, rail(m, open)); !strings.Contains(markup, "Nothing you asked is still open.") {
		t.Fatalf("empty section: %s", markup)
	}
	m = issue3787Model("en-US")
	m.FollowUpsNote = "failed"
	if markup = renderNode(t, rail(m, open)); !strings.Contains(markup, "That change could not be saved. Try again.") || !strings.Contains(markup, "Can you confirm the shift swap?") {
		t.Fatalf("a refused change must keep the rows and say so: %s", markup)
	}
}

func TestIssue3787_NamesAreNeverIdentifiers(t *testing.T) {
	m := issue3787Model("en-US")
	m.FollowUps = []FollowUpRow{{ConversationID: "c-9f", PostID: "p9", Direction: "owed", Body: "ping", AgeHours: 5}}
	markup := renderNode(t, rail(m, handlers{local: localUI{followUpsOpen: true}}))
	if !strings.Contains(markup, "Someone asked you") || strings.Contains(markup, "c-9f</") {
		t.Fatalf("an unknown asker or conversation must read as plain words: %s", markup)
	}
}

func TestIssue3787_StylesAreInTheServedStylesheet(t *testing.T) {
	if !strings.Contains(Stylesheet, Issue3787Styles) {
		t.Fatal("Issue3787Styles is not part of Stylesheet")
	}
	if !strings.Contains(Issue3787Styles, "min-height:44px") || strings.Contains(Issue3787Styles, "margin-left") || strings.Contains(Issue3787Styles, "text-align:left") {
		t.Fatal("styles must hold 44px targets and use logical properties")
	}
}

func TestIssue3787_CopyIsTranslated(t *testing.T) {
	for locale, want := range map[string]string{"de-DE": "Wartet auf Sie", "ar": "بانتظار ردك"} {
		m := issue3787Model(locale)
		markup := renderNode(t, rail(m, handlers{local: localUI{followUpsOpen: true}}))
		if !strings.Contains(markup, want) || strings.Contains(markup, "Waiting on you") || strings.Contains(markup, "chat.issue3787") {
			t.Fatalf("%s copy: %s", locale, markup)
		}
	}
	for key := range issue3787Copy["en-US"] {
		for _, locale := range []string{"de-DE", "ar"} {
			if strings.TrimSpace(issue3787Copy[locale][key]) == "" {
				t.Fatalf("%s lacks %s", locale, key)
			}
		}
	}
}

// A reply in the thread is shown for review and never closes the row on its own.
func TestIssue3787_ThreadReplyIsReviewableNotClosed(t *testing.T) {
	for locale, want := range map[string]string{"en-US": "Someone replied in the thread", "de-DE": "Jemand hat im Thread geantwortet", "ar": "ردّ أحدهم في السلسلة"} {
		m := issue3787Model(locale)
		m.FollowUps = []FollowUpRow{{ConversationID: "general", PostID: "p9", Direction: "waiting", Channel: "general", InChannel: true, Who: "Priya", Body: "Can you confirm the swap?", AgeHours: 5, Evidence: "replied", Sequence: 3}}
		markup := renderNode(t, rail(m, handlers{local: localUI{followUpsOpen: true}}))
		for _, need := range []string{want, `data-action="followups-keep"`, `data-action="followups-done"`, "issue3787-chip"} {
			if !strings.Contains(markup, need) {
				t.Fatalf("%s: missing %q in %s", locale, need, markup)
			}
		}
	}
}
