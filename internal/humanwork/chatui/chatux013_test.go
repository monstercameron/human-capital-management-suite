package chatui

import (
	"strings"
	"testing"
)

// TestTodo_CHATUX_013 reads the two rules the phone needs: the application bar
// is one row inside a conversation, and the search field's keyboard hint is for
// screens that have a keyboard.
func TestTodo_CHATUX_013(t *testing.T) {
	sheet := ScopedStylesheet()
	if !strings.Contains(sheet, `@media(max-width:760px){body:has(.chat-workspace) .topbar>.header-navigation-tools{display:none}}`) {
		t.Error("the second row of application controls is still drawn above a conversation on a phone")
	}
	if strings.Contains(Stylesheet, ChatUX013Global) {
		t.Error("the application bar rule sits inside the workspace's scope, where it cannot reach the bar")
	}
	if !strings.Contains(Stylesheet, `@media(hover:none) and (pointer:coarse){.chat-workspace .chat-search-shortcut{display:none}}`) {
		t.Error("a touch screen shows a keyboard shortcut")
	}
}

// TestTodo_CHATUX_013_Browser draws the search field and checks the hint is
// there for a keyboard (named for assistive technology by the field, hidden from
// it as decoration) and carries the class the touch rule hides.
func TestTodo_CHATUX_013_Browser(t *testing.T) {
	m := Model{State: StateReady, Locale: "en-US", SelectedID: "room", Conversations: []Conversation{{ID: "room", Name: "general", Kind: PublicChannel, Joined: true}}}
	markup := renderNode(t, Build(m))
	if !strings.Contains(markup, `class="chat-search-shortcut"`) || !strings.Contains(markup, `aria-hidden="true"`) {
		t.Fatal("the search field has no shortcut hint to hide on a phone")
	}
	m.Search = "budget"
	if strings.Contains(renderNode(t, Build(m)), `class="chat-search-shortcut"`) {
		t.Fatal("the hint stays beside words in the box")
	}
}

// TestTodo_CHATUX_013_Band: with the tools row hidden above a conversation on a
// phone, the bar has one row. The shell sets two fixed 44 px rows at 430 px and
// below; left alone, the second stays as an empty band above the conversation
// header, so the chat's rule gives the bar one row.
func TestTodo_CHATUX_013_Band(t *testing.T) {
	sheet := ScopedStylesheet()
	if !strings.Contains(sheet, chatux013BandRule) || !strings.Contains(sheet, ChatUX013Global) {
		t.Fatal("the application bar keeps its second row as empty space")
	}
	for _, want := range []string{
		"body:has(.chat-workspace) .app-shell .topbar",
		"body:has(.chat-workspace) .app-shell.nav-collapsed .topbar",
		"grid-template-rows:minmax(44px,auto)",
		"row-gap:0",
	} {
		if !strings.Contains(chatux013BandRule, want) {
			t.Errorf("the band rule lacks %q", want)
		}
	}
	// It must outrank the shell's own rule, which is written for .app-shell.nav-collapsed .topbar
	// (three classes): the chat's selector carries more.
	if !strings.Contains(chatux013BandRule, ".app-shell.nav-collapsed .topbar") {
		t.Error("the collapsed navigation's bar is not covered")
	}
}
