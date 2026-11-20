package chat

import (
	"context"
	"testing"
)

// The line a decision posts always opens with the informal label; a caller
// cannot make the route post a line that opens with other words.
func TestIssue3792_PostedLineAlwaysCarriesTheInformalLabel_Security(t *testing.T) {
	for label, want := range map[string]string{
		"Company policy":           "Decision (informal): Ship Friday",
		"":                         "Decision (informal): Ship Friday",
		"Entscheidung (informell)": "Entscheidung (informell): Ship Friday",
		"قرار (غير رسمي)":          "قرار (غير رسمي): Ship Friday",
	} {
		s, f, _ := issue3792Service(t, "", FullHistory)
		r := issue3792Request("k", "Ship Friday")
		r.Label = label
		if _, err := s.Record(context.Background(), r); err != nil {
			t.Fatal(err)
		}
		if f.sent.Body != want {
			t.Errorf("label %q posted %q, want %q", label, f.sent.Body, want)
		}
	}
}
