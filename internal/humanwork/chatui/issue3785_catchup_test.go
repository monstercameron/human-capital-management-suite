package chatui

import (
	"strings"
	"testing"
	"time"
)

func issue3785Model(locale string) Model {
	room := Conversation{ID: "ops", Name: "ops", Kind: PublicChannel, Joined: true}
	return Model{State: StateReady, Locale: locale, CurrentUser: "alice", CurrentTenantID: "tenant", SelectedID: room.ID,
		Conversations: []Conversation{room},
		Messages: []Message{
			{ID: "m1", AuthorID: "bob", Author: "Bob", Body: "first", Sequence: 5, SentAt: time.Now().Add(-time.Hour)},
			{ID: "m2", AuthorID: "carol", Author: "Carol", Body: "second", Sequence: 6, SentAt: time.Now().Add(-time.Minute)},
		},
		UnreadFromID: "m1",
		Callbacks:    Callbacks{RequestCatchUp: func(string, string) {}, DismissCatchUp: func(string) {}}}
}

func issue3785Brief() CatchUpBrief {
	return CatchUpBrief{Phase: CatchUpReady, ConversationID: "ops", Through: 6, Cursor: 4, After: 4, CoveredFrom: 5, Total: 2, Covered: 2, GeneratedAt: time.Now(),
		Sections: []CatchUpSection{
			{Kind: "asked_you", Lines: []CatchUpLine{{PostID: "m1", Sequence: 5, AuthorID: "bob", Text: "Alice, can you review the rota?"}}},
			{Kind: "decisions", More: 3, Lines: []CatchUpLine{{PostID: "m2", Sequence: 6, AuthorID: "carol", Text: "We decided to move the launch."}}},
		}}
}

func TestIssue3785_OfferSitsOnTheUnreadLineAndKeepsItsLabel(t *testing.T) {
	m := issue3785Model("en-US")
	got := renderNodes3737(t, timelineBody(m, handlers{}))
	if !strings.Contains(got, `data-action="catchup-open"`) || !strings.Contains(got, "Catch me up") {
		t.Fatalf("no offer on the unread line: %s", got)
	}
	if !strings.Contains(got, `data-id="m1"`) {
		t.Fatalf("the offer must start from the first unread message: %s", got)
	}
	// With the offer the line must not be a separator that holds a button.
	if strings.Contains(got, `class="unread-divider has-catchup" role="separator"`) {
		t.Fatalf("a separator holds a control: %s", got)
	}
	none := m
	none.Callbacks.RequestCatchUp = nil
	if got := renderNodes3737(t, timelineBody(none, handlers{})); strings.Contains(got, "catchup-open") || !strings.Contains(got, `role="separator"`) {
		t.Fatalf("without a way to ask, the line is as it was: %s", got)
	}
}

