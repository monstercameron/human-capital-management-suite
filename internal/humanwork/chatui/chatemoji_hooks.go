package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// useChatEmoji is the workspace's one call for the emoji picker: it refreshes
// the host from this render and installs the listeners once. The pickers
// themselves are drawn, shown and placed by the chatEmojiLayers component.
func useChatEmoji(model Model, local localStore) {
	chatEmojiHost.bind(model, local)
	ui.UseEffectOf(func() func() { return bindChatEmoji() }, struct{}{})
}

// closeEmojiPickers closes the composer's picker. The workspace calls it for any
// click that is not on the picker (a click on another control, or on nothing),
// which is how an open picker gives way. The reaction picker is closed through
// the message model, like every other reaction picker.
func closeEmojiPickers(restoreFocus bool) {
	if st := chatEmojiHost.state().emoji; st.Open && !st.Reaction {
		chatEmojiClose(restoreFocus)
	}
}

// chatEmojiQuickReactions are the one-click reactions on a message's action bar:
// the person's three most used emoji, with the defaults until they have their own.
func chatEmojiQuickReactions() []string {
	if emojiFlagsDrawn() {
		return chatEmojiHost.prefs.quickReactions()
	}
	// No flag glyphs here: a flag is never one of the three.
	p := chatEmojiHost.prefs
	p.Usage = nil
	for _, use := range chatEmojiHost.prefs.Usage {
		if !emojiNeedsFlagGlyphs(use.Glyph) {
			p.Usage = append(p.Usage, use)
		}
	}
	return p.quickReactions()
}
