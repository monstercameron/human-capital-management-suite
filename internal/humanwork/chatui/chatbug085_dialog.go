package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATBUG-085. Report message and "Remove this message?" opened beside the
// message menu they were chosen from, at the left edge of the conversation,
// with no backdrop and no Close, in type larger than the rest of Chat, and
// their button could be pressed before a reason was chosen. They are now the
// standard Chat dialog: the layer the client opens is the backdrop
// (.chat-dialog-backdrop) and the form the server renders is the dialog in it
// (.chat-dialog), with the heading row and Close every Chat dialog has. The
// rules below are only what the moderation markup needs on top of that.
//
// Two more faults of the same report are fixed here because they are one line
// of style each: while a message menu is open no other message shows its
// action bar, and on a touch screen the add-reaction button is the size of the
// reactions beside it and stays on their row.

// chatbug085NeedsReason marks the button a dialog keeps off until a reason is
// chosen. The client reads it (chatbug085_dialog_wasm.go).
const chatbug085NeedsReason = "chatremove-needs-reason"

// ChatBug085NeedsReasonAttr is that mark as the attribute the client looks for.
const ChatBug085NeedsReasonAttr = "data-" + chatbug085NeedsReason

// ChatBug085Styles is joined into the workspace stylesheet through
// ChatMsgListStyles.
const ChatBug085Styles = `
.chat-workspace .chatremove-overlay.chatremove-overlay-dialog{z-index:1600;background:rgba(7,17,24,.5);overflow:hidden}
:root[data-hcm-color-mode="dark"] .chat-workspace .chatremove-overlay.chatremove-overlay-dialog{background:rgba(0,0,0,.62)}
@media(prefers-color-scheme:dark){:root[data-hcm-color-mode="system"] .chat-workspace .chatremove-overlay.chatremove-overlay-dialog{background:rgba(0,0,0,.62)}}
.chatremove-overlay-dialog>.chatremove-link,.chatremove-overlay-dialog>p{margin:0;padding:12px 16px;border-radius:var(--hcm-radius-control);background:var(--hcm-color-surface);color:var(--hcm-color-text);font-size:.875rem}
.chatremove-overlay-dialog .chatremove.chat-dialog{padding:0 24px 22px;font-size:.875rem;line-height:1.45}
.chatremove-overlay-dialog .chatremove .side-heading{margin:0 -24px 12px;padding:0 24px;border-block-end:1px solid var(--hcm-color-border)}
.chatremove-overlay-dialog .chatremove .side-heading h2{margin:0;font-size:1rem;line-height:1.3}
.chatremove-overlay-dialog .chatremove .side-heading .icon-button{flex:none;min-block-size:32px;inline-size:32px;block-size:32px;padding:0;border-color:transparent;background:transparent;justify-content:center}
.chatremove-overlay-dialog .chatremove>p{margin:0 0 8px;color:var(--hcm-color-text-muted)}
.chatremove-overlay-dialog .chatremove form{border:0;padding:0;margin-block:12px 0;gap:10px}
.chatremove-overlay-dialog .chatremove-quote{margin-block:4px}
.chatremove-overlay-dialog .chatremove-quote blockquote{white-space:normal;max-block-size:11rem;overflow:auto}
.chatremove-overlay-dialog .chatremove-quote .message-body{font-size:.875rem}
.chatremove-overlay-dialog .chatremove-quote .message-body>:first-child{margin-block-start:0}
.chatremove-overlay-dialog .chatremove-quote .message-body>:last-child{margin-block-end:0}
.chatremove-overlay-dialog .chatremove :is(button,a,input,textarea,label,legend,summary){font:inherit}
.chatremove-overlay-dialog .chatremove-reasons legend{font-weight:650}
.chatremove-overlay-dialog .chatremove-actions{justify-content:flex-end}
.chatremove-overlay-dialog .chatremove-actions>*{min-block-size:36px}
@media(max-width:640px),(pointer:coarse){.chatremove-overlay-dialog .chatremove-actions>*,.chatremove-overlay-dialog .chatremove .side-heading .icon-button{min-block-size:44px}.chatremove-overlay-dialog .chatremove .side-heading .icon-button{inline-size:44px;block-size:44px}}
@media(max-width:560px){.chatremove-overlay-dialog .chatremove.chat-dialog{padding:0 16px 16px}.chatremove-overlay-dialog .chatremove .side-heading{margin:0 -16px 12px;padding:0 16px}}
@media(hover:hover) and (pointer:fine){.chat-workspace .message-list:has(.message-menu) .message:not(:has(.message-menu)) .message-actions{opacity:0;pointer-events:none}}
@media(pointer:coarse),(max-width:760px){
.chat-workspace .reaction-row{align-items:center}
.chat-workspace .reaction-row>.reaction.add{position:relative;flex:none;min-width:0;min-height:0;height:32px;padding:0 11px}
.chat-workspace .reaction-row>.reaction.add::after{content:"";position:absolute;inset:-6px}
}
` + chatbug085PhoneMoreStyles

// On a phone or a touch screen a message has one action, More, inside the row
// at its top corner. It was a 40 px box (44 on touch) over a 22 px author
// line, so its lower half covered the end of the first line of the message
// ("…the vendor pushed deliv" under the button). More is now as tall as the
// author line and sits beside it, in the room the author line already keeps
// free, and still answers a 44 px touch through its ::after. A grouped message
// has no author line. On a phone its More goes in the empty gutter before the
// text, where the avatar column would be; on a wider touch screen that gutter
// shows the time, so the text keeps room free at its end instead. The room is
// always kept, whether More is shown or not, so showing it never moves a line
// (AGENTUX-058).
const chatbug085PhoneMoreStyles = `
@media(max-width:767px),(pointer:coarse){
.chat-workspace .message-list .message>.message-content>.message-meta{padding-inline-end:44px}
.chat-workspace .message-list .message .message-actions .message-action[data-action="menu"]{position:relative;inline-size:36px;min-inline-size:36px;block-size:26px;min-block-size:26px}
.chat-workspace .message-list .message .message-actions .message-action[data-action="menu"]::after{content:"";position:absolute;inset:-9px -4px}
.chat-workspace .message-list .message.continued .message-actions{top:1px}
.chat-workspace .message-list .message.continued .message-content{padding-inline-end:44px}
}
@media(max-width:760px){
.chat-workspace .message-list .message.continued .message-actions{inset-inline:10px auto}
.chat-workspace .message-list .message.continued .message-content{padding-inline-end:0}
}
`

// chatbug085Heading is the heading row of the dialog: its title and Close, as
// in every Chat dialog. Close closes the layer the dialog is in.
func chatbug085Heading(locale, title string) ui.Node {
	closeLabel := chatremoveText(locale, "close")
	return html.Div(html.Props{Class: "side-heading"},
		html.H2(html.Props{ID: "chatremove-title", Dir: "auto", Text: title}),
		html.Button(html.Props{Class: "icon-button", Type: "button", Title: closeLabel, Aria: map[string]string{"label": closeLabel}, Data: map[string]string{"chatremove-close": "true"}}, icon("close")))
}
