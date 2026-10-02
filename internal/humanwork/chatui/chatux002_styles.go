package chatui

// ChatUX002Styles draws the Chat preferences control, its panel, the Channels
// menu and the quiet-hours moon. Every colour, radius and shadow is a token
// the stylesheet already uses; there is no inline style anywhere.
const ChatUX002Styles = `
.rail-title-group{display:flex;align-items:center;gap:6px;min-inline-size:0}
.rail-quiet-moon{display:inline-flex;align-items:center;flex:none;color:var(--muted)}
.rail-quiet-moon .chat-icon{inline-size:14px;block-size:14px}
.rail-prefs-control,.section-menu{position:relative;display:inline-flex;flex:none}
.chatux002-gear[aria-expanded=true],.section-menu-trigger[aria-expanded=true]{background:var(--soft);color:var(--ink)}
.chatux002-gear .chat-icon{inline-size:18px;block-size:18px}
.section-menu-trigger{display:inline-flex;align-items:center;justify-content:center;inline-size:24px;block-size:24px;padding:0;border:0;border-radius:var(--hcm-radius-control);background:none;color:var(--muted);cursor:pointer}
.section-menu-trigger:hover{background:var(--soft);color:var(--ink)}
.section-menu-trigger .chat-icon{inline-size:14px;block-size:14px}
.chatux002-layer{box-sizing:border-box;min-inline-size:min(260px,calc(100vw - 16px));padding:6px;overflow-x:hidden}
.chatux002-layer .rail-prefs-body{position:static;inset:auto;z-index:auto;max-inline-size:none;display:grid;gap:8px;padding:0 4px 4px;border:0;border-radius:0;background:transparent;box-shadow:none}
.chat-prefs-section{display:grid;gap:4px;padding:6px 4px}
.chat-prefs-section+.chat-prefs-section{border-block-start:1px solid var(--line);padding-block-start:10px}
.chat-prefs-head{display:flex;align-items:center;gap:8px;min-block-size:36px}
.chat-prefs-title{flex:1 1 auto;min-inline-size:0;white-space:nowrap;margin:0;font-size:.9375rem;font-weight:600}
.chat-prefs-value{flex:none;max-inline-size:45%;color:var(--muted);font-size:.8125rem;text-align:end;overflow-wrap:anywhere}
.chat-prefs-head>.switch{flex:none;margin:0}
.chat-prefs-note{margin:0;color:var(--muted);font-size:.75rem;line-height:1.35}
.rail-prefs-body[data-quiet=off] :is(.prefs-times,.prefs-field){display:none}
.chat-prefs-head>.chat-prefs-change{flex:none;inline-size:auto;min-block-size:32px;padding:4px 10px;border:1px solid var(--line);color:var(--accent);font-size:.8125rem;font-weight:600}
.chat-prefs-head>.chat-prefs-change:hover,.chat-prefs-head>.chat-prefs-change[aria-expanded=true]{background:var(--soft)}
.chat-prefs-change>.chat-icon{display:none}
.chat-prefs-inline-body{display:grid;gap:8px;padding:4px 0 0}
.chat-prefs-inline-body[hidden]{display:none}
.chat-prefs-section .chatrender-settings>h3{position:absolute;inline-size:1px;block-size:1px;margin:-1px;padding:0;overflow:hidden;clip:rect(0 0 0 0);white-space:nowrap}
.chatux002-layer .menu-item.chatux002-item,.chatux002-layer .section-create-trigger{display:flex;align-items:center;gap:12px;inline-size:100%;block-size:auto;min-block-size:48px;padding:8px 10px;border:0;color:var(--ink);font-size:.875rem;font-weight:400;white-space:normal}
.chatux002-layer .menu-item.chatux002-item:hover,.chatux002-layer .menu-item.chatux002-item:focus-visible,.chatux002-layer .section-create-trigger:hover{background:color-mix(in srgb,var(--accent) 12%,transparent)}
.chatux002-item-icon{flex:none;display:inline-flex;align-items:center;justify-content:center;inline-size:34px;block-size:34px;border-radius:var(--hcm-radius-control);background:var(--soft);color:var(--muted)}
.chatux002-item-icon .chat-icon{inline-size:18px;block-size:18px}
.chatux002-item-text{flex:1 1 auto;display:flex;flex-direction:column;align-items:flex-start;gap:2px;min-inline-size:0;text-align:start}
.chatux002-item-name{display:block;color:var(--ink);font-size:.875rem;font-weight:400;line-height:1.3}
.chatux002-item-note{display:block;color:var(--muted);font-size:.75rem;font-weight:400;line-height:1.3}
.chatux002-layer .section-create{margin:0}
.chatux002-layer .section-create-trigger>.chat-icon{flex:none;margin-inline-start:auto;inline-size:14px;block-size:14px;color:var(--muted)}
.chatux002-layer .section-create-form{margin:4px 4px 4px}
@media(pointer:coarse){.chatux002-gear,.section-menu-trigger{inline-size:40px;block-size:40px}}
`
