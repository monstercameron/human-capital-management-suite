package chatui

import (
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATGATE-005: a person is told what joining through a gate involves before
// they press anything, and a member is told when the questions changed.
//
// The Browse list used to offer "Join" on a gated channel and answer the press
// with a form. The row now says "Answer 3 questions to join", what the channel
// is for, and whether answers admit at once or wait for a person. A member of
// a channel whose gate had a major change sees one line above the conversation
// with the date and a button that opens the form; before, the sentence existed
// only inside the form, which nothing opened for them.

// GateJoin is what the page knows about one channel's gate before the form is
// opened (Model.GateJoins, by conversation). The server lists only the gates
// the viewer may know of. A nil map means the list was not read: the page then
// offers a plain Join and asks about the gate when it is pressed.
type GateJoin struct {
	// Questions is how many questions the form asks.
	Questions int
	// Purpose is what the gate says it is for; ChannelPurpose what the channel is.
	Purpose, ChannelPurpose string
	// Mode is "automatic", "rule" or "review".
	Mode string
	// AnswerAgain is true for a member whose accepted answers are for an earlier
	// major version; AnswerBy is the date they have.
	AnswerAgain bool
	AnswerBy    time.Time
}

// chatgateJoin is the gate a non-member would join the conversation through.
func chatgateJoin(m Model, c Conversation) (GateJoin, bool) {
	if m.ChatFeatures != nil && !m.ChatFeatures.Gates {
		return GateJoin{}, false
	}
	gate, ok := m.GateJoins[c.ID]
	return gate, ok && gate.Questions > 0
}

// chatgateJoinLabel is the text of a Browse row's join button: "Join", or for a
// gated channel what joining takes.
func chatgateJoinLabel(m Model, c Conversation) string {
	if gate, ok := chatgateJoin(m, c); ok {
		return gateJoinTitle(chatgateLocale(m.Locale), gate.Questions)
	}
	return m.t(KeyJoin)
}

// chatgateLocale is the locale the gate copy is kept under.
func chatgateLocale(locale string) string {
	switch {
	case strings.HasPrefix(locale, "de"):
		return "de-DE"
	case strings.HasPrefix(locale, "ar"):
		return "ar"
	}
	return "en-US"
}

// chatgateBrowseNote is the line under a gated channel's name in Browse: what
// it is for and what happens to a request. Nil for a channel with no gate.
func chatgateBrowseNote(m Model, c Conversation) ui.Node {
	gate, ok := chatgateJoin(m, c)
	if !ok || c.Joined {
		return nil
	}
	locale := chatgateLocale(m.Locale)
	parts := []string{}
	for _, purpose := range []string{gate.ChannelPurpose, gate.Purpose} {
		if purpose = strings.TrimSpace(purpose); purpose != "" && (len(parts) == 0 || parts[0] != purpose) {
			parts = append(parts, purpose)
		}
	}
	how := "browseReview"
	switch gate.Mode {
	case "automatic":
		how = "browseAutomatic"
	case "rule":
		how = "browseRule"
	}
	parts = append(parts, GateText(locale, how))
	return html.P(html.Props{Class: "chatgate-browse-note", Dir: "auto", Text: strings.Join(parts, " · ")})
}

// chatgateBanner is the line above a conversation for a member who must answer
// its gate again. Nil for everyone else.
func chatgateBanner(m Model) ui.Node {
	if m.SelectedID == "" || (m.ChatFeatures != nil && !m.ChatFeatures.Gates) {
		return nil
	}
	gate, ok := m.GateJoins[m.SelectedID]
	if !ok || !gate.AnswerAgain {
		return nil
	}
	locale := chatgateLocale(m.Locale)
	text := GateText(locale, "changedSoon")
	if !gate.AnswerBy.IsZero() {
		text = fmt.Sprintf(GateText(locale, "changed"), formatDay(m.Locale, gate.AnswerBy, gate.AnswerBy.Year() != time.Now().Year()))
	}
	return html.Div(html.Props{Class: "chatgate-banner", Role: "status", Dir: direction(m.Locale)},
		icon("pin"), html.Span(html.Props{Class: "chatgate-banner-text", Text: text}),
		html.Button(html.Props{Type: "button", Class: "button small", Text: GateText(locale, "answerNow"), Data: map[string]string{"gate-open": m.SelectedID}}))
}

// chatgateBrowseStyles: the note is a muted line that wraps; the banner is one
// row above the messages that wraps on a phone.
const chatgateBrowseStyles = `.chat-workspace .chatgate-browse-note{margin:2px 0 0;color:var(--hcm-color-text-muted);font-size:.8125rem;overflow-wrap:anywhere}` +
	`.chat-workspace .chatgate-banner{display:flex;flex-wrap:wrap;align-items:center;gap:8px;margin:8px 20px 0;padding:8px 12px;border:1px solid var(--hcm-color-border);border-inline-start:3px solid var(--hcm-color-brand-primary);border-radius:var(--hcm-radius-control);background:var(--hcm-color-brand-soft);color:var(--hcm-color-text)}` +
	`.chat-workspace .chatgate-banner .chat-icon{flex:none;inline-size:16px;block-size:16px}` +
	`.chat-workspace .chatgate-banner-text{flex:1 1 200px;min-inline-size:0;overflow-wrap:anywhere}` +
	`.chat-workspace .chatgate-banner .button{flex:none;min-block-size:32px}` +
	`@media(max-width:767px){.chat-workspace .chatgate-banner{margin-inline:12px}}` +
	`@media(pointer:coarse){.chat-workspace .chatgate-banner .button{min-block-size:44px}}`
