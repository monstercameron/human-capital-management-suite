package chatui

// chatux015CloseSearch is what the search layer's close button does to the query
// (CHATUX-015): closing a search clears the field, so the box does not keep an
// old query that the next opening would show again.
func chatux015CloseSearch(m Model) {
	if m.Search == "" || m.Callbacks.Search == nil {
		return
	}
	m.Callbacks.Search("")
	setDOMValue("chat-search", "")
}
