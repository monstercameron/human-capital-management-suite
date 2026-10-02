package chatui

// ChatBug090Styles is the sidebar's one scroll area. The header, the search
// field and the Saved and Moderation rows are fixed above it, the Archived band
// is fixed below it, and the rows scroll between them: under the section
// heading's opaque edge at the top (a short fade softens the cut) and into a
// 12 px fade at the foot. Rows snap to the heading, so a list at rest starts on
// a whole row. Archived opens inside its band with its own scroll and never
// takes the list down to nothing.
const ChatBug090Styles = `
.rail-scroll{min-height:96px;scroll-snap-type:y proximity;scroll-padding-block:46px 12px;mask-image:linear-gradient(to bottom,#000 calc(100% - 12px),transparent);-webkit-mask-image:linear-gradient(to bottom,#000 calc(100% - 12px),transparent)}
.rail-scroll .chat-rail-row{scroll-snap-align:start}
.rail-scroll>.sidebar-section>.section-controls::after{content:"";position:absolute;inset-inline:0;top:100%;height:10px;background:linear-gradient(var(--canvas),transparent);pointer-events:none}
.chat-rail>.chatstate-archived{flex:none;border-top:1px solid var(--line);background:var(--canvas)}
.chatstate-archived .chat-disclosure-body{max-height:min(220px,34vh);overflow-y:auto;overscroll-behavior:contain;padding:0 8px 8px;background:var(--canvas)}
.chatstate-archived .chat-disclosure-body :is(h2,h3){margin:8px 0 4px;font-size:.8125rem}
`
