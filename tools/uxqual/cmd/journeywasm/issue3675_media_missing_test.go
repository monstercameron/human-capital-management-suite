package main

import (
	"testing"
	"time"
)

// Issue 3675: a media object the server no longer has was asked about again every
// twenty seconds. It is asked about once and shown as unavailable.
func TestIssue3675_MissingMediaIsShownOnceWithoutRetrying(t *testing.T) {
	for _, status := range []int{404, 410, 413} {
		cooldown, unavailable := chatMediaFailurePolicy(status)
		if !unavailable || cooldown < 30*time.Minute {
			t.Errorf("status %d: cooldown %v unavailable %v, want unavailable and a long wait", status, cooldown, unavailable)
		}
	}
	for _, status := range []int{0, 401, 403, 500, 502, 503} {
		cooldown, unavailable := chatMediaFailurePolicy(status)
		if unavailable || cooldown != 20*time.Second {
			t.Errorf("status %d: cooldown %v unavailable %v, want a short wait and no verdict", status, cooldown, unavailable)
		}
	}
}
