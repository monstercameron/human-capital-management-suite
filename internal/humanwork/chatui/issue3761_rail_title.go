package chatui

// Issue3761Styles gives the conversation list's title and the search results
// heading room beside the bell and the account button, which a phone draws over
// the end of the list's bar (#3761). The bar's end is reserved for those two
// buttons; at 320px what is left for the title was narrower than its word, so it
// broke inside it ("Conversa/tions"). The title is never broken inside a word
// now: the bar wraps, so the new-conversation and preferences buttons take a
// row of their own when the title needs the room. The results heading keeps the
// same room clear of the two buttons, so its closing quote is never under them.
const Issue3761Styles = `@media(max-width:760px){` +
	`.chat-workspace .chat-rail .rail-head{flex-wrap:wrap;block-size:auto;min-block-size:52px;row-gap:0}` +
	`.chat-workspace .chat-rail .rail-head .rail-title-group{min-inline-size:max-content}` +
	`.chat-workspace .chat-rail .rail-head .rail-title{overflow-wrap:normal;word-break:normal;hyphens:manual}` +
	`.chat-workspace .chat-rail .rail-head .rail-head-actions{margin-inline-start:auto}` +
	`.chat-workspace[data-sidebar-open="true"] .chat-search-results .search-results-head{padding-inline-end:calc(2 * var(--hcm-control-height) + var(--hcm-space-1))}` +
	`}`
