package application

import (
	"strings"
	"time"
)

// agentUX073RunWorkedFor is how long a run was worked on. A run records when
// it was created and when its row last changed. For a run that ended in
// failure the last change can be the sweep that closed it, long after anybody
// was working on it: a run abandoned by a restart was shown as failing after
// "45 minutes". No run is worked on past its deadline, so a failed, expired or
// cancelled run closed after its deadline is measured to the deadline instead.
func agentUX073RunWorkedFor(started, updated, deadline time.Time, state string) time.Duration {
	if started.IsZero() || updated.Before(started) {
		return 0
	}
	end := updated
	switch strings.ToUpper(strings.TrimSpace(state)) {
	case "FAILED", "EXPIRED", "CANCELLED":
		if !deadline.IsZero() && deadline.After(started) && updated.After(deadline) {
			end = deadline
		}
	}
	return end.Sub(started)
}
