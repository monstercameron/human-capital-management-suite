package chatui

// CHATBUG-035: "Jump to newest" is for a reader who has newer messages out of
// view, either below the fold of what is loaded or in a page not yet loaded.
// A conversation with nothing to scroll has neither, whatever the wheel did.

// chatJumpScrollable reports whether the list overflows its box at all.
func chatJumpScrollable(scrollHeight, clientHeight float64) bool {
	return scrollHeight-clientHeight > 2
}

// chatNewestOutOfView reports whether messages newer than the ones in view
// exist: rows below the visible area, or more pages after the loaded one.
func chatNewestOutOfView(scrollHeight, scrollTop, clientHeight float64, hasNewerPage bool) bool {
	return hasNewerPage || scrollHeight-scrollTop-clientHeight > 2
}
