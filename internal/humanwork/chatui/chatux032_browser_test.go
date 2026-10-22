package chatui_test

import (
	"math"
	"strings"
	"testing"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	xhtml "golang.org/x/net/html"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// w7Contrast is the WCAG contrast ratio of two #rrggbb colours.
func w7Contrast(a, b string) float64 {
	luminance := func(hex string) float64 {
		hex = strings.TrimPrefix(hex, "#")
		channel := func(s string) float64 {
			v := 0
			for _, r := range s {
				v = v*16 + strings.IndexRune("0123456789abcdef", r)
			}
			c := float64(v) / 255
			if c <= 0.03928 {
				return c / 12.92
			}
			return math.Pow((c+0.055)/1.055, 2.4)
		}
		return 0.2126*channel(hex[0:2]) + 0.7152*channel(hex[2:4]) + 0.0722*channel(hex[4:6])
	}
	la, lb := luminance(a), luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// TestTodo_CHATUX_032_Browser renders the thread beside the details, Saved and
// Moderation in each language: every panel draws the one shared header (the same
// classes, a title, exactly one close button with a name), and the thread's reply
// composer is the channel composer in compact form (one field, one tool row that
// holds the tools, "Also send to #general" as a checkbox and Reply, the formatting
// row closed).
func TestTodo_CHATUX_032_Browser(t *testing.T) {
	also := map[string]string{"en-US": "Also send to #general", "de-DE": "Auch an #general senden", "ar": "إرسال أيضًا إلى #general"}
	for _, locale := range w7Locales {
		page := w7Parse(t, w7Page(t, locale, func(m *chatui.Model) {
			parent := chatui.Message{ID: "p1", AuthorID: "walt", Author: "Walt Brennan", Body: "Parent question", Revision: 1, Replies: 1}
			m.Messages = []chatui.Message{parent}
			m.ShowThread, m.ThreadParentID, m.ThreadParent = true, "p1", &parent
			m.ThreadMessages = []chatui.Message{{ID: "r1", AuthorID: "jake", Author: "Jake Sullivan", Body: "A reply", Revision: 1}}
			m.ComposerFormatRow = "shown" // the channel composer's choice must not open the thread's row
			m.Callbacks.CloseThread = func() {}
		}))
		thread := w7One(t, locale+" thread panel", page, func(n *xhtml.Node) bool { return w7Class(n, "thread-pane") })
		detailsPage := w7Parse(t, w7Page(t, locale, func(m *chatui.Model) { m.ShowDetails = true }))
		details := w7One(t, locale+" details panel", detailsPage, func(n *xhtml.Node) bool { return w7Class(n, "chat-details") && n.Data == "aside" })

		// One header component for the thread, the details, Saved and Moderation.
		saved, err := ui.RenderToString(chatui.RenderSavedMessages(chatui.SavedMessagesView{Locale: locale, TodoCount: 1, AllCount: 1}))
		if err != nil {
			t.Fatal(err)
		}
		panels := map[string]*xhtml.Node{
			"thread":     thread,
			"details":    details,
			"saved":      w7Parse(t, saved),
			"moderation": w7Parse(t, chatui.ModerationFailedMarkup(locale, "queue", "error")),
		}
		for name, panel := range panels {
			head := w7All(panel, func(n *xhtml.Node) bool { return w7Class(n, "side-heading") && w7Class(n, "chat-panel-head") })
			if len(head) != 1 {
				t.Errorf("%s: the %s panel has %d shared headers, want 1", locale, name, len(head))
				continue
			}
			if titles := w7All(head[0], func(n *xhtml.Node) bool { return n.Data == "h2" }); len(titles) == 0 || w7Text(titles[0]) == "" {
				t.Errorf("%s: the %s header has no title", locale, name)
			}
			closes := w7All(head[0], func(n *xhtml.Node) bool { return w7Class(n, "chat-panel-close") })
			if len(closes) != 1 || closes[0].Data != "button" || w7Attr(closes[0], "type") != "button" || (w7Attr(closes[0], "aria-label") == "" && w7Text(closes[0]) == "") {
				t.Errorf("%s: the %s header does not hold exactly one named close button", locale, name)
			}
			// The shared look ships with the page: one height, a keyboard-only focus ring.
		}

		// The thread's reply composer is the channel composer in compact form.
		form := w7One(t, locale+" reply composer", thread, func(n *xhtml.Node) bool { return n.Data == "form" && w7Class(n, "thread-composer") })
		if got := w7Attr(form, "data-format-row"); got != "hidden" {
			t.Errorf("%s: the thread's formatting row is %q while the channel's is shown, want it closed", locale, got)
		}
		if fields := w7All(form, func(n *xhtml.Node) bool {
			return n.Data == "textarea" || (n.Data == "input" && w7Attr(n, "type") == "text")
		}); len(fields) != 1 {
			t.Errorf("%s: the reply composer has %d fields, want one", locale, len(fields))
		}
		toolbar := w7One(t, locale+" reply tool row", form, func(n *xhtml.Node) bool { return w7Class(n, "composer-toolbar") })
		toggle := w7All(toolbar, func(n *xhtml.Node) bool { return w7Class(n, "composer-format-toggle") })
		if len(toggle) != 1 || w7Attr(toggle[0], "aria-pressed") != "false" || w7Attr(toggle[0], "aria-controls") != w7Attr(w7One(t, "format row", form, func(n *xhtml.Node) bool { return w7Class(n, "composer-format-row") }), "id") {
			t.Errorf("%s: the formatting toggle is not an unpressed button for the closed row", locale)
		}
		for _, tool := range []string{"composer-mention-button", "emoji-trigger"} {
			if len(w7All(toolbar, func(n *xhtml.Node) bool {
				return n.Data == "button" && w7Class(n, tool) && w7Attr(n, "aria-label") != ""
			})) != 1 {
				t.Errorf("%s: the reply tool row lacks the named %s button", locale, tool)
			}
		}
		check := w7One(t, locale+" Also send to the channel", toolbar, func(n *xhtml.Node) bool {
			return n.Data == "input" && w7Attr(n, "type") == "checkbox" && w7Attr(n, "name") == "also"
		})
		label := check.Parent
		if label.Data != "label" || !w7Class(label, "thread-also") || w7Text(label) != also[locale] {
			t.Errorf("%s: the choice to also send to the channel reads %q, want %q", locale, w7Text(label), also[locale])
		}
		send := w7One(t, locale+" Reply", toolbar, func(n *xhtml.Node) bool { return n.Data == "button" && w7Class(n, "send-button") })
		if w7Attr(send, "type") != "submit" || !w7Has(send, "disabled") || w7Attr(send, "aria-disabled") != "true" {
			t.Errorf("%s: Reply is not a submit button that is disabled while the field is empty", locale)
		}
	}

	// One disabled look, readable: the muted ink on the soft surface, 3:1 at least.
	for _, want := range []string{".send-button:disabled", ".button:not(.secondary):disabled", "background:var(--soft);border-color:var(--line);color:var(--muted);opacity:1", "height:52px", ".chat-panel-close:focus:not(:focus-visible){outline:none;box-shadow:none}"} {
		if !strings.Contains(chatui.ChatUX032Styles, want) {
			t.Errorf("the shared panel styles lack %q", want)
		}
	}
	if !strings.Contains(chatui.ScopedStylesheet(), ".chat-panel-close") {
		t.Error("the page's stylesheet does not carry the shared panel styles")
	}
	if ratio := w7Contrast("#515861", "#f4f4f2"); ratio < 3 {
		t.Errorf("a disabled button's text is %.2f:1 on its surface, want at least 3:1", ratio)
	}
}
