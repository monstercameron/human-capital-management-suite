package chatui

// issue3850BrowseStyles keeps the actions of Browse channels rows visible (a
// Join button that only appeared on hover could not be found) and lets a row
// reflow at phone widths: the name wraps and the actions move under it instead
// of reserving a column that truncates the name and scrolls the list sideways.
const issue3850BrowseStyles = `.chat-workspace .browse-row .browse-actions>.button{opacity:1}` +
	`.chat-workspace .browse-list{overflow-x:hidden}` +
	`@media(max-width:560px){` +
	`.chat-workspace .browse-row{flex-wrap:wrap}` +
	`.chat-workspace .browse-text{flex:1 1 0;min-width:0}` +
	`.chat-workspace .browse-text strong{white-space:normal;overflow-wrap:anywhere}` +
	`.chat-workspace .browse-actions{flex:1 1 100%;flex-wrap:wrap;justify-content:flex-start;padding-inline-start:44px}` +
	`.chat-workspace .browse-actions>.button{min-height:44px}}`
