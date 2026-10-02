package main

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATBUG_052 holds the address of the three pages that had none: each
// writes a fragment that reads back to the same page, and the page the address
// names is the one on top.
func TestTodo_CHATBUG_052(t *testing.T) {
	for _, c := range []struct {
		kind, query, fragment string
	}{
		{chatui.ChatPageModeration, "", "#moderation"},
		{chatui.ChatPageSaved, "", "#saved"},
		{chatui.ChatPageSearch, "open enrollment", "#search=open+enrollment"},
		{chatui.ChatPageSearch, "in:#design from:@Walt & more", "#search=in%3A%23design+from%3A%40Walt+%26+more"},
	} {
		if got := chatPageFragment(c.kind, c.query); got != c.fragment {
			t.Errorf("%s %q writes %q, want %q", c.kind, c.query, got, c.fragment)
		}
		if kind, query := parseChatPageFragment(c.fragment); kind != c.kind || query != c.query {
			t.Errorf("%q reads back as %q %q", c.fragment, kind, query)
		}
	}
	if got := chatPageFragment(chatui.ChatPageSearch, "   "); got != "" {
		t.Errorf("an empty search has the address %q", got)
	}
	for _, hash := range []string{"", "#channel=design", "#search=", "#search=%zz", "#moderation2", "#person=walt"} {
		if kind, _ := parseChatPageFragment(hash); kind != "" {
			t.Errorf("%q is read as the %s page", hash, kind)
		}
	}

	for _, c := range []struct {
		moderation, saved, opened bool
		search, kind, query       string
	}{
		{false, false, false, "", "", ""},
		{true, true, false, "budget", chatui.ChatPageModeration, ""},
		{false, true, false, "budget", chatui.ChatPageSearch, "budget"},
		{false, true, true, "budget", chatui.ChatPageSaved, ""},
		{false, false, true, "budget", "", ""},
		{false, true, false, "  ", chatui.ChatPageSaved, ""},
	} {
		if kind, query := chatPageShowing(c.moderation, c.saved, c.search, c.opened); kind != c.kind || query != c.query {
			t.Errorf("%+v shows %q %q", c, kind, query)
		}
	}
}

// TestTodo_CHATBUG_052_Browser follows the tab title through the moves the
// review made: into the conversation with an agent, out to a channel, then to
// Moderation and a search. The title always names what is shown, and a title
// the shell sets for another page becomes the new base.
func TestTodo_CHATBUG_052_Browser(t *testing.T) {
	document, last, base := "Chat · Iron Bank", "", ""
	apply := func(shown string) string {
		base = chatTabTitleBase(document, last, base)
		document = chatTabTitleText(base, shown)
		last = document
		return document
	}
	for _, c := range []struct{ shown, want string }{
		{"Policy Helper", "Policy Helper · Chat · Iron Bank"},
		{"#random", "#random · Chat · Iron Bank"},
		{"Moderation", "Moderation · Chat · Iron Bank"},
		{"Search: open enrollment", "Search: open enrollment · Chat · Iron Bank"},
		{"", "Chat · Iron Bank"},
	} {
		if got := apply(c.shown); got != c.want {
			t.Fatalf("showing %q the tab reads %q, want %q", c.shown, got, c.want)
		}
	}
	// The shell moves to another page and back: its title is the new base.
	document = "People · Iron Bank"
	if got := apply("#random"); got != "#random · People · Iron Bank" {
		t.Fatalf("after the shell changed the title: %q", got)
	}
}
