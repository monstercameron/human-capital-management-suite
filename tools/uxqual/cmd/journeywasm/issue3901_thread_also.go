package main

// setThreadAlso records the "Also send to the conversation" choice of the reply
// box under one root message (#3901). It is local only and, like the reply's
// text, is kept per root message so it neither follows the pane to another
// thread nor is lost when the thread, the conversation or the page is left and
// come back to.
func (s *chatState) setThreadAlso(parent string, also bool) {
	if parent == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := make(map[string]bool, len(s.model.ThreadAlso)+1)
	for k, v := range s.model.ThreadAlso {
		if k != parent {
			next[k] = v
		}
	}
	if also {
		next[parent] = true
	}
	if len(next) == 0 {
		next = nil
	}
	s.model.ThreadAlso = next
}
