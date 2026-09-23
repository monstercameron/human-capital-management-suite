//go:build js && wasm

package chatui

import (
	"strconv"
	"syscall/js"
)

const chatImageSelector = ".attachment-image-open[data-media-thumb][data-media-display][data-media-id]"

type chatImageRecord struct {
	button, image        js.Value
	thumbURL, displayURL string
	abort                js.Value
	objectURL            string
}

type chatImageLoader struct {
	root                 js.Value
	room, principal      string
	observer, mutations  js.Value
	intersection, change js.Func
	records              map[string]*chatImageRecord
}

var activeChatImageLoader *chatImageLoader
var nextChatImageRecord uint64

func startChatImageLoading() func() {
	doc := js.Global().Get("document")
	if !doc.Truthy() {
		return func() {}
	}
	root := doc.Call("querySelector", ".chat-workspace")
	if !root.Truthy() {
		return func() {}
	}
	initChatImageLoading(root)
	loader := activeChatImageLoader
	return func() {
		if activeChatImageLoader == loader {
			loader.close()
		}
	}
}

// initChatImageLoading observes the current workspace. Timeline images remain
// src-less until they are within 400px of the viewport.
func initChatImageLoading(root js.Value) {
	if !root.Truthy() {
		return
	}
	if activeChatImageLoader != nil && activeChatImageLoader.root.Equal(root) {
		activeChatImageLoader.sync()
		return
	}
	if activeChatImageLoader != nil {
		activeChatImageLoader.close()
	}
	loader := &chatImageLoader{
		root: root, room: root.Get("dataset").Get("selectedId").String(),
		principal: root.Get("dataset").Get("principal").String(), records: map[string]*chatImageRecord{},
	}
	activeChatImageLoader = loader
	if constructor := js.Global().Get("IntersectionObserver"); constructor.Type() == js.TypeFunction {
		loader.intersection = js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) == 0 {
				return nil
			}
			entries := args[0]
			for i := 0; i < entries.Get("length").Int(); i++ {
				entry := entries.Index(i)
				if entry.Get("isIntersecting").Truthy() {
					loader.loadThumbnail(entry.Get("target"))
				}
			}
			return nil
		})
		loader.observer = constructor.New(loader.intersection, js.ValueOf(map[string]any{"rootMargin": "400px 0px"}))
	}
	loader.change = js.FuncOf(func(js.Value, []js.Value) any { loader.sync(); return nil })
	if constructor := js.Global().Get("MutationObserver"); constructor.Type() == js.TypeFunction {
		loader.mutations = constructor.New(loader.change)
		loader.mutations.Call("observe", root, js.ValueOf(map[string]any{"childList": true, "subtree": true, "attributes": true, "attributeFilter": []any{"data-selected-id", "data-principal", "data-media-thumb", "data-media-display"}}))
	}
	loader.sync()
}

func (l *chatImageLoader) sync() {
	if l == nil || !l.root.Get("isConnected").Truthy() ||
		l.root.Get("dataset").Get("selectedId").String() != l.room ||
		l.root.Get("dataset").Get("principal").String() != l.principal {
		if activeChatImageLoader == l {
			l.close()
		}
		return
	}
	buttons := l.root.Call("querySelectorAll", chatImageSelector)
	present := make(map[string]bool, buttons.Get("length").Int())
	for i := 0; i < buttons.Get("length").Int(); i++ {
		button := buttons.Index(i)
		image := button.Call("querySelector", "img")
		if !image.Truthy() {
			continue
		}
		if button.Get("dataset").Get("mediaId").String() == "" {
			continue
		}
		key := button.Get("__chatImageRecordKey").String()
		if key == "" {
			nextChatImageRecord++
			key = strconv.FormatUint(nextChatImageRecord, 10)
			button.Set("__chatImageRecordKey", key)
		}
		present[key] = true
		thumb := button.Get("dataset").Get("mediaThumb").String()
		display := button.Get("dataset").Get("mediaDisplay").String()
		if !validChatImageVariantDescriptor(thumb, "thumbnail") || !validChatImageVariantDescriptor(display, "display") {
			if old := l.records[key]; old != nil {
				l.releaseRecord(key, old)
			}
			continue
		}
		if old := l.records[key]; old != nil {
			if !old.button.Equal(button) || old.thumbURL != thumb || old.displayURL != display {
				l.releaseRecord(key, old)
			} else {
				continue
			}
		}
		record := &chatImageRecord{button: button, image: image, thumbURL: thumb, displayURL: display}
		l.records[key] = record
		if l.observer.Truthy() {
			l.observer.Call("observe", button)
		} else {
			l.loadThumbnail(button)
		}
	}
	for key, record := range l.records {
		if !present[key] {
			l.releaseRecord(key, record)
		}
	}
}

