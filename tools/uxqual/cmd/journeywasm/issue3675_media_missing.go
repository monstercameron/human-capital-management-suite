package main

import (
	"net/http"
	"time"
)

// chatMediaFailurePolicy is how long a failed media request is left alone, and
// whether the post should show its attachment as unavailable. An object the server
// says is missing (or gone) or too large will not be different a moment later, so
// it is asked about once and shown as unavailable; anything else may be a passing
// fault and is tried again after a short wait.
func chatMediaFailurePolicy(status int) (cooldown time.Duration, unavailable bool) {
	switch status {
	case http.StatusNotFound, http.StatusGone, http.StatusRequestEntityTooLarge:
		return time.Hour, true
	}
	return 20 * time.Second, false
}
