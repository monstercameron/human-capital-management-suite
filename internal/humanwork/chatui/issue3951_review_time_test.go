package chatui

import (
	"regexp"
	"strings"
	"testing"
)

// TestIssue3951_ReviewChoiceShowsTimeOfTimedDeadline: the "Deadline for ..."
// choice that explains where the date came from names the time and zone of a
// timed deadline, and only the day for a date alone.
func TestIssue3951_ReviewChoiceShowsTimeOfTimedDeadline(t *testing.T) {
	timedChoice := regexp.MustCompile(`read from[^<]*`)
	_ = timedChoice
	for _, line := range []string{"/todo Review the plan due in an hour", "/todo Review the plan due 2026-11-13T12:30:00-05:00"} {
		_, _, markup := issue3951Preview(t, "en-US", line)
		found := false
		for _, part := range strings.Split(markup, "<button") {
			if strings.Contains(part, "chatcmd003-choice") && strings.Contains(part, "read from") {
				found = true
				if !regexp.MustCompile(`Due \d{4}-\d{2}-\d{2} \d{2}:\d{2} UTC`).MatchString(part) {
					t.Fatalf("%q: review choice lacks time and zone:\n%s", line, part)
				}
			}
		}
		if !found {
			t.Fatalf("%q: no review choice found:\n%s", line, markup)
		}
	}
	_, _, markup := issue3951Preview(t, "en-US", "/todo Review the plan due 2027-03-13")
	if regexp.MustCompile(`Due 2027-03-13 \d{2}:\d{2}[^<]*read from`).MatchString(markup) {
		t.Fatalf("date-only review choice must show only the day:\n%s", markup)
	}
}
