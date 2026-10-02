package chatui

import (
	"strings"
	"testing"
)

// TestTodo_AGENTUX_071_Numerals: one numeral rule per locale for every number
// Chat prints. Under ar the voice recorder's timer, counts without a client
// formatter and the reply timer use Arabic-Indic digits like message times do;
// every other locale keeps Latin digits.
func TestTodo_AGENTUX_071_Numerals(t *testing.T) {
	recorder := renderNode(t, RenderVoiceComposer(VoiceComposerProps{Locale: "ar", ConversationID: "c1", Enabled: true}))
	if !strings.Contains(recorder, "٠:٠٠ / ٢:٠٠") || strings.Contains(recorder, "0:00") {
		t.Errorf("the Arabic recorder's timer is not in Arabic-Indic digits: %s", recorder)
	}
	if got := renderNode(t, RenderVoiceComposer(VoiceComposerProps{Locale: "en-US", ConversationID: "c1", Enabled: true})); !strings.Contains(got, "0:00 / 2:00") {
		t.Errorf("the English recorder's timer changed: %s", got)
	}
	if got := VoiceClock("ar", 65000); got != "١:٠٥" {
		t.Errorf("VoiceClock(ar, 65s) = %q", got)
	}
	if got := VoiceClock("de-DE", 65000); got != "1:05" {
		t.Errorf("VoiceClock(de, 65s) = %q", got)
	}
	ar, en := Model{Locale: "ar"}, Model{Locale: "en-US"}
	if ar.n(1234) != "١٢٣٤" || en.n(1234) != "1234" {
		t.Errorf("a count with no client formatter: ar %q, en %q", ar.n(1234), en.n(1234))
	}
	if ar.nz(0) != "٠" || en.nz(0) != "0" {
		t.Errorf("a zero count: ar %q, en %q", ar.nz(0), en.nz(0))
	}
	if got := chatCount("ar", 12); got != "١٢" {
		t.Errorf("chatCount(ar, 12) = %q", got)
	}
	progress := renderNode(t, progressCounter("ar", 65))
	if !strings.Contains(progress, "١:٠٥") {
		t.Errorf("the reply timer is not in Arabic-Indic digits: %s", progress)
	}
	if got := ModerationCountText("ar", 3); strings.ContainsAny(got, "0123456789") {
		t.Errorf("the moderation count has Latin digits: %q", got)
	}
}
