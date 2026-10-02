package chatui

import "strconv"

// AGENTUX-071 (finding on numerals). One rule for every number Chat prints:
// digits are written in the locale's own numeral system, the way card and
// message times already are (arabicDigits under ar, Latin elsewhere), so a line
// never mixes two systems. Counts that go through Model.n already follow it
// (the client's Number callback); this is the same rule for the places that
// formatted a number themselves.

// chatNumeral rewrites the ASCII digits of text in the numerals of locale.
func chatNumeral(locale, text string) string {
	if dateLocale(locale) == "ar" {
		return arabicDigits(text)
	}
	return text
}

// chatCount writes a count, zero included, in the locale's numerals.
func chatCount(locale string, n int) string { return chatNumeral(locale, strconv.Itoa(n)) }

// VoiceClock is the recorder's and player's "minutes:seconds" in the locale's
// numerals; the browser half formats its running clock with it too.
func VoiceClock(locale string, ms int64) string { return chatNumeral(locale, voiceTime(ms)) }

// voiceMaxClock is the length a recording is limited to, as the recorder's
// clock shows it before anything is recorded.
const voiceMaxClockMS = 120000
