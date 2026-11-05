package chatui

// ChatUX097Styles holds the thread pane's narrow-composer rules and the
// landings under the message list's own header.
const ChatUX097Styles = `` +
	// The thread composer's tools wrap inside the narrow pane: the base
	// tools rule pins nowrap with !important, and without this the last
	// tools slide under Reply.
	`.chat-workspace .thread-composer .composer-tools{flex-wrap:wrap!important}` +
	// The list fades its top 16 px under the header (styles.go); a reveal or
	// a jump parks 20 px down so the landed row is never caught half-faded.
	// The earlier 8 px rule stays where it was pinned (CHATUX-049).
	`.message-list{scroll-padding-top:20px}`
