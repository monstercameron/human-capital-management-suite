package chatui

// Issue3873ThreadPreviewStyles keeps the reply field's tool row, and with it the
// Preview toggle, on screen while the reply is shown as a preview. The compact
// layouts hide that row whenever focus is outside the composer, and a preview is
// not a field, so focus leaves it: the toggle disappeared and the draft could
// not be edited without widening the window.
const Issue3873ThreadPreviewStyles = `.chat-workspace .thread-composer[data-preview="true"]:not(:focus-within) .composer-tools{display:flex}` +
	`.chat-workspace .thread-composer[data-preview="true"] .composer-preview-toggle{display:inline-flex;min-width:44px;min-height:44px}` +
	// The row must not move when focus does: pressing the toggle focuses it, and a
	// layout that changes with focus slides the button from under the pointer, so
	// the press never completes. A preview holds one layout, focused or not.
	`.chat-workspace .thread-composer[data-preview="true"]{flex-direction:column;align-items:stretch;padding:0}` +
	`.chat-workspace .thread-composer[data-preview="true"] .composer-input{flex:none}` +
	`.chat-workspace .thread-composer[data-preview="true"] .composer-toolbar{padding:2px 6px 6px;flex:none}` +
	`.chat-workspace .thread-composer[data-preview="true"] :is(.format-tools,.emoji-control){display:none}`

// issue3873ComposerData marks the thread composer that is showing its preview.
func issue3873ComposerData(local localUI, data map[string]string) map[string]string {
	if composerPreviewOn(local, threadComposerID) {
		data["preview"] = "true"
	}
	return data
}
