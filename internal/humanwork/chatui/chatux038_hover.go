package chatui

import "strings"

// CHATUX-038. A phone has no pointer to hover with, but a browser on one keeps
// the last touched element in the :hover state until the next touch elsewhere,
// so every hover rule left a grey band or a filled button on whatever a finger
// had last landed on. chatux038HoverGuard rewrites a stylesheet so that every
// rule written for :hover applies only where the primary input can hover
// (@media(hover:hover)). Rules that name other states stay as they were, and a
// selector list that mixes hover with other states is split in two, so the
// focus and selected looks keep working on touch.

// chatux038HoverGuard returns css with its hover selectors confined to devices
// that can hover. A rule already inside a hover media query is left alone, as
// are keyframes, font faces and any block it does not understand.
func chatux038HoverGuard(css string) string {
	var out strings.Builder
	out.Grow(len(css) + len(css)/16)
	chatux038Walk(&out, css, false)
	return out.String()
}

func chatux038Walk(out *strings.Builder, css string, insideHover bool) {
	for len(css) > 0 {
		open := chatux038Find(css, '{')
		if open < 0 {
			out.WriteString(css)
			return
		}
		close := chatux038Match(css, open)
		if close < 0 {
			out.WriteString(css)
			return
		}
		prelude, body := css[:open], css[open+1:close]
		css = css[close+1:]
		trimmed := strings.TrimSpace(prelude)
		switch {
		case strings.HasPrefix(trimmed, "@"):
			name := trimmed[1:]
			if end := strings.IndexAny(name, " (\t\r\n"); end >= 0 {
				name = name[:end]
			}
			out.WriteString(prelude)
			out.WriteByte('{')
			switch strings.ToLower(name) {
			case "media", "supports", "container", "layer", "scope":
				chatux038Walk(out, body, insideHover || strings.Contains(trimmed, "hover:"))
			default:
				out.WriteString(body)
			}
			out.WriteByte('}')
		case insideHover || !strings.Contains(prelude, ":hover"):
			out.WriteString(prelude)
			out.WriteByte('{')
			out.WriteString(body)
			out.WriteByte('}')
		default:
			var plain, hover []string
			for _, selector := range chatux038Split(prelude) {
				if chatux038IsHover(selector) {
					hover = append(hover, selector)
				} else {
					plain = append(plain, selector)
				}
			}
			if len(plain) > 0 {
				out.WriteString(strings.Join(plain, ","))
				out.WriteByte('{')
				out.WriteString(body)
				out.WriteByte('}')
			}
			if len(hover) > 0 {
				out.WriteString("@media(hover:hover){")
				out.WriteString(strings.Join(hover, ","))
				out.WriteByte('{')
				out.WriteString(body)
				out.WriteString("}}")
			}
		}
	}
}

// chatux038IsHover reports whether a selector needs a hovering pointer: it
// names :hover other than inside :not(:hover), which is true on touch too.
func chatux038IsHover(selector string) bool {
	return strings.Contains(strings.ReplaceAll(selector, ":not(:hover)", ""), ":hover")
}

// chatux038Split splits a selector list on its top-level commas.
func chatux038Split(list string) []string {
	var parts []string
	depth, from := 0, 0
	for i := 0; i < len(list); i++ {
		switch list[i] {
		case '(', '[':
			depth++
		case ')', ']':
			depth--
		case '"', '\'':
			i = chatux038SkipString(list, i)
		case ',':
			if depth == 0 {
				parts = append(parts, strings.TrimSpace(list[from:i]))
				from = i + 1
			}
		}
	}
	return append(parts, strings.TrimSpace(list[from:]))
}

// chatux038Find returns the index of the first target byte outside strings,
// comments and brackets, or -1.
func chatux038Find(css string, target byte) int {
	for i := 0; i < len(css); i++ {
		switch css[i] {
		case '"', '\'':
			i = chatux038SkipString(css, i)
		case '/':
			if i+1 < len(css) && css[i+1] == '*' {
				end := strings.Index(css[i+2:], "*/")
				if end < 0 {
					return -1
				}
				i += end + 3
			}
		case target:
			return i
		}
	}
	return -1
}

// chatux038Match returns the index of the brace that closes the one at open.
func chatux038Match(css string, open int) int {
	depth := 0
	for i := open; i < len(css); i++ {
		switch css[i] {
		case '"', '\'':
			i = chatux038SkipString(css, i)
		case '/':
			if i+1 < len(css) && css[i+1] == '*' {
				end := strings.Index(css[i+2:], "*/")
				if end < 0 {
					return -1
				}
				i += end + 3
			}
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// chatux038SkipString returns the index of the quote that ends the string
// starting at i.
func chatux038SkipString(css string, i int) int {
	quote := css[i]
	for j := i + 1; j < len(css); j++ {
		switch css[j] {
		case '\\':
			j++
		case quote:
			return j
		}
	}
	return len(css) - 1
}
