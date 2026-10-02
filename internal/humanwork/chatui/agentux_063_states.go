package chatui

// AGENTUX-063. The state styles every Chat control shares. Hover tints, the
// pressed dim, the disabled look and the motion all come from the rules in
// agentux_chat5_styles.go and styles.go; the two things they left to each
// feature were the colour of the focus ring (a dozen features drew theirs in
// the brand colour, the shared rule in the focus colour) and a busy state, so a
// button that was working looked like one waiting to be pressed.
//
// The ring keeps the shared width and the colour of --hcm-color-focus on every
// control in the workspace. The composer's own fields keep their transparent
// outline: the composer draws its ring on the frame around them. A rule that
// removes an outline (a pane handle, a quiet row) removes the style, which
// these declarations do not set, so it stays removed.
//
// The mention badge printed #fff on the danger colour (styles.go). In dark mode
// the shell's danger colour is a light rose, so the white was unreadable; the
// label now takes the shell's own "on brand" colour, which is white in light mode
// and near black in dark mode.
//
// No target is under 24 by 24 px. The switches (Quiet hours, the language rows,
// the channel filters) were 38 by 22, and the row actions of the Saved list 22
// high; both are 24 now, and a switch's thumb keeps its place in the middle of
// the taller track.
const agentux063StateStyles = `.chat-workspace.chat-workspace :focus-visible:not(.composer-input):not(.chip-input){outline-width:2px;outline-color:var(--hcm-color-focus)}` +
	`.chat-workspace :is(button,a,summary,[role="button"],[role="tab"],[role="menuitem"])[aria-busy="true"]{cursor:progress;opacity:.7}` +
	`:root .chat-workspace :is(.switch,.chatmod-switch-track){block-size:24px;min-block-size:24px}` +
	`:root .chat-workspace :is(.switch,.chatmod-switch-track)::after{inset-block-start:3px}` +
	`:root[data-hcm-color-mode="dark"] .chat-workspace input.switch:not(:checked){block-size:24px}` +
	`@media(prefers-color-scheme:dark){:root[data-hcm-color-mode="system"] .chat-workspace input.switch:not(:checked){block-size:24px}}` +
	`.chat-workspace .chatsave-actions .message-action{height:24px;min-height:24px}` +
	// The conversation's menu is an anchored layer now (AGENTUX-062); it keeps the
	// compact look it had before, which the layer's own padding would loosen.
	`.chat-workspace .rail-row-menu[data-chat-layer]{padding:4px;border-radius:var(--hcm-radius-control)}` + agentux061SourceStyles + agentux062ComposerStyles
