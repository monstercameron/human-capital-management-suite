package chatui

// chatImagePhase is what the viewer sees for one attachment preview.
type chatImagePhase uint8

const (
	// chatImagePending: no bytes on screen yet; the tile shows a neutral
	// placeholder at its final size.
	chatImagePending chatImagePhase = iota
	// chatImageLoaded: the preview decoded and is on screen.
	chatImageLoaded
	// chatImageFailed: the media really could not be loaded.
	chatImageFailed
)

// chatImageEvent is something that happened to an attachment's preview.
type chatImageEvent uint8

const (
	// chatImageEventDecoded: the <img> decoded the fetched bytes.
	chatImageEventDecoded chatImageEvent = iota
	// chatImageEventBroke: a real failure -- an error status, an empty
	// response, a decode error, or the watchdog expiring.
	chatImageEventBroke
	// chatImageEventCancelled: the request was aborted because the timeline
	// replaced or released the element. It says nothing about the media.
	chatImageEventCancelled
	// chatImageEventRetry: the one retry of a failed preview starts.
	chatImageEventRetry
)

// chatImageEffect is what the DOM must be made to show after an event or a
// remount.
type chatImageEffect uint8

const (
	chatImageEffectNone chatImageEffect = iota
	// chatImageEffectShowLoaded: give the element the remembered object URL.
	chatImageEffectShowLoaded
	// chatImageEffectShowFailed: mark the tile failed without refetching.
	chatImageEffectShowFailed
	// chatImageEffectFetch: the element has nothing yet; fetch the preview.
	chatImageEffectFetch
)

// chatImageMemo is what the image loader remembers about one attachment
// between the elements that display it. The timeline re-renders on hover,
// menus and streams and may hand the loader a brand new <img> for the same
// attachment; that element starts with no src and no class, so everything the
// viewer already saw has to come from here rather than from the old element.
type chatImageMemo struct {
	phase chatImagePhase
	// objectURL is the fetched thumbnail, owned by the memo once it is set; a
	// released element must not revoke it, because the next element reuses it.
	objectURL string
	// retried is set once the single retry of a failed preview was spent.
	retried bool
}

// fetched records the bytes of a first successful fetch. The phase stays
// pending until the <img> decodes them. A second fetch of the same attachment
// (two elements racing) keeps the first URL; the loser stays owned by its own
// element.
func (m *chatImageMemo) fetched(objectURL string) {
	if m.objectURL == "" && objectURL != "" {
		m.objectURL = objectURL
	}
}

// apply folds one event into the memo.
func (m *chatImageMemo) apply(event chatImageEvent) {
	switch event {
	case chatImageEventDecoded:
		if m.objectURL != "" {
			m.phase = chatImageLoaded
		}
	case chatImageEventBroke:
		// A loaded preview is not unloaded by a failure of a request that was
		// superseded; only a failure of the displayed bytes arrives here.
		m.phase = chatImageFailed
	case chatImageEventCancelled:
		// An aborted request is not a failed one: nothing changes.
	case chatImageEventRetry:
		if m.phase == chatImageFailed && !m.retried {
			m.retried = true
			m.phase = chatImagePending
		}
	}
}

// discardURL gives up the remembered URL (decode error, room change) and
// returns it for revoking.
func (m *chatImageMemo) discardURL() string {
	url := m.objectURL
	m.objectURL = ""
	return url
}

// remounted says what a new element for this attachment must be made to show.
func (m *chatImageMemo) remounted() chatImageEffect {
	switch {
	case m.phase == chatImageLoaded && m.objectURL != "":
		return chatImageEffectShowLoaded
	case m.phase == chatImageFailed:
		return chatImageEffectShowFailed
	}
	return chatImageEffectFetch
}
