package project

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTodo_PM_024_DomainTaskTextBoundsAndClassificationBoundary(t *testing.T) {
	cases := []struct {
		name, title, description string
		valid                    bool
	}{
		{"plain text", "Launch plan", "first line\nsecond line\t✓", true},
		{"rich text remains source data", "<strong>Launch</strong>", "<p>Details</p>", true},
		{"empty title", " \t ", "", false},
		{"title over limit", strings.Repeat("x", MaxTaskTitleRunes+1), "", false},
		{"description over limit", "Valid", strings.Repeat("x", MaxTaskDescriptionRunes+1), false},
		{"title control", "bad\x00title", "", false},
		{"description control", "Valid", "bad\x00text", false},
		{"invalid utf8", string([]byte{0xff}), "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ValidateTaskText(tc.title, tc.description); got != tc.valid {
				t.Fatalf("ValidateTaskText() = %v, want %v", got, tc.valid)
			}
		})
	}
}

func FuzzTodo_PM_024_DomainTaskText(f *testing.F) {
	f.Add("Title", "Description")
	f.Add("<script>alert(1)</script>", "<p>data</p>")
	f.Add("", "")
	f.Fuzz(func(t *testing.T, title, description string) {
		got := ValidateTaskText(title, description)
		if got && (len(title) > MaxTaskTitleRunes*utf8.UTFMax || len(description) > MaxTaskDescriptionRunes*utf8.UTFMax) {
			t.Fatal("accepted text exceeds maximum UTF-8 byte size implied by rune bounds")
		}
	})
}