func TestIssue3785_CardIsPrivateAttributedAndLinksEachLine(t *testing.T) {
	m := issue3785Model("en-US")
	m.CatchUp = issue3785Brief()
	got := renderNodes3737(t, issue3785Rows(m))
	for _, want := range []string{
		"Only visible to you", "Catch me up",
		"Asked of you", "Possible decisions",
		"Alice, can you review the rota?", "We decided to move the launch.",
		`href="/workspace/app/chat#channel=ops&amp;message=m1&amp;at=5"`, `href="/workspace/app/chat#channel=ops&amp;message=m2&amp;at=6"`,
		"Bob", "Carol",
		"3 more not listed",
		"not confirmed", "Making this did not mark anything as read",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("card lacks %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Approved") || strings.Contains(got, "approval receipt") {
		t.Errorf("the card must not state workflow status: %s", got)
	}
}

func TestIssue3785_CardSaysWhatWasNotCovered(t *testing.T) {
	m := issue3785Model("en-US")
	b := issue3785Brief()
	b.Total, b.Covered, b.Omitted = 450, 300, 150
	m.CatchUp = b
	got := renderNodes3737(t, issue3785Rows(m))
	if !strings.Contains(got, "150 older messages were not read") || !strings.Contains(got, "300 of 450") {
		t.Fatalf("a truncated range must say so: %s", got)
	}
	// Messages that arrived after the brief was taken.
	m.CatchUp = issue3785Brief()
	m.Messages = append(m.Messages, Message{ID: "m3", AuthorID: "bob", Author: "Bob", Body: "late", Sequence: 7}, Message{ID: "m4", AuthorID: "bob", Author: "Bob", Body: "later", Sequence: 8})
	if got := renderNodes3737(t, issue3785Rows(m)); !strings.Contains(got, "2 newer messages arrived after this brief") {
		t.Fatalf("newer messages are not mentioned: %s", got)
	}
}

func TestIssue3785_StatesAreAnnounced(t *testing.T) {
	m := issue3785Model("en-US")
	m.CatchUp = CatchUpBrief{Phase: CatchUpLoading, ConversationID: "ops"}
	if got := renderNodes3737(t, issue3785Rows(m)); !strings.Contains(got, `role="status"`) || !strings.Contains(got, "Reading what you missed") || !strings.Contains(got, `aria-busy="true"`) {
		t.Fatalf("loading: %s", got)
	}
	for phase, want := range map[string]string{"denied": "can no longer be opened", "gone": "no longer available", "unavailable": "could not be made"} {
		m.CatchUp = CatchUpBrief{Phase: CatchUpFailed, ConversationID: "ops", Error: phase}
		got := renderNodes3737(t, issue3785Rows(m))
		if !strings.Contains(got, `role="alert"`) || !strings.Contains(got, want) || !strings.Contains(got, "incomplete") && phase == "unavailable" {
			t.Fatalf("%s: %s", phase, got)
		}
		if strings.Contains(got, "Alice") || strings.Contains(got, "rota") {
			t.Fatalf("a failed brief must not keep earlier content: %s", got)
		}
	}
	m.CatchUp = CatchUpBrief{Phase: CatchUpReady, ConversationID: "ops", Through: 6, Total: 0}
	if got := renderNodes3737(t, issue3785Rows(m)); !strings.Contains(got, "You have not missed anything") {
		t.Fatalf("nothing missed: %s", got)
	}
	m.CatchUp = CatchUpBrief{Phase: CatchUpReady, ConversationID: "ops", Through: 6, Total: 4, Covered: 4, CoveredFrom: 3}
	if got := renderNodes3737(t, issue3785Rows(m)); !strings.Contains(got, "asks you something") {
		t.Fatalf("no highlights: %s", got)
	}
}

func TestIssue3785_CardBelongsToItsConversationAndNeedsTheCallbacks(t *testing.T) {
	m := issue3785Model("en-US")
	m.CatchUp = issue3785Brief()
	m.SelectedID = "other"
	if got := issue3785Rows(m); len(got) != 0 {
		t.Fatal("another conversation's brief is shown here")
	}
	m.SelectedID = "ops"
	m.CatchUp.ThreadRootID = "root"
	if got := issue3785Rows(m); len(got) != 0 {
		t.Fatal("a thread's brief is shown in the channel")
	}
	m.CatchUp.ThreadRootID = ""
	m.Callbacks.DismissCatchUp = nil
	if got := issue3785Rows(m); len(got) != 0 {
		t.Fatal("a card nobody can close is drawn")
	}
}

func TestIssue3785_ThreadHeaderOffersItsOwnBrief(t *testing.T) {
	m := issue3785Model("en-US")
	m.ThreadParentID = "m1"
	m.ThreadMessages = []Message{{ID: "r1", AuthorID: "bob", Author: "Bob", Body: "reply", Sequence: 9}}
	offer := renderNode(t, issue3785ThreadSlot(m))
	if !strings.Contains(offer, `data-action="catchup-open"`) || !strings.Contains(offer, `data-extra="m1"`) {
		t.Fatalf("thread offer: %s", offer)
	}
	m.CatchUp = issue3785Brief()
	m.CatchUp.ThreadRootID = "m1"
	card := renderNode(t, issue3785ThreadSlot(m))
	if !strings.Contains(card, "Possible decisions") || !strings.Contains(card, "Only visible to you") {
		t.Fatalf("thread card: %s", card)
	}
}

func TestIssue3785_Actions(t *testing.T) {
	var asked, dismissed []string
	m := issue3785Model("en-US")
	m.Callbacks.RequestCatchUp = func(thread, since string) { asked = append(asked, thread+"|"+since) }
	m.Callbacks.DismissCatchUp = func(thread string) { dismissed = append(dismissed, thread) }
	if !issue3785Act(m, "catchup-open", "m1", "") || !issue3785Act(m, "catchup-open", "", "root") || !issue3785Act(m, "catchup-close", "", "") || !issue3785Act(m, "catchup-close", "", "root") {
		t.Fatal("the card's own actions were not recognised")
	}
	if strings.Join(asked, ",") != "|m1,root|" || strings.Join(dismissed, ",") != ",root" {
		t.Fatalf("asked %v dismissed %v", asked, dismissed)
	}
	if issue3785Act(m, "something-else", "", "") {
		t.Fatal("a stranger's action was taken")
	}
	// A brief already being read is not asked for twice.
	m.CatchUp = CatchUpBrief{Phase: CatchUpLoading, ConversationID: "ops"}
	asked = nil
	issue3785Act(m, "catchup-open", "m1", "")
	if len(asked) != 0 {
		t.Fatal("asked again while loading")
	}
}

func TestIssue3785_CopyInEveryLanguageAndRTL(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := issue3785Model(locale)
		m.CatchUp = issue3785Brief()
		got := renderNodes3737(t, issue3785Rows(m))
		if strings.Contains(got, "catchup.") || strings.Contains(got, "⟦") || strings.Contains(got, "{") {
			t.Fatalf("%s prints a key or a placeholder: %s", locale, got)
		}
		if !strings.Contains(got, `dir="auto"`) {
			t.Fatalf("%s: a message's words must take their own direction: %s", locale, got)
		}
	}
	de := issue3785Model("de-DE")
	de.CatchUp = issue3785Brief()
	if got := renderNodes3737(t, issue3785Rows(de)); !strings.Contains(got, "Nur für Sie sichtbar") || !strings.Contains(got, "Möglicherweise Entscheidungen") {
		t.Fatalf("de-DE: %s", got)
	}
	ar := issue3785Model("ar")
	ar.CatchUp = issue3785Brief()
	if got := renderNodes3737(t, issue3785Rows(ar)); !strings.Contains(got, "مرئي لك وحدك") {
		t.Fatalf("ar: %s", got)
	}
}

