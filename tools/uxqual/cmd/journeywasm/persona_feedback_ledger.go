package main

import (
	"sync"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// personaFeedbackLedger remembers, per answer, the rating the server last
// confirmed. A rating or undo that could not be saved falls back to it, so the
// controls never claim a rating the server does not hold.
//
// The server's word arrives two ways: as the answer to a rating the person
// just gave, and with the agent activity of the open conversation, which
// carries the rating stored for every answer (CHATBUG-066). While a change is
// on its way to the server the activity may still carry the rating from before
// it, so an answer with a change in flight keeps what the person chose until
// that change is settled.
type personaFeedbackLedger struct {
	mu    sync.Mutex
	saved map[string]string
	busy  map[string]int
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
	l.set(invocation, rating)
}

func (l *personaFeedbackLedger) set(invocation, rating string) {
	if rating == "" {
		delete(l.saved, invocation)
		return
	}
	if l.saved == nil {
		l.saved = make(map[string]string)
	}
	l.saved[invocation] = rating
}

// begin marks a change to an answer's rating as on its way to the server.
func (l *personaFeedbackLedger) begin(invocation string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.busy == nil {
		l.busy = make(map[string]int)
	}
	l.busy[invocation]++
}

// end marks that change as settled, whatever came of it.
func (l *personaFeedbackLedger) end(invocation string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.busy[invocation] <= 1 {
		delete(l.busy, invocation)
		return
	}
	l.busy[invocation]--
}

// stored takes the ratings the agent activity carries, by invocation, and
// returns what the page should show as stored: the server's rating for every
// answer listed, except one whose change is still in flight, which keeps what
// the ledger held. The map is a new one each time.
func (l *personaFeedbackLedger) stored(invocations []personaChatInvocation) map[string]string {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, invocation := range invocations {
		if invocation.InvocationID == "" || l.busy[invocation.InvocationID] > 0 {
			continue
		}
		switch invocation.Feedback {
		case chatui.AgentFeedbackHelpful, chatui.AgentFeedbackNotRight:
			l.set(invocation.InvocationID, invocation.Feedback)
		default:
			l.set(invocation.InvocationID, "")
		}
	}
	return l.snapshot()
}

// shown is the stored ratings as the page should show them now.
func (l *personaFeedbackLedger) shown() map[string]string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.snapshot()
}

func (l *personaFeedbackLedger) snapshot() map[string]string {
	out := make(map[string]string, len(l.saved))
	for invocation, rating := range l.saved {
		out[invocation] = rating
	}
	return out
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

// personaFeedbackWithout returns a copy of the stored ratings without the
// invocation: what the page shows from the moment the person removes a rating,
// before the server has answered.
func personaFeedbackWithout(current map[string]string, invocation string) map[string]string {
	return personaFeedbackRestoredWithout(current, invocation)
}
