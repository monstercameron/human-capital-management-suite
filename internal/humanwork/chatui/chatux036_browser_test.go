package chatui_test

import (
	"strings"
	"testing"
	"time"

	xhtml "golang.org/x/net/html"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// TestTodo_CHATUX_036_Browser draws the elements the accent used to colour on a
// page in each language (a link in an answer, a secondary button, a switch, the
// selected row, a timestamp, a status) and reads the rules the page ships for
// them: running-text links and secondary buttons are ink, an off switch is the
// neutral grey and an on switch the accent, the selected row is a light tint,
// help text is at least 12 px, the status is a pill with a dot, and the muted
// help colour keeps 4.5:1 on the rail.
func TestTodo_CHATUX_036_Browser(t *testing.T) {
	for _, locale := range w7Locales {
		page := w7Parse(t, w7Page(t, locale, func(m *chatui.Model) {
			m.Messages = []chatui.Message{{ID: "m1", AuthorID: "policy-helper", Author: "Policy Helper", Body: "See the [handbook](/workspace/app/docs?document=doc-1).", Revision: 1, SentAt: time.Date(2026, 10, 1, 11, 18, 0, 0, time.Local), Sequence: 1}}
			m.ShowCreate = true
			m.Callbacks.SavePreferences = func(chatui.Preferences) {}
			m.ChatFeatures = &chatui.ChatFeatures{Status: true}
			m.ChannelStatuses = map[string]chatui.ChannelStatusView{"general": {Status: chat.ChannelStatus{ConversationID: "general", Name: "general", Status: chatpolicy.StatusArchived}}}
		}))

		if links := w7All(page, func(n *xhtml.Node) bool {
			return n.Data == "a" && w7Inside(n, func(p *xhtml.Node) bool { return w7Class(p, "message-body") })
		}); len(links) != 1 || w7Text(links[0]) != "handbook" {
			t.Errorf("%s: the answer's link is not drawn as a link in the message body", locale)
		}
		secondary := w7All(page, func(n *xhtml.Node) bool { return n.Data == "button" && w7Class(n, "button") && w7Class(n, "secondary") })
		if len(secondary) == 0 {
			t.Errorf("%s: no secondary button is drawn (the Create dialog's Cancel)", locale)
		}
		for _, button := range secondary {
			if w7Text(button) == "" {
				t.Errorf("%s: a secondary button has no label", locale)
			}
		}
		switches := w7All(page, func(n *xhtml.Node) bool {
			return n.Data == "input" && w7Class(n, "switch") && w7Attr(n, "role") == "switch"
		})
		if len(switches) == 0 {
			t.Errorf("%s: the preferences draw no switch", locale)
		}
		for _, sw := range switches {
			if w7Attr(sw, "type") != "checkbox" || (w7Attr(sw, "aria-label") == "" && sw.Parent.Data != "label" && w7Attr(sw, "id") == "") {
				t.Errorf("%s: a switch is not a labelled checkbox", locale)
			}
		}
		rail := w7One(t, locale+" sidebar", page, func(n *xhtml.Node) bool { return w7Class(n, "chat-rail") })
		selected := w7All(rail, func(n *xhtml.Node) bool {
			return n.Data == "button" && w7Class(n, "chat-row") && w7Class(n, "selected")
		})
		if len(selected) != 1 || w7Attr(selected[0], "aria-current") != "true" {
			t.Errorf("%s: the selected row is not one row marked current", locale)
		}
		details := w7Parse(t, w7Page(t, locale, func(m *chatui.Model) {
			m.ShowDetails = true
			m.ChatFeatures = &chatui.ChatFeatures{Status: true}
			m.ChannelStatuses = map[string]chatui.ChannelStatusView{"general": {Status: chat.ChannelStatus{ConversationID: "general", Name: "general", Status: chatpolicy.StatusOpen}}}
		}))
		pills := w7All(details, func(n *xhtml.Node) bool {
			return w7Class(n, "chatstate-badge") && w7Inside(n, func(p *xhtml.Node) bool { return w7Class(p, "chat-details") })
		})
		if len(pills) == 0 {
			t.Errorf("%s: the channel status is not drawn in the details panel", locale)
		}
		for _, pill := range pills {
			dot := w7All(pill, func(n *xhtml.Node) bool { return n.Data == "span" && w7Attr(n, "aria-hidden") == "true" })
			label := w7All(pill, func(n *xhtml.Node) bool { return w7Class(n, "chatstate-label") })
			if len(dot) != 1 || len(label) != 1 || w7Text(label[0]) == "" {
				t.Errorf("%s: a status is not a dot and a label", locale)
			}
		}
		if times := w7All(page, func(n *xhtml.Node) bool { return n.Data == "time" && w7Class(n, "message-time") }); len(times) != 1 || w7Text(times[0]) == "" {
			t.Errorf("%s: a message has no timestamp", locale)
		}
	}

	sheet := strings.ReplaceAll(chatui.ScopedStylesheet(), ":scope", ".chat-workspace")
	for _, want := range []string{
		`.chat-workspace .message-body a,.chat-workspace .agent-reply-answer a{color:var(--ink);text-decoration:underline`,
		`.chat-workspace .button.secondary{color:var(--ink);border-color:var(--hcm-color-control-border)`,
		`:root .chat-workspace :is(.switch,.chatmod-switch-track){border-color:var(--hcm-color-control-border);background:var(--hcm-color-control-border)}`,
		`:root .chat-workspace :is(.switch:checked,.chatmod-switch[aria-checked="true"] .chatmod-switch-track){border-color:var(--accent);background:var(--accent)}`,
		`.chat-row.selected:hover{background:var(--rail-selected);color:var(--ink);font-weight:600`,
		`.chat-workspace .message-time,.chat-workspace .menu-section-title`,
		`.chat-details .chatstate-badge>span[aria-hidden=true]{display:inline-block;inline-size:8px;block-size:8px;border-radius:50%`,
		`.chat-details .details-about-status .chatstate-badge{display:inline-flex;align-items:center;gap:6px;margin:0;padding:2px 8px;border-radius:999px`,
	} {
		if !strings.Contains(sheet, want) {
			t.Errorf("the page's stylesheet lacks %q", want)
		}
	}
	if ratio := w7Contrast("#515861", "#f4f4f2"); ratio < 4.5 {
		t.Errorf("help text on the rail is %.2f:1, want 4.5:1", ratio)
	}
	if ratio := w7Contrast("#515861", "#ffffff"); ratio < 4.5 {
		t.Errorf("help text on the panel is %.2f:1, want 4.5:1", ratio)
	}
}
