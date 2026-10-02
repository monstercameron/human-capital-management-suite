package chatui

import "time"

// pastDeadline reports whether a waiting state has outlived its deadline. Such a
// state is drawn as an interrupted answer, never as work in progress, so nothing
// about it may keep counting seconds.
func (p PersonaProgressProps) pastDeadline(now time.Time) bool {
	return !p.Deadline.IsZero() && !now.Before(p.Deadline)
}
