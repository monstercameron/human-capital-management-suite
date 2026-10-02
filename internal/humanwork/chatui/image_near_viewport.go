package chatui

// chatImagePrefetchMargin is how far outside the timeline's visible box a
// thumbnail starts loading, matching the observer's root margin.
const chatImagePrefetchMargin = 400.0

// chatImageNearViewport reports whether a tile (its top and bottom in
// viewport coordinates) lies within margin of the scroller's visible box. A
// tile with no height has no layout yet and is left to the observer.
//
// CHATBUG-031: IntersectionObserver reports crossings only when the page
// renders a frame. A page that does not paint (the review browser pane while
// it is not frontmost, a background tab) never reports the first crossing, so
// the tile stayed an empty box for good. Layout is available without a frame,
// so the loader asks the geometry itself as well.
func chatImageNearViewport(top, bottom, rootTop, rootBottom, margin float64) bool {
	if !(bottom > top) {
		return false
	}
	return bottom >= rootTop-margin && top <= rootBottom+margin
}
