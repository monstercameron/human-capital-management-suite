package chatui

// chatImageViewerNeedsActualSize reports whether the source image is currently
// being reduced to fit the viewer. Images that already fit need no 1:1 toggle.
func chatImageViewerNeedsActualSize(naturalWidth, naturalHeight, availableWidth, availableHeight int) bool {
	if naturalWidth <= 0 || naturalHeight <= 0 || availableWidth <= 0 || availableHeight <= 0 {
		return false
	}
	return naturalWidth > availableWidth || naturalHeight > availableHeight
}
