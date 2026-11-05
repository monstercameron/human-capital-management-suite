package chatrewrite

import "testing"

// The word lists are matched whole-word and case-insensitively — that is the
// documented contract above them. The heated list keeps it, but the abusive
// list was matched as a raw substring, so ordinary messages that merely start
// a longer word ("I will find your report", "the one skill you need", "go
// diet", "hurt your feelings") read as abusive and followed the hard filter.
func TestChatReview_AbusiveScreenNeedsWholeWords(t *testing.T) {
	for text, want := range map[string]Heat{
		"I will find your report by Friday.":    HeatCalm,
		"The one skill you need is patience.":   HeatCalm,
		"Let us go diet together in January.":   HeatCalm,
		"I did not mean to hurt your feelings.": HeatCalm,
		// True threats still read as abusive.
		"I will find you":        HeatAbusive,
		"You should go die now":  HeatAbusive,
		"I will hurt you, idiot": HeatAbusive,
	} {
		if got := ScreenHeat(text); got != want {
			t.Errorf("ScreenHeat(%q) = %s, want %s", text, got, want)
		}
	}
}
