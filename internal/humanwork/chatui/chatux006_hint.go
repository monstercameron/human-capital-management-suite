package chatui

import "time"

// chatux006HintWindow is how long after it was sent a message still offers to
// ask the agent it named without mentioning.
const chatux006HintWindow = 10 * time.Minute

// chatux006HintVisible reports whether the "was not mentioned" hint belongs
// under a message now. A hint is useful once, at the moment of the mistake: it
// is the author's own message, and either it is the newest message in the
// conversation or it was sent less than ten minutes ago. Nobody else sees it,
// and it does not stay on old messages.
func chatux006HintVisible(model Model, message Message, now time.Time) bool {
	if model.CurrentUser == "" || message.AuthorID != model.CurrentUser {
		return false
	}
	for _, messages := range [][]Message{model.Messages, model.ThreadMessages} {
		if len(messages) > 0 && messages[len(messages)-1].ID == message.ID && message.ID != "" {
			return true
		}
	}
	return !message.SentAt.IsZero() && now.Sub(message.SentAt) < chatux006HintWindow
}

// chatux006HintNow is chatux006HintVisible at the present moment.
func chatux006HintNow(model Model, message Message) bool {
	return chatux006HintVisible(model, message, time.Now())
}
