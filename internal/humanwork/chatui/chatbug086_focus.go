package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// CHATBUG-086. Accepting an emoji from the colon list left the caret on the
// page instead of in the message box; a press on the composer's own padding
// focused the page's main region; and a draft restored on load that ended in a
// shortcode opened the list before anyone had typed.
//
// The first is fixed where the list is drawn (emojiCompletionMenu keeps a slot
// when closed) and held by chatbug086HoldCaret. The other two are here.

// chatbug086Composers are the two composers; chatbug086Controls is what a
// press inside one is left to. Everything else in a composer is its own
// surface, and a press there means "type here".
const (
	chatbug086Composers = ".chat-composer,.thread-composer"
	chatbug086Controls  = `button,a[href],input,textarea,select,label,summary,[contenteditable],[role="option"],[role="listbox"],[role="menu"],[role="menuitem"],[role="dialog"],[tabindex]:not([tabindex="-1"])`
)

// chatbug086FocusesField decides whether a press focuses the composer's text
// area: it was inside a composer, not on a control, the text area can take
// text and does not already hold the caret, and the press was not the end of
// selecting text (a preview's words, say), which focusing would throw away.
func chatbug086FocusesField(inComposer, onControl, selecting, fieldUsable, fieldFocused bool) bool {
	return inComposer && !onControl && !selecting && fieldUsable && !fieldFocused
}

// chatbug086OnControl decides whether a press was on one of the composer's
// controls. The nearest control above the press counts only when it is inside
// the composer: the page's own main region is focusable (it scrolls by
// keyboard) and holds every composer, and counting it made every press in a
// composer a press "on a control", so the text area was never focused.
func chatbug086OnControl(controlFound, controlInsideComposer bool) bool {
	return controlFound && controlInsideComposer
}

// chatbug086OpensList decides whether an input event may open the colon list:
// only the person's own typing, in the box that holds the caret. The events
// this page raises itself when it writes a draft (a restored draft, a chosen
// mention or emoji) are not typing.
func chatbug086OpensList(trusted, fieldFocused bool) bool { return trusted && fieldFocused }

// emojiCompletionTyped runs the colon list for one input event in a composer.
func emojiCompletionTyped(e ui.Event, local localStore, target string) {
	if !chatbug086Typed(e, target) {
		if local.get().emojiCompletion.Open {
			local.update(func(u *localUI) { u.emojiCompletion = emojiCompletion{Active: -1} })
		}
		return
	}
	emojiCompletionTrack(local, target)
}
