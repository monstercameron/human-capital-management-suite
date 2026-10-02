package main

import (
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// personaChatAnswer is one stored private answer as the first read of a
// conversation's agent activity carries it (CHATBUG-040).
type personaChatAnswer struct {
	ID         string    `json:"id"`
	ThreadID   string    `json:"thread_id"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"created_at"`
	ExpiresAt  time.Time `json:"expires_at"`
	ThreadLink string    `json:"thread_link"`
	// InvocationID is the run the answer belongs to, and SharedPostID the
	// message it was shared to the channel as, when it was (CHATUX-026).
	InvocationID string `json:"invocation_id"`
	SharedPostID string `json:"shared_post_id"`
}

// personaChatActivity is one event of a conversation's agent activity. The
// first event after the page opens the conversation also holds the caller's
// stored private answers; the events that follow hold the activity alone.
type personaChatActivity struct {
	Invocations []personaChatInvocation `json:"invocations"`
	Answers     []personaChatAnswer     `json:"answers"`
}

// chatbug040ApplyAnswers puts the stored private answers that came with the
// agent activity on the page, in the same step as the activity itself, so a
// finished question is drawn answered the first time its card is drawn. The
// event stream delivers the same answers a little later; one already held is
// replaced by id, never added twice. It reports whether the page changed.
//
// The answers go into a new slice: a render in flight may be reading the old.
func chatbug040ApplyAnswers(model *chatui.Model, answers []personaChatAnswer, now time.Time) bool {
	if model == nil || len(answers) == 0 {
		return false
	}
	next := append([]chatui.EphemeralMessage(nil), model.EphemeralMessages...)
	changed := false
	for _, answer := range answers {
		if strings.TrimSpace(answer.ID) == "" || strings.TrimSpace(answer.ThreadID) == "" || strings.TrimSpace(answer.Body) == "" ||
			answer.CreatedAt.IsZero() || !answer.CreatedAt.Before(answer.ExpiresAt) || !now.Before(answer.ExpiresAt) {
			continue
		}
		message := chatui.EphemeralMessage{ID: answer.ID, ThreadID: answer.ThreadID, Body: answer.Body, OnlyVisibleToYou: true, CreatedAt: answer.CreatedAt, ExpiresAt: answer.ExpiresAt, ThreadLink: answer.ThreadLink}
		held := false
		for index := range next {
			if next[index].ID == message.ID {
				held = true
				if next[index] != message {
					next[index], changed = message, true
				}
				break
			}
		}
		if !held {
			next, changed = append(next, message), true
		}
	}
	if changed {
		model.EphemeralMessages = next
	}
	return changed
}
