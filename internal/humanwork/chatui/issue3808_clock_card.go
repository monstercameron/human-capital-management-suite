package chatui

import (
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
)

// Issue 3808: a time-clock card in a conversation. The post's body names the
// kind of card, whom it is about and the day; the sentence is written here,
// from names the page already holds, in the reader's language. The worker's
// card opens the missing-punch page, which runs on the worker's own session; a
// supervisor's card says whose punch it is and has no action, because nothing
// the supervisor could press here would be theirs to decide.

// issue3808FixPunchPath is the page a worker fixes a punch on. The href is
// built from this constant, never from the post, so a card cannot point away.
const issue3808FixPunchPath = "/workspace/app/time/missing-punch"

const (
	keyIssue3808Title    = "chat.issue3808.title"
	keyIssue3808Self     = "chat.issue3808.self"
	keyIssue3808Review   = "chat.issue3808.review"
	keyIssue3808Action   = "chat.issue3808.action"
	keyIssue3808Teammate = "chat.issue3808.teammate"
)

var issue3808Copy = map[string]map[string]string{
	"en-US": {
		keyIssue3808Title:    "Missing punch",
		keyIssue3808Self:     "You have a missing punch for {day}.",
		keyIssue3808Review:   "{name}'s punch for {day} needs review.",
		keyIssue3808Action:   "Fix punch",
		keyIssue3808Teammate: "A teammate",
	},
	"de-DE": {
		keyIssue3808Title:    "Fehlende Stempelung",
		keyIssue3808Self:     "Für {day} fehlt Ihre Stempelung.",
		keyIssue3808Review:   "Die Stempelung von {name} für {day} muss geprüft werden.",
		keyIssue3808Action:   "Stempelung korrigieren",
		keyIssue3808Teammate: "Ein Teammitglied",
	},
	"ar": {
		keyIssue3808Title:    "بصمة ناقصة",
		keyIssue3808Self:     "لديك بصمة ناقصة ليوم {day}.",
		keyIssue3808Review:   "بصمة {name} ليوم {day} تحتاج إلى مراجعة.",
		keyIssue3808Action:   "تصحيح البصمة",
		keyIssue3808Teammate: "زميل",
	},
}

// issue3808CardText is the sentence a time-clock card stands for, or false for
// any other message.
func issue3808CardText(m Model, msg Message) (string, bool) {
	card, ok := chat.ParseClockCard(msg.Body)
	if !ok {
		return "", false
	}
	day := card.Day
	if at, err := time.Parse("2006-01-02", card.Day); err == nil {
		day = chatsave002Weekday(m.Locale, at)
	}
	if card.Audience == chat.ClockCardSelf {
		return laneTextf(m, issue3808Copy, keyIssue3808Self, map[string]string{"day": day}), true
	}
	name := chatKnownName(m, card.Subject)
	if name == "" {
		name = laneText(m, issue3808Copy, keyIssue3808Teammate)
	}
	return laneTextf(m, issue3808Copy, keyIssue3808Review, map[string]string{"day": day, "name": name}), true
}

// issue3808ClockCard draws the card as its own row: a heading, the sentence and,
// for the worker, one link. It is a labelled region, not a message: no avatar,
// no reply or react actions.
func issue3808ClockCard(m Model, msg Message) (ui.Node, bool) {
	text, ok := issue3808CardText(m, msg)
	if !ok {
		return nil, false
	}
	card, _ := chat.ParseClockCard(msg.Body)
	title := laneText(m, issue3808Copy, keyIssue3808Title)
	children := []ui.Node{
		html.Strong(html.Props{Class: "chat-clock-card-title", Dir: "auto", Text: title}),
		html.P(html.Props{Class: "chat-clock-card-text", Dir: "auto", Text: text}),
	}
	if card.Audience == chat.ClockCardSelf {
		children = append(children, html.A(html.Props{Class: "button primary small chat-clock-card-action", Href: issue3808FixPunchPath, Data: map[string]string{"action": "fix-punch"}}, ui.Text(laneText(m, issue3808Copy, keyIssue3808Action))))
	}
	if msg.TimeLabel != "" {
		children = append(children, html.Time(html.Props{Class: "message-time", Text: msg.TimeLabel}))
	}
	region := html.Section(html.Props{Class: "chat-clock-card", Aria: map[string]string{"label": title}}, children...)
	return html.Div(html.Props{Class: "message-row chat-clock-card-row", Data: map[string]string{"message-id": msg.ID}}, region), true
}

// Issue3808Styles lays the card out for a phone first: the action is a 44px
// target, the card wraps rather than scrolls at 320px, and logical properties
// keep it right in a right-to-left language.
const Issue3808Styles = `
.chat-clock-card-row{display:flex;padding:6px 16px}
.chat-clock-card{display:flex;flex-direction:column;align-items:flex-start;gap:6px;box-sizing:border-box;max-inline-size:min(100%,32rem);min-inline-size:0;padding:12px 14px;border:1px solid var(--hcm-color-border);border-inline-start:3px solid var(--hcm-color-brand-primary);border-radius:var(--hcm-radius-control);background:var(--soft);overflow-wrap:anywhere}
.chat-clock-card-title{font-size:.875rem}
.chat-clock-card-text{margin:0;font-size:.875rem}
.chat-clock-card-action{display:inline-flex;align-items:center;justify-content:center;min-block-size:44px;min-inline-size:44px;text-decoration:none}
.chat-clock-card .message-time{font-size:.6875rem;color:var(--hcm-color-text-muted)}
`
