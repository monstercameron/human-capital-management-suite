//go:build !(js && wasm)

package chatui

// GiphyPicker is inert outside the browser.
type GiphyPicker struct{}

func NewGiphyPicker(config GiphyPickerConfig, callbacks GiphyPickerCallbacks) *GiphyPicker {
	if !GiphyConfigured(config) && callbacks.OnUnavailable != nil {
		callbacks.OnUnavailable()
	}
	return &GiphyPicker{}
}

func (*GiphyPicker) Search(string)           {}
func (*GiphyPicker) Trending()               {}
func (*GiphyPicker) Resolve([]string)        {}
func (*GiphyPicker) LoadMore()               {}
func (*GiphyPicker) Cancel()                 {}
func (*GiphyPicker) Close()                  {}
func (*GiphyPicker) Select(GiphyResult) bool { return false }
