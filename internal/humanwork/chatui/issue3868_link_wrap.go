package chatui

// Issue3868Styles lets a link in a message wrap on a phone (#3868). A link is
// one unbreakable line cut with an ellipsis (CHATBUG-053), which on a small
// screen hid the end of the address, and a touch screen has no hover to show the
// title. At phone width the whole address is shown: it wraps inside the message,
// at any character when it has no break of its own, and a shortened address
// draws its middle instead of the ellipsis, so the reader can read the domain
// and the path before following it.
const Issue3868Styles = `@media(max-width:760px){` +
	`.chat-workspace a.chat-link[href]{display:inline;max-inline-size:100%;overflow:visible;text-overflow:clip;white-space:normal;overflow-wrap:anywhere}` +
	`.chat-workspace a.chat-link[href] .chat-link-cut{display:inline;max-inline-size:none;overflow:visible;text-overflow:clip;white-space:normal}` +
	`.chat-workspace a.chat-share-link[data-share-link="unresolved"][href]{display:inline;overflow:visible;text-overflow:clip;white-space:normal;overflow-wrap:anywhere}` +
	`}`
