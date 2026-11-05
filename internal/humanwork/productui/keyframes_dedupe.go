package productui

import (
	"strings"
)

// dedupeKeyframesBlocks drops earlier copies of byte-identical @keyframes
// blocks, keeping the last. The typed stylesheets declare shared animations
// (hcm-item-enter, hcm-shimmer and others) once per using selector, and each
// declaration hoists its own @keyframes block with the same content-hashed
// name; the per-site animation-name declaration is needed, but only the last
// same-named block can ever win, so every earlier identical copy is dead
// weight on every page load. Only blocks with identical names and identical
// bodies (up to whitespace) are dropped, and only within the same at-rule
// context: same-named blocks with different bodies and blocks under different
// @media queries are left untouched. Anything that does not scan cleanly is
// returned unchanged.
//
// It runs once over each cached platform stylesheet in initPlatformStyles,
// never per request. TestTodo_CHATUX_046_DuplicateRules pins the served
// sheets, so a duplicate this misses fails loudly there instead.
func dedupeKeyframesBlocks(sheet string) string {
	type block struct {
		start, end int
		key        string
	}
	type frame struct {
		depth int
		text  string
	}
	var blocks []block
	var context []frame
	depth := 0
	pos := 0
	for pos < len(sheet) {
		switch {
		case sheet[pos] == '/' && pos+1 < len(sheet) && sheet[pos+1] == '*':
			end := strings.Index(sheet[pos+2:], "*/")
			if end < 0 {
				return sheet
			}
			pos += end + 4
		case sheet[pos] == '"' || sheet[pos] == '\'':
			pos = keyframesQuotedEnd(sheet, pos)
		case sheet[pos] == '{':
			depth++
			pos++
		case sheet[pos] == '}':
			if depth > 0 {
				depth--
			}
			for len(context) > 0 && context[len(context)-1].depth >= depth {
				context = context[:len(context)-1]
			}
			pos++
		case sheet[pos] == '@' && keyframesAt(sheet, pos):
			start := pos
			namePos := pos + len("@keyframes")
			nameEnd := namePos
			for nameEnd < len(sheet) && sheet[nameEnd] != '{' && sheet[nameEnd] != ';' && sheet[nameEnd] != '}' {
				if sheet[nameEnd] == '"' || sheet[nameEnd] == '\'' {
					nameEnd = keyframesQuotedEnd(sheet, nameEnd)
					continue
				}
				nameEnd++
			}
			if nameEnd >= len(sheet) || sheet[nameEnd] != '{' {
				pos++
				continue
			}
			end := keyframesBlockEnd(sheet, nameEnd)
			if end < 0 {
				return sheet
			}
			var ancestors []string
			for _, entry := range context {
				ancestors = append(ancestors, entry.text)
			}
			key := strings.Join(ancestors, "\x00") + "\x00" +
				keyframesSpace(sheet[namePos:nameEnd]) + "\x00" +
				keyframesFold(sheet[nameEnd+1:end-1])
			blocks = append(blocks, block{start: start, end: end, key: key})
			pos = end
		case sheet[pos] == '@':
			end := pos + 1
			for end < len(sheet) && sheet[end] != '{' && sheet[end] != ';' && sheet[end] != '}' {
				if sheet[end] == '"' || sheet[end] == '\'' {
					end = keyframesQuotedEnd(sheet, end)
					continue
				}
				end++
			}
			if end < len(sheet) && sheet[end] == '{' {
				context = append(context, frame{depth: depth, text: keyframesSpace(sheet[pos:end])})
				depth++
				pos = end + 1
			} else {
				pos = end
			}
		default:
			pos++
		}
	}
	byKey := map[string][]block{}
	for _, found := range blocks {
		byKey[found.key] = append(byKey[found.key], found)
	}
	drop := map[int]bool{}
	for _, group := range byKey {
		if len(group) < 2 {
			continue
		}
		for _, earlier := range group[:len(group)-1] {
			drop[earlier.start] = true
		}
	}
	if len(drop) == 0 {
		return sheet
	}
	var out strings.Builder
	out.Grow(len(sheet))
	cursor := 0
	for _, found := range blocks {
		if !drop[found.start] {
			continue
		}
		out.WriteString(sheet[cursor:found.start])
		cursor = found.end
	}
	out.WriteString(sheet[cursor:])
	return out.String()
}

// keyframesAt reports whether an @keyframes header starts at pos.
func keyframesAt(sheet string, pos int) bool {
	const keyword = "@keyframes"
	if !strings.HasPrefix(sheet[pos:], keyword) {
		return false
	}
	rest := pos + len(keyword)
	if rest >= len(sheet) {
		return false
	}
	switch sheet[rest] {
	case ' ', '\t', '\n', '\r', '\f', '"', '\'':
		return true
	}
	return false
}

func keyframesQuotedEnd(sheet string, pos int) int {
	quote := sheet[pos]
	pos++
	for pos < len(sheet) {
		char := sheet[pos]
		if char == '\\' && pos+1 < len(sheet) {
			pos += 2
			continue
		}
		pos++
		if char == quote {
			break
		}
	}
	return pos
}

// keyframesBlockEnd returns the offset just past the closing brace matching
// the opening brace at open, or -1 when the block never closes.
func keyframesBlockEnd(sheet string, open int) int {
	depth := 0
	pos := open
	for pos < len(sheet) {
		switch {
		case sheet[pos] == '/' && pos+1 < len(sheet) && sheet[pos+1] == '*':
			end := strings.Index(sheet[pos+2:], "*/")
			if end < 0 {
				return -1
			}
			pos += end + 4
		case sheet[pos] == '"' || sheet[pos] == '\'':
			pos = keyframesQuotedEnd(sheet, pos)
		case sheet[pos] == '{':
			depth++
			pos++
		case sheet[pos] == '}':
			depth--
			pos++
			if depth == 0 {
				return pos
			}
		default:
			pos++
		}
	}
	return -1
}

func keyframesSpace(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

// keyframesFold reduces a block body to its significant bytes: comments and
// whitespace outside quoted strings are dropped, so bodies that differ only
// in formatting compare equal. Strings are kept byte for byte, so values
// that differ inside quotes never match.
func keyframesFold(text string) string {
	var out strings.Builder
	out.Grow(len(text))
	pos := 0
	for pos < len(text) {
		char := text[pos]
		switch {
		case char == '/' && pos+1 < len(text) && text[pos+1] == '*':
			end := strings.Index(text[pos+2:], "*/")
			if end < 0 {
				return out.String()
			}
			pos += end + 4
		case char == '"' || char == '\'':
			end := keyframesQuotedEnd(text, pos)
			out.WriteString(text[pos:end])
			pos = end
		case char == ' ' || char == '\t' || char == '\n' || char == '\r' || char == '\f':
			pos++
		default:
			out.WriteByte(char)
			pos++
		}
	}
	return out.String()
}
