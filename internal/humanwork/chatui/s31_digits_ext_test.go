package chatui_test

import (
	"strconv"
	"strings"
)

// s31Digits writes the ASCII digits of text in the numerals Chat prints for
// locale: Arabic-Indic under ar, unchanged elsewhere (AGENTUX-071).
func s31Digits(locale, text string) string {
	if !strings.HasPrefix(locale, "ar") {
		return text
	}
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return '٠' + (r - '0')
		}
		return r
	}, text)
}

// issue3763CountTail is what follows a heading's words for its count: " · 18",
// or " (١٨)" in Arabic where a middle dot beside the digits reads as a zero.
func issue3763CountTail(locale string, n int) string {
	if strings.HasPrefix(locale, "ar") {
		return s31Digits(locale, " ("+strconv.Itoa(n)+")")
	}
	return " · " + strconv.Itoa(n)
}
