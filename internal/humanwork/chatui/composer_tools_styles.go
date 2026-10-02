package chatui

// ChatComposerToolsStyles lays out the composer's tool row: the Add menu, the
// Mention and Formatting buttons, the formatting row, the Enter hint and the
// command list. It is joined last in Stylesheet so its rules win the cascade
// over the older composer rules, which it relies on only for the buttons' look.
//
// The breakpoints are the viewport's: the formatting row shows from 800 px up
// until the viewer chooses (data-format-row says "shown" or "hidden"), and the
// Enter hint shows from 600 px up. On a coarse pointer every control is at
// least 44 px.
const ChatComposerToolsStyles = `.chat-workspace .chat-composer .composer-toolbar{flex-wrap:nowrap;gap:4px}` +
	`.chat-workspace .chat-composer .composer-tools{position:relative;flex:0 1 auto;flex-wrap:nowrap!important;align-items:center;gap:2px;min-width:0}` +
	`@container chatmain (max-width:560px){.chat-workspace .chat-composer .composer-tools{flex-wrap:wrap!important}}` +
	// The Add menu opens above its button.
	`.chat-workspace .composer-add{position:relative;display:inline-flex;flex:none}` +
	`.chat-workspace .composer-add-menu{position:absolute;inset-inline-start:0;inset-block-end:calc(100% + 8px);z-index:34;display:flex;flex-direction:column;gap:2px;inline-size:min(340px,calc(100vw - 32px));max-block-size:min(360px,50vh);overflow-y:auto;padding:6px;background:var(--surface);border:1px solid var(--line);border-radius:var(--hcm-radius-surface);box-shadow:var(--hcm-shadow-raised)}` +
	`.chat-workspace .composer-add-item{align-items:center;gap:12px;min-block-size:48px;padding:8px 10px}` +
	`.chat-workspace .composer-add-item:hover,.chat-workspace .composer-add-item:focus-visible{background:color-mix(in srgb,var(--accent) 12%,transparent)}` +
	`.chat-workspace .composer-add-icon{flex:none;display:inline-flex;align-items:center;justify-content:center;inline-size:34px;block-size:34px;border-radius:var(--hcm-radius-control);background:var(--soft);color:var(--muted)}` +
	`.chat-workspace .composer-add-icon .chat-icon{inline-size:18px;block-size:18px}` +
	`.chat-workspace .composer-add-text{display:flex;flex-direction:column;gap:1px;min-width:0}` +
	`.chat-workspace .composer-add-name{color:var(--ink);font-size:.875rem;font-weight:600}` +
	`.chat-workspace .composer-add-note{color:var(--muted);font-size:.75rem;line-height:1.3}` +
	`.chat-workspace .composer-add-trigger[aria-expanded="true"],.chat-workspace .composer-format-toggle[aria-pressed="true"]{background:color-mix(in srgb,var(--accent) 16%,transparent);color:var(--ink)}` +
	`.chat-workspace .composer-glyph{font-size:.875rem;font-weight:700;line-height:1;letter-spacing:0;pointer-events:none}.chat-workspace .composer-mention-button .composer-glyph{font-size:1rem}` +
	// Voice message and Location are opened from the Add menu. Their own buttons
	// stay in the page as the openers their panels anchor to and return focus to,
	// laid over the + button, drawn as nothing and out of the tab order.
	`.chat-workspace .chat-composer .composer-tools>.chatvoice-tool,.chat-workspace .chat-composer .composer-tools>.chatmap-control{display:contents}` +
	`.chat-workspace .chat-composer .composer-tools>.chatvoice-tool>.tool-button,.chat-workspace .chat-composer .composer-tools>.chatmap-control>.tool-button{position:absolute;inset-block-start:0;inset-inline-start:0;background:transparent;color:transparent;pointer-events:none}` +
	`.chat-workspace .chat-composer .composer-tools>.chatvoice-tool>.tool-button .chat-icon,.chat-workspace .chat-composer .composer-tools>.chatmap-control>.tool-button .chat-icon{display:none}` +
	// The formatting row sits between the text and the tool row.
	`.chat-workspace .chat-composer .composer-format-row{display:none;align-items:center;flex:none;min-width:0;padding:2px 2px 0}` +
	`.chat-workspace .chat-composer[data-format-row="shown"] .composer-format-row{display:flex}` +
	`@media(min-width:800px){.chat-workspace .chat-composer[data-format-row="auto"] .composer-format-row{display:flex}}` +
	`.chat-workspace .composer-format-row .format-tools{display:flex;flex-wrap:wrap;gap:2px;margin:0;padding:0;border:0}` +
	`.chat-workspace .composer-format-row .format-inline,.chat-workspace .thread-composer .format-inline{display:flex;align-items:center;gap:2px}` +
	// The Enter hint: muted, at the right, from 600 px up.
	`.chat-workspace .chat-composer .composer-toolbar .composer-help{display:none}` +
	`@media(min-width:600px){.chat-workspace .chat-composer .composer-toolbar .composer-help{display:block;position:static;flex:1 1 0%;min-width:0;margin:0;padding-inline:8px;overflow:hidden;white-space:nowrap;text-overflow:ellipsis;text-align:end;color:var(--muted);font-size:.75rem;opacity:1}}` +
	// Send is at the right with or without the hint.
	`.chat-workspace .chat-composer .composer-toolbar .send-button{margin-inline-start:auto}` +
	// An opener pressed from the Add menu shows where focus is: a ring over the +.
	`.chat-workspace .chat-composer .composer-tools>.chatvoice-tool>.tool-button:focus-visible,.chat-workspace .chat-composer .composer-tools>.chatmap-control>.tool-button:focus-visible{outline:2px solid var(--accent);outline-offset:-2px}` +
	// CHATUX-011, phone width (under 600 px). The empty hint slot gives its 44 px
	// back, and the field is one line tall, growing with its text to six lines
	// and then scrolling. While the composer holds neither focus nor text it is
	// one row, the field and Send, and the tool row and the formatting row are
	// out of the page; focus or text brings them back. :focus-within and
	// :placeholder-shown are CSS state: nothing waits for a focus event or a timer.
	`@media(max-width:599px){` +
	`.chat-workspace .chat-composer .composer-hint-slot:empty{display:none}` +
	`.chat-workspace .chat-composer .composer-draft .composer-input{min-block-size:40px;max-block-size:calc(6*1.45*.9375rem + 18px);padding-block:9px;overflow-y:auto;resize:none}` +
	`.chat-workspace .chat-composer:not(:focus-within):has(.composer-input:placeholder-shown){flex-direction:row;align-items:center;gap:4px;padding:3px 6px}` +
	`.chat-workspace .chat-composer:not(:focus-within):has(.composer-input:placeholder-shown) .composer-draft{flex:1 1 0%;min-width:0}` +
	`.chat-workspace .chat-composer:not(:focus-within):has(.composer-input:placeholder-shown) .composer-toolbar{flex:none;min-height:0;padding:0}` +
	`.chat-workspace .chat-composer:not(:focus-within):has(.composer-input:placeholder-shown) :is(.composer-tools,.composer-help,.composer-format-row,.composer-embeds){display:none}` +
	`}` +
	// The command list reads like the mention list; a long description gives way.
	`.chat-workspace .command-option .mention-name{flex:none}.chat-workspace .command-option .mention-detail{flex:1 1 auto;min-width:0;overflow:hidden;text-overflow:ellipsis;text-align:start}` +
	// Coarse pointers: every control is at least 44 px.
	`@media(pointer:coarse){.chat-workspace .chat-composer .composer-tools .tool-button,.chat-workspace .chat-composer .composer-format-row .tool-button{min-inline-size:44px;min-block-size:44px}.chat-workspace .chat-composer .send-button{min-inline-size:44px;min-block-size:44px}.chat-workspace .composer-add-item,.chat-workspace .command-option{min-block-size:48px}}`
