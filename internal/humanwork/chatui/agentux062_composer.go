package chatui

// AGENTUX-062, composer layers. One placement rule covers everything the
// composer opens: the layer sits wholly above the composer, its bottom edge
// chatComposerGap clear of the composer's top edge, so the message field stays
// visible and typeable while it is open. chatComposerLayerPlace (in
// agentux_chat5_layers.go) is the rule; this file lists who follows it and how
// each one closes.
//
//	layer          placed by                            closes with                              focus returns to
//	add (+) menu   shared layer code, "add-menu"        Escape, outside press, Tab, an item      the + button
//	GIF picker     shared layer code, "gif"             Escape, outside press, its × button      the GIF button
//	emoji picker   chatemoji_js.go (self placed)        Escape, outside press, a pick            the emoji button
//	voice panel    shared layer code, "voice"           Escape, its own buttons                  the voice opener
//	writing styles shared layer code, "writing-style"   Escape, its own buttons                  the style button
//	location       shared layer code, "location"        Escape, its own buttons                  the location opener
//	formatting     shared layer code, "formatting"      Escape, outside press                    the Aa / format button
//
// The "@" list, the document list and the "/" command list are completions
// attached to the field being typed in, not layers: they are drawn by CSS as
// children of the composer, wholly above it at the same chatComposerGap, they
// open and close with the text (Escape closes them and the caret never leaves
// the field, so there is no focus to return), and a press outside the composer
// closes the "/" list. What the CSS cannot know is how much room there is
// between the conversation header and the composer, so the layer code writes
// that as --chat-composer-room and the lists scroll inside it. The search
// field's recent-searches list is the same kind of completion under a field in
// the sidebar and is not a composer layer.

// chatComposerLayerKind lists the layer kinds that follow the composer rule when
// they are opened from a composer.
func chatComposerLayerKind(kind string) bool {
	switch kind {
	case chatComposerAddKind, "gif", "emoji", "voice", "writing-style", "location", "formatting":
		return true
	}
	return false
}

// chatComposerAddKind is the kind of the + button's menu.
const chatComposerAddKind = "add-menu"

// chatComposerLayerPlacement is chatComposerLayerPlace as a placement that
// does not depend on the content's height staying what it was when measured:
// the layer is pinned by its bottom edge and grows upward until the header.
func chatComposerLayerPlacement(opener, composer, area chatLayerRect, width, height, viewportHeight float64, rtl bool) chatLayerPlacement {
	g := chatComposerLayerPlace(opener, composer, area, width, height, rtl)
	return chatLayerPlacement{
		left: g.left, width: g.width, up: true,
		bottom:    max(0, viewportHeight-(composer.top-chatComposerGap)),
		maxHeight: chatComposerRoom(composer, area),
	}
}

// chatComposerRoom is the height between the conversation header and the
// composer that a layer or list above the composer may use.
func chatComposerRoom(composer, area chatLayerRect) float64 {
	return max(0, composer.top-chatComposerGap-(area.top+8))
}

// agentux062ComposerStyles puts the Add menu and the GIF picker, now layers,
// where the shared placement draws them (the older rules attached them to their
// buttons), and gives the field's completion lists the same clearance above the
// composer and the room cap.
const agentux062ComposerStyles = `.chat-workspace .composer-add-menu[data-chat-layer],.chat-workspace .giphy-picker[data-chat-layer]{position:fixed;inset-inline-start:auto;inset-block-end:auto;z-index:1600}` +
	`.chat-workspace .giphy-picker[data-chat-layer]{max-block-size:none}` +
	// A layer is drawn only while it is in the top layer, whatever display the older rules gave it.
	`.chat-workspace :is(.composer-add-menu,.giphy-picker)[data-chat-layer]:not(:popover-open){display:none}` +
	`.chat-workspace :is(.chat-composer,.thread-composer) .mention-menu{inset-block-end:calc(100% + 8px);max-block-size:min(372px,50vh,var(--chat-composer-room,9999px))}` +
	`.chat-workspace :is(.chat-composer,.thread-composer) .command-menu{inset-block-end:calc(100% + 8px);max-block-size:min(440px,70vh,var(--chat-composer-room,9999px))}`