func (l *chatImageLoader) loadThumbnail(button js.Value) {
	if l == nil || activeChatImageLoader != l || !button.Get("isConnected").Truthy() {
		return
	}
	key := button.Get("__chatImageRecordKey").String()
	record := l.records[key]
	if record == nil || record.abort.Truthy() || record.objectURL != "" {
		return
	}
	controller := js.Global().Get("AbortController").New()
	record.abort = controller
	url := record.thumbURL
	startProtectedImageFetch(button, "thumbnail", url, controller.Get("signal"), func(objectURL string) {
		if activeChatImageLoader != l || l.records[key] != record || !button.Get("isConnected").Truthy() || button.Get("dataset").Get("mediaThumb").String() != url {
			if objectURL != "" {
				revokeChatImageURL(objectURL)
			}
			return
		}
		record.abort = js.Undefined()
		if objectURL == "" {
			return
		}
		record.objectURL = objectURL
		record.image.Set("decoding", "async")
		record.image.Set("src", objectURL)
	})
}

// loadChatImageDisplay is called only after an explicit viewer open. The
// returned cleanup aborts the request and revokes its short-lived URL.
func loadChatImageDisplay(button, fullImage js.Value, ready func(bool)) func() {
	if !button.Truthy() || !fullImage.Truthy() {
		return func() {}
	}
	workspace := button.Call("closest", ".chat-workspace")
	if !workspace.Truthy() {
		ready(false)
		return func() {}
	}
	room := workspace.Get("dataset").Get("selectedId").String()
	principal := workspace.Get("dataset").Get("principal").String()
	url := button.Get("dataset").Get("mediaDisplay").String()
	if !validChatImageVariantDescriptor(url, "display") {
		ready(false)
		return func() {}
	}
	controller := js.Global().Get("AbortController").New()
	objectURL := ""
	closed := false
	settled := false
	var onload, onerror js.Func
	finish := func(success bool) {
		if settled {
			return
		}
		settled = true
		if onload.Value.Truthy() {
			fullImage.Call("removeEventListener", "load", onload)
			fullImage.Call("removeEventListener", "error", onerror)
			onload.Release()
			onerror.Release()
		}
		if success && !closed {
			fullImage.Get("classList").Call("add", "chat-image-display-ready")
		}
		if !closed {
			ready(success)
		}
	}
	startProtectedImageFetch(button, "display", url, controller.Get("signal"), func(fetched string) {
		if closed || !button.Get("isConnected").Truthy() || !workspace.Get("isConnected").Truthy() ||
			workspace.Get("dataset").Get("selectedId").String() != room ||
			workspace.Get("dataset").Get("principal").String() != principal ||
			button.Get("dataset").Get("mediaDisplay").String() != url {
			if fetched != "" {
				revokeChatImageURL(fetched)
			}
			return
		}
		if fetched == "" {
			ready(false)
			return
		}
		objectURL = fetched
		fullImage.Set("decoding", "async")
		onload = js.FuncOf(func(js.Value, []js.Value) any { finish(true); return nil })
		onerror = js.FuncOf(func(js.Value, []js.Value) any { finish(false); return nil })
		fullImage.Call("addEventListener", "load", onload, js.ValueOf(map[string]any{"once": true}))
		fullImage.Call("addEventListener", "error", onerror, js.ValueOf(map[string]any{"once": true}))
		fullImage.Set("src", objectURL)
	})
	return func() {
		if closed {
			return
		}
		closed = true
		controller.Call("abort")
		if !settled {
			settled = true
			if onload.Value.Truthy() {
				fullImage.Call("removeEventListener", "load", onload)
				fullImage.Call("removeEventListener", "error", onerror)
				onload.Release()
				onerror.Release()
			}
		}
		fullImage.Get("classList").Call("remove", "chat-image-display-ready")
		fullImage.Call("removeAttribute", "src")
		if objectURL != "" {
			revokeChatImageURL(objectURL)
		}
	}
}

