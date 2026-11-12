package chatui

// chatDesignThreadComposerStyles gives the thread reply composer the main
// composer's tool row (#3774). The tools shared the row's free space with the
// keyboard hint (both flex:1), so in the narrow pane they were squeezed into
// two short rows beside a tall Reply button. Now, as in the main composer:
//   - the row is divided from the field by a hairline and has the same
//     padding and minimum height;
//   - the tools are one row at their own width, at the start edge;
//   - Reply sits at the end edge, centred on the row, at its own height;
//   - the hint keeps only the space left over and disappears first.
//
// If the tools and Reply cannot share the pane, Reply moves to a line of its
// own instead of the tools sliding under it (the case CHATUX-097 wrapped for).
// "Also send to #channel" is the last strip of that same row: full width,
// under a hairline, starting at the tools' edge.
const chatDesignThreadComposerStyles = `.chat-workspace .thread-composer .composer-toolbar.thread-toolbar{flex-wrap:wrap;align-items:center;justify-content:flex-start;gap:2px 6px;min-block-size:40px;padding:4px 2px 0;border-block-start:1px solid var(--line)}` +
	`.chat-workspace .thread-composer .thread-toolbar>.composer-tools{flex:0 0 auto;flex-wrap:nowrap!important}` +
	`.chat-workspace .thread-composer .thread-toolbar>.composer-help{flex:1 1 0%;min-inline-size:0}` +
	`.chat-workspace .thread-composer .thread-toolbar>.send-button{flex:none;align-self:center;margin-inline-start:auto}` +
	`.chat-workspace .thread-composer .thread-toolbar>.thread-also{order:3;flex:1 1 100%;min-block-size:28px;margin:4px -2px 0;padding:2px 8px;border-block-start:1px solid var(--line)}` +
	`@media(pointer:coarse){.chat-workspace .thread-composer .thread-toolbar>.thread-also{min-block-size:44px}}`