func TestIssue3785_Styles(t *testing.T) {
	for _, want := range []string{".catchup-card", "@media (pointer:coarse)", "min-height:44px", "inline-size", "prefers-reduced-motion"} {
		if !strings.Contains(issue3785Styles, want) {
			t.Errorf("styles lack %q", want)
		}
	}
	if strings.Contains(issue3785Styles, "margin-left") || strings.Contains(issue3785Styles, "padding-right") || strings.Contains(issue3785Styles, "text-align:left") {
		t.Error("physical left/right properties break right-to-left layout")
	}
}

func TestIssue3785_ThreadBriefDoesNotClaimTheMessagesWereMissed(t *testing.T) {
	m := issue3785Model("en-US")
	m.ThreadParentID = "m1"
	m.ThreadMessages = []Message{{ID: "r1", AuthorID: "bob", Author: "Bob", Body: "reply", Sequence: 9}}
	m.CatchUp = issue3785Brief()
	m.CatchUp.ThreadRootID = "m1"
	card := renderNode(t, issue3785ThreadSlot(m))
	if !strings.Contains(card, "2 messages in this thread") || strings.Contains(card, "you missed") {
		t.Fatalf("a thread has no read position, so its brief covers the thread: %s", card)
	}
	m.CatchUp.Sections = nil
	if card := renderNode(t, issue3785ThreadSlot(m)); !strings.Contains(card, "messages of this thread") || strings.Contains(card, "you missed") {
		t.Fatalf("an empty thread brief: %s", card)
	}
}

func TestIssue3785_OneOmittedMessageIsSingular(t *testing.T) {
	m := issue3785Model("en-US")
	m.CatchUp = issue3785Brief()
	m.CatchUp.Total, m.CatchUp.Covered, m.CatchUp.Omitted = 301, 300, 1
	got := renderNodes3737(t, issue3785Rows(m))
	if !strings.Contains(got, "1 older message was not read") || strings.Contains(got, "1 older messages") {
		t.Fatalf("plural for one: %s", got)
	}
}
