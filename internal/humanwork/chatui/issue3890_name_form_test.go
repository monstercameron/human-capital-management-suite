package chatui

import (
	"strings"
	"testing"
)

// Issue 3890: the form is scrolled fully into the sidebar scroll area.
func TestIssue3890_FormScrolledFullyIntoView(t *testing.T) {
	cases := []struct {
		name                         string
		viewTop, viewBottom, top, bt float64
		want                         float64
	}{
		{"already inside", 100, 500, 150, 250, 0},
		{"clipped above", 100, 500, 60, 160, -48},
		{"label just above the margin", 100, 500, 104, 204, -4},
		{"clipped below", 100, 500, 450, 560, 68},
		{"taller than the area aligns its top", 100, 200, 90, 400, -18},
	}
	for _, c := range cases {
		if got := issue3890ScrollDelta(c.viewTop, c.viewBottom, c.top, c.bt); got != c.want {
			t.Errorf("%s: delta = %v, want %v", c.name, got, c.want)
		}
	}
}

// Issue 3890: cancelling returns focus to the options control that opened the form.
func TestIssue3890_CancelReturnsToOptionsControl(t *testing.T) {
	if got := issue3890ReturnID("sec-1", ""); got != "section:sec-1" {
		t.Fatalf("rename return id = %q", got)
	}
	if got := issue3890ReturnID("", "conv-9"); got != "conv-9" {
		t.Fatalf("move-to-new return id = %q", got)
	}
	if got := issue3890ReturnID("", ""); got != "" {
		t.Fatalf("no form open returned %q", got)
	}
}

// Issue 3890: the id focus returns to is the id of the options button the
// sidebar really draws, and the open rename form sits inside the sidebar's
// scroll area (the area the form is scrolled into).
func TestIssue3890_ReturnTargetIsTheDrawnOptionsButton(t *testing.T) {
	room := Conversation{ID: "sales", Name: "Sales", Kind: PublicChannel}
	m := Model{Locale: "en-US", State: StateReady, Conversations: []Conversation{room},
		Sections:  []SidebarSection{{ID: "mine", Name: "QA Section", Chats: []Conversation{room}}},
		Callbacks: Callbacks{RenameSection: func(string, string) {}, ToggleSection: func(string) {}, OpenRailMenu: func(string) {}}}
	closed := renderNode(t, rail(m, handlers{}))
	want := `data-id="` + issue3890ReturnID("mine", "") + `"`
	if !strings.Contains(closed, "section-more") || !strings.Contains(closed, want) {
		t.Fatalf("the section options button is not drawn with %s", want)
	}
	m.side.renaming = "mine"
	open := renderNode(t, rail(m, handlers{}))
	scroll := strings.Index(open, `class="rail-scroll"`)
	field := strings.Index(open, `id="`+chatside001NameField+`"`)
	if scroll < 0 || field < scroll {
		t.Fatalf("the rename field is not inside the sidebar scroll area (scroll %d, field %d)", scroll, field)
	}
	if strings.Contains(open, want) {
		t.Error("the options button is still drawn beside the open rename form")
	}
}
