package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/emojiset"
)

// CHATBUG-043: some platforms (Windows) have no glyphs for flag emoji, and a flag
// there is drawn as the two letters of its country code in a box. The client asks
// once whether the platform draws flags. Where it does not, the picker leaves the
// Flags group out (and flags out of Frequently used, of search and of the
// one-click reactions), and a flag that is already in a reaction or a message is
// drawn as a small chip: the country code in the page's own type, with the
// country's name as its accessible name and tooltip.

// emojiFlagsDrawn reports whether this platform draws flag emoji. The browser
// build measures it once (chatEmojiPlatformDrawsFlags); tests replace it.
var emojiFlagsDrawn = chatEmojiPlatformDrawsFlags

const emojiFlagsGroupID = "flags"

func isRegionalIndicator(r rune) bool { return r >= 0x1F1E6 && r <= 0x1F1FF }

// emojiIsCountryFlag reports whether glyph is exactly a country flag: two
// regional-indicator letters.
func emojiIsCountryFlag(glyph string) bool {
	runes := []rune(strings.TrimSpace(glyph))
	return len(runes) == 2 && isRegionalIndicator(runes[0]) && isRegionalIndicator(runes[1])
}

// emojiIsSubdivisionFlag is a black flag followed by tag letters (England, say).
func emojiIsSubdivisionFlag(glyph string) bool {
	runes := []rune(glyph)
	if len(runes) < 3 || runes[0] != 0x1F3F4 {
		return false
	}
	for _, r := range runes[1:] {
		if r < 0xE0020 || r > 0xE007F {
			return false
		}
	}
	return true
}

// emojiNeedsFlagGlyphs is a flag the platform cannot draw: country and
// subdivision flags. (The chequered, triangular and rainbow flags draw anywhere.)
func emojiNeedsFlagGlyphs(glyph string) bool {
	return emojiIsCountryFlag(glyph) || emojiIsSubdivisionFlag(glyph)
}

// emojiFlagCode is the country code a country flag spells ("US"), or "".
func emojiFlagCode(glyph string) string {
	if !emojiIsCountryFlag(glyph) {
		return ""
	}
	var code []rune
	for _, r := range glyph {
		code = append(code, 'A'+(r-0x1F1E6))
	}
	return string(code)
}

// emojiFlagName is the name a flag chip speaks and shows as its tooltip: the
// emoji's name in the reader's language once the data is here, the country code
// until then.
func emojiFlagName(m Model, glyph string) string {
	if ix := chatEmojiData.index; ix != nil {
		if i, ok := ix.Lookup(glyph); ok {
			if name := ix.Name(i); name != "" {
				return name
			}
		}
	} else {
		chatEmojiEnsureData(false)
	}
	if code := emojiFlagCode(glyph); code != "" {
		return chatEmojiFormat(m, emojiKeyFlag, map[string]string{"code": code})
	}
	return chatEmojiText(m, emojiKeyFlagPlain)
}

// emojiFlagChip is one flag, drawn as a chip.
func emojiFlagChip(m Model, glyph string) ui.Node {
	name := emojiFlagName(m, glyph)
	code := emojiFlagCode(glyph)
	if code == "" {
		code = "⚑"
	}
	return html.Span(html.Props{Class: "emoji-flag-chip", Role: "img", Title: name, Aria: map[string]string{"label": name}, Dir: "ltr", Text: code})
}

// chatEmojiGlyphNode is an emoji as a node: its text, or a chip when it is a flag
// the platform cannot draw.
func chatEmojiGlyphNode(m Model, glyph string) ui.Node {
	if emojiNeedsFlagGlyphs(glyph) && !emojiFlagsDrawn() {
		return emojiFlagChip(m, glyph)
	}
	return ui.Text(glyph)
}

// chatEmojiSpoken is what an emoji is called where its glyph cannot be read out:
// the flag's name when the platform cannot draw it, the glyph otherwise.
func chatEmojiSpoken(m Model, glyph string) string {
	if emojiNeedsFlagGlyphs(glyph) && !emojiFlagsDrawn() {
		return emojiFlagName(m, glyph)
	}
	return glyph
}

// emojiTextSegment is a run of message text, or one flag in it.
type emojiTextSegment struct {
	Text string
	Flag bool
}

// emojiSplitFlags cuts text around the flags in it; nil when there is none.
func emojiSplitFlags(text string) []emojiTextSegment {
	runes := []rune(text)
	var out []emojiTextSegment
	start, found := 0, false
	for i := 0; i < len(runes); {
		end := 0
		switch {
		case i+1 < len(runes) && isRegionalIndicator(runes[i]) && isRegionalIndicator(runes[i+1]):
			end = i + 2
		case runes[i] == 0x1F3F4 && i+2 < len(runes) && runes[i+1] >= 0xE0020 && runes[i+1] <= 0xE007F:
			end = i + 1
			for end < len(runes) && runes[end] >= 0xE0020 && runes[end] <= 0xE007F {
				end++
			}
		}
		if end == 0 {
			i++
			continue
		}
		found = true
		if i > start {
			out = append(out, emojiTextSegment{Text: string(runes[start:i])})
		}
		out = append(out, emojiTextSegment{Text: string(runes[i:end]), Flag: true})
		start, i = end, end
	}
	if !found {
		return nil
	}
	if start < len(runes) {
		out = append(out, emojiTextSegment{Text: string(runes[start:])})
	}
	return out
}

// emojiWithoutFlags drops the flags a platform cannot draw from a list of glyphs.
func emojiWithoutFlags(glyphs []string) []string {
	out := make([]string, 0, len(glyphs))
	for _, glyph := range glyphs {
		if !emojiNeedsFlagGlyphs(glyph) {
			out = append(out, glyph)
		}
	}
	return out
}

// emojiFlagsGroup finds the Flags group of a set when it is the last group, as
// Unicode orders it, so that leaving it out leaves the rest in place.
func emojiFlagsGroup(set *emojiset.Set) (index int, ok bool) {
	if set == nil || len(set.Groups) == 0 {
		return 0, false
	}
	last := len(set.Groups) - 1
	return last, set.Groups[last].ID == emojiFlagsGroupID
}
