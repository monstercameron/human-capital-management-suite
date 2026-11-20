package chatui

import "testing"

// TestIssue3921_GatedJoinNameKeepsItsSentenceAndAddsTheChannel: a gated row's
// visible label is a sentence, so the accessible name is that sentence, a
// comma and the channel; an ungated row reads "Join <channel>".
func TestIssue3921_GatedJoinNameKeepsItsSentenceAndAddsTheChannel(t *testing.T) {
	for _, locale := range chatgateLocales {
		m := chatgateBrowseModel(locale)
		gated := Conversation{ID: "gated", Name: "payroll-help"}
		if got, want := issue3921JoinAria(m, gated)["label"], chatgateJoinLabel(m, gated)+", payroll-help"; got != want {
			t.Errorf("%s: gated Join name = %q, want %q", locale, got, want)
		}
		open := Conversation{ID: "open", Name: "random"}
		if got, want := issue3921JoinAria(m, open)["label"], m.t(KeyJoin)+" random"; got != want {
			t.Errorf("%s: plain Join name = %q, want %q", locale, got, want)
		}
		if got := issue3921JoinAria(m, gated)["describedby"]; got != "browse-meta-gated" {
			t.Errorf("%s: describedby = %q", locale, got)
		}
	}
}
