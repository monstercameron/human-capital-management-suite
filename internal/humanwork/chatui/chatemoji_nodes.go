package chatui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// chatEmojiComposerPicker is the composer's emoji button. The picker it opens is
// not drawn here: it is one of the workspace's emoji layers (chatEmojiLayers), so
// that opening it and typing into it redraw the picker alone, not the page.
func chatEmojiComposerPicker(m Model, local localUI, targetID string, disabled bool) ui.Node {
	st := local.emoji
	open := st.Open && !st.Reaction && st.Target == targetID
	return html.Div(html.Props{Class: "emoji-control"},
		html.Button(html.Props{
			Class: "tool-button emoji-trigger", Type: "button", Disabled: disabled,
			Data:  map[string]string{"action": "emoji-toggle", "id": targetID},
			Aria:  map[string]string{"label": chatEmojiText(m, emojiKeyTrigger), "expanded": boolString(open), "controls": "emoji-emoji-" + targetID, "haspopup": "dialog"},
			Title: chatEmojiText(m, emojiKeyTrigger),
		}, icon("smile")),
	)
}

// chatEmojiComposerLayer is a composer's picker: while open, the picker itself;
// while closed, the hidden shell the click shows at once (see chatEmojiShell).
// Both are the same element in the page, so opening gives it content, not a new node.
func chatEmojiComposerLayer(m Model, local localUI, targetID string) ui.Node {
	st := local.emoji
	if st.Open && !st.Reaction && st.Target == targetID {
		return chatEmojiPicker(m, st, "emoji", chatEmojiData.index, chatEmojiData.status, chatEmojiHost.prefs)
	}
	return chatEmojiShell(m, targetID, chatEmojiHost.prefs)
}

// chatEmojiReactionLayer is the reaction picker for the message the model has
// one open for.
func chatEmojiReactionLayer(m Model, local localUI) ui.Node {
	if m.PickerID == "" {
		return nil
	}
	st := emojiStateFor(local.emoji, m.PickerID, true)
	return chatEmojiPicker(m, st, "reaction", chatEmojiData.index, chatEmojiData.status, chatEmojiHost.prefs)
}

// emojiLayersProps is what the emoji layers component is given by the workspace.
type emojiLayersProps struct{ Model Model }

// chatEmojiLayers is the one component that holds every emoji picker: the
// reaction picker and the two composers' pickers. It has a state of its own, so a
// click on the emoji button, a typed letter or the arrival of the emoji data
// redraws these few elements and not the whole workspace around them.
func chatEmojiLayers(p emojiLayersProps) ui.Node {
	tick := ui.UseState(uint64(0))
	chatEmojiHost.layerRender = func() {
		chatEmojiHost.layerSeq++
		tick.Set(chatEmojiHost.layerSeq)
	}
	ui.UseEffectOf(func() func() { return func() { chatEmojiHost.layerRender = nil } }, struct{}{})
	// After each draw of the layers: shown, placed, sized and given the cursor.
	ui.UseLayoutEffect(func() func() { chatEmojiAfterRender(); return nil })
	return ui.Fragment(chatEmojiLayerNodes(p.Model, chatEmojiHost.state())...)
}

// chatEmojiLayerNodes draws the layers for a model and the picker's state.
func chatEmojiLayerNodes(m Model, local localUI) []ui.Node {
	nodes := []ui.Node{chatEmojiReactionLayer(m, local), nil, nil}
	// A composer's picker exists where the composer does.
	if m.SelectedID != "" {
		nodes[1] = chatEmojiComposerLayer(m, local, "chat-composer")
	}
	if m.SelectedID != "" && m.ShowThread {
		nodes[2] = chatEmojiComposerLayer(m, local, "thread-composer")
	}
	return nodes
}
