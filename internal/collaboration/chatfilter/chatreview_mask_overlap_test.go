package chatfilter

import "testing"

// Two mask rules can match overlapping spans of one message. Mask merged them
// by moving the write cursor backwards, so text inside an earlier span
// reappeared in the output: masked content leaked to the reader.
func TestChatReview_MaskKeepsOverlappingSpansMasked(t *testing.T) {
	for _, tc := range []struct {
		name  string
		body  string
		spans []Span
		want  string
	}{
		{"contained", "0123456789", []Span{{0, 7}, {2, 5}}, "[x]789"},
		{"contained reversed", "0123456789", []Span{{2, 5}, {0, 7}}, "[x]789"},
		{"partial overlap", "0123456789", []Span{{0, 5}, {3, 8}}, "[x]89"},
		{"duplicate", "0123456789", []Span{{2, 5}, {2, 5}}, "01[x]56789"},
		{"adjacent", "0123456789", []Span{{0, 5}, {5, 8}}, "[x][x]89"},
		{"disjoint", "0123456789", []Span{{0, 2}, {8, 10}}, "[x]234567[x]"},
	} {
		if got := Mask(tc.body, tc.spans, "[x]"); got != tc.want {
			t.Errorf("%s: Mask = %q, want %q", tc.name, got, tc.want)
		}
	}
}
