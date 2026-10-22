package chatui

// ChatUX048Styles finishes three places an administrator looks at (CHATUX-048):
//
//   - the reword settings in Chat preferences are rows like the ones above them:
//     the same 44 px row with a 12 px inline padding, the label first, a switch
//     or a segmented control for its answer, one line of help under the label
//     and the saved tick at the row end;
//   - Remove in Moderation is the danger style (an outlined button in the danger
//     colour, the way Leave a channel is), never a filled one;
//   - a removed message is a row of the message grid with its notice in the text
//     column, and Restore is the standard secondary button.
//
// Every colour, radius and size is a token the page already uses, every inset is
// logical, and nothing here sets a duration or a shadow.
const ChatUX048Styles = `.chat-workspace .chattone-reword{display:grid;gap:0;min-inline-size:0}` +
	`.chat-workspace .chattone-reword-help,.chat-workspace .chattone-reword-note{margin:0;padding:0 12px 8px;font-size:.75rem;line-height:16px;color:var(--muted);overflow-wrap:anywhere}` +
	`.chat-workspace .chattone-reword-intro{padding-block-start:0}` +
	`.chat-workspace .chattone-reword-admin{margin:0;padding:12px 12px 0;border-block-start:1px solid var(--line);font-size:.75rem;font-weight:600;line-height:16px;color:var(--muted)}` +
	`.chat-workspace .chattone-reword-scope{margin:0;padding:8px 12px 0;font-size:.875rem;font-weight:600;line-height:20px;color:var(--ink)}` +
	`.chat-workspace .chattone-reword-admin-scope+.chattone-reword-admin-scope{margin-block-start:4px;border-block-start:1px solid var(--line)}` +
	`.chat-workspace .chattone-reword-row+.chattone-reword-row{border-block-start:1px solid var(--line)}` +
	`.chat-workspace .chattone-reword-seg{margin:0 12px 10px;block-size:auto;min-block-size:28px;flex-wrap:nowrap}` +
	`.chat-workspace .chattone-reword-choice{flex:1 1 auto;block-size:auto;min-block-size:24px;padding:4px 8px;line-height:1.2;text-align:center;white-space:normal;overflow-wrap:anywhere}` +
	`.chat-workspace .chattone-reword-choice[aria-pressed="true"]{background:var(--surface);border-color:var(--accent);color:var(--ink)}` +
	`.chat-workspace .chattone-reword-choice:disabled{opacity:.6;cursor:default}` +
	`.chat-workspace .chattone-reword-choice:focus-visible,.chat-workspace .chattone-reword-switch:focus-visible .chatmod-switch-track{outline:2px solid var(--hcm-color-focus);outline-offset:2px}` +
	`.chat-workspace .chattone-reword-switch{flex:none;display:inline-flex;align-items:center;justify-content:flex-end;min-inline-size:44px;min-block-size:32px;padding:0;border:0;background:transparent;cursor:pointer}` +
	`.chat-workspace .chattone-reword-switch:disabled{opacity:.6;cursor:default}` +
	`.chat-workspace .chattone-reword-override{display:grid;justify-items:start;gap:0;padding-block-start:8px;border-block-start:1px solid var(--line)}` +
	`.chat-workspace .chattone-reword-inherit,.chat-workspace .chattone-reword-retry{margin:0 12px 12px}` +
	`.chat-workspace .chattone-reword-status{margin:0;padding:0 12px 8px;font-size:.8125rem;line-height:20px;color:var(--muted)}` +
	`.chat-workspace .chattone-reword-status[hidden]{display:none}` +
	`.chat-workspace .chattone-reword-status[data-state="saved"]{position:absolute;inline-size:1px;block-size:1px;margin:-1px;padding:0;overflow:hidden;clip:rect(0 0 0 0);white-space:nowrap}` +
	`.chat-workspace .chattone-reword-status[data-state="error"]{color:var(--hcm-color-danger)}` +
	`@media(pointer:coarse){.chat-workspace .chattone-reword-choice{min-block-size:36px}}` +
	`.chatremove .chatmod005-action.danger{background:var(--surface);border-color:var(--hcm-color-danger);color:var(--hcm-color-danger)}` +
	`.chatremove .chatmod005-action.danger:hover{background:var(--hcm-color-danger-surface)}` +
	`.chatremove .chatmod005-action.danger:focus-visible{outline:2px solid var(--hcm-color-danger);outline-offset:2px}` +
	`.chat-workspace .chatux048-removed{display:grid;grid-template-columns:56px minmax(0,1fr);column-gap:10px;box-sizing:border-box;min-inline-size:0;margin:10px -8px 0;padding:4px 8px;color:var(--muted);overflow-wrap:anywhere}` +
	`.chat-workspace .chatux048-removed>.chatux048-removed-body{grid-column:2;display:grid;justify-items:start;gap:4px;min-inline-size:0}` +
	`.chat-workspace .chatux048-removed-title{margin:1px 0 0;font-size:.9375rem;font-weight:500;line-height:1.45;color:var(--muted)}` +
	`.chat-workspace .chatux048-removed-why{margin:0;font-size:.8125rem;line-height:1.4;color:var(--muted)}` +
	`.chat-workspace .chatux048-removed-actions{display:flex;flex-wrap:wrap;align-items:center;gap:8px;margin-block-start:2px}` +
	`.chat-workspace .chatux048-removed-actions .chatremove-appeal{margin:0;padding:0;border:0}` +
	`.chat-workspace .chatux048-removed .chatux048-removed-button{min-block-size:28px}` +
	`@media(max-width:760px){.chat-workspace .chatux048-removed{grid-template-columns:28px minmax(0,1fr);column-gap:8px;margin-inline:-12px;padding:6px 12px}}`
