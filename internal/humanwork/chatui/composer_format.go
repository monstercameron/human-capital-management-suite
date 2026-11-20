package chatui

import (
	"strings"
	"unicode"
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
		// #3903: the mark is already there, so a repeat takes it off.
		// Marks that close one span and open the next ("**a** x **b**") are
		// not one pair around the text between them, so those wrap again.
		if len(selected) >= len(open)+len(close) && strings.HasPrefix(selected, open) && strings.HasSuffix(selected, close) &&
			(open == "_" || !strings.Contains(selected[len(open):len(selected)-len(close)], open)) {
			inner := selected[len(open) : len(selected)-len(close)]
			return before + inner + after, start, start + u16(inner)
		}
		if strings.HasSuffix(before, open) && strings.HasPrefix(after, close) && !markWithinWord(before, open) &&
			(open == "_" || strings.Count(before, open)%2 == 1) {
			from := start - u16(open)
			return before[:len(before)-len(open)] + selected + after[len(close):], from, from + u16(selected)
		}
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
		// A selection that takes in the newline ending its last line (a
		// triple click) stops before it, so the line after is not prefixed.
		if strings.HasSuffix(selected, "\n") {
			selected, after = strings.TrimSuffix(selected, "\n"), "\n"+after
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
		// #3913: the next unselected line would read as part of the last
		// list item or quote line, so a blank line keeps it apart.
		if end := strings.Index(after, "\n"); end >= 0 {
			next, _, _ := strings.Cut(after[end+1:], "\n")
			if next != "" && !strings.HasPrefix(next, prefix) {
				after = after[:end] + "\n" + after[end:]
			}
		}
		return head + joined + after, caret, caret
	}
	return value, start, end
}

// markWithinWord reports whether an underscore mark ending before sits against
// a letter or digit, as in snake_case_name, where it is not emphasis.
func markWithinWord(before, open string) bool {
	if open != "_" {
		return false
	}
	rest := []rune(strings.TrimSuffix(before, open))
	return len(rest) > 0 && (unicode.IsLetter(rest[len(rest)-1]) || unicode.IsDigit(rest[len(rest)-1]))
}

// formatToolbar renders the composer's formatting buttons. They act on the
// field through the delegated click hook, so they add no event hooks.
//
// Every tool is its own icon button in one row. The row carries no label word
// and no menu: whether it is on screen at all is the composer's Aa toggle
// (composer_tools.go).
func formatToolbar(m Model, target string, disabled bool) ui.Node {
	return formatToolbarNote(m, target, disabled, "")
}

// formatToolbarNote is formatToolbar with a note that replaces each button's
// tooltip, used to say why the buttons are disabled.
func formatToolbarNote(m Model, target string, disabled bool, note string) ui.Node {
	buttons := []ui.Node{}
	for _, f := range composerFormats {
		label := m.t(f.key)
		tip := label
		if note != "" {
			tip = label + ": " + note
		}
		buttons = append(buttons, html.Button(html.Props{Class: "tool-button format-button", Type: "button", Disabled: disabled,
			Data: map[string]string{"action": "format", "id": target, "extra": f.kind}, Aria: map[string]string{"label": tip}, Title: tip}, icon(f.icon)))
	}
	return html.Div(html.Props{Class: "format-tools"},
		html.Div(html.Props{Class: "format-inline", Role: "group", Aria: map[string]string{"label": m.t(KeyFormatToolbar)}}, buttons...))
}
