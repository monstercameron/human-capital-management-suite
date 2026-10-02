package chatui

// composerField is the text field the mention menu writes its choice into. The
// page's field is the browser's textarea (mention_js.go); a test supplies one
// that raises the same input event, so the order of events on the page can be
// walked without a browser.
type composerField struct {
	selection func(id string) (value string, caret int, ok bool)
	replace   func(id, value string, caret int)
}

// pickMention puts the choice at index of the open menu into the field.
//
// Everything the draft must remember about the choice is stored before the
// field's text changes, because writing the text raises the field's input
// event and that event reconciles the store against the new text (and asks for
// a render). Stored afterwards, a person outside the conversation was added
// after the render that should have shown the line under the draft, and
// nothing asked for another one (CHATBUG-071, page check of 2026-10-02: the
// name in the draft and no line under it, not even after the next key).
func pickMention(m Model, mention mentionStore, field composerField, state mentionState, index int) {
	options, _ := mentionOptionsForState(m, state.Query, state.Target, state.ShowAllPeople)
	mention.Set(mentionState{})
	if !state.Open || index < 0 || index >= len(options) {
		return
	}
	// Re-read the field: the list was drawn a keystroke ago, and the
	// replacement must land on the "@query" the field holds now.
	value, caret, ok := field.selection(state.Target)
	if !ok {
		return
	}
	_, start, found := mentionTokenAt(value, caret)
	if !found || start != state.Start {
		return
	}
	option := options[index]
	if option.person != nil {
		updated, next := applyMention(value, start, caret, option.person.Name)
		// The range stops before the space the menu adds, the way the name does.
		mention.AddPerson(state.Target, m.SelectedID, start, next-1, *option.person)
		field.replace(state.Target, updated, next)
		mentionPicked(m)
		return
	}
	if option.persona != nil {
		_, _, reference, ok := applyPersonaMention(value, start, caret, *option.persona, m.SelectedID)
		if !ok {
			return
		}
		updated, next := insertEmojiAtUTF16(value, "", start, caret)
		mention.AddPersonaToken(state.Target, m.SelectedID, reference)
		field.replace(state.Target, updated, next)
		mention.Set(mentionState{})
		mentionPicked(m)
	}
}

// mentionPicked asks the page to draw once the choice is in the draft. Putting
// the text into the field only updates the stored draft, which schedules no
// render, so the note under the composer (composer-outside-note) and the
// count on the form waited for the next unrelated render.
func mentionPicked(m Model) {
	if m.Callbacks.MentionPicked != nil {
		m.Callbacks.MentionPicked()
	}
}

// pageComposerField is the field the page's menus write to: the browser's
// textarea on the page, nothing in a native build.
var pageComposerField = composerField{selection: composerSelection, replace: replaceComposerText}
