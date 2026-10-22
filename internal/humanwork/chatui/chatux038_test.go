package chatui

import (
	"strings"
	"testing"
)

// TestTodo_CHATUX_038: a hover rule applies only where the device can hover;
// the conversation list's bar carries the workspace menu and no close button on
// a phone; row avatars are centred on their row; and a direct conversation's
// header draws the person's face while the agent list is still being read.
func TestTodo_CHATUX_038(t *testing.T) {
	in := `.a:hover{x:1}.b,.c:hover{y:2}@media(max-width:760px){.d:hover{z:3}}@media(hover:none){.e:hover{w:4}}.f:not(:hover){q:5}` +
		`@keyframes k{from{opacity:0}}.g[data-x="a,b"]:hover,.g:focus-visible{v:6}`
	want := `@media(hover:hover){.a:hover{x:1}}.b{y:2}@media(hover:hover){.c:hover{y:2}}` +
		`@media(max-width:760px){@media(hover:hover){.d:hover{z:3}}}@media(hover:none){.e:hover{w:4}}.f:not(:hover){q:5}` +
		`@keyframes k{from{opacity:0}}.g:focus-visible{v:6}@media(hover:hover){.g[data-x="a,b"]:hover{v:6}}`
	if got := chatux038HoverGuard(in); got != want {
		t.Errorf("the hover guard rewrote\n%s\nas\n%s\nwant\n%s", in, got, want)
	}

	// Every hover rule of the shipped stylesheet is inside a hover media query.
	for _, rule := range s11ParseRules(chatux038HoverGuard(Stylesheet)) {
		if !chatux038IsHover(rule.selector) {
			continue
		}
		guarded := false
		for _, cond := range rule.conds {
			guarded = guarded || strings.Contains(cond, "hover:")
		}
		if !guarded {
			t.Fatalf("a hover rule still applies on touch: %s{%s}", rule.selector, rule.body)
		}
	}
	if !strings.Contains(chatux038HoverGuard(Stylesheet), "@media(hover:hover){.message:hover") {
		t.Error("the message row's hover band is not confined to devices that hover")
	}

	// The list's bar: the workspace menu button is part of the markup, and its
	// style shows it and hides the close button on a phone only.
	if markup := renderNode(t, chatux038WorkspaceMenu(Model{Locale: "en-US"})); !strings.Contains(markup, `data-action="open-workspace-menu"`) || !strings.Contains(markup, "Open workspace menu") {
		t.Errorf("the workspace menu button is %s", markup)
	}
	display := func(selector string) func(string) bool {
		return func(sel string) bool { return strings.HasSuffix(strings.TrimSpace(sel), selector) }
	}
	for _, c := range []struct {
		width          int
		menu, closeBtn string
	}{{390, "inline-flex", "none"}, {1280, "none", "*"}} {
		env := s11Env{width: c.width}
		if got := s11Value(t, env, "display", display(".rail-workspace-menu")); got != c.menu {
			t.Errorf("%dpx: the workspace menu button is display:%q, want %q", c.width, got, c.menu)
		}
		if got := s11Value(t, env, "display", display(".rail-head .mobile-chat-close")); c.closeBtn != "*" && got != c.closeBtn {
			t.Errorf("%dpx: the list's close button is display:%q, want %q", c.width, got, c.closeBtn)
		}
	}

	// Row avatars sit on the row's middle line.
	if got := s11Value(t, s11Env{width: 390}, "align-items", display(".chat-rail-row .chat-row")); got != "center" {
		t.Errorf("a list row aligns its avatar %q, want center", got)
	}

	// The header avatar is the person's from the first paint when their
	// photograph is held, and stays an empty slot when nothing says who it is.
	room := Conversation{ID: "dm-1", Kind: DirectMessage, Name: "Loretta Haynes"}
	person := Model{AgentRailPending: true, PeerIDs: map[string]string{"dm-1": "p-1"}, PhotoURLs: map[string]string{"p-1": "/photo/p-1.png"}}
	if markup := renderNode(t, conversationHeaderAvatar(person, room)); strings.Contains(markup, "agent-icon-pending") || !strings.Contains(markup, "LH") || !strings.Contains(markup, "/photo/p-1.png") {
		t.Errorf("the header avatar of a person is %s, want their initials and photograph", markup)
	}
	unknown := Model{AgentRailPending: true, PeerIDs: map[string]string{"dm-1": "p-1"}}
	if markup := renderNode(t, conversationHeaderAvatar(unknown, room)); !strings.Contains(markup, "agent-icon-pending") {
		t.Errorf("the header avatar of an unknown peer is %s, want the empty slot", markup)
	}
}

// TestTodo_CHATUX_038_Browser is the cascade of the phone list at the three
// widths a phone, a tablet and a laptop have.
func TestTodo_CHATUX_038_Browser(t *testing.T) {
	for _, width := range []int{320, 390, 760} {
		env := s11Env{width: width}
		got := s11Value(t, env, "display", func(sel string) bool {
			return strings.HasSuffix(strings.TrimSpace(sel), ".rail-head .mobile-chat-close")
		})
		if got != "none" {
			t.Errorf("%dpx: the close button of the list is display:%q, want none", width, got)
		}
	}
}
