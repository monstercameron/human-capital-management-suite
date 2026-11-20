package chat

import "strings"

// chatcmd004Digits writes Arabic-Indic (٠-٩) and extended Arabic-Indic (۰-۹)
// digits as the figures 0-9, so a date typed with the numerals a reader of
// Arabic uses means what the same date in Western digits means (#3940).
func chatcmd004Digits(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= '\u0660' && r <= '\u0669':
			return '0' + (r - '\u0660')
		case r >= '\u06F0' && r <= '\u06F9':
			return '0' + (r - '\u06F0')
		}
		return r
	}, s)
}
