package chatui

import (
	"strings"
	"testing"
)

// s35Chain reports whether sel is a chain of the given class tokens ending in
// last, with only the chat root in front: the selectors that can style that one
// kind of element and nothing else about its surroundings.
func s35Chain(sel, last string, between ...string) bool {
	tokens := strings.Fields(strings.ReplaceAll(strings.ReplaceAll(sel, ":scope", ".chat-workspace"), ">", " "))
	if len(tokens) == 0 || tokens[len(tokens)-1] != last {
		return false
	}
	for _, token := range tokens[:len(tokens)-1] {
		ok := token == ".chat-workspace"
		for _, b := range between {
			ok = ok || token == b
		}
		if !ok {
			return false
		}
	}
	return true
}

// TestTodo_CHATUX_033: at 390 px (and 320, and the 760 px edge) the cascade
// gives a message a 28 px avatar and a text column that starts at 48 px, a
// composer with the add menu, mention and emoji in every state and every control
// at least 44 px, and a wider window keeps its own sizes.
func TestTodo_CHATUX_033(t *testing.T) {
	avatar := func(sel string) bool { return s35Chain(sel, ".avatar", ".message", ".message-list") }
	row := func(sel string) bool { return s35Chain(sel, ".message", ".message-list") }
	for _, width := range []int{320, 390, 760} {
		env := s11Env{width: width}
		if got := s11Value(t, env, "width|inline-size", avatar); got != "28px" {
			t.Errorf("%dpx: a message avatar is %q, want 28px", width, got)
		}
		if got := s11Value(t, env, "grid-template-columns", row); got != "28px minmax(0,1fr)" {
			t.Errorf("%dpx: a message row's columns are %q, want 28px and the text", width, got)
		}
		if got := s11Value(t, env, "column-gap|gap", row); got != "8px" {
			t.Errorf("%dpx: a message row's gap is %q; 12px padding + 28px + 8px starts the text at 48px", width, got)
		}
		if got := s11Value(t, env, "padding", row); got != "6px 12px" {
			t.Errorf("%dpx: a message row's padding is %q; the text column starts at 48px only with 12px at the start", width, got)
		}
	}
	if got := s11Value(t, s11Env{width: 1440}, "grid-template-columns", row); got == "28px minmax(0,1fr)" {
		t.Errorf("1440px: the desktop message row took the phone's columns")
	}

	box := func(state s11ComposerState) func(string) bool {
		return func(sel string) bool {
			core := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(sel, ":not(:focus-within)", ""), ":has(.composer-input:placeholder-shown)", ""))
			return (core == ".chat-composer" || core == ".chat-workspace .chat-composer") && s11InState(sel, state)
		}
	}
	tools := func(state s11ComposerState) func(string) bool {
		return func(sel string) bool {
			isTools := strings.HasSuffix(sel, ".composer-tools") || strings.Contains(sel, ":is(.composer-tools,")
			return isTools && s11Without(sel, "thread-composer") && s11InState(sel, state)
		}
	}
	toolButton := func(state s11ComposerState) func(string) bool {
		return func(sel string) bool {
			return strings.HasSuffix(sel, ".tool-button") && s11Without(sel, "thread-composer", "composer-format-row", "conversation-header", "chatvoice", "chatmap", ">", "[disabled]") && s11InState(sel, state)
		}
	}
	send := func(state s11ComposerState) func(string) bool {
		return func(sel string) bool {
			return strings.HasSuffix(sel, ".send-button") && s11Without(sel, "thread-composer", "chatmod") && s11InState(sel, state)
		}
	}
	states := map[string]s11ComposerState{
		"idle":    {unfocused: true, empty: true},
		"focused": {unfocused: false, empty: true},
		"typed":   {unfocused: true, empty: false},
	}
	for _, width := range []int{320, 390, 599} {
		env := s11Env{width: width}
		for name, state := range states {
			if got := s11Value(t, env, "display", tools(state)); got == "none" {
				t.Errorf("%dpx %s: the add, mention and emoji row is hidden", width, name)
			}
			for _, prop := range []string{"min-width|min-inline-size", "min-height|min-block-size"} {
				if got := s11Value(t, env, prop, toolButton(state)); got != "44px" {
					t.Errorf("%dpx %s: a composer button's %s is %q, want 44px", width, name, prop, got)
				}
				if got := s11Value(t, env, prop, send(state)); got != "44px" {
					t.Errorf("%dpx %s: Send's %s is %q, want 44px", width, name, prop, got)
				}
			}
			if got := s11Value(t, env, "field-sizing", func(sel string) bool {
				return strings.HasSuffix(sel, ".composer-input") && s11Without(sel, "thread-composer", "agent-token") && s11InState(sel, state)
			}); got != "content" {
				t.Errorf("%dpx %s: the field's sizing is %q, want it to grow with its text", width, name, got)
			}
		}
		if got := s11Value(t, env, "flex-direction", box(states["idle"])); got != "row" {
			t.Errorf("%dpx: the idle composer is %q, want one row", width, got)
		}
		if got := s11Value(t, env, "flex-direction", box(states["focused"])); got != "column" {
			t.Errorf("%dpx: the focused composer is %q, want the field over its tools", width, got)
		}
		if got := s11Value(t, env, "order", func(sel string) bool {
			return strings.HasSuffix(sel, ".composer-add") && s11InState(sel, states["idle"])
		}); got != "-1" {
			t.Errorf("%dpx: the idle composer's add menu is at order %q, want it before the field", width, got)
		}
	}

	// Header buttons and the answer card's buttons.
	for _, width := range []int{320, 390} {
		env := s11Env{width: width}
		header := func(sel string) bool {
			return strings.HasSuffix(sel, ".conversation-header .icon-button") && s11Without(sel, "chatux001", "channel-")
		}
		for _, prop := range []string{"width|inline-size", "height|block-size"} {
			if got := s11Value(t, env, prop, header); got != "44px" {
				t.Errorf("%dpx: a header button's %s is %q, want 44px", width, prop, got)
			}
		}
		for _, class := range []string{".agent-feedback-button", ".agent-reply-action", ".jump-newest"} {
			class := class
			for _, prop := range []string{"min-width|min-inline-size", "min-height|min-block-size"} {
				got := s11Value(t, env, prop, func(sel string) bool {
					return strings.HasSuffix(sel, class) && s11Without(sel, "[", ":has", ":not", ":hover")
				})
				if got != "44px" {
					t.Errorf("%dpx: %s %s is %q, want 44px", width, class, prop, got)
				}
			}
		}
	}
	// Text targets get a 44 px hit area from an invisible box, not from layout.
	if !strings.Contains(ChatUX033Styles, ".reaction") || !strings.Contains(ChatUX033Styles, `)::after{content:"";position:absolute;inset:-10px -4px`) {
		t.Error("reactions, links and chips have no extended hit area on a phone")
	}
	// The conversation list is a screen of its own: it fills the chat area.
	rail := func(sel string) bool { return strings.HasSuffix(sel, `[data-sidebar-open="true"] .chat-rail`) }
	if got := s11Value(t, s11Env{width: 390}, "inset", rail); got != "0" {
		t.Errorf("390px: the open conversation list's inset is %q, want the whole chat area", got)
	}
	if got := s11Value(t, s11Env{width: 390}, "width", rail); got != "100%" {
		t.Errorf("390px: the open conversation list is %q wide, want the full width", got)
	}
}
