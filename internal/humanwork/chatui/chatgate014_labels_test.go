package chatui

import (
	"regexp"
	"strings"
	"testing"
)

// TestTodo_CHATGATE_014: Conversation details lists the labels a channel's
// joining questions gave and who holds each, in English, German and Arabic: the
// heading and the plain sentence that a label is only a name, one row per label
// with its holders by the name shown (the reader marked), a long list cut with a
// Show all button, people who are not in the member list left out, and nothing
// at all for a channel without labels.
func TestTodo_CHATGATE_014(t *testing.T) {
	m := chatux030Model(8)
	m.CurrentUser = "p1"
	if page := chatux021Details(t, m, handlers{}); strings.Contains(page, `id="chat-details-labels"`) {
		t.Fatal("a channel without labels shows a Labels section")
	}
	room := m.SelectedID
	m.GateLabels = map[string][]GateLabel{room: {
		{Name: "On call", People: []string{"p3", "p1", "gone"}},
		{Name: "Denver crew", People: []string{"p0", "p1", "p2", "p3", "p4", "p5", "p6"}},
		{Name: "Nobody here", People: []string{"gone"}},
	}}
	page := chatux021Details(t, m, handlers{})
	section := page[strings.Index(page, `id="chat-details-labels"`):]
	section = section[:strings.Index(section, "</section>")]
	if !strings.Contains(section, ">Labels<") || !strings.Contains(section, "A label is only a name: it gives no access.") {
		t.Errorf("the heading or the note is missing: %s", section)
	}
	if got := strings.Count(section, `class="details-label-row"`); got != 2 {
		t.Errorf("%d label rows, want 2 (a label whose holders have all left is not listed)", got)
	}
	if !regexp.MustCompile(`>Person 03, Person 01 \(You\)<`).MatchString(section) && !regexp.MustCompile(`>Person 01 \([^)]*\), Person 03<`).MatchString(section) {
		t.Errorf("the holders of On call are not listed by name, sorted, with the reader marked: %s", section)
	}
	if strings.Contains(section, "gone") {
		t.Errorf("a person who is not in the member list is named: %s", section)
	}
	if !strings.Contains(section, ">Show all 7<") || strings.Contains(section, "Person 05") {
		t.Errorf("a label with seven holders shows %s", section)
	}
	open := chatux021Details(t, m, handlers{local: localUI{detailGroups: map[string]bool{"gate-label-Denver crew": true}}})
	if !strings.Contains(open, "Person 05") || !strings.Contains(open, ">Show fewer<") {
		t.Error("an opened label does not list everyone with Show fewer")
	}
	// The section sits between the joining row and the member list.
	joining, labels, members := strings.Index(page, ">Joining questions<"), strings.Index(page, `id="chat-details-labels"`), strings.Index(page, `id="chat-details-members"`)
	if !(joining >= 0 && joining < labels && labels < members) {
		t.Errorf("order is joining %d, labels %d, members %d", joining, labels, members)
	}
	// Every language has both strings, so no key is printed.
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		l := Model{Locale: locale}
		for _, key := range []string{chatgate014KeyHeading, chatgate014KeyNote} {
			if text := laneText(l, chatgate014Copy, key); text == "" || strings.HasPrefix(text, "⟦") || strings.Contains(text, "chat.gate014") {
				t.Errorf("%s: %s reads %q", locale, key, text)
			}
		}
	}
	// Arabic and German read in their own words.
	de := m
	de.Locale = "de-DE"
	if page := chatux021Details(t, de, handlers{}); !strings.Contains(page, "Ein Label ist nur ein Name") {
		t.Error("the German note is not shown")
	}
	ar := m
	ar.Locale = "ar"
	if page := chatux021Details(t, ar, handlers{}); !strings.Contains(page, "التسمية مجرد اسم") {
		t.Error("the Arabic note is not shown")
	}
}
