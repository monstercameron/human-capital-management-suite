package main

// Escape in a report or removal dialog (CHATBUG-085).
//
// The dialog is drawn inside the Chat workspace, and the workspace's own key
// handler takes every Escape that reaches it and stops it there: it closes the
// thread or the details behind the dialog and the dialog stays. The dialog's
// listener sat on the document, after the workspace, and so never heard a key
// pressed on one of the dialog's own controls (a reason, the details box). The
// listener is now the first to hear the key (the window, capturing), and these
// two functions hold its decisions.

// chatremoveEscapeCloses names what Escape closes: the dialog whenever one is
// open, wherever focus is; otherwise the Moderation page when the key was
// pressed inside it. "" leaves the key to the rest of Chat. A key that ends an
// input-method composition is the composition's, not the dialog's.
func chatremoveEscapeCloses(dialogOpen, pageOpen, targetInPage, composing bool) string {
	switch {
	case composing:
		return ""
	case dialogOpen:
		return "dialog"
	case pageOpen && targetInPage:
		return "page"
	}
	return ""
}

// chatbug085AwaitsMore decides whether focus, handed to a message row whose
// More button is not on the page, will move on to that button. The button is
// drawn only while its row is active, and a focused row is active when focus
// shows (the keyboard closed the dialog) or on a phone or touch screen. After
// a mouse click the row keeps focus and no button is waited for.
func chatbug085AwaitsMore(rowFocusVisible, compact bool) bool {
	return rowFocusVisible || compact
}
