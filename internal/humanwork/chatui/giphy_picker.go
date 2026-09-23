package chatui

// GiphyPickerConfig contains browser-side GIPHY configuration. An empty key
// keeps the picker unavailable and prevents any request from being sent.
type GiphyPickerConfig struct {
	APIKey         string
	DebounceMillis int
}

// GiphyPickerCallbacks reports transient results to the owning view. Selection
// returns the provider's canonical share URL and its validated ID; callers
// persist the share URL only, never a rendition URL.
type GiphyPickerCallbacks struct {
	OnResults     func([]GiphyResult, bool, bool)
	OnLoading     func(bool)
	OnError       func(string)
	OnUnavailable func()
	OnSelect      func(GiphyResult)
}
