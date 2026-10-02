package chatui_test

import "strings"

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
