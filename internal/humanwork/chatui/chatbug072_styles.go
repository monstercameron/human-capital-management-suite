package chatui

// ChatLane3Styles gathers the rules this lane's todos add (CHATBUG-072,
// CHATUX-021, CHATUX-019 and the ones after). It is joined after the other
// chat rules so it wins ties, and each todo appends its own constant.
const ChatLane3Styles = ChatBug072Styles + ChatUX021Styles + ChatBug048Styles + ChatUX020Styles + ChatlangPageStyles + ChatUX027Styles + ChatSide001Styles

// ChatBug072Styles is the create dialog's name rule note: the hint turns to the
// error colour when a character was refused.
const ChatBug072Styles = `
.field-hint.field-error{color:var(--hcm-color-danger)}
`
