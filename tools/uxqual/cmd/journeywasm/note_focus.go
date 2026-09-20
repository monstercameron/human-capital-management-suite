package main

import "github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"

// noteFocusTracker decides when the journey notes composer should take
// keyboard focus back. The Add note button is disabled while a note is being
// sent, and a disabled element drops focus to <body>; once the submission
// settles -- recorded or refused -- the reader belongs back in the textarea,
// ready to write the next note or fix the refused one.
type noteFocusTracker struct {
	busy bool
}

// observe records the composer's current state and reports whether a
// submission has just settled.
func (t *noteFocusTracker) observe(page journey.Page) bool {
	var composer *journey.NoteComposer
	if page.Detail != nil && page.Detail.Notes != nil {
		composer = page.Detail.Notes.Composer
	}
	if composer == nil {
		t.busy = false
		return false
	}
	settled := t.busy && !composer.Busy
	t.busy = composer.Busy
	return settled
}
