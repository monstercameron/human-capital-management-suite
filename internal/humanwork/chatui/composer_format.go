package chatui

import (
	"strings"
	"unicode/utf16"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// composerFormats are the toolbar's Markdown actions, limited to syntax the
// message renderer (markdown.go) turns into formatting.
var composerFormats = []struct{ kind, key, icon string }{
	{"bold", KeyFormatBold, "format-bold"},
	{"italic", KeyFormatItalic, "format-italic"},
	{"code", KeyFormatCode, "format-code"},
	{"link", KeyFormatLink, "link"},
	{"bullets", KeyFormatBullets, "format-list"},
	{"quote", KeyFormatQuote, "format-quote"},
}

// formatSelection applies one Markdown action to the UTF-16 selection
// [start,end) and returns the new text and the selection to leave behind.
// Inline marks wrap the selection (or an empty pair the caret lands inside);
// line marks prefix every selected line; a link selects its URL placeholder.
func formatSelection(value, kind string, start, end int) (string, int, int) {
	units := utf16.Encode([]rune(value))
	if start < 0 {
		start = 0
	}
	if end > len(units) {
		end = len(units)
	}
	if start > end {
		start = end
	}
	selected := string(utf16.Decode(units[start:end]))
	before, after := string(utf16.Decode(units[:start])), string(utf16.Decode(units[end:]))
	u16 := func(s string) int { return len(utf16.Encode([]rune(s))) }
	wrap := func(open, close string) (string, int, int) {
		inner := open + selected + close
		return before + inner + after, start + u16(open), start + u16(open) + u16(selected)
	}
	switch kind {
	case "bold":
		return wrap("**", "**")
	case "italic":
		return wrap("_", "_")
	case "code":
		if strings.Contains(selected, "\n") {
			return wrap("```\n", "\n```")
		}
		return wrap("`", "`")
	case "link":
		label := selected
		if label == "" {
			label = "text"
		}
		out := before + "[" + label + "](url)" + after
		urlStart := start + u16("["+label+"](")
		return out, urlStart, urlStart + u16("url")
	case "bullets", "quote":
		prefix := "- "
		if kind == "quote" {
			prefix = "> "
		}
		lineStart := strings.LastIndex(before, "\n") + 1
		head, body := before[:lineStart], before[lineStart:]+selected
		lines := strings.Split(body, "\n")
		for i, line := range lines {
			if !strings.HasPrefix(line, prefix) {
				lines[i] = prefix + line
			}
		}
		joined := strings.Join(lines, "\n")
		caret := u16(head + joined)
		return head + joined + after, caret, caret
	}
	return value, start, end
}

// formatToolbar renders the composer's formatting buttons. They act on the
// field through the delegated click hook, so they add no event hooks.
func formatToolbar(m Model, target string, disabled bool) ui.Node {
	buttons := make([]ui.Node, 0, len(composerFormats))
	for _, f := range composerFormats {
		label := m.t(f.key)
		buttons = append(buttons, html.Button(html.Props{Class: "tool-button format-button", Type: "button", Disabled: disabled,
			Data: map[string]string{"action": "format", "id": target, "extra": f.kind}, Aria: map[string]string{"label": label}, Title: label}, icon(f.icon)))
	}
	return html.Div(html.Props{Class: "format-tools", Role: "group", Aria: map[string]string{"label": m.t(KeyFormatToolbar)}}, buttons...)
}
