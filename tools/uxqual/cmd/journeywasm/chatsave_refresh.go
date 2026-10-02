package main

import "time"

// Failed event reads wait for a later open/change; no timer retries a broken endpoint.
type chatsaveRefresh struct {
	failures int
	next     time.Time
}

func (s chatsaveRefresh) ready(now time.Time) bool { return !now.Before(s.next) }
func (s *chatsaveRefresh) complete(now time.Time, err error) {
	if err == nil {
		*s = chatsaveRefresh{}
		return
	}
	s.failures = min(s.failures+1, 6)
	s.next = now.Add(time.Second * time.Duration(1<<s.failures))
}
