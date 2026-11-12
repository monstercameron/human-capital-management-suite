package chatui

import "testing"

// TestIssue3718_CopyTextStripsMarkdown: Copy text puts the words a reader sees
// on the clipboard, not the Markdown that produced them (#3718).
func TestIssue3718_CopyTextStripsMarkdown(t *testing.T) {
	cases := []struct {
		name, body, want string
	}{
		{"emphasis and code", "second **bold** message with `code` and https://example.com", "second bold message with code and https://example.com"},
		{"plain text kept", "first\nsecond\n", "first\nsecond"},
		{"paragraphs", "one\n\ntwo", "one\n\ntwo"},
		{"strikethrough", "~~gone~~ kept", "gone kept"},
		{"link keeps destination", "see [docs](https://docs.example/a)", "see docs (https://docs.example/a)"},
		{"fenced code", "```\nx := 1\n```", "x := 1"},
		{"bullets", "- one\n- two", "- one\n- two"},
		{"numbered", "1. first\n2. second", "1. first\n2. second"},
		{"german", "**fett** Grüße", "fett Grüße"},
		{"arabic", "**مرحبا** بالعالم", "مرحبا بالعالم"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Issue3718CopyText(tc.body); got != tc.want {
				t.Fatalf("Issue3718CopyText(%q) = %q, want %q", tc.body, got, tc.want)
			}
		})
	}
}
