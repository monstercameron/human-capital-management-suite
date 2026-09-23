//go:build js && wasm

package chatui

import (
	"strings"
	"syscall/js"
)

type giphyView struct {
	picker                                       *GiphyPicker
	query                                        js.Func
	key                                          js.Func
	close                                        js.Func
	closeButton                                  js.Value
	loadingText, errorText, emptyText, closeText string
	results                                      map[string]GiphyResult
}

// giphyPickerViews belongs to one Workspace hook lifecycle. Only one picker
// can be active at a time, so the controller retains at most one view.
type giphyPickerViews struct {
	targetID string
	view     *giphyView
}

func newGiphyPickerViews() *giphyPickerViews { return &giphyPickerViews{} }

func (views *giphyPickerViews) toggle(targetID, apiKey string) {
	doc := js.Global().Get("document")
	pickerID := targetID + "-giphy-picker"
	dialog := doc.Call("getElementById", pickerID)
	if !dialog.Truthy() {
		return
	}
	if !dialog.Get("hidden").Bool() {
		views.close(targetID, true)
		return
	}
	views.closeAll()
	dialog.Set("hidden", false)
	trigger := doc.Call("querySelector", "[data-action=giphy-toggle][data-id='"+targetID+"']")
	if trigger.Truthy() {
		trigger.Call("setAttribute", "aria-expanded", "true")
	}
	query := doc.Call("getElementById", pickerID+"-query")
	status := doc.Call("getElementById", pickerID+"-status")
	dataset := dialog.Get("dataset")
	view := &giphyView{
		results:     map[string]GiphyResult{},
		loadingText: giphyDatasetText(dataset, "loading", "Loading GIFs…"),
		errorText:   giphyDatasetText(dataset, "loadError", "GIFs could not be loaded."),
		emptyText:   giphyDatasetText(dataset, "noResults", "No GIFs found."),
		closeText:   giphyDatasetText(dataset, "close", "Close GIF picker"),
	}
	view.close = js.FuncOf(func(js.Value, []js.Value) any { views.close(targetID, true); return nil })
	view.closeButton = createGiphyCloseButton(doc, query, view.closeText, view.close)
	view.key = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 {
			views.handleKey(targetID, args[0].Get("key").String())
		}
		return nil
	})
	doc.Call("addEventListener", "keydown", view.key)
	view.picker = NewGiphyPicker(GiphyPickerConfig{APIKey: apiKey}, GiphyPickerCallbacks{
		OnUnavailable: func() { status.Set("textContent", view.errorText) },
		OnLoading: func(v bool) {
			if v {
				status.Set("textContent", view.loadingText)
			}
		},
		OnError: func(string) { status.Set("textContent", view.errorText) },
		OnResults: func(items []GiphyResult, appendPage, hasMore bool) {
			results := doc.Call("getElementById", pickerID+"-results")
			if !appendPage {
				results.Set("textContent", "")
				view.results = map[string]GiphyResult{}
			}
			for _, item := range items {
				if len(view.results) >= 40 {
					break
				}
				view.results[item.ID] = item
				button := doc.Call("createElement", "button")
				button.Set("type", "button")
				button.Set("className", "giphy-result")
				button.Call("setAttribute", "data-action", "giphy-select")
				button.Call("setAttribute", "data-id", targetID)
				button.Call("setAttribute", "data-extra", item.ID)
				alt := item.Alt
				if alt == "" {
					alt = "GIF"
				}
				button.Call("setAttribute", "aria-label", alt)
				img := doc.Call("createElement", "img")
				img.Set("loading", "lazy")
				img.Set("decoding", "async")
				img.Set("src", item.ThumbnailURL)
				img.Set("alt", alt)
				button.Call("appendChild", img)
				results.Call("appendChild", button)
			}
			more := doc.Call("querySelector", "[data-action=giphy-more][data-id='"+targetID+"']")
			if more.Truthy() {
				more.Set("hidden", !hasMore || len(view.results) >= 40)
			}
			if len(view.results) == 0 {
				status.Set("textContent", view.emptyText)
			} else {
				status.Set("textContent", "")
			}
		},
		OnSelect: func(item GiphyResult) { views.close(targetID, false); insertGiphyLink(targetID, item.URL) },
	})
	view.query = js.FuncOf(func(js.Value, []js.Value) any {
		value := LimitGiphyQuery(query.Get("value").String())
		if strings.TrimSpace(value) == "" {
			view.picker.Trending()
		} else {
			view.picker.Search(value)
		}
		return nil
	})
	query.Call("addEventListener", "input", view.query)
	view.results = map[string]GiphyResult{}
	// A second listener owns result selection and pagination in the delegated root handler.
	views.targetID, views.view = targetID, view
	query.Call("focus")
	view.picker.Trending()
}

