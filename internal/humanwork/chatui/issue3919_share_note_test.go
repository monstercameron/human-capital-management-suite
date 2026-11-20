package chatui

import (
	"strings"
	"testing"
)

func issue3919Model(locale, destination, note string) Model {
	m := issue3918Model(locale)
	m.ShareQuery = ""
	m.ShareDestinations = []Conversation{
		{ID: "eng", Name: "engineering", Kind: PublicChannel, MemberCount: 40},
		{ID: "mgr", Name: "managers", Kind: PrivateChannel, MemberCount: 6},
		{ID: "ana", Name: "Ana Souza", Kind: DirectMessage},
		{ID: "trio", Name: "Launch crew", Kind: GroupChat, MemberCount: 3},
	}
	m.ShareDestinationID, m.ShareNote = destination, note
	m.ShareSource = &Message{ID: "post", Author: "Ari", Body: "Review this", TimeLabel: "9:41"}
	m.Callbacks.ChangeShareNote = func(string) {}
	return m
}

func TestIssue3919_ChooserListsDirectAndGroupConversationsBesideChannels(t *testing.T) {
	markup := render(t, issue3919Model("en-US", "", ""))
	for _, id := range []string{"eng", "mgr", "ana", "trio"} {
		if !strings.Contains(markup, `data-action="share-destination" data-id="`+id+`"`) {
			t.Errorf("destination %s is not offered", id)
		}
	}
	if strings.Contains(markup, "Share message to a channel") {
		t.Error("the dialog still says it shares to a channel only")
	}
}

func TestIssue3919_NoteFieldIsLabelledAndKeepsItsText(t *testing.T) {
	markup := render(t, issue3919Model("en-US", "ana", "Can we talk?"))
	for _, want := range []string{`id="share-note"`, `for="share-note"`, "Add a note (optional)", "Say why you are sharing this", `maxLength="1000"`, "Can we talk?"} {
		if !strings.Contains(markup, want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestIssue3919_PreviewNamesTheExactAudienceNoteAndSource(t *testing.T) {
	cases := []struct{ destination, audience string }{
		{"eng", "Everyone in engineering, 40 people"},
		{"mgr", "Members of managers, 6 people"},
		{"ana", "Only Ana Souza"},
		{"trio", "The 3 people in Launch crew"},
	}
	for _, c := range cases {
		markup := render(t, issue3919Model("en-US", c.destination, "Can we talk?"))
		for _, want := range []string{c.audience, "Your note", "Can we talk?", "Ari", "9:41", `class="share-summary"`} {
			if !strings.Contains(markup, want) {
				t.Errorf("%s: preview misses %q", c.destination, want)
			}
		}
	}
}

func TestIssue3919_PreviewSaysNothingIsSentUntilAChoiceIsMade(t *testing.T) {
	markup := render(t, issue3919Model("en-US", "", "draft"))
	if !strings.Contains(markup, "Nothing is sent until you press Share") {
		t.Fatal("the empty preview does not say that nothing is sent yet")
	}
	if strings.Contains(markup, `class="share-summary"`) {
		t.Fatal("a preview was drawn for no destination")
	}
}

func TestIssue3919_RefusalKeepsTheNoteAndSaysNothingWasSent(t *testing.T) {
	m := issue3919Model("en-US", "ana", "Can we talk?")
	m.ShareError = "Could not share this message. Check your access and try again."
	markup := render(t, m)
	for _, want := range []string{`role="alert"`, "Could not share this message", "Nothing was sent. Your note is still here.", "Can we talk?"} {
		if !strings.Contains(markup, want) {
			t.Errorf("missing %q", want)
		}
	}
	m.ShareNote = ""
	if strings.Contains(render(t, m), "Your note is still here") {
		t.Error("claimed a note was kept when there was none")
	}
}

func TestIssue3919_ShareIsStillOnlyOfferedWithADestination(t *testing.T) {
	markup := render(t, issue3919Model("en-US", "", "a note"))
	i := strings.Index(markup, `data-action="share-submit"`)
	if i < 0 || !strings.Contains(markup[i:], "disabled") {
		t.Fatal("Share is enabled with no destination")
	}
}

func TestIssue3919_EligibleDestinations(t *testing.T) {
	cases := []struct {
		name string
		room Conversation
		want bool
	}{
		{"public", Conversation{ID: "a", Kind: PublicChannel}, true},
		{"private", Conversation{ID: "a", Kind: PrivateChannel}, true},
		{"direct", Conversation{ID: "a", Kind: DirectMessage}, true},
		{"group", Conversation{ID: "a", Kind: GroupChat}, true},
		{"source", Conversation{ID: "src", Kind: PublicChannel}, false},
		{"agent", Conversation{ID: "a", Kind: DirectMessage, Agent: true}, false},
	}
	for _, c := range cases {
		if got := ShareDestinationEligible(c.room, "src"); got != c.want {
			t.Errorf("%s: eligible = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestIssue3919_CopyIsPresentInEveryLanguage(t *testing.T) {
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		markup := render(t, issue3919Model(locale, "ana", "x"))
		for _, bad := range []string{"⟦", "share.note", "issue3919"} {
			if strings.Contains(markup, bad) {
				t.Errorf("%s: placeholder %q in the dialog", locale, bad)
			}
		}
	}
	de := render(t, issue3919Model("de-DE", "ana", "x"))
	for _, want := range []string{"Notiz hinzufügen (optional)", "Nur Ana Souza"} {
		if !strings.Contains(de, want) {
			t.Errorf("de-DE missing %q", want)
		}
	}
	ar := render(t, issue3919Model("ar", "ana", "x"))
	if !strings.Contains(ar, "أضف ملاحظة (اختياري)") {
		t.Error("ar missing the note label")
	}
}

func TestIssue3919_ForwardedMessageShowsTheSharersNoteAboveTheCopy(t *testing.T) {
	m := Model{State: StateReady, SelectedID: "dm", Conversations: []Conversation{{ID: "dm", Name: "Ana", Kind: DirectMessage}},
		Messages: []Message{{ID: "f", AuthorID: "me", Author: "Maya", Body: "launch moved", ForwardedAuthor: "Ben", ForwardedAuthorID: "ben", ForwardNote: "Can we talk about this?"}}}
	markup := render(t, m)
	note := strings.Index(markup, "Can we talk about this?")
	body := strings.Index(markup, "launch moved")
	if note < 0 || body < 0 || note > body {
		t.Fatalf("note at %d, copy at %d; want the note first", note, body)
	}
	for _, want := range []string{`class="forward-note"`, "Note from Maya", "Forwarded from Ben"} {
		if !strings.Contains(markup, want) {
			t.Errorf("missing %q", want)
		}
	}
	m.Messages[0].ForwardNote = ""
	if strings.Contains(render(t, m), "forward-note") {
		t.Error("an empty note drew a note block")
	}
}

// The summary is a status region so the audience is announced when the choice
// changes, but the note in it changes with every key typed and must not be
// read out again each time.
func TestIssue3919_SummaryDoesNotAnnounceEveryKeystrokeOfTheNote(t *testing.T) {
	markup := render(t, issue3919Model("en-US", "ana", "Can we talk?"))
	i := strings.Index(markup, `class="share-summary-note"`)
	if i < 0 {
		t.Fatal("no note line in the summary")
	}
	start := strings.LastIndex(markup[:i], "<dd")
	if end := strings.Index(markup[i:], ">"); start < 0 || end < 0 || !strings.Contains(markup[start:i+end], `aria-live="off"`) {
		t.Fatalf("the note line is not excluded from the live region: %q", markup[start:i+60])
	}
}
