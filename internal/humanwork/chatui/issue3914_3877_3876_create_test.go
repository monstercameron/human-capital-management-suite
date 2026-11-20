package chatui

import (
	"regexp"
	"strings"
	"testing"
)

// lastDeclaration returns the value the cascade ends with for one property
// across every rule whose selector list is exactly selector.
func lastDeclaration(css, selector, property string) string {
	rule := regexp.MustCompile(`(?:^|[}{;])\s*` + regexp.QuoteMeta(selector) + `\s*\{([^}]*)\}`)
	prop := regexp.MustCompile(`(?:^|;)\s*` + regexp.QuoteMeta(property) + `\s*:\s*([^;]+)`)
	value := ""
	for _, r := range rule.FindAllStringSubmatch(css, -1) {
		for _, p := range prop.FindAllStringSubmatch(r[1], -1) {
			value = strings.TrimSpace(p[1])
		}
	}
	return value
}

// TestIssue3914_DialogHeadingIsOpaque: the sticky heading of the create dialog
// must paint over the cards that scroll under it; "inherit" resolved to the
// form's transparent background, so the title was drawn across the cards.
func TestIssue3914_DialogHeadingIsOpaque(t *testing.T) {
	bg := lastDeclaration(Stylesheet, ".chat-dialog .side-heading", "background")
	if bg != "var(--surface)" {
		t.Fatalf("dialog heading background = %q, want the opaque dialog surface", bg)
	}
	if z := lastDeclaration(Stylesheet, ".chat-dialog .side-heading", "z-index"); z != "2" {
		t.Fatalf("dialog heading z-index = %q, want 2 so it stacks over the scrolled cards", z)
	}
	if got := lastDeclaration(Stylesheet, ".chat-dialog", "background"); got != "var(--surface)" {
		t.Fatalf("dialog surface = %q; the heading must match it", got)
	}
}

// TestIssue3877_KindRadioArrowKeys: the conversation type group follows the
// radio pattern: one tab stop on the selected option, arrows move and select.
func TestIssue3877_KindRadioArrowKeys(t *testing.T) {
	cases := []struct {
		from ConversationKind
		key  string
		rtl  bool
		want ConversationKind
	}{
		{PublicChannel, "ArrowRight", false, PrivateChannel},
		{PublicChannel, "ArrowDown", false, PrivateChannel},
		{PrivateChannel, "ArrowDown", false, GroupChat},
		{DirectMessage, "ArrowDown", false, PublicChannel},
		{PublicChannel, "ArrowUp", false, DirectMessage},
		{PublicChannel, "ArrowLeft", false, DirectMessage},
		{PublicChannel, "ArrowLeft", true, PrivateChannel},
		{PublicChannel, "ArrowRight", true, DirectMessage},
		{PublicChannel, "Home", false, PublicChannel},
		{GroupChat, "Home", false, PublicChannel},
		{GroupChat, "End", false, DirectMessage},
	}
	for _, c := range cases {
		got, ok := chatissue3877NextKind(c.from, c.key, c.rtl)
		if !ok || got != c.want {
			t.Errorf("%s from %s (rtl=%v) = %s,%v want %s", c.key, c.from, c.rtl, got, ok, c.want)
		}
	}
	if _, ok := chatissue3877NextKind(PublicChannel, "Tab", false); ok {
		t.Error("Tab must leave the group, not move inside it")
	}
	if _, ok := chatissue3877NextKind(PublicChannel, "Space", false); ok {
		t.Error("Space is the button's own selection")
	}
}

func TestIssue3877_KindRadioRovingTabindex(t *testing.T) {
	m := chatux002Model("en-US")
	m.ShowCreate = true
	markup := renderNode(t, createDialog(m, handlers{local: localUI{createKind: GroupChat}}))
	if n := strings.Count(markup, `role="radio"`); n != 4 {
		t.Fatalf("want 4 radios, got %d: %s", n, markup)
	}
	if n := strings.Count(markup, `tabIndex="-1"`); n != 3 {
		t.Fatalf("the three unselected options must leave the tab order: %s", markup)
	}
	if !strings.Contains(markup, `id="create-kind-group"`) {
		t.Fatalf("options need ids so focus can follow the arrow: %s", markup)
	}
	if !regexp.MustCompile(`create-kind-group[^>]*aria-checked="true"|aria-checked="true"[^>]*create-kind-group`).MatchString(markup) {
		t.Fatalf("group option not checked: %s", markup)
	}
	if !strings.Contains(markup, `name="kind"`) || !strings.Contains(markup, `value="group"`) {
		t.Fatalf("hidden kind field must follow the selection: %s", markup)
	}
}

// TestIssue3876_EscapeClosesSuggestionsFirst: with recipient suggestions open,
// Escape dismisses the list and keeps the dialog and its form.
func TestIssue3876_EscapeClosesSuggestionsFirst(t *testing.T) {
	if got := chatEscapeStep(chatEscapeState{Create: true, PickSuggest: true}); got != "pick-suggest" {
		t.Fatalf("Escape with suggestions open closes %q", got)
	}
	if got := chatEscapeStep(chatEscapeState{Create: true}); got != "create" {
		t.Fatalf("a second Escape closes %q, want the dialog", got)
	}
}

func TestIssue3876_SuggestionsOpenAndDismissed(t *testing.T) {
	m := chatux002Model("en-US")
	m.ShowCreate = true
	open := localUI{createKind: GroupChat, pickQuery: "Camila"}
	if !pickSuggestOpen(m, open, GroupChat) {
		t.Fatal("a typed query opens suggestions")
	}
	dismissed := open
	dismissed.pickDismissed = true
	if pickSuggestOpen(m, dismissed, GroupChat) {
		t.Fatal("dismissed suggestions still count as open")
	}
	if pickSuggestOpen(m, localUI{createKind: GroupChat}, GroupChat) {
		t.Fatal("an empty query has nothing to dismiss")
	}
	shown := renderNode(t, memberPicker(m, handlers{local: open}, GroupChat))
	if !strings.Contains(shown, `id="new-chat-member-options"`) {
		t.Fatalf("open suggestions not drawn: %s", shown)
	}
	hidden := renderNode(t, memberPicker(m, handlers{local: dismissed}, GroupChat))
	if strings.Contains(hidden, `id="new-chat-member-options"`) {
		t.Fatalf("dismissed suggestions still drawn: %s", hidden)
	}
	if !strings.Contains(hidden, `aria-expanded="false"`) || !strings.Contains(shown, `aria-expanded="true"`) {
		t.Fatalf("search box must announce the list's state: %s / %s", shown, hidden)
	}
	if !strings.Contains(hidden, "Camila") {
		t.Fatalf("the search text must survive dismissal: %s", hidden)
	}
}

func TestIssue3876_ResetClearsDismissed(t *testing.T) {
	box := &localUI{pickDismissed: true}
	localStore{box: box}.resetCreate()
	if box.pickDismissed {
		t.Fatal("a fresh dialog starts with suggestions allowed")
	}
}
