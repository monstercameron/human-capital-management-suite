package chatui

// Chatsave002Styles is the Saved panel (CHATSAVE-002). It is joined last in the
// workspace stylesheet because it replaces the first design's rules and the
// panel's phone layout, which earlier files also write. Every colour, radius,
// shadow and motion value is a shell token; the type and spacing scale is the
// conversation list's and the details panel's (16px gutters, hairlines in
// var(--line), 15px text, 13px and 12px for what is secondary).
const Chatsave002Styles = `
.chatsave-content{position:relative;display:flex;flex-direction:column;min-height:100%;min-width:0;padding:0 16px}
.chatsave-top{position:sticky;top:0;z-index:3;margin-inline:-16px;padding-inline:16px;padding-block-end:16px;background:var(--canvas);border-bottom:1px solid var(--line)}
.chatsave-header{height:auto;min-height:56px;margin:0;padding:6px 0;border:0;position:static;justify-content:space-between;gap:8px}
.chatsave-heading-text{display:flex;flex-direction:column;flex:1;min-width:0;gap:1px}
.chatsave-header h2{margin:0;font-size:1rem;line-height:1.2}
.chatsave-sub{margin:0;color:var(--muted);font-size:.75rem;line-height:1.3;font-variant-numeric:tabular-nums}
.chatsave-back{display:none}
.chatsave-seg{display:flex;gap:2px;box-sizing:border-box;height:28px;padding:2px;background:var(--soft);border-radius:var(--hcm-radius-control)}
.chatsave-seg-button{flex:1 1 0;min-width:0;display:inline-flex;align-items:center;justify-content:center;gap:6px;box-sizing:border-box;height:24px;padding:0 8px;border:1px solid transparent;border-radius:var(--hcm-radius-xs);background:transparent;color:var(--muted);font:inherit;font-size:.8125rem;font-weight:600;line-height:1;cursor:pointer;white-space:nowrap}
.chatsave-seg-button:hover{color:var(--ink)}
.chatsave-seg-button[aria-selected="true"]{background:var(--surface);border-color:var(--line);color:var(--ink)}
.chatsave-seg-count{color:var(--muted);font-size:.75rem;font-weight:500;font-variant-numeric:tabular-nums}
.chatsave-search{margin:12px 0 0}
.chatsave-search .member-filter{margin:0}
.chatsave-status{margin:12px 0 0;color:var(--hcm-color-danger,var(--ink));font-size:.8125rem}
.chatsave-status:empty{display:none}
.chatsave-retry{align-self:flex-start;margin:8px 0 0}
.chatsave-tabpanel{flex:1 0 auto;min-width:0}
.chatsave-items{list-style:none;margin:0;padding:0}
.chatsave-item{position:relative;display:grid;grid-template-columns:24px minmax(0,1fr);column-gap:10px;margin-inline:-16px;padding:14px 16px;min-width:0}
.chatsave-item+.chatsave-item{border-top:1px solid var(--line)}
.chatsave-item[data-saved-action="open"]{cursor:pointer}
.chatsave-item:hover,.chatsave-item:focus-visible{background:color-mix(in srgb,var(--ink) 5%,transparent)}
.chatsave-item:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:-2px}
.chatsave-avatar{display:flex;align-items:center;justify-content:center;flex:none;width:24px;height:24px;border-radius:6px;background:var(--soft)}
.chatsave-item .avatar.chatsave-avatar{width:24px;height:24px;font-size:.625rem;border-radius:6px}
.chatsave-avatar-empty{color:var(--muted)}
.chatsave-avatar-empty .chat-icon{width:14px;height:14px}
.chatsave-main{min-width:0}
.chatsave-meta{display:flex;align-items:baseline;flex-wrap:nowrap;column-gap:6px;min-width:0;line-height:1.4}
.chatsave-author{flex:0 0 auto;max-width:60%;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--ink);font-size:.9375rem;font-weight:700}
.chatsave-meta .agent-badge{flex:none}
.chatsave-where{flex:0 1 auto;min-width:0;min-height:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;color:var(--muted);font-size:.8125rem;text-decoration:none}
.chatsave-where:hover{color:var(--accent);text-decoration:underline}
.chatsave-time{flex:none;color:var(--muted);font-size:.75rem;white-space:nowrap}
.chatsave-where+.chatsave-time::before{content:"·";margin-inline-end:6px}
.chatsave-text{position:relative;margin-block-start:2px;max-height:5.9em;overflow:hidden;min-width:0}
.chatsave-text.is-expanded{max-height:none}
.chatsave-text[data-clamped="true"]{-webkit-mask-image:linear-gradient(to bottom,black calc(100% - 1.6em),transparent);mask-image:linear-gradient(to bottom,black calc(100% - 1.6em),transparent)}
.chatsave-text .message-body{width:auto;max-width:none}
.chatsave-more{margin:2px 0 0;padding:0;border:0;background:none;color:var(--accent);font:inherit;font-size:.8125rem;font-weight:600;cursor:pointer}
.chatsave-more:hover{text-decoration:underline}
.chatsave-attach{margin-block-start:4px}
.chatsave-chips{display:flex;flex-wrap:wrap;gap:6px;margin-block-start:6px}
.chatsave-chip{display:inline-flex;align-items:center;gap:4px;height:22px;padding:0 4px 0 7px;border-radius:var(--hcm-radius-control);background:var(--soft);color:var(--muted);font-size:.75rem}
.chatsave-chip .chat-icon{width:12px;height:12px}
.chatsave-chip-label{padding:0;border:0;background:none;color:inherit;font:inherit;cursor:pointer}
.chatsave-chip-label:hover{color:var(--ink)}
.chatsave-chip-clear{display:inline-flex;align-items:center;justify-content:center;width:16px;height:16px;padding:0;border:0;border-radius:var(--hcm-radius-xs);background:none;color:inherit;cursor:pointer}
.chatsave-chip-clear .chat-icon{width:10px;height:10px}
.chatsave-chip-clear:hover{background:color-mix(in srgb,var(--ink) 10%,transparent);color:var(--ink)}
.chatsave-chip.is-overdue{background:color-mix(in srgb,var(--accent) 12%,transparent);color:var(--accent);font-weight:600}
.chatsave-note{display:flex;align-items:flex-start;gap:6px;max-width:100%;margin-block-start:6px;padding:0;border:0;background:none;color:var(--muted);font:inherit;font-size:.8125rem;text-align:start;cursor:pointer}
.chatsave-note .chat-icon{flex:none;width:12px;height:12px;margin-block-start:.3em}
.chatsave-note span{display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden;overflow-wrap:anywhere}
.chatsave-note:hover span{color:var(--ink)}
.chatsave-note-form{margin:6px 0 0}
.chatsave-note-input,.chatsave-due-input{width:100%;min-width:0;height:32px;box-sizing:border-box;padding:0 8px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface);color:var(--ink);font:inherit;font-size:.875rem}
.chatsave-note-hint{margin:3px 0 0;color:var(--muted);font-size:.6875rem}
.chatsave-donebar{display:flex;align-items:center;justify-content:space-between;gap:8px;margin-block-start:6px;color:var(--muted);font-size:.8125rem}
.chatsave-done-mark{display:inline-flex;align-items:center;gap:4px}
.chatsave-done-mark .chat-icon{width:14px;height:14px;color:var(--hcm-color-success)}
.chatsave-reopen{padding:0 4px;border:0;background:none;color:var(--accent);font:inherit;font-weight:600;cursor:pointer}
.chatsave-reopen:hover{text-decoration:underline}
.chatsave-item.is-done .chatsave-author,.chatsave-item.is-done .message-body{color:var(--muted)}
.chatsave-item.is-done .chatsave-avatar{opacity:.6}
.chatsave-unavailable{margin:2px 0 0;color:var(--muted);font-size:.875rem}
.chatsave-actions{position:absolute;top:-12px;inset-inline-end:12px;z-index:4;display:flex;align-items:center;gap:2px;box-sizing:border-box;height:24px;padding:0 2px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface);box-shadow:var(--hcm-shadow-raised);opacity:0;pointer-events:none;transition:opacity var(--hcm-motion-fast) var(--hcm-motion-easing)}
.chatsave-item:hover>.chatsave-actions,.chatsave-item:focus-visible>.chatsave-actions,.chatsave-item:has(:focus-visible)>.chatsave-actions,.chatsave-item.is-active>.chatsave-actions{opacity:1;pointer-events:auto}
@media(hover:hover) and (pointer:fine){.chatsave-items:has(.chatsave-item:hover) .chatsave-item:not(:hover):not(.has-menu)>.chatsave-actions{opacity:0;pointer-events:none}}
.chatsave-actions .message-action{width:32px;min-width:32px;height:22px;min-height:22px;padding:2px}
.chatsave-act-done{color:var(--accent)}
.chatsave-act-remove{color:var(--muted)}
.chatsave-filled path{fill:currentColor}
.chatsave-anchor{position:relative;display:inline-flex}
.chatsave-menu{position:absolute;top:calc(100% + 6px);bottom:auto;inset-inline-end:0;inset-inline-start:auto;z-index:6;display:flex;flex-direction:column;box-sizing:border-box;min-width:224px;max-width:calc(100vw - 32px);padding:4px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface);box-shadow:var(--hcm-shadow-raised);transform:translateX(var(--chatsave-menu-shift,0px))}
.chatsave-menu.is-above{top:auto;bottom:calc(100% + 6px)}
.chatsave-menu-item{white-space:nowrap}
.chatsave-pick{display:flex;align-items:center;gap:6px;padding:6px 8px 4px}
.chatsave-pick .button{flex:none}
.chatsave-empty{display:flex;flex-direction:column;align-items:center;gap:8px;padding:40px 16px;color:var(--muted);text-align:center}
.chatsave-empty .chat-icon{width:28px;height:28px}
.chatsave-empty p{max-width:26ch;margin:0;font-size:.875rem}
.chatsave-skeleton .chatsave-bar{display:block;height:10px;margin-block:6px;border-radius:var(--hcm-radius-xs);background:var(--soft)}
.chatsave-skeleton .chatsave-bar-name{width:40%}
.chatsave-skeleton .chatsave-bar-short{width:60%}
.chatsave-load-more{align-self:flex-start;margin:12px 0}
.timeline-frame:has(.message.search-target) .jump-newest{visibility:hidden;opacity:0;pointer-events:none}
.chatsave-undo{position:sticky;inset-block-end:0;z-index:4;display:flex;align-items:center;justify-content:space-between;gap:12px;margin:auto -16px 0;padding:10px 16px;border-top:1px solid var(--line);background:var(--surface);color:var(--ink);font-size:.875rem}
.chatsave-undo-button{padding:2px 6px;border:0;border-radius:var(--hcm-radius-xs);background:none;color:var(--accent);font:inherit;font-weight:700;cursor:pointer}
.chatsave-undo-button:hover{background:var(--soft)}
@media(max-width:760px),(pointer:coarse){
.chatsave-actions{position:static;grid-column:2;justify-content:flex-start;gap:4px;height:auto;margin-block-start:6px;padding:0;border:0;background:none;box-shadow:none;opacity:1;pointer-events:auto}
.chatsave-actions .message-action{width:44px;min-width:44px;height:44px;min-height:44px}
.chatsave-menu-item{min-height:44px}
.chatsave-reopen,.chatsave-more,.chatsave-undo-button{min-height:44px}
.chatsave-chip{height:32px}
.chatsave-chip-clear{width:32px;height:32px}
.chatsave-note-hint{display:none}
}
@media(max-width:760px){
.chatsave-panel{inset:0;inline-size:100%;max-inline-size:none;block-size:100dvh;max-block-size:none;border-inline-start:0}
.chatsave-back{display:inline-flex}
.chatsave-header>.icon-button:not(.chatsave-back){display:none}
}
`
