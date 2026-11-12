package chatui

// chatDesignUnreadBadgeClass is the class of a conversation row's unread count
// (#3772). A direct message asks for the reader the way a mention does, so its
// count takes the accent fill; an ordinary channel's count is the neutral pill.
func chatDesignUnreadBadgeClass(c Conversation) string {
	if c.Kind == DirectMessage {
		return "chat-badge direct"
	}
	return "chat-badge"
}

// chatDesignUnreadStyles puts every count in the conversation list in a pill
// (#3772). Unread in a channel, the total on a collapsed section, Saved and
// Moderation are the neutral pill (--chat-pill-neutral: the text colour mixed
// into the surface, so it follows the theme); mentions and direct messages are
// the accent pill. The accessible names are untouched: the pill is only paint.
// A count hidden with the hidden attribute stays hidden.
const chatDesignUnreadStyles = `.chat-workspace{--chat-pill-neutral:color-mix(in srgb,var(--ink) 14%,var(--surface))}` +
	`.chat-workspace :is(.chat-rail-row .chat-badge,.section-title .chat-badge.section-unread,.chatsave-sidebar-row>.chat-count:not([hidden])){display:inline-flex;align-items:center;justify-content:center;flex:none;box-sizing:border-box;min-inline-size:20px;block-size:20px;padding:0 6px;border-radius:999px;background:var(--chat-pill-neutral);color:var(--ink);font-size:.6875rem;font-weight:700;line-height:1;font-variant-numeric:tabular-nums}` +
	`.chat-workspace :is(.chat-rail-row,.section-title) .chat-badge:is(.mention,.direct){background:var(--accent);color:var(--hcm-color-on-brand)}`
