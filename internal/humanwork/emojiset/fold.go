package emojiset

import (
	"strings"
	"unicode"
)

// Fold turns text into the form emoji search compares: lower case, without
// accents, with Arabic spelling variants merged and every separator reduced to
// one space. It is applied to the data (names, keywords) and to the query, so
// "Feuer", "FEUER" and "feuer" are one word, "café" and "cafe" are one word, and
// ":thumbs_up:" folds to "thumbs up".
func Fold(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	space := true
	put := func(text string) {
		b.WriteString(text)
		space = false
	}
	for _, r := range s {
		switch {
		case r == ' ' || r == '\t' || r == '\n' || r == ' ' || isSeparator(r):
			if !space {
				b.WriteByte(' ')
				space = true
			}
			continue
		case r >= 0x0300 && r <= 0x036f, r == 0x200d, r == 0xfe0f, r == 0xfe0e, r == 0x200c:
			// Combining accents (decomposed input) and joiners carry no word.
			continue
		case r >= 0x0600 && r <= 0x06ff:
			if folded, ok := foldArabic(r); ok {
				if folded != 0 {
					put(string(folded))
				}
				continue
			}
		}
		if r < 0x80 {
			if r >= 'A' && r <= 'Z' {
				r += 'a' - 'A'
			}
			put(string(r))
			continue
		}
		r = unicode.ToLower(r)
		if replacement, ok := latinFold[r]; ok {
			put(replacement)
			continue
		}
		put(string(r))
	}
	return strings.TrimRight(b.String(), " ")
}

func isSeparator(r rune) bool {
	switch r {
	case '_', '-', ':', ',', '.', ';', '\'', '’', '‘', '‚', '"', '“', '”', '„',
		'(', ')', '[', ']', '!', '?', '/', '\\', '|', '«', '»', '،', '؛', '؟', '–', '—':
		return true
	}
	return false
}

// foldArabic merges the spelling variants people type interchangeably. The
// second result is false for an Arabic-block rune that has no rule, which is then
// kept as it is. A folded rune of 0 means "drop it" (vowel marks, tatweel).
func foldArabic(r rune) (rune, bool) {
	switch {
	case r >= 0x064b && r <= 0x065f, r == 0x0670, r == 0x0640, r == 0x06d6, r == 0x06ed:
		return 0, true
	case r == 0x0622, r == 0x0623, r == 0x0625, r == 0x0671:
		return 0x0627, true // alef with madda, hamza, or wasla -> alef
	case r == 0x0649, r == 0x06cc, r == 0x0626:
		return 0x064a, true // alef maqsura, Farsi yeh, hamza on yeh -> yeh
	case r == 0x0629:
		return 0x0647, true // teh marbuta -> heh
	case r == 0x0624:
		return 0x0648, true // hamza on waw -> waw
	case r == 0x06a9:
		return 0x0643, true // Keheh -> kaf
	case r >= 0x0660 && r <= 0x0669:
		return '0' + (r - 0x0660), true
	case r >= 0x06f0 && r <= 0x06f9:
		return '0' + (r - 0x06f0), true
	}
	return 0, false
}

// latinFold maps the accented Latin letters of the product's languages and the
// common neighbours to their base letters.
var latinFold = func() map[rune]string {
	table := map[rune]string{
		'ß': "ss", 'æ': "ae", 'œ': "oe", 'ø': "o", 'đ': "d", 'ð': "d", 'ł': "l", 'þ': "th", 'ı': "i",
	}
	groups := map[string]string{
		"a": "àáâãäåāăąǎ", "c": "çćĉċč", "d": "ď", "e": "èéêëēĕėęě", "g": "ĝğġģ", "h": "ĥ",
		"i": "ìíîïĩīĭįǐ", "j": "ĵ", "k": "ķ", "l": "ĺļľ", "n": "ñńņňŉ", "o": "òóôõöōŏőǒ",
		"r": "ŕŗř", "s": "śŝşš", "t": "ţť", "u": "ùúûüũūŭůűųǔ", "w": "ŵ", "y": "ýÿŷ", "z": "źżž",
	}
	for base, letters := range groups {
		for _, r := range letters {
			table[r] = base
		}
	}
	return table
}()
