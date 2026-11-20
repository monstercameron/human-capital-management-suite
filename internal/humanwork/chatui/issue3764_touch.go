package chatui

// #3764: on a phone, or any touch screen, the controls people press are at
// least 44 by 44 CSS pixels. Sizes set earlier in the sheet (a 24px row menu, a
// 28px reaction chip, 24px Saved tabs) stay for pointer devices; this block is
// last so it wins where the screen is narrow or coarse. Switches keep their
// 24px track: it meets the 24px floor and the whole row they sit in is the target.
const issue3764MediaQuery = `@media(max-width:760px),(pointer:coarse){`

const Issue3764Styles = issue3764MediaQuery +
	`.chat-workspace .chat-rail .chat-row{min-block-size:44px}` +
	`.chat-workspace .rail-row-more{min-block-size:44px;min-inline-size:44px;inline-size:auto;block-size:auto}` +
	`.chat-workspace .section-menu-trigger,.chat-workspace .section-controls .icon-button{min-block-size:44px;min-inline-size:44px}` +
	`.chat-workspace .message-stats{display:inline-flex;align-items:center;min-block-size:44px;min-inline-size:44px}` +
	`.chat-workspace .reaction{min-block-size:44px;min-inline-size:44px;justify-content:center;height:auto}` +
	// The add button is pinned to 32px by a more specific rule (CHATBUG-085);
	// it is a reaction's size, so it follows the chips.
	`.chat-workspace .reaction-row>.reaction.add{min-block-size:44px;min-inline-size:44px;height:auto}` +
	`.chat-workspace .message-meta button{min-block-size:44px;min-inline-size:44px}` +
	`.chat-workspace .chatsave-seg-button,.chat-workspace .chatmod005-tabs .chatmod005-tab.chatsave-seg-button{min-block-size:44px;height:auto}` +
	`.chat-workspace .thread-pane .side-heading .icon-button{min-block-size:44px;min-inline-size:44px;inline-size:44px;block-size:44px}` +
	`.chat-workspace .thread-notify{min-block-size:44px;min-inline-size:44px}` +
	`.chat-workspace .chat-skip:focus{min-block-size:44px;min-inline-size:44px;display:inline-flex;align-items:center}` +
	`}`
