package chatui

// A grouped follow-up draws no author name, so its meta row holds only a hidden
// time. A later rule gives every meta row a 24px minimum height (room for the
// hover bar), which left an empty band the height of the avatar row between the
// two messages (#3847). A grouped follow-up keeps no minimum; the row still grows
// when it carries a pinned badge, a forwarded note or a tag.
const issue3847GroupedGapStyles = `.chat-workspace .message-list .message.continued .message-meta{min-height:0}`
