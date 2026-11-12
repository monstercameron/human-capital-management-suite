// Package quotawindow is the shared fixed-window counter used by connector
// dispatch and inbound application-credential admission, as pure value
// arithmetic over an explicit clock.
//
// It lives apart from internal/connectivity/operation so the admission gate can
// use the window without linking the operation journal and its privacy gate.
// The operation package re-exports these names as thin wrappers.
package quotawindow

import (
	"math"
	"time"
)

// Window is one fixed quota window: the count used so far and its bounds.
type Window struct {
	Used    int
	StartAt time.Time
	ResetAt time.Time
}

// Advance opens the current fixed window or preserves an active one. The
// returned value has a zero Used count when a new window begins.
func Advance(window Window, now time.Time, duration time.Duration) Window {
	if duration <= 0 {
		duration = time.Minute
	}
	if window.StartAt.IsZero() || !now.Before(window.ResetAt) {
		start := now.Truncate(duration)
		return Window{StartAt: start, ResetAt: start.Add(duration)}
	}
	return window
}

// RetryAfterSeconds returns the whole-second delay until reset, rounded up so
// a caller never retries before the window actually resets.
func RetryAfterSeconds(now, reset time.Time) int {
	remaining := reset.Sub(now)
	if remaining <= 0 {
		return 1
	}
	seconds := int(math.Ceil(remaining.Seconds()))
	if seconds < 1 {
		return 1
	}
	return seconds
}
