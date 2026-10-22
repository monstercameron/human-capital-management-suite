package chatui

// ChatUX050Styles is the message box at rest and the small copy around it
// (CHATUX-050).
//
// The composer is one line of text tall when it is empty: the field, then the
// tool row, about 77 px in all. Four older rules kept it near 165 px: a 60 to
// 78 px minimum on the field, a 44 px hint line that was held open for a hint
// that can only appear once there is text, a 44 px Send button and a 44 px
// tool row. The hint line now takes its room only while the draft has text (the
// only time it can be drawn), the field is one line high and grows with its
// text until the whole composer is a third of the window, and then scrolls
// inside itself. With a touch screen the controls keep their 44 px targets and
// the composer is taller by exactly that. The Aa toggle keeps its saved choice;
// the automatic empty-draft row is hidden by ChatComposerToolsStyles.
//
// Phone widths (760 px and narrower) are the phone composer's own rules; only
// the empty hint line is folded at every width, which the phone already does.
//
// Every size is a token the page already uses or a plain length, every inset is
// logical, and nothing here sets a colour, a shadow or a duration.
const ChatUX050Styles = `.chat-workspace .chat-composer .composer-hint-slot:empty{display:none}` +
	`@media(min-width:761px){` +
	`.chat-workspace form.chat-composer{padding:4px 6px;max-block-size:33.333dvh;box-sizing:border-box}` +
	// AGENTUX-071's unfocused composer rule has more specificity than the base
	// form rule above. Keep its empty-draft padding at four pixels so the field,
	// toolbar, padding and border fit CHATUX-050's 56–80 px desktop target.
	`.chat-workspace .chat-composer:not(:focus-within)[data-draft-empty="true"]{padding:4px 6px}` +
	// AGENTUX-071's unfocused composer rule has more specificity than the base
	// form rule above. Keep its empty-draft padding at four pixels so the field,
	// toolbar, padding and border fit CHATUX-050's 56–80 px desktop target.
	`.chat-workspace .chat-composer:not(:focus-within):has(.composer-input:placeholder-shown){padding:4px 6px}` +
	`.chat-workspace form.chat-composer .composer-draft{flex:0 1 auto;min-block-size:0;display:flex;flex-direction:column}` +
	`.chat-workspace form.chat-composer .composer-draft .composer-input{flex:0 1 auto;min-block-size:34px;max-block-size:none;padding-block:6px;overflow-y:auto;resize:none}` +
	`.chat-workspace form.chat-composer.chat-composer .composer-toolbar{flex:none;min-block-size:34px;padding:2px 2px 0}` +
	`.chat-workspace form.chat-composer .composer-tools .tool-button,.chat-workspace form.chat-composer .composer-tools .emoji-control,.chat-workspace form.chat-composer .composer-tools .composer-add{min-block-size:30px}` +
	`.chat-workspace form.chat-composer .send-button{min-block-size:32px;block-size:32px}` +
	`}` +
	`@media(min-width:761px) and (pointer:coarse){.chat-workspace form.chat-composer .composer-tools .composer-add-trigger,.chat-workspace form.chat-composer .composer-tools .composer-mention-button,.chat-workspace form.chat-composer .composer-tools .emoji-trigger,.chat-workspace form.chat-composer .composer-tools .giphy-trigger,.chat-workspace form.chat-composer .composer-tools .composer-format-toggle,.chat-workspace form.chat-composer .composer-tools .chattone-trigger{min-block-size:44px}.chat-workspace form.chat-composer.chat-composer .composer-toolbar{min-block-size:46px}.chat-workspace form.chat-composer .send-button{min-block-size:44px;block-size:44px}}`
