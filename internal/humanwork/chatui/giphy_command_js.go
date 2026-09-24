//go:build js && wasm

package chatui

import "syscall/js"

// openSearch opens a composer's GIF picker (if closed) and runs a search, the
// way "/giphy <search>" does in Slack. An empty query keeps the trending list.
func (views *giphyPickerViews) openSearch(targetID, apiKey, query string) {
	doc := js.Global().Get("document")
	dialog := doc.Call("getElementById", targetID+"-giphy-picker")
	if !dialog.Truthy() {
		return
	}
	if dialog.Get("hidden").Bool() {
		views.toggle(targetID, apiKey)
	}
	if query == "" || views.view == nil || views.targetID != targetID {
		return
	}
	if input := doc.Call("getElementById", targetID+"-giphy-picker-query"); input.Truthy() {
		input.Set("value", query)
	}
	views.view.picker.Search(query)
}
