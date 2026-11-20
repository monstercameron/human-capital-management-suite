package chatui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func issue3938Model(locale string) (Model, Message) {
	people := []string{"sam", "kim", "lee", "ghost"}
	msg := Message{ID: "p1", Chips: []ReactionChip{
		{Emoji: "👍", Count: 5, Mine: true, PeopleIDs: people},
		{Emoji: "👀", Count: 1, PeopleIDs: []string{"kim"}},
	}}
	m := Model{Locale: locale, CurrentUser: "me", CurrentUserName: "Me Myself",
		Members:  []Member{{ID: "sam", Name: "Sam Rivera"}, {ID: "kim", Name: "Kim Park"}, {ID: "lee", Name: "Lee Chen"}},
		Messages: []Message{msg}}
	m.Callbacks.ReactWith = func(string, string) {}
	return m, msg
}

// The View reactions control is its own button: it never carries the toggle action.
func TestIssue3938_ViewControlIsSeparateFromToggle(t *testing.T) {
	m, msg := issue3938Model("en-US")
	row := renderNode(t, reactionRow(m, msg))
	if !strings.Contains(row, `data-action="view-reactions"`) || !strings.Contains(row, `aria-label="View reactions"`) {
		t.Fatalf("no View reactions control: %s", row)
	}
	if strings.Count(row, `data-action="toggle-reaction"`) != 2 {
		t.Errorf("chips must keep toggling: %s", row)
	}
	// Viewing is allowed even when the viewer cannot react.
	m.Callbacks = Callbacks{}
	if row := renderNode(t, reactionRow(m, msg)); strings.Contains(row, `data-action="view-reactions"`) && strings.Contains(row, `data-action="view-reactions" disabled`) {
		t.Errorf("view must not be disabled: %s", row)
	}
	// The concise chip labels are unchanged.
	m, msg = issue3938Model("en-US")
	if row := renderNode(t, reactionRow(m, msg)); !strings.Contains(row, `aria-label="5 people reacted with thumbs up"`) {
		t.Errorf("chip label changed: %s", row)
	}
}

func TestIssue3938_DialogListsPeoplePerReaction(t *testing.T) {
	m, _ := issue3938Model("en-US")
	got := renderNode(t, issue3938Dialog(m, "p1"))
	for _, want := range []string{`role="dialog"`, `aria-modal="true"`, "Who reacted", "You", "Sam Rivera", "Kim Park", "Lee Chen", "thumbs up", "eyes", "does not mean", `data-action="reactions-close"`} {
		if !strings.Contains(got, want) {
			t.Errorf("dialog lacks %q: %s", want, got)
		}
	}
	// One person unknown: honest remainder, never the raw id.
	if strings.Contains(got, "ghost") || !strings.Contains(got, "1 more") {
		t.Errorf("unknown person handling: %s", got)
	}
	if strings.Contains(got, `data-action="toggle-reaction"`) || strings.Contains(got, `data-action="react-`) {
		t.Errorf("opening the list must not offer a toggle: %s", got)
	}
}

func TestIssue3938_DialogIsBounded(t *testing.T) {
	m, msg := issue3938Model("en-US")
	var ids []string
	for i := 0; i < 60; i++ {
		id := fmt.Sprintf("u%d", i)
		ids = append(ids, id)
		m.Members = append(m.Members, Member{ID: id, Name: fmt.Sprintf("Person %d", i)})
	}
	msg.Chips = []ReactionChip{{Emoji: "👍", Count: 60, PeopleIDs: ids}}
	m.Messages = []Message{msg}
	got := renderNode(t, issue3938Dialog(m, "p1"))
	if n := strings.Count(got, "<li"); n != issue3938PageSize {
		t.Errorf("listed %d people, want %d", n, issue3938PageSize)
	}
	if !strings.Contains(got, "35 more") {
		t.Errorf("no honest remainder: %s", got)
	}
}

