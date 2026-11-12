package chatui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// The work-object card (#3777) is the one card Chat draws for a piece of work
// someone shared: today a journey and a project ticket or board. Its parts are
// named once and both previews use them:
//
//	chat-work-card          the card
//	chat-work-card-type     the type icon and the kind of object
//	chat-work-card-title    what it is
//	chat-work-card-status   the status chip
//	chat-work-card-waits    who it waits on
//	chat-work-card-action   the primary action slot, the card's last row
//
// A card is informational unless the open object waits on the reader. Then the
// card carries data-waits="you", the accent frame and tint, and the action
// slot says so in words, so the difference is not colour alone.

// workCardWaitsOnViewer reports whether an open work object waits on the
// reader. The previews carry the display name the resolver authorized, not an
// id, so the match is on that name.
func workCardWaitsOnViewer(m Model, name string, open bool) bool {
	name, viewer := strings.TrimSpace(name), strings.TrimSpace(m.CurrentUserName)
	return open && name != "" && viewer != "" && strings.EqualFold(name, viewer)
}

// workCardWaitingText is the action slot's sentence for the reader.
func workCardWaitingText(locale string) string {
	switch lower := strings.ToLower(locale); {
	case strings.HasPrefix(lower, "de"):
		return "Wartet auf Sie"
	case strings.HasPrefix(lower, "ar"):
		return "بانتظارك"
	}
	return "Waiting on you"
}

// workCardProps turns an embed's root props into the shared card's: the card
// class, and for a card that waits on the reader the marker the style reads
// and the same fact in the link's accessible name.
func workCardProps(m Model, props html.Props, waits bool) html.Props {
	props.Class += " chat-work-card"
	if !waits {
		return props
	}
	data := map[string]string{"waits": "you"}
	for name, value := range props.Data {
		data[name] = value
	}
	props.Data = data
	if label := props.Aria["label"]; label != "" {
		aria := map[string]string{}
		for name, value := range props.Aria {
			aria[name] = value
		}
		aria["label"] = label + ", " + workCardWaitingText(m.Locale)
		props.Aria = aria
	}
	return props
}

// workCardAction fills the primary action slot. The card itself is the link
// that opens the object, so the slot holds the one thing that asks for the
// reader: nothing on an informational card.
func workCardAction(m Model, waits bool) ui.Node {
	if !waits {
		return nil
	}
	return html.Span(html.Props{Class: "chat-work-card-action", Text: workCardWaitingText(m.Locale)})
}

// chatDesignWorkCardStyles is the card's one style. It replaces the per-kind
// bar colours (a journey's warning bar, a board's info bar): the status chip
// says the state, and the frame says only whether the card is asking for the
// reader. Informational cards sit on the surface with a neutral bar.
const chatDesignWorkCardStyles = `.chat-workspace .chat-work-card{border:1px solid var(--line);border-inline-start:3px solid var(--hcm-color-control-border);background:var(--surface)}` +
	`.chat-workspace .chat-work-card[data-waits="you"]{border-color:var(--accent);border-inline-start-color:var(--accent);background:var(--soft)}` +
	`.chat-workspace .chat-work-card-type{display:inline-flex;align-items:center;gap:5px;font-weight:600}` +
	`.chat-workspace .chat-work-card-title{font-size:.9375rem;font-weight:700;color:var(--ink)}` +
	`.chat-workspace .chat-work-card-status{box-shadow:inset 0 0 0 1px var(--line)}` +
	`.chat-workspace .chat-work-card-waits{display:inline-flex;align-items:center;gap:5px;color:var(--ink);font-weight:600}` +
	`.chat-workspace .chat-work-card-action{justify-self:start;display:inline-flex;align-items:center;min-block-size:24px;margin-block-start:4px;padding:0 10px;border-radius:999px;background:var(--accent);color:var(--hcm-color-on-brand);font-size:.75rem;font-weight:700}` +
	`@media(forced-colors:active){.chat-workspace .chat-work-card[data-waits="you"]{border-width:2px}}`
