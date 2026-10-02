package chatui

// CHATBUG-090: the sidebar row menu closed itself a moment after it opened.
// It listened for scroll events anywhere in the document and treated every one
// as "the page moved under the menu"; the message list scrolls on its own
// (virtualized rows, scroll anchoring after a render), so the menu was gone
// before it could be read.

// chatbug090RailMenuScrollDismisses says whether a scroll closes the open row
// menu. Only the places the menu is anchored to count: the sidebar list (its
// button moved) and the page itself (the whole layout moved). A scroll inside
// the menu, or in the message list or any other pane, leaves it open.
func chatbug090RailMenuScrollDismisses(inMenu, inRail, isPage bool) bool {
	if inMenu {
		return false
	}
	return inRail || isPage
}
