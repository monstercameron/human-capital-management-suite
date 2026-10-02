package chatui

// chatcmd003Styles lays out the "/" command list and the poll and to-do
// preview that is drawn above the composer from what the composer holds.
//
// The command list is in the page whether it is open or not (see
// composerCommandMenuView), so it carries its own rules in place of the mention
// list's: data-open decides whether it shows, a row that does not match the
// typed word is hidden, and rows are ordered by their own order property. It is
// tall enough for every command the product has; if a skill or an integration
// adds more than fit, it scrolls, its rows keep their height, the key hint
// stays at the end of the list, and a shadow at the edge says there is more.
const chatcmd003Styles = `.chat-workspace .command-menu{position:absolute;inset-inline-start:0;inset-block-end:calc(100% + 6px);z-index:32;display:flex;flex-direction:column;box-sizing:border-box;width:min(380px,100%);max-height:min(440px,70vh);overflow-y:auto;overscroll-behavior:contain;padding:6px;border:1px solid var(--line);border-radius:var(--hcm-radius-surface);box-shadow:var(--hcm-shadow-raised);` +
	`background:linear-gradient(var(--surface) 30%,transparent) center top/100% 28px no-repeat local,linear-gradient(transparent,var(--surface) 70%) center bottom/100% 28px no-repeat local,` +
	`radial-gradient(farthest-side at 50% 0,color-mix(in srgb,var(--ink) 24%,transparent),transparent) center top/100% 10px no-repeat scroll,radial-gradient(farthest-side at 50% 100%,color-mix(in srgb,var(--ink) 24%,transparent),transparent) center bottom/100% 10px no-repeat scroll,var(--surface)}` +
	`.chat-workspace .command-menu:not([data-open^="true"]){display:none}` +
	`.chat-workspace .command-menu>.mention-heading{order:-1;flex:none}.chat-workspace .command-menu>.command-option{flex:none}.chat-workspace .command-menu>.command-option[hidden]{display:none}` +
	// A row's place among the shown rows (data-order) is its order. Twelve is
	// more commands than one word can match; a row past that keeps its place
	// in the registry.
	`.chat-workspace .command-menu>[data-order="1"]{order:1}.chat-workspace .command-menu>[data-order="2"]{order:2}.chat-workspace .command-menu>[data-order="3"]{order:3}.chat-workspace .command-menu>[data-order="4"]{order:4}` +
	`.chat-workspace .command-menu>[data-order="5"]{order:5}.chat-workspace .command-menu>[data-order="6"]{order:6}.chat-workspace .command-menu>[data-order="7"]{order:7}.chat-workspace .command-menu>[data-order="8"]{order:8}` +
	`.chat-workspace .command-menu>[data-order="9"]{order:9}.chat-workspace .command-menu>[data-order="10"]{order:10}.chat-workspace .command-menu>[data-order="11"]{order:11}.chat-workspace .command-menu>[data-order="12"]{order:12}` +
	`.chat-workspace .command-menu>.mention-hint{order:999;flex:none;position:static;margin:6px 8px 2px;padding:6px 0 2px;background:none}` +
	`.chat-workspace .command-line-none{display:none}` +
	`@media(max-width:767px){.chat-workspace .command-menu{inset-inline:0;width:100%;max-width:none;inset-block-end:calc(100% + 4px);border-radius:var(--hcm-radius-surface) var(--hcm-radius-surface) 0 0}}` +
	// The preview: the card as it will be posted, what was changed, the
	// settings and Post. It scrolls inside itself so a long list cannot push the
	// composer off the page.
	`.chat-workspace .chatcmd003-slot:empty{display:none}` +
	`.chat-workspace .chatcmd003-preview{max-height:min(46vh,440px);overflow-y:auto;overscroll-behavior:contain;gap:8px;margin:0 0 6px}` +
	// A reply box is held to two fifths of its pane; with a preview in it, it
	// may take more, and the preview itself less than in the conversation.
	`.chat-workspace .thread-composer:has(.chatcmd003-preview){max-block-size:75%}.chat-workspace .thread-composer .chatcmd003-preview{max-height:min(36vh,340px);margin:6px}` +
	`.chat-workspace .chatcmd003-problem{margin:0;color:var(--hcm-color-danger);font-size:.8125rem}.chat-workspace #chatcmd003-error{margin:0;color:var(--hcm-color-danger);font-size:.8125rem}` +
	`.chat-workspace .chatcmd003-settings{display:flex;flex-direction:column;gap:8px}.chat-workspace .chatcmd003-switches{display:flex;flex-wrap:wrap;gap:6px}` +
	`.chat-workspace .chatcmd003-setting{display:flex;flex-wrap:wrap;align-items:center;gap:4px 8px;min-width:0;font-size:.8125rem}.chat-workspace .chatcmd003-setting-label{flex:none;color:var(--muted)}` +
	`.chat-workspace .chatcmd003-choices{display:flex;flex-wrap:wrap;gap:4px;min-width:0}` +
	`.chat-workspace .chatcmd003-choice{min-block-size:28px;padding:2px 10px;border:1px solid var(--line);border-radius:999px;background:var(--surface);color:var(--ink);font:inherit;font-size:.8125rem;cursor:pointer}` +
	`.chat-workspace .chatcmd003-choice:hover:not(:disabled){background:color-mix(in srgb,var(--ink) 6%,transparent)}` +
	`.chat-workspace .chatcmd003-choice.selected{border-color:var(--accent);background:color-mix(in srgb,var(--accent) 16%,transparent);font-weight:600}` +
	`.chat-workspace .chatcmd003-choice:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:1px}.chat-workspace .chatcmd003-choice:disabled{opacity:.6;cursor:default}` +
	`.chat-workspace .chatcmd003-actions{align-items:center}.chat-workspace .chatcmd003-keys{margin-inline-start:auto;color:var(--muted);font-size:.75rem}` +
	`@media(pointer:coarse){.chat-workspace .chatcmd003-choice{min-block-size:44px;padding-inline:14px}}` +
	// At phone width an empty composer without focus is one row (CHATUX-011).
	// With a preview above it the composer stays a column, or the preview and
	// the field would share that row.
	`@media(max-width:599px){.chat-workspace .chat-composer:not(:focus-within):has(.composer-input:placeholder-shown):has(.chatcmd003-preview){flex-direction:column;align-items:stretch;gap:0}}`