func createGiphyCloseButton(doc, before js.Value, label string, onClick js.Func) js.Value {
	button := doc.Call("createElement", "button")
	button.Set("type", "button")
	button.Set("className", "giphy-close")
	button.Set("textContent", "×")
	button.Call("setAttribute", "aria-label", label)
	button.Call("setAttribute", "style", "float:inline-end")
	button.Call("addEventListener", "click", onClick)
	before.Call("before", button)
	return button
}

func (views *giphyPickerViews) handleKey(targetID, key string) bool {
	if key != "Escape" {
		return false
	}
	views.close(targetID, true)
	return true
}

func giphyDatasetText(dataset js.Value, key, fallback string) string {
	value := dataset.Get(key)
	if value.Type() == js.TypeString && value.String() != "" {
		return value.String()
	}
	return fallback
}

func (views *giphyPickerViews) loadMore(targetID string) {
	if views.targetID == targetID && views.view != nil {
		views.view.picker.LoadMore()
	}
}

func (views *giphyPickerViews) selectResult(targetID, id string) {
	if views.targetID == targetID && views.view != nil {
		if item, ok := views.view.results[id]; ok {
			views.view.picker.Select(item)
		}
	}
}

func (views *giphyPickerViews) close(targetID string, restoreFocus bool) {
	if views.targetID == targetID && views.view != nil {
		v := views.view
		v.picker.Close()
		query := js.Global().Get("document").Call("getElementById", targetID+"-giphy-picker-query")
		if query.Truthy() {
			query.Call("removeEventListener", "input", v.query)
		}
		v.query.Release()
		v.closeButton.Call("removeEventListener", "click", v.close)
		v.closeButton.Call("remove")
		v.close.Release()
		doc := js.Global().Get("document")
		doc.Call("removeEventListener", "keydown", v.key)
		v.key.Release()
		views.targetID, views.view = "", nil
	}
	doc := js.Global().Get("document")
	dialog := doc.Call("getElementById", targetID+"-giphy-picker")
	if dialog.Truthy() {
		dialog.Set("hidden", true)
	}
	trigger := doc.Call("querySelector", "[data-action=giphy-toggle][data-id='"+targetID+"']")
	if trigger.Truthy() {
		trigger.Call("setAttribute", "aria-expanded", "false")
		if restoreFocus {
			trigger.Call("focus")
		}
	}
}

func (views *giphyPickerViews) closeAll() {
	if views.view != nil {
		views.close(views.targetID, false)
	}
}

func insertGiphyLink(targetID, link string) {
	el := js.Global().Get("document").Call("getElementById", targetID)
	if !el.Truthy() {
		return
	}
	start, end := 0, 0
	if s := el.Get("selectionStart"); s.Type() == js.TypeNumber {
		start = s.Int()
	}
	if e := el.Get("selectionEnd"); e.Type() == js.TypeNumber {
		end = e.Int()
	}
	text, cursor := InsertGiphyLinkAtUTF16(el.Get("value").String(), link, start, end)
	el.Set("value", text)
	el.Call("setSelectionRange", cursor, cursor)
	constructor := js.Global().Get("Event")
	if constructor.Type() == js.TypeFunction {
		el.Call("dispatchEvent", constructor.New("input", map[string]any{"bubbles": true}))
	}
	el.Call("focus")
}
