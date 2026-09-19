package journeyclient

import (
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// UXLIVE-016's RED was measured on the running server: a journey card read
// "Effective 1 Jul 2026" beside "Updated 2026-09-17 22:50 UTC", so one card
// carried a human date and a machine date for two facts of the same kind.
//
// An instant now renders in the same date vocabulary as a civil date, with
// its clock and zone after it.

// TestTodo_UXLIVE_016 is the primary red/green test.
func TestTodo_UXLIVE_016(t *testing.T) {
	ts := timestamppb.New(time.Date(2026, 9, 17, 22, 50, 0, 0, time.UTC))
	got := formatTime(ts)
	if strings.HasPrefix(got, "2026-") {
		t.Fatalf("an instant still renders as a machine date: %q", got)
	}
	for _, want := range []string{"17 Sep 2026", "22:50", "UTC"} {
		if !strings.Contains(got, want) {
			t.Fatalf("an instant lost %q: %q", want, got)
		}
	}
	// A civil date and an instant now share their date vocabulary.
	if date := formatDate("2026-09-17"); !strings.HasPrefix(got, date) {
		t.Fatalf("instant %q does not start with the civil date form %q", got, date)
	}
	// Localized instants keep the same shape in their own conventions.
	if de := formatTimeLocale("de-DE", ts); !strings.Contains(de, ", ") || !strings.HasSuffix(de, "UTC") {
		t.Fatalf("the localized instant lost the shared shape: %q", de)
	}
}
