//go:build js && wasm

package chatui

import (
	"strconv"
	"syscall/js"
)

const chatImageSelector = ".attachment-image-open[data-media-thumb][data-media-display][data-media-id]"

// chatImageWatchdogMillis bounds how long a thumbnail fetch may sit
// unresolved before it is treated as failed (C-2 live re-check). A var, not
// a const, so a test can shorten it rather than sleep for the real bound.
var chatImageWatchdogMillis = 15000

type chatImageRecord struct {
	button, image          js.Value
	thumbURL, displayURL   string
	abort, retryTarget     js.Value
	objectURL              string
	failed, retryAttempted bool
}

type chatImageLoader struct {
	root                                    js.Value
	room, principal                         string
	observer, retryObserver, mutations      js.Value
	intersection, retryIntersection, change js.Func
	records                                 map[string]*chatImageRecord
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
	if loader == nil {
		return func() {}
	}
	// The loader may be reused (rebound) by the next room's effect before
	// this cleanup runs; only close it if it still serves this effect's room.
	room, principal := loader.room, loader.principal
	return func() {
		if activeChatImageLoader == loader && loader.room == room && loader.principal == principal {
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
	// A prefetch can fail while the image is still inside the warm-up
	// margin. In that case the primary observer may not report another
	// crossing as the image moves into view. Arm one viewport-only retry.
	loader.retryIntersection = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		entries := args[0]
		for i := 0; i < entries.Get("length").Int(); i++ {
			entry := entries.Index(i)
			if entry.Get("isIntersecting").Truthy() {
				target := entry.Get("target")
				button := target
				if target.Get("matches").Type() == js.TypeFunction && target.Call("matches", ".attachment-image").Bool() {
					button = target.Call("querySelector", chatImageSelector)
				}
				if button.Truthy() {
					loader.retryThumbnail(button)
				}
			}
		}
		return nil
	})
	loader.installIntersectionObservers()
	loader.change = js.FuncOf(func(js.Value, []js.Value) any { loader.sync(); return nil })
	if constructor := js.Global().Get("MutationObserver"); constructor.Type() == js.TypeFunction {
		loader.mutations = constructor.New(loader.change)
		// The route renderer can replace the workspace node while posts arrive.
		// Observing that node would strand the loader on a detached subtree, so
		// watch the stable document body and resolve the current workspace in sync.
		observationRoot := js.Null()
		if doc := js.Global().Get("document"); doc.Truthy() {
			observationRoot = doc.Get("body")
		}
		if !observationRoot.Truthy() {
			observationRoot = root
		}
		loader.mutations.Call("observe", observationRoot, js.ValueOf(map[string]any{"childList": true, "subtree": true, "attributes": true, "attributeFilter": []any{"data-selected-id", "data-principal", "data-media-thumb", "data-media-display"}}))
	}
	loader.sync()
}

func (l *chatImageLoader) installIntersectionObservers() {
	constructor := js.Global().Get("IntersectionObserver")
	if constructor.Type() != js.TypeFunction {
		l.observer = js.Undefined()
		l.retryObserver = js.Undefined()
		return
	}
	// The timeline scrolls inside #chat-main. Using the browser viewport as
	// the observer root can leave older, virtualized rows unreported while
	// that inner scrollport moves, so observe against the current scroller.
	scrollRoot := js.Null()
	if query := l.root.Get("querySelector"); query.Type() == js.TypeFunction {
		scrollRoot = l.root.Call("querySelector", "#chat-main")
	}
	observerOptions := js.Global().Get("Object").New()
	observerOptions.Set("rootMargin", "400px 0px")
	if scrollRoot.Truthy() {
		observerOptions.Set("root", scrollRoot)
	}
	l.observer = constructor.New(l.intersection, observerOptions)
	retryOptions := js.Global().Get("Object").New()
	retryOptions.Set("rootMargin", "0px")
	if scrollRoot.Truthy() {
		retryOptions.Set("root", scrollRoot)
	}
	l.retryObserver = constructor.New(l.retryIntersection, retryOptions)
}

