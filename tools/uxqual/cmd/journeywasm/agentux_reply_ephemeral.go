package main

import (
	"strings"
	"time"

	chatv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/chat/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// agentReplyEphemeralStream keeps the ordinary event drain unchanged while
// consuming the recipient-only envelope carried beside conversation events.
type agentReplyEphemeralStream struct {
	next      chatEventStream
	apply     func(*chatv1.EphemeralDelivery, string) bool
	delivered bool
}

func (s *agentReplyEphemeralStream) Recv() (*chatv1.WatchConversationResponse, error) {
	message, err := s.next.Recv()
	if err == nil && message != nil && message.GetEphemeralDelivery() != nil && s.apply != nil {
		s.delivered = s.apply(message.GetEphemeralDelivery(), message.GetResumeCursor()) || s.delivered
	}
	return message, err
}

func agentReplyEphemeralMessage(delivery *chatv1.EphemeralDelivery, now time.Time) (chatui.EphemeralMessage, bool) {
	if delivery == nil || strings.TrimSpace(delivery.GetId()) == "" || strings.TrimSpace(delivery.GetThreadId()) == "" || strings.TrimSpace(delivery.GetBody()) == "" || !delivery.GetOnlyVisibleToYou() || delivery.GetCreatedAt() == nil || delivery.GetExpiresAt() == nil {
		return chatui.EphemeralMessage{}, false
	}
	created, expires := delivery.GetCreatedAt().AsTime(), delivery.GetExpiresAt().AsTime()
	if created.IsZero() || expires.IsZero() || !created.Before(expires) || !now.Before(expires) {
		return chatui.EphemeralMessage{}, false
	}
	return chatui.EphemeralMessage{ID: delivery.GetId(), ThreadID: delivery.GetThreadId(), Body: delivery.GetBody(), OnlyVisibleToYou: true, CreatedAt: created, ExpiresAt: expires, ThreadLink: delivery.GetThreadLink()}, true
}

func applyAgentReplyEphemeral(model *chatui.Model, delivery *chatv1.EphemeralDelivery, now time.Time) bool {
	message, ok := agentReplyEphemeralMessage(delivery, now)
	if model == nil || !ok {
		return false
	}
	for index := range model.EphemeralMessages {
		if model.EphemeralMessages[index].ID == message.ID {
			model.EphemeralMessages[index] = message
			return true
		}
	}
	model.EphemeralMessages = append(model.EphemeralMessages, message)
	return true
}

// preservePersonaElapsed keeps the seconds a working row has counted when the
// next reading of the same run arrives. The server says how long the run has
// been going since its admission (AGENTUX-026), which is the time shown after
// a reload or a switch of conversation; between two readings the page counts
// on by itself, and the count never steps back when the two differ by a
// second.
func preservePersonaElapsed(previous, next []chatui.PersonaThreadInvocation) []chatui.PersonaThreadInvocation {
	elapsed := make(map[string]int, len(previous))
	for _, invocation := range previous {
		if invocation.Projection.Progress != nil {
			elapsed[invocation.Projection.InvocationID] = invocation.Projection.Progress.ElapsedSeconds
		}
	}
	for index := range next {
		if progress := next[index].Projection.Progress; progress != nil {
			progress.ElapsedSeconds = max(progress.ElapsedSeconds, elapsed[next[index].Projection.InvocationID])
		}
	}
	return next
}

func advancePersonaElapsed(model *chatui.Model) bool {
	if model == nil {
		return false
	}
	changed := false
	for index := range model.PersonaInvocations {
		progress := model.PersonaInvocations[index].Projection.Progress
		if agentProgressExpired(progress, time.Now()) {
			// Past its deadline the card is drawn as an interrupted answer and
			// stops counting; one more render, within two ticks, draws it so.
			changed = changed || time.Since(progress.Deadline) < 2*time.Second
			continue
		}
		if model.PersonaInvocations[index].Projection.AnswerStored {
			// A finished run does not count seconds (CHATBUG-079).
			continue
		}
		if progress != nil && progress.Visible && !progress.ResultReady {
			progress.ElapsedSeconds++
			changed = true
		}
	}
	return changed
}
