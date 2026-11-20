package chatui

// issue3765Styles keeps the thread panel's author names whole (#3765). The
// timeline cuts a long author with an ellipsis so one line stays one line; in
// the narrow thread panel the long timestamp (and, on a phone, "View in
// channel") took the row and left the name "Elena…" or "M…". Here the name may
// wrap, and the timestamp is the part that moves: the row wraps so it drops
// under the name when the two do not fit. Only logical properties are used, so
// the same holds in right-to-left locales.
const issue3765Styles = `.chat-workspace .thread-pane .message-meta{flex-wrap:wrap}` +
	`.chat-workspace .thread-pane .message-author{flex:0 1 auto;min-inline-size:0;max-inline-size:100%;overflow:visible;text-overflow:clip;white-space:normal;overflow-wrap:anywhere}`

// Issue3765Styles is appended to the Chat stylesheet.
const Issue3765Styles = issue3765Styles
