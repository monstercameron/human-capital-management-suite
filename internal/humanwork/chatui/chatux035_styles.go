package chatui

// ChatUX035Styles: in a right-to-left page a message block starts at the right
// whatever language its text is in. The body takes the page's direction (a
// direction set in the stylesheet beats the dir attribute, so a body detected
// as English still starts at the right), and the paragraphs and headings inside
// it carry dir="auto" in the markup, so their own words are ordered by their
// own language and their text-align follows the block (match-parent).
const ChatUX035Styles = `.chat-workspace[dir="rtl"] .message-body,.chat-workspace[dir="rtl"] .agent-reply-answer{direction:rtl;text-align:start}` +
	`.chat-workspace[dir="rtl"] .message-body :is(p,h3,h4,h5,h6),.chat-workspace[dir="rtl"] .agent-reply-answer :is(p,h3,h4,h5,h6){text-align:match-parent}` +
	`.chat-workspace .agent-reply-source-version{unicode-bidi:isolate;direction:ltr}`
