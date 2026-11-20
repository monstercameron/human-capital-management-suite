package chatui

// composerPreviewFocusSelector names the Preview toggle of one composer: the
// channel composer's or the thread reply form's.
func composerPreviewFocusSelector(target string) string {
	if target == threadComposerID {
		return "form.thread-composer .composer-preview-toggle"
	}
	return "form.chat-composer .composer-preview-toggle"
}

// composerPreviewFlip switches one composer between writing and previewing, then
// asks for the keyboard caret to return to that composer's Preview toggle
// (#3869). The redraw replaces the button that was pressed, and a removed
// element leaves the caret on the page body, so Tab would restart at the page's
// first link. Preview stays a real, pressed-or-not button the reader can press
// again to return to the text.
func composerPreviewFlip(target string, update func(func(*localUI)), focus func(string)) {
	if target == threadComposerID {
		update(func(u *localUI) { u.threadComposerPreview = !u.threadComposerPreview })
	} else {
		update(func(u *localUI) { u.composerPreview = !u.composerPreview })
	}
	focus(composerPreviewFocusSelector(target))
}
