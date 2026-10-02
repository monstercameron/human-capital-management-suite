package chatui

import (
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// EphemeralMessage is the viewer-scoped projection delivered by the chat
// stream. It is intentionally separate from Message: ephemeral content must
// never be treated as conversation history, search data, or an unread post.
type EphemeralMessage struct {
	ID               string
	ThreadID         string
	Body             string
	OnlyVisibleToYou bool
	CreatedAt        time.Time
	ExpiresAt        time.Time
	ThreadLink       string
}

// VisibleEphemeralMessages filters the stream projection at render time. A
// missing expiry is rejected, and the boundary is exclusive: a message is
// gone at its expiry instant. The input order is retained for cursor order.
func VisibleEphemeralMessages(messages []EphemeralMessage, now time.Time) []EphemeralMessage {
	visible := make([]EphemeralMessage, 0, len(messages))
	for _, message := range messages {
		if !message.OnlyVisibleToYou || message.ID == "" || strings.TrimSpace(message.Body) == "" || message.ExpiresAt.IsZero() || !now.Before(message.ExpiresAt) {
			continue
		}
		visible = append(visible, message)
	}
	return visible
}

// RenderEphemeralMessage renders the recipient-only conversation item. The
// marker is text and an accessible label so it remains clear in forced-colour
// mode and to assistive technology; callers should filter with
// VisibleEphemeralMessages before invoking it.
func RenderEphemeralMessage(model Model, message EphemeralMessage, now time.Time) ui.Node {
	if len(VisibleEphemeralMessages([]EphemeralMessage{message}, now)) == 0 {
		return html.Div(html.Props{Class: "chat-ephemeral chat-ephemeral-expired", Aria: map[string]string{"hidden": "true"}})
	}
	for _, post := range model.Messages {
		if post.ID == message.ThreadID {
			return html.Div(html.Props{Class: "chat-ephemeral chat-ephemeral-thread-owned", Hidden: true})
		}
	}
	return renderPersonaPrivateAnswer(model, localUI{}, message, PersonaProgressProjection{})
}

func ephemeralText(model Model, key, fallback string) string {
	return chatbug039Text(key, agentReplyFallback(model.Locale, key, fallback), fallback)
}

// EphemeralStyles is appended by the chat shell when the renderer is wired
// into the timeline. The reduced-motion rule keeps insertion free of motion
// even when the host stylesheet supplies a transition for chat cards.
const EphemeralStyles = `.chat-ephemeral{margin:8px 16px;padding:10px 12px;border:1px solid var(--line);border-inline-start:3px solid var(--accent);border-radius:var(--hcm-radius-control);background:var(--soft);overflow-wrap:anywhere}.chat-ephemeral-marker{font-size:.75rem;font-weight:650;color:var(--muted)}.chat-ephemeral-body{margin:4px 0;white-space:pre-wrap}.chat-ephemeral-thread-link{font-size:.8125rem;color:var(--accent)}.chat-ephemeral-expired{display:none}@media(prefers-reduced-motion:reduce){.chat-ephemeral,.chat-ephemeral *{animation:none!important;transition:none!important;scroll-behavior:auto!important}}`
