package chatui

// chatbug042OffLook is what an off switch looks like: the declarations the dark
// rules below repeat.
const chatbug042OffLook = `appearance:none;box-sizing:border-box;inline-size:38px;block-size:22px;border:2px solid var(--hcm-color-text-muted);border-radius:999px;background:var(--hcm-color-surface)`

// CHATBUG-042: every switch Chat draws has a visible track and a visible thumb in
// both colour modes. There is one rule for all of them. A switch is the
// checkbox with class "switch" (Quiet hours, the language and writing-style
// rows) or the track of a "chatmod-switch" button (the channel filters); both
// are drawn here from the same two shell tokens, so a new colour mode changes
// them with everything else and none of them names a colour of its own.
//
// Off: a 2px edge and a thumb in the muted text colour on the surface colour. The
// muted text colour already meets 3:1 against the surface in every colour mode
// (the test reads both modes' values from the product stylesheet). On: the track
// is filled with the accent and the thumb takes the surface colour, which is the
// opposite end of the scale from the accent in light and in dark.
//
// The old rule drew the track as the text colour at 18 to 22 percent over the
// surface and the thumb in the surface colour. On a dark panel that is a dark
// rectangle with an invisible thumb.
const ChatBug042Styles = `:root .chat-workspace :is(.switch,.chatmod-switch-track){box-sizing:border-box;position:relative;flex:none;display:inline-block;inline-size:38px;min-inline-size:38px;block-size:22px;min-block-size:22px;padding:0;border:2px solid var(--hcm-color-text-muted);border-radius:999px;background:var(--hcm-color-surface)}` +
	`:root .chat-workspace :is(.switch,.chatmod-switch-track)::after{content:"";position:absolute;inset-block-start:2px;inset-inline-start:2px;inline-size:14px;block-size:14px;border-radius:50%;background:var(--hcm-color-text-muted);box-shadow:none;transition:inset-inline-start var(--hcm-motion-fast) var(--hcm-motion-easing)}` +
	`:root .chat-workspace :is(.switch:checked,.chatmod-switch[aria-checked="true"] .chatmod-switch-track){border-color:var(--accent);background:var(--accent)}` +
	`:root .chat-workspace :is(.switch:checked,.chatmod-switch[aria-checked="true"] .chatmod-switch-track)::after{inset-inline-start:18px;background:var(--hcm-color-surface)}` +
	`:root .chat-workspace .switch:disabled,:root .chat-workspace .chatmod-switch[disabled] .chatmod-switch-track{opacity:.6}` +
	`:root .chat-workspace .switch:focus-visible{outline:2px solid var(--hcm-color-focus);outline-offset:2px}` +
	// The shell's dark colour mode draws every unchecked checkbox as a clear box
	// with a 1.5px border in the border colour (":root[data-hcm-color-mode=dark]
	// :where(.app-shell) input[type=checkbox]:not(:checked)"). That rule is more
	// specific than the one above and turned an off switch into a dark rectangle on
	// a dark panel. These two say the same thing again with more specificity, for the
	// dark mode and for the system mode when the device is dark, so an off switch
	// keeps its edge and its thumb in both.
	`:root[data-hcm-color-mode="dark"] .chat-workspace input.switch:not(:checked){` + chatbug042OffLook + `}` +
	`@media(prefers-color-scheme:dark){` + `:root[data-hcm-color-mode="system"] .chat-workspace input.switch:not(:checked){` + chatbug042OffLook + `}` + `}` +
	`@media(forced-colors:active){:root .chat-workspace :is(.switch,.chatmod-switch-track){border-color:ButtonText;background:Canvas}:root .chat-workspace :is(.switch,.chatmod-switch-track)::after{background:ButtonText;forced-color-adjust:none}:root .chat-workspace :is(.switch:checked,.chatmod-switch[aria-checked="true"] .chatmod-switch-track){border-color:Highlight;background:Highlight}:root .chat-workspace :is(.switch:checked,.chatmod-switch[aria-checked="true"] .chatmod-switch-track)::after{background:HighlightText}}`
