package chatui

// ISSUE-900001: the Schedule and Away sections of Chat preferences share the
// popover's one section style.
//
// Those sections added their own bodies (helper text, option groups, a weekly
// form, date and cover fields, toggles) as direct children of the section, and
// nothing gave those children the 12px inset the section's heading row has, so
// they sat flush against the popover's edge. This sheet gives every such piece
// the heading's inset and the same bottom spacing, lays option groups out as one
// wrapping row of equal pills (a single column only on a phone), sets the helper
// text to the size of the other status lines, and bounds the popover by the
// viewport in CSS itself so it scrolls inside at any height, whether or not the
// placement script has measured it yet. Logical properties only, so it holds in
// right-to-left; controls stay 44px tall.
const Issue900001Styles = `.chat-workspace .chatux002-layer[data-chat-layer="chat-prefs"]{box-sizing:border-box;max-block-size:calc(100vh - 16px);max-block-size:calc(100dvh - 16px);overflow-x:hidden;overflow-y:auto;overscroll-behavior:contain}` +
	`.chat-workspace .chatux002-layer[data-chat-layer="chat-prefs"] :is([data-prefs-section="quiet-schedule"],[data-prefs-section="away"],[data-prefs-section="composer-enter"])>:not(.chat-prefs-head):not(button){box-sizing:border-box;margin:0;padding-inline:12px;padding-block-end:8px}` +
	`.chat-workspace .chatux002-layer[data-chat-layer="chat-prefs"] :is([data-prefs-section="quiet-schedule"],[data-prefs-section="away"],[data-prefs-section="composer-enter"])>button.away-toggle{box-sizing:border-box;inline-size:calc(100% - 24px);margin-inline:12px;margin-block:0 8px;text-align:start}` +
	`.chat-workspace .chatux002-layer[data-chat-layer="chat-prefs"] :is([data-prefs-section="quiet-schedule"],[data-prefs-section="away"],[data-prefs-section="composer-enter"])>p.chat-prefs-note{font-size:.8125rem;line-height:18px;color:var(--muted)}` +
	`.chat-workspace .chatux002-layer[data-chat-layer="chat-prefs"] :is([data-prefs-section="quiet-schedule"],[data-prefs-section="away"],[data-prefs-section="composer-enter"])>p.chat-prefs-note.away-note[data-away-note]:not([data-away-note="saved"]):not([data-away-note="ended"]),.chat-workspace .chatux002-layer[data-chat-layer="chat-prefs"] :is([data-prefs-section="quiet-schedule"],[data-prefs-section="away"],[data-prefs-section="composer-enter"])>p.chat-prefs-note.quiet-note[data-quiet-note]:not([data-quiet-note="saved"]){color:var(--danger,var(--ink))}` +
	`.chat-workspace .chatux002-layer[data-chat-layer="chat-prefs"] :is([data-prefs-section="quiet-schedule"],[data-prefs-section="away"],[data-prefs-section="composer-enter"]) :is(.chat-prefs-choices,.quiet-pause){display:grid;grid-template-columns:repeat(auto-fit,minmax(min(100%,96px),1fr));gap:6px;align-items:stretch}` +
	`.chat-workspace .chatux002-layer[data-chat-layer="chat-prefs"] :is([data-prefs-section="quiet-schedule"],[data-prefs-section="away"],[data-prefs-section="composer-enter"]) .quiet-pause{grid-template-columns:repeat(auto-fit,minmax(min(100%,72px),1fr))}` +
	`.chat-workspace .chatux002-layer[data-chat-layer="chat-prefs"] :is(.chat-prefs-choices,.quiet-pause)>:is(.chat-prefs-choice,.button){justify-content:center;text-align:center;min-inline-size:0;min-block-size:44px;padding-inline:6px;font-size:.875rem;line-height:20px;overflow-wrap:anywhere}` +
	`.chat-workspace .chatux002-layer[data-chat-layer="chat-prefs"] .quiet-pause-title{grid-column:1/-1;margin:0;font-size:.875rem;font-weight:500;line-height:20px;color:var(--ink)}` +
	`.chat-workspace .chatux002-layer[data-chat-layer="chat-prefs"] .away-label{font-size:.875rem;font-weight:500;line-height:20px;color:var(--ink)}` +
	`.chat-workspace .chatux002-layer[data-chat-layer="chat-prefs"] .quiet-week>.button{inline-size:100%}`
