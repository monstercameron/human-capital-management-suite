package chatui

// ChatUX007Styles is how the conversation list reads at a glance. Red is for
// what needs attention, and a count of saved messages does not: Saved shows its
// open count as plain muted text. An unread conversation is bold with a plain
// neutral count; one where the viewer was mentioned shows its count in the
// accent colour; the open conversation is the only filled row. The rules sit
// after every earlier .chat-badge rule on purpose.
const ChatUX007Styles = `
.chatsave-count{margin-inline-start:auto;flex:none;color:var(--muted);font-size:.75rem;font-weight:500;font-variant-numeric:tabular-nums}
.chatsave-count[hidden]{display:none}
.chat-rail-row .chat-row.unread{color:var(--ink);font-weight:700}
.chat-rail-row .chat-badge{min-inline-size:0;block-size:auto;padding:0 4px;border-radius:0;background:none;color:var(--ink);font-size:.75rem;font-weight:600;font-variant-numeric:tabular-nums}
.chat-rail-row .chat-badge.mention{background:none;color:var(--accent);font-weight:700}
.chat-rail-row .chat-badge.mention::before{content:"@";font-weight:600}
.chatux007-jump-slot{position:sticky;z-index:5;block-size:0;display:flex;justify-content:center;pointer-events:none}
.chatux007-jump-up{inset-block-start:44px}
.chatux007-jump-down{inset-block-end:20px}
.chatux007-jump{position:absolute;pointer-events:auto;display:inline-flex;align-items:center;gap:6px;min-block-size:32px;padding:4px 14px;border:1px solid var(--line);border-radius:999px;background:var(--surface);color:var(--ink);font:inherit;font-size:.8125rem;font-weight:600;box-shadow:var(--hcm-shadow-raised);cursor:pointer}
.chatux007-jump-up .chatux007-jump{inset-block-start:0}
.chatux007-jump-down .chatux007-jump{inset-block-end:0}
.chatux007-jump[hidden]{display:none}
.chatux007-jump .chat-icon{inline-size:14px;block-size:14px}
.chatux007-jump:hover{background:var(--soft)}
@media(pointer:coarse){.chatux007-jump{min-block-size:40px}}
`
