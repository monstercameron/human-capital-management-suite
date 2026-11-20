package chatui

// chatImageViewerFocus says where focus goes when the image viewer closes.
type chatImageViewerFocus int

const (
	// chatImageViewerFocusNone: nothing the viewer knows is on the page.
	chatImageViewerFocusNone chatImageViewerFocus = iota
	// chatImageViewerFocusOrigin: the thumbnail that opened the viewer.
	chatImageViewerFocusOrigin
	// chatImageViewerFocusOriginByID: that thumbnail, drawn again by a render.
	chatImageViewerFocusOriginByID
	// chatImageViewerFocusLast: the last image viewed, scrolled into view because
	// the opening thumbnail is no longer on the page.
	chatImageViewerFocusLast
)

// chatImageViewerFocusPlan picks the focus target on close. The opening
// thumbnail always wins, so stepping through the gallery never moves the
// reader's place in the conversation; the last image viewed is only a fallback.
func chatImageViewerFocusPlan(originConnected, originFoundByID, lastConnected bool) chatImageViewerFocus {
	switch {
	case originConnected:
		return chatImageViewerFocusOrigin
	case originFoundByID:
		return chatImageViewerFocusOriginByID
	case lastConnected:
		return chatImageViewerFocusLast
	}
	return chatImageViewerFocusNone
}

// scrolls reports whether focusing may move the page. Returning to the opener
// keeps the scroll position; the fallback brings its thumbnail into view so
// focus is never left off screen.
func (f chatImageViewerFocus) scrolls() bool { return f == chatImageViewerFocusLast }
