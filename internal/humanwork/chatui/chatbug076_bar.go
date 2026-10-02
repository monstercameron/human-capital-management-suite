package chatui

// CHATBUG-076. The hover bar of a message was drawn across the message's own
// top edge, half of it over the author line: with a thread, the details or
// Saved open the name was cut to "Walt Bren..." and "Pinned" to "Pi", and
// 196 px of the author line were kept empty for the bar at every width. On a
// grouped message the bar sat on the boundary with its neighbour and it was
// not clear which message it belonged to.
//
// The bar now ends 4 px below the message's top edge, inside the message's top
// padding, and reaches up over the gap and the bottom padding of the message
// before it; it is never over the author line, so the author line keeps its
// whole width. In a column narrower than 560 px the bar holds the reaction
// picker, Reply and More, and the message menu shows Pin and Save instead
// (message_menu.go). A grouped message is tinted while its bar is shown.
//
// The first message of the list has nothing above it to reach over: its bar
// would be drawn above the list's top edge and cut off. There the bar sits
// inside the row instead, 4 px under its top edge, and that one row's author
// line keeps the room the bar takes. The earlier rule that did this
// (agentux_chat5_styles.go) has the same specificity as the placement rule
// below and comes first, so it lost; these selectors carry one part more. A
// virtualised list wraps each message in a row, so its first message is the
// one in the row after the leading spacer.
//
// A phone has no hover bar, and a touch screen of any width has only a large
// More inside the row (styles.go), so these rules are for 768 px and up with a
// pointer that can hover.

// ChatBug076Styles is joined into the workspace stylesheet through
// ChatMsgListStyles. Its selectors carry the four classes of the most specific
// rule that placed the bar before, so that they win wherever they are joined.
const ChatBug076Styles = `
@media(min-width:768px) and (pointer:fine){
.chat-workspace .message-list .message .message-actions{top:4px;transform:translateY(-100%)}
.chat-workspace .message-list .message .message-meta{padding-inline-end:0}
.chat-workspace .message-list .message.continued:has(>.message-actions){background:color-mix(in srgb,var(--hcm-color-text) 7%,transparent)}
.chat-workspace .message-list>.message:first-of-type .message-actions,.chat-workspace .message-list>[data-virtual-spacer="before"]+.virtual-row>.message .message-actions{top:4px;transform:none}
.chat-workspace .message-list>.message:first-of-type .message-meta,.chat-workspace .message-list>[data-virtual-spacer="before"]+.virtual-row>.message .message-meta{padding-inline-end:196px}
@container chatmain (max-width:560px){
.chat-workspace .message-list>.message:first-of-type .message-meta,.chat-workspace .message-list>[data-virtual-spacer="before"]+.virtual-row>.message .message-meta{padding-inline-end:84px}
}
}
@container chatmain (max-width:560px){
.chat-workspace .message-list .message-actions :is(.quick-react,.chatsave-action,[data-action="pin"],[data-action="unpin"]){display:none}
}
`