func (l *chatImageLoader) sync() {
	if l == nil {
		return
	}
	currentRoot := js.Null()
	if doc := js.Global().Get("document"); doc.Truthy() && doc.Get("querySelector").Type() == js.TypeFunction {
		currentRoot = doc.Call("querySelector", ".chat-workspace")
	} else if l.root.Get("isConnected").Truthy() {
		// Keep the direct-root seam available to the WASM unit tests.
		currentRoot = l.root
	}
	if !currentRoot.Truthy() || !currentRoot.Get("isConnected").Truthy() {
		if activeChatImageLoader == l {
			l.close()
		}
		return
	}
	// Round 3 C-4 (live): closing on a room or principal change left no
	// loader at all whenever the attribute mutation reached this observer
	// after the workspace's layout effect had already reused it, so every
	// tile in the next room kept a src-less <img> (the broken-image glyph)
	// and never even started its fetch or watchdog. Rebind instead: drop
	// the old room's records -- aborting their fetches, so a late response
	// still cannot decorate the new room -- and adopt the current one.
	if room, principal := currentRoot.Get("dataset").Get("selectedId").String(), currentRoot.Get("dataset").Get("principal").String(); room != l.room || principal != l.principal {
		if activeChatImageLoader != l {
			return
		}
		for key, record := range l.records {
			l.releaseRecord(key, record)
		}
		l.records = map[string]*chatImageRecord{}
		l.room, l.principal = room, principal
	}
	// GWC may replace .chat-workspace while an async route refresh keeps the
	// same room and principal. Recreate both observers too: the old root was
	// the detached tree's #chat-main scrollport.
	if !l.root.Equal(currentRoot) {
		for key, record := range l.records {
			l.releaseRecord(key, record)
		}
		l.records = map[string]*chatImageRecord{}
		if l.observer.Truthy() {
			l.observer.Call("disconnect")
		}
		if l.retryObserver.Truthy() {
			l.retryObserver.Call("disconnect")
		}
		l.root = currentRoot
		l.installIntersectionObservers()
	}
	l.root = currentRoot
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
		keyValue := button.Get("__chatImageRecordKey")
		key := ""
		if keyValue.Type() == js.TypeString {
			key = keyValue.String()
		}
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
			if !old.button.Equal(button) || !old.image.Equal(image) || old.thumbURL != thumb || old.displayURL != display {
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
	if record == nil || record.abort.Truthy() || record.objectURL != "" || record.failed {
		return
	}
	controller := js.Global().Get("AbortController").New()
	record.abort = controller
	url := record.thumbURL
	// C-2 live re-check: an <img> that never gets a src never fires "error",
	// so a request that stalls -- or resolves after this record was
	// discarded (see the guard just below) -- left the tile stuck looking
	// like it was still loading forever, with no fallback and nothing to
	// retry it. This bounds that: if the record is still unresolved once
	// the watchdog fires, it is treated as failed the same way an actual
	// error event would.
	var watchdogFn js.Func
	watchdogReleased := false
	releaseWatchdog := func() {
		if !watchdogReleased {
			watchdogReleased = true
			watchdogFn.Release()
		}
	}
	watchdogFn = js.FuncOf(func(js.Value, []js.Value) any {
		defer releaseWatchdog()
		if activeChatImageLoader == l && l.records[key] == record && record.abort.Equal(controller) && record.objectURL == "" && !record.failed {
			record.abort = js.Undefined()
			controller.Call("abort")
			record.failed = true
			markChatAttachmentImageFailed(button)
			l.armThumbnailRetry(record)
		}
		return nil
	})
	watchdog := js.Global().Call("setTimeout", watchdogFn, chatImageWatchdogMillis)
	startProtectedImageFetch(button, "thumbnail", url, controller.Get("signal"), func(objectURL string) {
		js.Global().Call("clearTimeout", watchdog)
		releaseWatchdog()
		if activeChatImageLoader != l || l.records[key] != record || !record.abort.Equal(controller) || !button.Get("isConnected").Truthy() || button.Get("dataset").Get("mediaThumb").String() != url {
			if objectURL != "" {
				revokeChatImageURL(objectURL)
			}
			return
		}
		record.abort = js.Undefined()
		if objectURL == "" {
			record.failed = true
			markChatAttachmentImageFailed(button)
			l.armThumbnailRetry(record)
			return
		}
		record.failed = false
		record.objectURL = objectURL
		// Fetching is already bounded by the near-viewport observer. Native lazy
		// loading would postpone decoding a retried image indefinitely while the
		// failure fallback hides its button with display:none.
		record.image.Set("loading", "eager")
		record.image.Set("decoding", "async")
		// Round 3 C-4: a fetched blob the browser cannot decode fails at the
		// <img>, after the watchdog above has been cleared. The declarative
		// OnError prop is not reliable for that (error does not bubble, and
		// a virtual row can remount the <img> under the reconciler), so the
		// loader listens on the element it just gave a src to and reaches
		// the same .failed state the watchdog and fetch-failure paths use.
		image := record.image
		if image.Get("addEventListener").Type() != js.TypeFunction {
			image.Set("src", objectURL)
			return
		}
		var decodeError, decoded js.Func
		settle := func() {
			image.Call("removeEventListener", "error", decodeError)
			image.Call("removeEventListener", "load", decoded)
			decodeError.Release()
			decoded.Release()
		}
		decodeError = js.FuncOf(func(js.Value, []js.Value) any {
			settle()
			if activeChatImageLoader == l && l.records[key] == record && image.Equal(record.image) {
				revokeChatImageURL(record.objectURL)
				record.objectURL = ""
				record.failed = true
				markChatAttachmentImageFailed(button)
				l.armThumbnailRetry(record)
			}
			return nil
		})
		decoded = js.FuncOf(func(js.Value, []js.Value) any {
			settle()
			if activeChatImageLoader == l && l.records[key] == record && image.Equal(record.image) {
				markChatAttachmentImageLoaded(button)
			}
			return nil
		})
		image.Call("addEventListener", "error", decodeError)
		image.Call("addEventListener", "load", decoded)
		image.Set("src", objectURL)
	})
}

// The failed style hides the image button, so observing the button itself
// cannot report a later viewport crossing. The surrounding figure remains
// visible as the fallback tile and is the stable retry target.
func (l *chatImageLoader) armThumbnailRetry(record *chatImageRecord) {
	if record.retryAttempted || !l.retryObserver.Truthy() {
		return
	}
	target := record.button
	if record.button.Get("closest").Type() == js.TypeFunction {
		if figure := record.button.Call("closest", ".attachment-image"); figure.Truthy() {
			target = figure
		}
	}
	record.retryTarget = target
	l.retryObserver.Call("observe", target)
}

func (l *chatImageLoader) retryThumbnail(button js.Value) {
	if l == nil || activeChatImageLoader != l || !button.Get("isConnected").Truthy() {
		return
	}
	key := button.Get("__chatImageRecordKey").String()
	record := l.records[key]
	if record == nil || !record.failed || record.retryAttempted {
		return
	}
	record.retryAttempted = true
	record.failed = false
	if l.retryObserver.Truthy() && record.retryTarget.Truthy() {
		l.retryObserver.Call("unobserve", record.retryTarget)
		record.retryTarget = js.Undefined()
	}
	l.loadThumbnail(button)
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
	if l.retryObserver.Truthy() && record.retryTarget.Truthy() {
		l.retryObserver.Call("unobserve", record.retryTarget)
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
	if l.retryObserver.Truthy() {
		l.retryObserver.Call("disconnect")
	}
	if l.mutations.Truthy() {
		l.mutations.Call("disconnect")
	}
	if l.intersection.Value.Truthy() {
		l.intersection.Release()
	}
	if l.retryIntersection.Value.Truthy() {
		l.retryIntersection.Release()
	}
	if l.change.Value.Truthy() {
		l.change.Release()
	}
	if activeChatImageLoader == l {
		activeChatImageLoader = nil
	}
}
