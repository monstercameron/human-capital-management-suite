//go:build !js || !wasm

package chatui

type giphyPickerViews struct{}

func newGiphyPickerViews() *giphyPickerViews          { return &giphyPickerViews{} }
func (*giphyPickerViews) toggle(string, string)       {}
func (*giphyPickerViews) loadMore(string)             {}
func (*giphyPickerViews) selectResult(string, string) {}
func (*giphyPickerViews) closeAll()                   {}
