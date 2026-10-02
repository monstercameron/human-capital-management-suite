package main

import (
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// The maps below belong to the model a render may be reading, so every change
// builds a new map and leaves the old one as it was.

// chatbug047Asked records that the person pressed "Ask again" under a
// question: the failed card gives way to the working state at once.
func chatbug047Asked(retries map[string]chatui.AgentRetryState, question string, now time.Time, from ...string) map[string]chatui.AgentRetryState {
	if question == "" {
		return retries
	}
	next := chatbug047Copy(retries)
	state := chatui.AgentRetryState{Asking: true, Since: now}
	if len(from) > 0 {
		state.From = from[0]
	}
	next[question] = state
	return next
}

// chatbug047Answered records what the server said to "Ask again": the recorded
// copy of the question the new attempt belongs to, or that nothing was started.
func chatbug047Answered(retries map[string]chatui.AgentRetryState, question, copyPost string, failed bool) map[string]chatui.AgentRetryState {
	state, asked := retries[question]
	if question == "" || !asked {
		return retries
	}
	next := chatbug047Copy(retries)
	switch {
	case failed:
		state.Asking, state.Failed = false, true
	case copyPost == question:
		// The question itself was admitted again; the new attempt is told apart
		// by its run, not by a message (see AgentRetryState.From).
		state.Failed = false
	default:
		state.PostID, state.Failed = copyPost, false
	}
	next[question] = state
	return next
}

// chatbug047Settled ends the wait of every question whose new attempt the
// server has now reported: from here the attempt's own state is drawn.
func chatbug047Settled(retries map[string]chatui.AgentRetryState, invocations []chatui.PersonaThreadInvocation) map[string]chatui.AgentRetryState {
	reported := make(map[string]bool, len(invocations))
	// A run of the question itself that is not the one asked again is the new attempt.
	attempted := make(map[string]string, len(invocations))
	for _, invocation := range invocations {
		reported[invocation.PostID] = true
		if id := invocation.Projection.InvocationID; id != "" {
			attempted[invocation.PostID] = id
		}
	}
	var next map[string]chatui.AgentRetryState
	for question, state := range retries {
		if !state.Asking {
			continue
		}
		newAttempt := state.From != "" && attempted[question] != "" && attempted[question] != state.From
		if !newAttempt && (state.PostID == "" || !reported[state.PostID]) {
			continue
		}
		if next == nil {
			next = chatbug047Copy(retries)
		}
		state.Asking = false
		next[question] = state
	}
	if next == nil {
		return retries
	}
	return next
}

// chatbug047Waiting reports whether any question asked again is still waiting
// for its new attempt, so the elapsed ticker keeps its working state counting
// and draws the end of the wait.
func chatbug047Waiting(retries map[string]chatui.AgentRetryState, now time.Time) bool {
	for _, state := range retries {
		if chatui.AgentRetryRedraw(state, now) {
			return true
		}
	}
	return false
}

func chatbug047Copy(retries map[string]chatui.AgentRetryState) map[string]chatui.AgentRetryState {
	next := make(map[string]chatui.AgentRetryState, len(retries)+1)
	for question, state := range retries {
		next[question] = state
	}
	return next
}

// chatbug047PostMessage is the part of a post the copy rule reads.
func chatbug047PostMessage(post *chatv1.Post) chatui.Message {
	return chatui.Message{ID: post.GetId(), AuthorID: post.GetAuthorId(), Body: post.GetBody(), PersonaReferences: personaChatPostReferences(post)}
}

// chatbug047CopyPost reports whether reply is the copy of question that "Ask
// again" recorded in the question's thread. The conversation list does not
// count such a copy among the question's replies: the person asked once.
func chatbug047CopyPost(question, reply *chatv1.Post) bool {
	if question == nil || reply == nil || reply.GetParentId() == "" || reply.GetParentId() != question.GetId() {
		return false
	}
	return chatui.AgentRetryCopy(chatbug047PostMessage(question), chatbug047PostMessage(reply))
}

// chatbug047Questions are the posts of a page that start a thread or could, by
// id: the posts a reply's parent is looked up in.
func chatbug047Questions(posts []*chatv1.Post) map[string]*chatv1.Post {
	questions := make(map[string]*chatv1.Post, len(posts))
	for _, post := range posts {
		if post != nil && post.GetId() != "" && post.GetParentId() == "" {
			questions[post.GetId()] = post
		}
	}
	return questions
}

// chatbug047CountsAsReply reports whether a reply that has just arrived adds to
// its question's reply count on the page. A recorded copy of the question does
// not; a reply whose question the page does not hold is counted, as before.
func chatbug047CountsAsReply(model *chatui.Model, reply *chatv1.Post) bool {
	if model == nil || reply == nil || reply.GetParentId() == "" {
		return true
	}
	candidate := chatbug047PostMessage(reply)
	if model.ThreadParent != nil && model.ThreadParent.ID == reply.GetParentId() {
		return !chatui.AgentRetryCopy(*model.ThreadParent, candidate)
	}
	for index := range model.Messages {
		if model.Messages[index].ID == reply.GetParentId() {
			return !chatui.AgentRetryCopy(model.Messages[index], candidate)
		}
	}
	return true
}

// chatbug054Dismiss records that the person dismissed a failed card.
func chatbug054Dismiss(dismissed map[string]bool, key string) map[string]bool {
	if key == "" || dismissed[key] {
		return dismissed
	}
	next := make(map[string]bool, len(dismissed)+1)
	for held := range dismissed {
		next[held] = true
	}
	next[key] = true
	return next
}

// chatbug054Undismiss lets a card show again once its question is asked again:
// a new failure of the same question is news the person has not dismissed.
func chatbug054Undismiss(dismissed map[string]bool, keys ...string) map[string]bool {
	found := false
	for _, key := range keys {
		found = found || dismissed[key]
	}
	if !found {
		return dismissed
	}
	next := make(map[string]bool, len(dismissed))
	for held := range dismissed {
		next[held] = true
	}
	for _, key := range keys {
		delete(next, key)
	}
	return next
}
