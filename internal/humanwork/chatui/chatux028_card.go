package chatui

// CHATUX-028: the answer card's row holds Helpful, Not right, Ask a follow-up
// and, when the card has more to offer, the "…" button. What a shared answer adds
// (View shared answer, Remove shared answer) is an item of that menu, beside the
// link to the copy saved in the person's own conversation with the agent. At
// phone width Helpful and Not right (and Share to channel, which sits in the row
// until the answer is shared) are icon buttons that keep their names and
// tooltips, so the row stays on one line; a note about what is happening to the
// answer takes a line of its own.

// chatux028CloseMenu closes the card's "…" menu after one of its items was used.
func chatux028CloseMenu(model Model) {
	if model.Callbacks.OpenMenu != nil {
		model.Callbacks.OpenMenu("")
	}
}

const chatUX028Styles = `.agent-reply-menu .menu-item{width:100%;border:0;background:none;font:inherit;text-align:start;cursor:pointer}.agent-reply-menu .menu-item:disabled{color:var(--muted);cursor:default}` +
	`.agent-reply-actions .agent-reply-share-note{flex:1 0 100%}` +
	`@media(max-width:480px){` +
	`.agent-reply-actions .agent-feedback{flex-wrap:nowrap}` +
	`.agent-reply-actions .agent-feedback .agent-feedback-button>span,.agent-reply-actions .agent-reply-share>span{position:absolute;width:1px;height:1px;margin:-1px;overflow:hidden;clip:rect(0 0 0 0);white-space:nowrap}` +
	`.agent-reply-actions .agent-feedback .agent-feedback-button,.agent-reply-actions .agent-reply-share{padding-inline:8px}` +
	`.agent-reply-actions .agent-follow-up{flex:1 1 0;min-width:0;justify-content:flex-start}` +
	`.agent-reply-actions .agent-follow-up>span{min-width:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}` +
	`}` +
	`@media(pointer:coarse) and (max-width:480px){.agent-reply-actions .agent-feedback .agent-feedback-button,.agent-reply-actions .agent-reply-share{min-width:44px;justify-content:center}}`
