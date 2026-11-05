package chatui_test

import (
	"strings"
	"testing"

	xhtml "golang.org/x/net/html"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATGATE_014_Browser renders a channel where one post is restricted
// to a gate segment: the restricted post carries the quiet audience tag and no
// raw copy keys, the sidebar badge shows the served unread count verbatim, and
// the composer offers the "Visible to" select with a restricted option.
func TestTodo_CHATGATE_014_Browser(t *testing.T) {
	for _, locale := range w7Locales {
		page := w7Page(t, locale, func(m *chatui.Model) {
			m.Conversations[1].Unread = 2
			m.Messages = []chatui.Message{
				{ID: "m1", AuthorID: "jake", Author: "Jake Sullivan", Body: "Standup moved to nine.", Revision: 1, TimeLabel: "9:31"},
				{ID: "m2", AuthorID: "walt", Author: "Walt Brennan", Body: "On-call rotation for the weekend.", Revision: 1, TimeLabel: "9:32",
					Audience: &chatui.MessageAudience{Segments: []string{"Denver crew"}, Restricted: true, People: 3}},
			}
			m.GateAudience = map[string]chatui.GateAudienceOptions{
				"general": {Segments: []chatui.GateAudienceChoice{{Name: "Denver crew", People: 3}}, Restricted: true},
			}
		})
		root := w7Parse(t, page)
		tags := w7All(root, func(n *xhtml.Node) bool { return w7Attr(n, "data-gate-audience-tag") == "only" })
		if len(tags) != 1 {
			t.Fatalf("%s: %d restricted audience tags, want 1", locale, len(tags))
		}
		if text := w7Text(tags[0]); text == "" || strings.Contains(text, "{") || strings.Contains(text, "chat.gate014") {
			t.Errorf("%s: the audience tag reads %q", locale, text)
		}
		var badges []string
		for _, badge := range w7All(root, func(n *xhtml.Node) bool { return w7Class(n, "chat-badge") }) {
			badges = append(badges, strings.TrimSpace(w7Text(badge)))
		}
		// Arabic renders Eastern Arabic digits; the count is still exactly two.
		want := map[string]string{"en-US": "2", "de-DE": "2", "ar": "٢"}[locale]
		found := false
		for _, badge := range badges {
			if badge == want {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: no sidebar badge shows the served unread count %q (badges %q)", locale, want, badges)
		}
		sel := w7One(t, locale+" composer", root, func(n *xhtml.Node) bool { return n.Data == "select" && w7Attr(n, "id") == chatui.GateAudienceField })
		restricted := false
		for _, opt := range w7All(sel, func(n *xhtml.Node) bool { return n.Data == "option" }) {
			if strings.HasPrefix(w7Attr(opt, "value"), "only:") {
				restricted = true
			}
		}
		if !restricted {
			t.Errorf("%s: the Visible to select offers no restricted option", locale)
		}
	}
}
