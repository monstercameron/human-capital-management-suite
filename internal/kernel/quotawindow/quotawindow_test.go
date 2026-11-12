package quotawindow

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 11, 10, 0, 30, 0, time.UTC)

func TestAdvanceOpensAWindowAlignedToTheDuration(t *testing.T) {
	w := Advance(Window{}, t0, time.Minute)
	if !w.StartAt.Equal(time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("StartAt = %v, want the minute boundary", w.StartAt)
	}
	if !w.ResetAt.Equal(w.StartAt.Add(time.Minute)) {
		t.Fatalf("ResetAt = %v, want start plus one minute", w.ResetAt)
	}
	if w.Used != 0 {
		t.Fatalf("a new window starts with Used = %d", w.Used)
	}
}

func TestAdvancePreservesAnActiveWindow(t *testing.T) {
	active := Window{Used: 4, StartAt: t0.Add(-10 * time.Second), ResetAt: t0.Add(20 * time.Second)}
	if got := Advance(active, t0, time.Minute); got != active {
		t.Fatalf("active window changed: %+v", got)
	}
}

func TestAdvanceRollsOverAtResetAndDefaultsTheDuration(t *testing.T) {
	expired := Window{Used: 9, StartAt: t0.Add(-2 * time.Minute), ResetAt: t0.Add(-time.Second)}
	got := Advance(expired, t0, 0)
	if got.Used != 0 {
		t.Fatalf("expired window kept Used = %d", got.Used)
	}
	if got.ResetAt.Sub(got.StartAt) != time.Minute {
		t.Fatalf("non-positive duration did not default to one minute: %v", got.ResetAt.Sub(got.StartAt))
	}
}

func TestRetryAfterSecondsRoundsUpAndNeverReturnsZero(t *testing.T) {
	cases := []struct {
		name  string
		reset time.Time
		want  int
	}{
		{"past reset", t0.Add(-time.Second), 1},
		{"exactly now", t0, 1},
		{"fractional rounds up", t0.Add(1500 * time.Millisecond), 2},
		{"whole seconds", t0.Add(7 * time.Second), 7},
	}
	for _, c := range cases {
		if got := RetryAfterSeconds(t0, c.reset); got != c.want {
			t.Errorf("%s: RetryAfterSeconds = %d, want %d", c.name, got, c.want)
		}
	}
}