func startProtectedImageFetch(button js.Value, variant, url string, signal js.Value, done func(string)) {
	if bridge := js.Global().Get("hcmChatMediaFetch"); bridge.Type() == js.TypeFunction {
		promise := bridge.Invoke(button.Get("dataset").Get("mediaId").String(), variant, signal)
		if !promise.Truthy() || !promise.Get("then").Truthy() {
			done("")
			return
		}
		settled := false
		var then, catch js.Func
		finish := func(raw string) {
			if settled {
				return
			}
			settled = true
			then.Release()
			catch.Release()
			done(raw)
		}
		then = js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 {
				finish(args[0].String())
			} else {
				finish("")
			}
			return nil
		})
		catch = js.FuncOf(func(js.Value, []js.Value) any { finish(""); return nil })
		promise.Call("then", then).Call("catch", catch)
		return
	}
	// The integrated app installs an authenticated bridge; URL-backed loading
	// remains as a small standalone test seam for chatui.
	var response, blob, failure js.Func
	responseReleased, blobReleased, failureReleased := false, false, false
	release := func() {
		if !responseReleased {
			response.Release()
			responseReleased = true
		}
		if !blobReleased {
			blob.Release()
			blobReleased = true
		}
		if !failureReleased {
			failure.Release()
			failureReleased = true
		}
	}
	response = js.FuncOf(func(_ js.Value, args []js.Value) any {
		response.Release()
		responseReleased = true
		if len(args) == 0 || !args[0].Get("ok").Truthy() {
			return js.Global().Get("Promise").Call("reject")
		}
		return args[0].Call("blob")
	})
	blob = js.FuncOf(func(_ js.Value, args []js.Value) any {
		value := ""
		if len(args) > 0 && js.Global().Get("URL").Get("createObjectURL").Type() == js.TypeFunction {
			value = js.Global().Get("URL").Call("createObjectURL", args[0]).String()
		}
		done(value)
		blob.Release()
		blobReleased = true
		failure.Release()
		failureReleased = true
		return nil
	})
	failure = js.FuncOf(func(js.Value, []js.Value) any {
		done("")
		release()
		return nil
	})
	options := js.ValueOf(map[string]any{"credentials": "same-origin", "cache": "no-store", "signal": signal})
	js.Global().Call("fetch", url, options).Call("then", response).Call("then", blob).Call("catch", failure)
}

func validChatImageVariantDescriptor(raw, variant string) bool {
	return raw == variant || validChatRenditionURL(raw, variant)
}

func revokeChatImageURL(raw string) {
	if raw != "" && js.Global().Get("URL").Get("revokeObjectURL").Type() == js.TypeFunction {
		js.Global().Get("URL").Call("revokeObjectURL", raw)
	}
}

func (l *chatImageLoader) releaseRecord(key string, record *chatImageRecord) {
	if l.observer.Truthy() {
		l.observer.Call("unobserve", record.button)
	}
	if record.abort.Truthy() {
		record.abort.Call("abort")
	}
	if record.objectURL != "" {
		revokeChatImageURL(record.objectURL)
	}
	if record.image.Truthy() {
		record.image.Call("removeAttribute", "src")
	}
	delete(l.records, key)
}

func (l *chatImageLoader) close() {
	if l == nil {
		return
	}
	for key, record := range l.records {
		l.releaseRecord(key, record)
	}
	if l.observer.Truthy() {
		l.observer.Call("disconnect")
	}
	if l.mutations.Truthy() {
		l.mutations.Call("disconnect")
	}
	if l.intersection.Value.Truthy() {
		l.intersection.Release()
	}
	if l.change.Value.Truthy() {
		l.change.Release()
	}
	if activeChatImageLoader == l {
		activeChatImageLoader = nil
	}
}
