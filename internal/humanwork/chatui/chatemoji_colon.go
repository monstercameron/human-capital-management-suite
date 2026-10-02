package chatui

import (
	"unicode"
	"unicode/utf16"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/emojiset"
)

// CHATEMOJI-003: an emoji without opening the picker. Typing a colon and two or
// more letters in a composer offers the five best matches (agentux_chat4_emoji.go
// draws and drives the list); a complete ":shortcode:" typed or pasted becomes
// the emoji when the message is sent, except inside code.

// emojiSuggestionsShown is the length of the list typing a colon opens.
const emojiSuggestionsShown = 5

// emojiQueryRune reports whether r can be part of what follows the colon: the
// letters, digits and underscore of a shortcode.
func emojiQueryRune(r rune) bool { return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r) }

// emojiSuggest is the limit best matches for a query, each in the person's skin
// tone, named in the reader's language, with its English ":shortcode:".
func emojiSuggest(ix *emojiset.Index, query string, prefs emojiPrefs, limit int) []emojiChoice {
	if ix == nil || limit <= 0 {
		return nil
	}
	var out []emojiChoice
	for _, i := range ix.Search(query, prefs.usageCounts()) {
		if len(out) == limit {
			break
		}
		entry := ix.Set.Entries[i]
		name := ix.Name(i)
		out = append(out, emojiChoice{name: name, glyph: entry.For(prefs.Tone), label: name, code: ix.Shortcode(i)})
	}
	return out
}

// emojiCompletionInsert is the text that takes the place of a typed ":query":
// the emoji and a space to keep typing in, unless a space or line break already
// follows the caret, so the sentence around it is never changed.
func emojiCompletionInsert(value string, caret int, glyph string) string {
	units := utf16.Encode([]rune(value))
	after := utf16.Decode(units[min(max(caret, 0), len(units)):])
	if len(after) > 0 && unicode.IsSpace(after[0]) {
		return glyph
	}
	return glyph + " "
}

// emojiShortcodesIn replaces every complete ":shortcode:" in body that names an
// emoji with that emoji (in the given skin tone). Text in a code span (`...`) or
// a fenced block (```...```) is left exactly as typed, as is a colon pair that
// names nothing ("10:30:45", ":not_an_emoji:"), and a shortcode glued to the end
// of a word ("a:b:").
func emojiShortcodesIn(body string, ix *emojiset.Index, tone int) string {
	if ix == nil {
		return body
	}
	runes := []rune(body)
	out := make([]rune, 0, len(runes))
	atLineStart := true
	for i := 0; i < len(runes); {
		r := runes[i]
		switch {
		case r == '`':
			end := emojiCodeEnd(runes, i, atLineStart)
			out = append(out, runes[i:end]...)
			i = end
			atLineStart = runes[end-1] == '\n'
			continue
		case r == ':' && (i == 0 || !emojiQueryRune(runes[i-1])):
			if end, glyph, ok := emojiShortcodeAt(runes, i, ix, tone); ok {
				out = append(out, []rune(glyph)...)
				i = end
				atLineStart = false
				continue
			}
		}
		out = append(out, r)
		atLineStart = r == '\n' || (atLineStart && (r == ' ' || r == '\t'))
		i++
	}
	return string(out)
}

// emojiCodeEnd is where the code that starts at runes[i] (a backtick) ends: a
// fence runs to the closing fence line (or the end of the text), a span to the
// next run of as many backticks. A run with no closing counterpart is only text.
func emojiCodeEnd(runes []rune, i int, atLineStart bool) int {
	n := 0
	for i+n < len(runes) && runes[i+n] == '`' {
		n++
	}
	if n >= 3 && atLineStart {
		// A fence: everything to the closing fence line.
		for j := i + n; j < len(runes); j++ {
			if runes[j] != '\n' {
				continue
			}
			k := j + 1
			for k < len(runes) && (runes[k] == ' ' || runes[k] == '\t') {
				k++
			}
			m := 0
			for k+m < len(runes) && runes[k+m] == '`' {
				m++
			}
			if m >= n {
				return k + m
			}
		}
		return len(runes)
	}
	for j := i + n; j < len(runes); j++ {
		if runes[j] != '`' {
			continue
		}
		m := 0
		for j+m < len(runes) && runes[j+m] == '`' {
			m++
		}
		if m == n {
			return j + m
		}
		j += m - 1
	}
	return i + n
}

// emojiShortcodeAt reads a ":name:" that starts at runes[i].
func emojiShortcodeAt(runes []rune, i int, ix *emojiset.Index, tone int) (end int, glyph string, ok bool) {
	j := i + 1
	for j < len(runes) && (emojiQueryRune(runes[j]) || runes[j] == '+' || runes[j] == '-') {
		j++
	}
	if j >= len(runes) || runes[j] != ':' || j-i < 3 {
		return 0, "", false
	}
	index, found := ix.FromShortcode(string(runes[i : j+1]))
	if !found {
		return 0, "", false
	}
	return j + 1, ix.Set.Entries[index].For(tone), true
}

// emojiShortcodesInText is emojiShortcodesIn with the emoji data and skin tone of
// this page; with no data loaded yet the text goes unchanged.
func emojiShortcodesInText(body string) string {
	return emojiShortcodesIn(body, chatEmojiData.index, chatEmojiHost.prefs.Tone)
}
