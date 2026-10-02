package chatui

// ChatLane2Styles holds the layout rules of the channel card work (CHATBUG-074,
// CHATBUG-057). They come last in the stylesheet so they decide ties with the
// earlier tray rules.
const ChatLane2Styles = chatbug074Styles + chatcmd001Styles + chatcmd003Styles + chatcmd002TrayStyles + chatcmd002PreviewStyles + chatcmd002EditStyles + chatux025Styles

// chatcmd001Styles: the line that stands where the command list was, once a
// command is chosen (its usage) or a word turned out not to be one.
const chatcmd001Styles = `.chat-workspace .command-line{display:flex;flex-wrap:wrap;align-items:center;gap:4px 10px;min-width:0;margin:0 0 6px;padding:6px 10px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--soft);color:var(--muted);font-size:.8125rem;line-height:1.4}` +
	`.chat-workspace .command-line-label{font-weight:600}.chat-workspace .command-line-usage{min-width:0;overflow-wrap:anywhere;color:var(--ink);font-family:var(--hcm-font-mono,ui-monospace,monospace);font-size:.8125rem}` +
	`.chat-workspace .command-line-text{min-width:0;color:var(--muted)}.chat-workspace .command-unknown .command-line-text{color:var(--ink)}.chat-workspace .command-line-invalid{flex:1 0 100%;color:var(--hcm-color-danger)}` +
	`.chat-workspace .command-line-actions{display:inline-flex;flex-wrap:wrap;gap:6px;margin-inline-start:auto}`

// chatbug074Styles: a task in the channel's to-do list is one line: the tick,
// the text, who finished it, who may complete it when that is not everyone, and
// the row's buttons. The rule's controls open under the row, across its width.
const chatbug074Styles = `.chat-workspace .channel-tray-card .channel-todo-row{display:flex;flex-wrap:wrap;align-items:center;gap:4px 8px;min-width:0;padding:4px 0}` +
	`.chat-workspace .channel-tray-card .channel-todo-row>.channel-todo-check{flex:none}` +
	`.chat-workspace .channel-tray-card .channel-todo-row>.channel-todo-text{flex:1 1 8ch;min-width:0;overflow-wrap:anywhere}` +
	`.chat-workspace .channel-tray-card .channel-todo-completed-by{flex:0 1 auto;min-width:0;max-width:45%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--muted);font-size:.75rem}` +
	`.chat-workspace .channel-tray-card .channel-todo-restriction{position:absolute;width:1px;height:1px;margin:-1px;padding:0;overflow:hidden;clip-path:inset(50%);white-space:nowrap}` +
	`.chat-workspace .channel-tray-card .channel-todo-rule{display:contents;margin:0}.chat-workspace .channel-tray-card .channel-todo-rule-none{display:none}` +
	`.chat-workspace .channel-tray-card .channel-todo-rule-chip{flex:none;padding:1px 8px;border-radius:999px;background:var(--soft);color:var(--muted);font-size:.75rem;white-space:nowrap}` +
	`.chat-workspace .channel-tray-card .channel-todo-rule-toggle,.chat-workspace .channel-tray-card .channel-todo-row>.icon-button{flex:none;width:28px;height:28px}` +
	`.chat-workspace .channel-tray-card .channel-todo-rule-body{flex:1 0 100%;display:grid;gap:6px;padding:6px 0 8px;font-size:.8125rem}.chat-workspace .channel-tray-card .channel-todo-rule-body[hidden]{display:none}` +
	// More options: both of its fields carry the panel's label style.
	`.chat-workspace .channel-tray-card .channel-todo-options label,.chat-workspace .channel-tray-card .channel-todo-rule-body label{display:block;margin:8px 0 4px;color:var(--muted);font-size:.8125rem;font-weight:600;line-height:1.3}`
