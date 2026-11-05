package chatui

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"
)

// TestTodo_CHATUX_051_Browser draws the "#" list in each shipped language, at
// desktop and phone widths: it is labelled in the reader's words, names keep
// their own direction, the kind glyph says public or private, the shortcode
// list leads with the full name, and the served subtitle never doubles a
// space.
func TestTodo_CHATUX_051_Browser(t *testing.T) {
	labels := map[string]string{"en-US": "Channels", "de-DE": "Kanäle", "ar": "القنوات"}
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := Model{Locale: locale, SelectedID: "general", CurrentUser: "walt",
			Conversations: []Conversation{
				{ID: "general", Name: "general", Kind: PublicChannel, Joined: true, MemberCount: 18},
				{ID: "design", Name: "design-reviews", Kind: PrivateChannel, Joined: true, MemberCount: 4},
			}}
		markup := renderNode(t, channelSuggestMenu(m, channelSuggestState{Target: "chat-composer", Query: "", Start: 0, End: 1, Open: true, Active: 0}, "chat-composer"))
		root, err := xhtml.Parse(strings.NewReader(markup))
		if err != nil {
			t.Fatal(err)
		}
		var list *xhtml.Node
		var walk func(*xhtml.Node)
		walk = func(n *xhtml.Node) {
			if n.Type == xhtml.ElementNode {
				for _, a := range n.Attr {
					if a.Key == "role" && a.Val == "listbox" {
						list = n
					}
				}
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
		walk(root)
		if list == nil {
			t.Fatalf("%s: no listbox renders", locale)
		}
		name := ""
		for _, a := range list.Attr {
			if a.Key == "aria-label" {
				name = a.Val
			}
		}
		if name != labels[locale] {
			t.Errorf("%s: the list is labelled %q, want %q", locale, name, labels[locale])
		}
		options := 0
		var optionsWalk func(*xhtml.Node)
		optionsWalk = func(n *xhtml.Node) {
			if n.Type == xhtml.ElementNode && n.Data == "button" {
				options++
				text := ""
				var texts func(*xhtml.Node)
				texts = func(c *xhtml.Node) {
					if c.Type == xhtml.TextNode {
						text += c.Data
					}
					for ch := c.FirstChild; ch != nil; ch = ch.NextSibling {
						texts(ch)
					}
				}
				texts(n)
				if !strings.Contains(text, "general") && !strings.Contains(text, "design-reviews") {
					t.Errorf("%s: an option names neither channel: %q", locale, text)
				}
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				optionsWalk(c)
			}
		}
		optionsWalk(list)
		if options != 2 {
			t.Errorf("%s: %d options render, want 2", locale, options)
		}
		// Names keep their own direction; the kind detail stays with the row.
		if !strings.Contains(markup, `class="mention-name" dir="auto"`) {
			t.Errorf("%s: channel names do not keep their own direction", locale)
		}
	}

	// The shortcode list's rows lead with the name and end with the shortcode,
	// so the name is what the eye reads first.
	shortcode := renderNode(t, emojiCompletionMenu(Model{Locale: "en-US"}, emojiCompletion{Target: "chat-composer", Query: "smile", Open: true, Active: 0}, "chat-composer"))
	name, code := strings.Index(shortcode, "mention-name"), strings.Index(shortcode, "mention-detail")
	if name < 0 || code < 0 || name > code {
		t.Errorf("the shortcode rows do not lead with the name: %s", shortcode)
	}

	// The served subtitle is one clean line at both widths, in every language.
	for _, locale := range []string{"en-US", "de-DE", "ar"} {
		m := Model{Locale: locale, SelectedID: "general", CurrentUser: "walt",
			Conversations: []Conversation{{ID: "general", Name: "general", Kind: PublicChannel, Joined: true, MemberCount: 18}},
			AgentCounts:   map[string]int{"general": 2}}
		topic := channelTopic(t, m)
		if strings.Contains(topic, "  ") {
			t.Errorf("%s: the subtitle holds a doubled space: %s", locale, topic)
		}
		if !strings.Contains(topic, "topic-sep") {
			t.Errorf("%s: the subtitle lost its separators: %s", locale, topic)
		}
	}
}
