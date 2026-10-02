package main

import (
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// personaFeedbackLedger remembers, per answer, the rating the server last
// confirmed. A rating or undo that could not be saved falls back to it, so the
// controls never claim a rating the server does not hold.
type personaFeedbackLedger struct {
	mu    sync.Mutex
	saved map[string]string
}

func (l *personaFeedbackLedger) rating(invocation string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.saved[invocation]
}

// confirm records the rating the server accepted; an empty rating is an undo.
func (l *personaFeedbackLedger) confirm(invocation, rating string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if rating == "" {
		delete(l.saved, invocation)
		return
	}
	if l.saved == nil {
		l.saved = make(map[string]string)
	}
	l.saved[invocation] = rating
}

func personaFeedbackRating(helpful bool) string {
	if helpful {
		return chatui.AgentFeedbackHelpful
	}
	return chatui.AgentFeedbackNotRight
}

// personaFeedbackRestoredWith returns a copy of the overrides in which the
// invocation shows the given rating ("" for none).
func personaFeedbackRestoredWith(current map[string]string, invocation, rating string) map[string]string {
	next := make(map[string]string, len(current)+1)
	for key, value := range current {
		next[key] = value
	}
	next[invocation] = rating
	return next
}

// personaFeedbackRestoredWithout returns a copy of the overrides without the
// invocation, so the person's next click shows its own choice again.
func personaFeedbackRestoredWithout(current map[string]string, invocation string) map[string]string {
	if _, ok := current[invocation]; !ok {
		return current
	}
	next := make(map[string]string, len(current))
	for key, value := range current {
		if key != invocation {
			next[key] = value
		}
	}
	return next
}
