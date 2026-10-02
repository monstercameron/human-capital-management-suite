package chatui

import (
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATBUG-047: asking a question again must not leave the page with a second
// question, a failed card that never goes away, and an answer far from what it
// answers. The server still records the new attempt as a reply in the first
// question's thread (it has no other way to start a run today), so the page
// does the rest: the new attempt is drawn in the failed card's place under the
// original question, the recorded copy is not shown as a reply, and an answer
// that does not directly follow its question carries a one-line quote of it.

// AgentRetryState is where one question stands after "Ask again".
type AgentRetryState struct {
	// Asking is true from the click until the server reports on the new attempt.
	Asking bool
	// Since is when the person pressed "Ask again".
	Since time.Time
	// PostID is the recorded copy of the question the new attempt belongs to,
	// once the server has accepted the request.
	PostID string
	// Failed is true when the request did not start a new attempt.
	Failed bool
	// From is the run that was asked again. The server admits the question that
	// stands as a new attempt (no copy of it is posted), so the new attempt is the
	// question's own run under another identifier: the wait ends when the server
	// reports a run of the question other than this one.
	From string
}

// agentRetryPendingWindow is how long the working state may stand on the click
// alone. The server reports a new attempt within seconds; one still unreported
// after this did not start.
const agentRetryPendingWindow = 90 * time.Second

// chatbug047Attempts are the later attempts at the question postID: the runs
// started from a copy of it recorded in its own thread, for this viewer.
func chatbug047Attempts(model Model, postID string) []PersonaThreadInvocation {
	var attempts []PersonaThreadInvocation
	known := model.AgentRetries[postID].PostID
	for _, invocation := range model.PersonaInvocations {
		projection := invocation.Projection
		if invocation.PostID == postID || projection.ViewerID != projection.InvokerID || (model.CurrentUser != "" && projection.ViewerID != model.CurrentUser) {
			continue
		}
		if invocation.ThreadID == postID || (known != "" && invocation.PostID == known) {
			attempts = append(attempts, invocation)
		}
	}
	return attempts
}

// chatbug047Standing decides what stands under a question whose own run
// failed: a later attempt still going, a later attempt whose stored answer is
// on its way, nothing (a later attempt answered, or the card was dismissed), or
// the failure. show is false when nothing is drawn.
func chatbug047Standing(model Model, postID string, failed PersonaThreadInvocation, now time.Time) (standing PersonaThreadInvocation, show bool) {
	attempts := chatbug047Attempts(model, postID)
	answered := false
	for _, attempt := range attempts {
		projection := attempt.Projection
		if projection.Failure == nil && projection.Progress != nil && projection.Progress.Visible && !projection.Progress.ResultReady {
			attempt.PostID = postID
			return attempt, true
		}
		if projection.Failure == nil && projection.AnswerStored {
			attempt.PostID = postID
			return attempt, true
		}
		answered = answered || projection.Failure == nil
	}
	if answered {
		return PersonaThreadInvocation{}, false
	}
	if retry := model.AgentRetries[postID]; chatbug047Asking(retry, now) {
		// Asked again a moment ago: the working state, in the failed card's place,
		// until the server reports the new attempt or the window closes.
		name := failed.Projection.AgentName
		return PersonaThreadInvocation{PostID: postID, ThreadID: failed.ThreadID, Projection: PersonaProgressProjection{InvocationID: "pending:" + postID, ViewerID: failed.Projection.ViewerID, InvokerID: failed.Projection.InvokerID, AgentName: name,
			Progress: &PersonaProgressProps{InvokerID: failed.Projection.InvokerID, AgentName: name, Visible: true, Provisional: true, Deadline: retry.Since.Add(agentRetryPendingWindow), ElapsedSeconds: max(0, int(now.Sub(retry.Since)/time.Second))}}}, true
	}
	if chatbug054Dismissed(model, chatbug047FailureFor(failed, postID)) {
		return PersonaThreadInvocation{}, false
	}
	return failed, true
}

// chatbug047Asking reports whether "Ask again" is still waiting for the server
// to report the new attempt.
func chatbug047Asking(retry AgentRetryState, now time.Time) bool {
	return retry.Asking && !retry.Since.IsZero() && now.Before(retry.Since.Add(agentRetryPendingWindow))
}

// chatbug047NotAsked reports whether the last "Ask again" came to nothing: the
// request failed, or no attempt was reported in time.
func chatbug047NotAsked(retry AgentRetryState, now time.Time) bool {
	return retry.Failed || (retry.Asking && !retry.Since.IsZero() && !chatbug047Asking(retry, now))
}

// AgentRetryRedraw reports whether a question asked again needs the page drawn
// again as time passes: while it waits its working state counts seconds, and
// when the wait runs out the card says once that nothing was started.
func AgentRetryRedraw(retry AgentRetryState, now time.Time) bool {
	return retry.Asking && !retry.Since.IsZero() && now.Before(retry.Since.Add(agentRetryPendingWindow+2*time.Second))
}

// chatbug047FailureFor is the failure as the card draws it, with the question
// it belongs to. A waiting card that ran out of time has no failure of its own
// and is named by its run, or by its question.
func chatbug047FailureFor(invocation PersonaThreadInvocation, postID string) PersonaProgressFailure {
	failure := PersonaProgressFailure{}
	if invocation.Projection.Failure != nil {
		failure = *invocation.Projection.Failure
	}
	if failure.InvocationID == "" {
		failure.InvocationID = invocation.Projection.InvocationID
	}
	if failure.AskAgainPostID == "" {
		failure.AskAgainPostID = postID
	}
	return failure
}

// AgentRetryCopy reports whether reply is the copy of question that "Ask again"
// records in the question's thread: the same person, the same words, and an
// agent named in it. It needs nothing but the two messages, so the conversation
// list can leave such a copy out of a question's reply count for every reader,
// with or without the thread open, and without knowing the asker's runs.
func AgentRetryCopy(question, reply Message) bool {
	if question.ID == "" || reply.ID == "" || reply.ID == question.ID || reply.AuthorID == "" || reply.AuthorID != question.AuthorID {
		return false
	}
	if strings.TrimSpace(reply.Body) == "" || strings.TrimSpace(reply.Body) != strings.TrimSpace(question.Body) {
		return false
	}
	for _, reference := range reply.PersonaReferences {
		if reference.Kind == "AGENT_MENTION" {
			return true
		}
	}
	return false
}

// chatbug047RetryCopy reports whether a reply in the open thread is the
// recorded copy of the thread's question: the same person, the same words, and
// an agent asked by it (named in the copy, or known to the page by the run it
// started). Such a reply is the page's bookkeeping for "Ask again", not
// something the person said twice, and is not shown as a reply.
func chatbug047RetryCopy(model Model, reply Message) bool {
	// The question is the open thread's first message, wherever the page holds it.
	parent, found := chatbug040MessageByID(model, model.ThreadParentID)
	if !found || model.ThreadParentID == "" || reply.ID == "" || reply.ID == parent.ID || reply.AuthorID == "" || reply.AuthorID != parent.AuthorID {
		return false
	}
	if strings.TrimSpace(reply.Body) == "" || strings.TrimSpace(reply.Body) != strings.TrimSpace(parent.Body) {
		return false
	}
	if AgentRetryCopy(parent, reply) || model.AgentRetries[parent.ID].PostID == reply.ID {
		return true
	}
	for _, invocation := range model.PersonaInvocations {
		if invocation.PostID == reply.ID {
			return true
		}
	}
	return false
}

// chatbug047QuestionFor finds the question an agent's message answers, in the
// agent's own conversation: the message the answering run was started from, or
// the first question of its thread when the run was started from a recorded
// copy.
func chatbug047QuestionFor(model Model, answer Message) (Message, bool) {
	if !model.selected().Agent || answer.ID == "" {
		return Message{}, false
	}
	for _, invocation := range model.PersonaInvocations {
		if invocation.Projection.DurablePostID != answer.ID {
			continue
		}
		for _, id := range []string{invocation.ThreadID, invocation.PostID} {
			for _, message := range model.Messages {
				if id != "" && message.ID == id && message.ID != answer.ID {
					return message, true
				}
			}
		}
	}
	return Message{}, false
}

// chatbug047QuestionQuote is the one-line quote of the question above an
// answer that does not directly follow it (a later attempt, or an answer that
// arrived after other messages). Pressing it goes to the question. An answer
// that already carries the question it was asked in a channel needs none.
func chatbug047QuestionQuote(model Model, answer Message, hasChannelContext bool) ui.Node {
	if hasChannelContext {
		return nil
	}
	question, found := chatbug047QuestionFor(model, answer)
	if !found {
		return nil
	}
	for index, message := range model.Messages {
		if message.ID == answer.ID && index > 0 && model.Messages[index-1].ID == question.ID {
			return nil
		}
	}
	text := strings.Join(strings.Fields(chatExcerptText(question.Body)), " ")
	if text == "" {
		return nil
	}
	label := chatbug047Text(model.Locale, "go_to_question")
	children := []ui.Node{icon("reply"), html.Span(html.Props{Class: "agent-question-quote-text", Dir: "auto", Text: text})}
	if question.TimeLabel != "" {
		children = append(children, html.Time(html.Props{Class: "agent-question-quote-time", Text: question.TimeLabel}))
	}
	return html.Button(html.Props{Class: "agent-question-quote", Type: "button", Disabled: model.Callbacks.OpenSearchMessage == nil, Title: label, Aria: map[string]string{"label": label + ": " + text},
		Data: map[string]string{"action": "agent-question-jump", "id": question.ID}}, children...)
}

// chatbug047JumpToQuestion goes to the quoted question: the conversation is
// scrolled to it and it is marked, the way a search result is opened.
func chatbug047JumpToQuestion(model Model, questionID string) {
	if model.Callbacks.OpenSearchMessage == nil {
		return
	}
	for _, message := range model.Messages {
		if message.ID == questionID {
			model.Callbacks.OpenSearchMessage(model.SelectedID, message.ID, message.Sequence)
			return
		}
	}
}

func chatbug047Text(locale, key string) string {
	copy := map[string][3]string{
		"go_to_question": {"Go to the question", "Zur Frage springen", "الانتقال إلى السؤال"},
	}
	return chatbug039Text(key, copy[key][chatbug039LocaleIndex(locale)], copy[key][0])
}

// chatBug047Styles keeps the quote to one muted line.
const chatBug047Styles = `.agent-question-quote{display:flex;align-items:center;gap:6px;max-width:100%;min-width:0;margin:2px 0 4px;padding:2px 8px;border:0;border-inline-start:2px solid var(--line);background:transparent;color:var(--muted);font:inherit;font-size:.8125rem;text-align:start;cursor:pointer}` +
	`.agent-question-quote .chat-icon{width:14px;height:14px;flex:none}` +
	`.agent-question-quote-text{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}` +
	`.agent-question-quote-time{flex:none;font-variant-numeric:tabular-nums;white-space:nowrap}` +
	`.agent-question-quote:hover,.agent-question-quote:focus-visible{color:var(--accent);border-inline-start-color:var(--accent)}` +
	`.agent-question-quote:disabled{cursor:default}`