func TestIssue3938_DialogRechecksAccess(t *testing.T) {
	m, _ := issue3938Model("en-US")
	m.Messages = nil // the message left the visible conversation
	got := renderNode(t, issue3938Dialog(m, "p1"))
	if strings.Contains(got, "Sam Rivera") || !strings.Contains(got, "no longer available") || !strings.Contains(got, `data-action="reactions-close"`) {
		t.Errorf("stale list shown: %s", got)
	}
	m, _ = issue3938Model("en-US")
	m.Messages[0].Chips = nil
	if got := renderNode(t, issue3938Dialog(m, "p1")); !strings.Contains(got, "No reactions yet") {
		t.Errorf("empty state: %s", got)
	}
}

func TestIssue3938_Localised(t *testing.T) {
	for locale, want := range map[string]string{"de-DE": "Wer reagiert hat", "ar": "من تفاعل"} {
		m, _ := issue3938Model(locale)
		got := renderNode(t, issue3938Dialog(m, "p1"))
		if !strings.Contains(got, want) || strings.Contains(got, "Who reacted") {
			t.Errorf("%s: %s", locale, got)
		}
	}
	for key := range issue3938Copy["en-US"] {
		for _, locale := range []string{"de-DE", "ar"} {
			if strings.TrimSpace(issue3938Copy[locale][key]) == "" {
				t.Errorf("%s lacks %s", locale, key)
			}
		}
	}
}

func TestIssue3938_ActionOpensAndClosesWithoutReacting(t *testing.T) {
	m, _ := issue3938Model("en-US")
	reacted := false
	m.Callbacks.ReactWith = func(string, string) { reacted = true }
	m.Callbacks.RemoveReactionWith = func(string, string) { reacted = true }
	local := localStore{box: &localUI{}, tick: ui.UseState(uint64(0))}
	if !issue3938Action(local, "view-reactions", "p1") || local.get().reactionsFor != "p1" {
		t.Fatalf("not opened: %+v", local.get().reactionsFor)
	}
	if !issue3938Action(local, "reactions-close", "") || local.get().reactionsFor != "" || reacted {
		t.Fatalf("not closed or reacted")
	}
	if issue3938Action(local, "toggle-reaction", "p1") {
		t.Errorf("toggle must not be taken")
	}
}

// The open list belongs to one conversation: moving to another closes it, so
// a message id from the old room is never looked up in the new one.
func TestIssue3938_RoomChangeClosesList(t *testing.T) {
	local := localStore{box: &localUI{room: "a"}, tick: ui.UseState(uint64(0))}
	issue3938Action(local, issue3938ViewAction, "p1")
	local.forRoom("b")
	if got := local.get().reactionsFor; got != "" {
		t.Fatalf("list stayed open across rooms: %q", got)
	}
}

// Focus moves into the dialog and Tab stays inside it only for dialogs the
// focus manager knows; the reactions dialog must be one of them.
func TestIssue3938_DialogIsFocusManaged(t *testing.T) {
	if !strings.Contains(chatFocusDialogSelector, ".reactions-dialog") {
		t.Fatalf("focus manager does not know the reactions dialog: %s", chatFocusDialogSelector)
	}
}

// The viewer plus one known person plus one unknown person: names are listed,
// the unknown one is counted honestly and the viewer is not listed twice.
func TestIssue3938_CountsAreHonest(t *testing.T) {
	m, msg := issue3938Model("en-US")
	msg.Chips = []ReactionChip{{Emoji: "👍", Count: 3, Mine: true, PeopleIDs: []string{"sam"}}}
	m.Messages = []Message{msg}
	got := renderNode(t, issue3938Dialog(m, "p1"))
	if strings.Count(got, "<li") != 2 || !strings.Contains(got, "Sam Rivera") || !strings.Contains(got, "1 more") {
		t.Errorf("counts: %s", got)
	}
	msg.Chips = []ReactionChip{{Emoji: "👍", Count: 1, Mine: true}}
	m.Messages = []Message{msg}
	if got := renderNode(t, issue3938Dialog(m, "p1")); strings.Contains(got, "more not listed") || strings.Count(got, "<li") != 1 {
		t.Errorf("own-only reaction: %s", got)
	}
}
