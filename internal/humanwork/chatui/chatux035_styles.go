package chatui

// ChatUX035Styles: in a right-to-left page a message block starts at the right
// whatever language its text is in. The body takes the page's direction (a
// direction set in the stylesheet beats the dir attribute, so a body detected
// as English still starts at the right), and the paragraphs and headings inside
// it carry dir="auto" in the markup, so their own words are ordered by their
// own language and their text-align follows the block (match-parent).
const ChatUX035Styles = `.chat-workspace[dir="rtl"] .message-body,.chat-workspace[dir="rtl"] .agent-reply-answer{direction:rtl;text-align:start}` +
	`.chat-workspace[dir="rtl"] .message-body :is(p,h3,h4,h5,h6),.chat-workspace[dir="rtl"] .agent-reply-answer :is(p,h3,h4,h5,h6){text-align:match-parent}` +
	`.chat-workspace .agent-reply-source-version{unicode-bidi:isolate;direction:ltr}` +
	// The Archived footer follows the section headers' pattern: the chevron at the
	// reading edge before the label, the label in the headers' size and weight, and
	// the same 12 px indent (8 px of list padding and 4 px of button padding), so it
	// is not the one control with its chevron at the other end.
	`.chat-workspace .chat-rail>.chatstate-archived>.chat-disclosure-button{padding-inline:12px;gap:4px;color:var(--muted);font-size:.8125rem;font-weight:600}` +
	`.chat-workspace .chat-rail>.chatstate-archived>.chat-disclosure-button>.chat-icon{order:-1;width:14px;height:14px}`
