package chatui

// chatlane9Styles holds the rules of the leftovers pass: the archive notice's
// Restore button, the Moderation and Saved segmented control, search results
// and the phone items. It is part of ChatLane4Styles, so it sits after the
// older chat rules and decides ties with them.
const chatlane9Styles = chatbug082Styles + chatux015Styles + chatux010RecentStyles + chatlane9VarDefaults

const chatbug082Styles = `.chatstate-notice-bar{display:flex;flex-wrap:wrap;align-items:center;gap:8px 12px;padding:10px 12px;border-block-start:1px solid var(--line);background:var(--surface)}` +
	`.chatstate-notice-bar>.chatstate-notice{flex:1 1 16rem;margin:0;padding:0;background:transparent;font-size:.875rem;color:var(--muted)}` +
	`.chatstate-notice-bar>.chatstate-restore{flex:none}`

// chatux015Styles: Moderation's Open and Resolved are the segmented control
// Saved uses, so the page's older tab-button rules are set aside for it.
const chatux015Styles = `.chatremove .chatmod005-tabs.chatsave-seg{flex-wrap:nowrap;max-inline-size:360px;margin-block:12px 4px}` +
	`.chatremove .chatmod005-tabs .chatmod005-tab.chatsave-seg-button{min-block-size:0;padding:0 8px;border:1px solid transparent;border-radius:var(--hcm-radius-xs);background:transparent;color:var(--muted);font-weight:600}` +
	`.chatremove .chatmod005-tabs .chatmod005-tab.chatsave-seg-button:hover{color:var(--ink)}` +
	`.chatremove .chatmod005-tabs .chatmod005-tab.chatsave-seg-button[aria-selected=true]{background:var(--surface);border-color:var(--line);color:var(--ink);font-weight:600}`

// chatsearch recents: a small list under the sidebar's search box, shown only
// while the box holds the cursor, and a mention in a result is the same small
// chip as in the conversation.
const chatux010RecentStyles = `.chat-workspace .rail-search-recent{display:none;position:absolute;inset-inline:12px;inset-block-start:calc(100% - 6px);z-index:30;box-sizing:border-box;max-block-size:240px;overflow:auto;padding:4px;border:1px solid var(--line);border-radius:var(--hcm-radius-control);background:var(--surface);box-shadow:var(--hcm-shadow-raised)}` +
	`.chat-workspace .rail-search:focus-within .rail-search-recent{display:block}` +
	`.chat-workspace .rail-search-recent-title{display:block;padding:4px 8px;color:var(--muted);font-size:.75rem;font-weight:600}` +
	`.chat-workspace .rail-search-recent-item{display:flex;align-items:center;gap:8px;inline-size:100%;min-block-size:32px;margin:0;padding:4px 8px;border:0;border-radius:var(--hcm-radius-xs);background:transparent;color:var(--ink);font:inherit;font-size:.875rem;text-align:start;cursor:pointer}` +
	`.chat-workspace .rail-search-recent-item>span{min-inline-size:0;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}` +
	`.chat-workspace .rail-search-recent-item .chat-icon{flex:none;inline-size:14px;block-size:14px;color:var(--muted)}` +
	`.chat-workspace .rail-search-recent-item:hover,.chat-workspace .rail-search-recent-item:focus-visible{background:var(--soft)}`

// chatlane9VarDefaults gives the custom properties the page sets from script a
// value in the stylesheet, on the element that script writes them to, so the
// stylesheet defines every property it reads and a script's value (an inline
// style) still wins.
const chatlane9VarDefaults = `.chat-image-viewer-media{--chat-viewer-w:auto;--chat-viewer-h:auto}` +
	`.chatsave-panel{--chatsave-top:82px}` +
	`.chatremove-overlay-page{--chatmod-top:0;--chatmod-bottom:0;--chatmod-left:0;--chatmod-right:0}`
