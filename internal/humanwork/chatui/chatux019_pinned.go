package chatui

import (
	"strings"
	"time"
	"unicode/utf8"

	stdhtml "html"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// pinPreviewRunes bounds how much of a pinned message is parsed for its
// preview: the row clips to one line, so more is never seen.
const pinPreviewRunes = 240

// chatux019PinnedSection is the Pinned section of Conversation details. Each
// pinned message is one row that opens the message when pressed, says who
// pinned it and when, and shows its text formatted, not as raw markdown. Copy
// link and Unpin sit under it; the server decides who may unpin.
func chatux019PinnedSection(m Model) ui.Node {
	rows := make([]ui.Node, 0, len(m.ChannelPins))
	for _, pin := range m.ChannelPins {
		copyLabel := chatux005Text(m, "pin_copy")
		if m.PinReferenceUnavailable {
			copyLabel = m.t(KeyPinCopyGuestUnavailable)
		}
		link := []ui.Node{
			html.Strong(html.Props{Class: "pinned-author", Dir: "auto", Text: pin.Author}),
			html.Span(html.Props{Class: "pinned-preview", Dir: "auto"}, chatux019PinPreview(m, pin)...),
		}
		if meta := chatux019PinMeta(m, pin); meta != "" {
			link = append(link, html.Span(html.Props{Class: "pinned-meta", Text: meta}))
		}
		rows = append(rows, html.Li(html.Props{Class: "pinned-row"},
			html.Button(html.Props{Class: "pinned-link", Type: "button", Title: m.t(KeyPinJump), Disabled: m.Callbacks.JumpToPin == nil || pin.Sequence == 0,
				Data: map[string]string{"action": "pin-jump", "id": pin.PostID}}, link...),
			html.Div(html.Props{Class: "pin-actions"},
				actionButton("button secondary small", "pin-copy", pin.PostID, copyLabel, m.Callbacks.CopyPinReference == nil || m.PinReferenceUnavailable, ui.Text(copyLabel)),
				actionButton("button secondary small", "unpin", pin.PostID, m.t(KeyUnpin), m.Callbacks.Unpin == nil, ui.Text(laneText(m, chatux021Copy, keyChatux019PinnedAction))))))
	}
	if len(rows) == 0 {
		return html.Span(html.Props{Class: "pinned-slot"})
	}
	return html.Section(html.Props{ID: "chat-details-pinned", Class: "details-section", TabIndex: -1, Aria: map[string]string{"label": m.t(KeyPinned)}},
		html.H3(html.Props{Text: countedLabel(m, m.t(KeyPinned), len(rows))}),
		html.Ul(html.Props{Class: "pinned-list"}, rows...))
}

// chatux019PinMeta is "Pinned by Walt Brennan · 9:41 AM": who pinned it and
// when, whichever of the two is known.
func chatux019PinMeta(m Model, pin ChannelPin) string {
	name := strings.TrimSpace(pin.PinnedBy)
	when := ""
	if !pin.PinnedAt.IsZero() {
		when = chatux019PinTime(m, pin.PinnedAt)
	}
	vars := map[string]string{"name": name, "when": when}
	switch {
	case name != "" && when != "":
		return laneTextf(m, chatux021Copy, keyChatux019PinnedByWhen, vars)
	case name != "":
		return laneTextf(m, chatux021Copy, keyChatux019PinnedBy, vars)
	case when != "":
		return laneTextf(m, chatux021Copy, keyChatux019PinnedWhen, vars)
	}
	return ""
}

// chatux019PinTime is the clock time for a pin made today and the short date
// with the time for an older one.
func chatux019PinTime(m Model, at time.Time) string {
	local, now := at.Local(), time.Now()
	if local.Year() == now.Year() && local.YearDay() == now.YearDay() {
		return chat5Clock(m.Locale, local)
	}
	return formatShortDate(m.Locale, local) + ", " + chat5Clock(m.Locale, local)
}

// chatux019PinPreview is the first line of the pinned message with its inline
// formatting drawn: bold, italic and code, links as their text. A preview sits
// inside the row's button, so it never holds a link or another control.
func chatux019PinPreview(m Model, pin ChannelPin) []ui.Node {
	body := chatDisplayBody(readerText(m, pin.PostID, pin.Body, pin.Revision))
	return pinPreviewNodes(body)
}

// pinPreviewNodes renders the first line of a markdown body as inline nodes.
func pinPreviewNodes(body string) []ui.Node {
	line := ""
	for _, candidate := range strings.Split(body, "\n") {
		if candidate = strings.TrimSpace(candidate); candidate != "" {
			line = candidate
			break
		}
	}
	if utf8.RuneCountInString(line) > pinPreviewRunes {
		line = string([]rune(line)[:pinPreviewRunes])
	}
	if line == "" {
		return nil
	}
	source := []byte(line)
	root := markdownParser.Parse(text.NewReader(source))
	var block ast.Node
	_ = ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n.(type) {
		case *ast.Paragraph, *ast.TextBlock, *ast.Heading:
			block = n
			return ast.WalkStop, nil
		}
		return ast.WalkContinue, nil
	})
	if block == nil {
		return []ui.Node{ui.Text(line)}
	}
	return pinInline(block, source)
}

func pinInline(parent ast.Node, source []byte) []ui.Node {
	var out []ui.Node
	for child := parent.FirstChild(); child != nil; child = child.NextSibling() {
		switch n := child.(type) {
		case *ast.Text:
			value := string(n.Value(source))
			if !n.IsRaw() {
				value = stdhtml.UnescapeString(string(util.UnescapePunctuations([]byte(value))))
			}
			if n.SoftLineBreak() || n.HardLineBreak() {
				value += " "
			}
			out = append(out, ui.Text(value))
		case *ast.String:
			out = append(out, ui.Text(string(n.Value)))
		case *ast.Emphasis:
			if n.Level == 2 {
				out = append(out, html.Strong(html.Props{}, pinInline(n, source)...))
			} else {
				out = append(out, html.Em(html.Props{}, pinInline(n, source)...))
			}
		case *ast.CodeSpan:
			out = append(out, html.Code(html.Props{}, ui.Text(string(n.Text(source)))))
		case *ast.AutoLink:
			out = append(out, ui.Text(string(n.Label(source))))
		case *ast.RawHTML:
			out = append(out, ui.Text(string(n.Text(source))))
		default:
			out = append(out, pinInline(child, source)...)
		}
	}
	return out
}
