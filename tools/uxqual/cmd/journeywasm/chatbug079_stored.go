package main

import (
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// chatbug079AnswerWait is how long a finished private answer may keep its
// placeholder while its text is on the way. The text comes over the
// conversation's own stream, a few seconds behind the record that says the run
// finished; past this the card points at the saved copy instead of waiting.
const chatbug079AnswerWait = 30 * time.Second

// personaRunInFlight reports whether the server's status names a run that is
// still going: admitted and not yet finished, failed, stopped or expired. The
// working text is drawn for these and for nothing else (CHATBUG-079).
func personaRunInFlight(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "claimed", "started", "ready", "running", "waiting", "reconciling":
		return true
	}
	return false
}

// chatbug079StoredAnswers gives each finished private answer in next the moment
// its placeholder stops waiting: the one it already had on this page, or
// chatbug079AnswerWait from now when it is seen for the first time.
func chatbug079StoredAnswers(previous, next []chatui.PersonaThreadInvocation, now time.Time) []chatui.PersonaThreadInvocation {
	due := make(map[string]time.Time, len(previous))
	for _, invocation := range previous {
		if invocation.Projection.AnswerStored && !invocation.Projection.AnswerDue.IsZero() {
			due[invocation.Projection.InvocationID] = invocation.Projection.AnswerDue
		}
	}
	for index := range next {
		projection := &next[index].Projection
		if !projection.AnswerStored {
			continue
		}
		projection.AnswerDue = due[projection.InvocationID]
		if projection.AnswerDue.IsZero() {
			projection.AnswerDue = now.Add(chatbug079AnswerWait)
		}
	}
	return next
}

// chatbug079AnswerJustDue reports whether a stored answer's wait ended within
// the last two ticks, so the elapsed ticker draws the saved-copy row once.
func chatbug079AnswerJustDue(invocations []chatui.PersonaThreadInvocation, now time.Time) bool {
	for _, invocation := range invocations {
		due := invocation.Projection.AnswerDue
		if invocation.Projection.AnswerStored && !due.IsZero() && !now.Before(due) && now.Sub(due) < 2*time.Second {
			return true
		}
	}
	return false
}
