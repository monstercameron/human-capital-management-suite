package chatui

// chatcmd002PreviewStyles is how the card preview above the composer behaves
// when the window is short. The preview already had a bounded height; what it
// lacked was a sign that it scrolls, and Post and Cancel, which sat at the end
// of the scrolled content, went out of view with the settings row. Now the
// body scrolls and the actions stay under it. A shadow at the top edge of the
// body shows when content has scrolled away, and a fade above the actions
// shows while there is more below. Where scroll-linked animation is not
// available, the rule above the actions still separates the two.
const chatcmd002PreviewStyles = `.chat-workspace .chatcmd003-preview{display:flex;flex-direction:column;gap:0;padding:0;overflow:hidden;timeline-scope:--chatcmd002-preview}` +
	`.chat-workspace .chatcmd003-preview>.chatcmd003-body{display:grid;align-content:start;gap:8px;flex:1 1 auto;min-height:0;padding:12px 14px 8px;overflow-y:auto;overscroll-behavior:contain;scroll-timeline:--chatcmd002-preview block;` +
	`background:linear-gradient(var(--surface) 30%,transparent) center top/100% 24px no-repeat local,` +
	`radial-gradient(farthest-side at 50% 0,color-mix(in srgb,var(--ink) 22%,transparent),transparent) center top/100% 8px no-repeat scroll,var(--surface)}` +
	`.chat-workspace .chatcmd003-preview>.chatcmd003-actions{position:relative;z-index:1;flex:none;padding:10px 14px 12px;border-top:1px solid var(--line);background:var(--surface)}` +
	`.chat-workspace .chatcmd003-preview>.chatcmd003-actions::before{content:"";position:absolute;inset-inline:0;inset-block-end:100%;height:20px;pointer-events:none;background:linear-gradient(to top,var(--surface),transparent);opacity:0}` +
	`@keyframes chatcmd002-preview-more{from{opacity:1}85%{opacity:1}to{opacity:0}}` +
	`@supports(animation-timeline:scroll()){.chat-workspace .chatcmd003-preview>.chatcmd003-actions::before{animation:chatcmd002-preview-more linear both;animation-timeline:--chatcmd002-preview}}`
