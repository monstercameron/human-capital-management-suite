package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Every day separator is sticky so the reader keeps the date in view, but sticky
// siblings in one flat list all stick at the same offset and paint over each
// other (#3871). The flat timeline therefore puts each day's separator and its
// messages in one wrapper: a pill stays stuck only while its own day is on
// screen and the next day's pill replaces it. The wrapper keeps the list's
// column layout, so rows are laid out exactly as before; the hover-bar rules for
// the first row after a separator are repeated for the wrapper's children.
const issue3871DayGroupStyles = `.chat-workspace .message-list .day-group{display:flex;flex-direction:column;flex:none;min-width:0}` +
	`@media(min-width:768px) and (pointer:fine){` +
	`.chat-workspace .message-list>.day-group>.message:first-of-type .message-actions,.chat-workspace .message-list>.day-group>.day-divider + .message .message-actions,.chat-workspace .message-list>.day-group>.unread-divider + .message .message-actions{top:4px;transform:none}` +
	`.chat-workspace .message-list>.day-group>.message:first-of-type .message-meta,.chat-workspace .message-list>.day-group>.day-divider + .message .message-meta,.chat-workspace .message-list>.day-group>.unread-divider + .message .message-meta{padding-inline-end:196px}` +
	`@container chatmain (max-width:560px){.chat-workspace .message-list>.day-group>.message:first-of-type .message-meta,.chat-workspace .message-list>.day-group>.day-divider + .message .message-meta,.chat-workspace .message-list>.day-group>.unread-divider + .message .message-meta{padding-inline-end:84px}}` +
	`}`

// issue3871Group wraps one day's rows; day is the day the group holds.
func issue3871Group(day string, rows []ui.Node) ui.Node {
	return html.WithKey(html.Div(html.Props{Class: "day-group"}, rows...), "daygroup:"+day)
}
