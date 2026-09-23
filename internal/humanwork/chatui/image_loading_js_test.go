//go:build js && wasm

package chatui

import (
	"strconv"
	"syscall/js"
	"testing"
	"time"
)

func TestChatImageLoadsNearViewportAndDisplayCanBeAborted(t *testing.T) {
	globals := []string{"IntersectionObserver", "MutationObserver", "fetch", "__chatTestObserver"}
	old := make(map[string]js.Value, len(globals))
	for _, key := range globals {
		old[key] = js.Global().Get(key)
	}
	defer func() {
		if activeChatImageLoader != nil {
			activeChatImageLoader.close()
		}
		for _, key := range globals {
			js.Global().Set(key, old[key])
		}
	}()

	constructor := js.Global().Get("Function").New("callback", "opts", "this.callback=callback;this.opts=opts;this.seen=[];this.observe=function(v){this.seen.push(v)};this.unobserve=function(){};this.disconnect=function(){};globalThis.__chatTestObserver=this")
	mutationConstructor := js.Global().Get("Function").New("callback", "this.observe=function(){};this.disconnect=function(){}")
	js.Global().Set("IntersectionObserver", constructor)
	js.Global().Set("MutationObserver", mutationConstructor)

	var fetchCalls int
	var fetchURL string
	var fetchOptions js.Value
	succeedNext := false
	var blobFunc js.Func
	fetchMock := js.FuncOf(func(_ js.Value, args []js.Value) any {
		fetchCalls++
		fetchURL = args[0].String()
		fetchOptions = args[1]
		if succeedNext {
			succeedNext = false
			if blobFunc.Value.Truthy() {
				blobFunc.Release()
			}
			response := js.Global().Get("Object").New()
			response.Set("ok", true)
			blobFunc = js.FuncOf(func(js.Value, []js.Value) any {
				blob := js.Global().Get("Blob").New(js.Global().Get("Array").New("image-data"))
				return js.Global().Get("Promise").Call("resolve", blob)
			})
			response.Set("blob", blobFunc)
			return js.Global().Get("Promise").Call("resolve", response)
		}
		return js.Global().Get("Promise").Call("reject")
	})
	defer fetchMock.Release()
	defer func() {
		if blobFunc.Value.Truthy() {
			blobFunc.Release()
		}
	}()
	js.Global().Set("fetch", fetchMock)

	button := js.Global().Get("Object").New()
	button.Set("isConnected", true)
	button.Set("dataset", js.ValueOf(map[string]any{
		"mediaId": "artifact", "mediaThumb": "/v1/chat/media/artifact?grant=grant-a&variant=thumbnail",
		"mediaDisplay": "/v1/chat/media/artifact?grant=grant-a&variant=display",
	}))
	image := js.Global().Get("Object").New()
	removeAttribute := js.FuncOf(func(js.Value, []js.Value) any { return nil })
	buttonQuery := js.FuncOf(func(js.Value, []js.Value) any { return image })
	button.Set("querySelector", buttonQuery)
	image.Set("removeAttribute", removeAttribute)
	defer buttonQuery.Release()
	defer removeAttribute.Release()

	root := js.Global().Get("Object").New()
	root.Set("isConnected", true)
	root.Set("dataset", js.ValueOf(map[string]any{"selectedId": "room", "principal": "viewer"}))
	buttons := js.Global().Get("Array").New()
	buttons.Call("push", button)
	rootQuery := js.FuncOf(func(js.Value, []js.Value) any { return buttons })
	root.Set("querySelectorAll", rootQuery)
	defer rootQuery.Release()

	initChatImageLoading(root)
	observer := js.Global().Get("__chatTestObserver")
	if observer.Get("opts").Get("rootMargin").String() != "400px 0px" || observer.Get("seen").Get("length").Int() != 1 {
		t.Fatal("thumbnail was not registered with the near-viewport observer")
	}
	entry := func(intersecting bool) js.Value {
		return js.ValueOf(map[string]any{"target": button, "isIntersecting": intersecting})
	}
	entries := js.Global().Get("Array").New()
	entries.Call("push", entry(false))
	observer.Get("callback").Invoke(entries)
	if fetchCalls != 0 {
		t.Fatal("offscreen thumbnail started a request")
	}
	entries = js.Global().Get("Array").New()
	entries.Call("push", entry(true))
	observer.Get("callback").Invoke(entries)
	if fetchCalls != 1 || fetchURL != button.Get("dataset").Get("mediaThumb").String() ||
		fetchOptions.Get("credentials").String() != "same-origin" || fetchOptions.Get("cache").String() != "no-store" {
		t.Fatal("near-viewport thumbnail did not use its protected no-store rendition URL")
	}

	workspace := js.Global().Get("Object").New()
	workspace.Set("isConnected", true)
	workspace.Set("dataset", root.Get("dataset"))
	closest := js.FuncOf(func(js.Value, []js.Value) any { return workspace })
	button.Set("closest", closest)
	defer closest.Release()
	classList := js.Global().Get("Object").New()
	removeClass := js.FuncOf(func(js.Value, []js.Value) any { return nil })
	classList.Set("remove", removeClass)
	fullImage := js.Global().Get("Object").New()
	fullImage.Set("classList", classList)
	fullImage.Set("removeAttribute", removeAttribute)
	defer removeClass.Release()
	addEventListener := js.FuncOf(func(js.Value, []js.Value) any { return nil })
	removeEventListener := js.FuncOf(func(js.Value, []js.Value) any { return nil })
	fullImage.Set("addEventListener", addEventListener)
	fullImage.Set("removeEventListener", removeEventListener)
	defer addEventListener.Release()
	defer removeEventListener.Release()
	readyCalls := 0
	cleanup := loadChatImageDisplay(button, fullImage, func(bool) { readyCalls++ })
	if fetchCalls != 2 || fetchURL != button.Get("dataset").Get("mediaDisplay").String() {
		t.Fatal("explicit viewer open did not request the display rendition")
	}
	displaySignal := fetchOptions.Get("signal")
	cleanup()
	if !displaySignal.Get("aborted").Bool() {
		t.Fatal("closing the viewer did not abort the display request")
	}

	listeners := js.Global().Get("Object").New()
	addClass := js.FuncOf(func(_ js.Value, args []js.Value) any {
		classList.Set(args[0].String(), true)
		return nil
	})
	classList.Set("add", addClass)
	defer addClass.Release()
	removeClassAgain := js.FuncOf(func(_ js.Value, args []js.Value) any {
		classList.Set(args[0].String(), false)
		return nil
	})
	classList.Set("remove", removeClassAgain)
	addListener := js.FuncOf(func(_ js.Value, args []js.Value) any {
		listeners.Set(args[0].String(), args[1])
		return nil
	})
	removeListener := js.FuncOf(func(_ js.Value, args []js.Value) any {
		listeners.Set(args[0].String(), js.Undefined())
		return nil
	})
	fullImage.Set("addEventListener", addListener)
	fullImage.Set("removeEventListener", removeListener)
	defer removeClassAgain.Release()
	defer addListener.Release()
	defer removeListener.Release()
	setter := js.FuncOf(func(js.Value, []js.Value) any {
		if load := listeners.Get("load"); load.Truthy() {
			load.Invoke()
		}
		return nil
	})
	js.Global().Get("Object").Call("defineProperty", fullImage, "src", js.ValueOf(map[string]any{"set": setter, "configurable": true}))
	defer setter.Release()
	succeedNext = true
	ready := make(chan bool, 1)
	cleanup = loadChatImageDisplay(button, fullImage, func(ok bool) { ready <- ok })
	select {
	case ok := <-ready:
		if !ok || !classList.Get("chat-image-display-ready").Bool() {
			t.Fatal("display rendition was not revealed after its image loaded")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("display rendition did not finish decoding")
	}
	if fetchCalls != 3 || fetchURL != button.Get("dataset").Get("mediaDisplay").String() {
		t.Fatal("viewer upgrade did not use the protected display rendition")
	}
	button.Get("dataset").Set("mediaOriginal", "/v1/chat/media/artifact?grant=grant-a")
	button.Get("dataset").Set("mediaWidth", "4032")
	button.Get("dataset").Set("mediaHeight", "3024")
	button.Get("dataset").Set("mediaBytes", strconv.FormatInt(8<<20, 10))
	downloadOnly := false
	var originalImage js.Value
	var originalClassList js.Value
	var originalReady js.Value
	newOriginalImage := func() js.Value {
		originalImage = js.Global().Get("Object").New()
		originalClassList = js.Global().Get("Object").New()
		originalImage.Set("classList", originalClassList)
		originalImage.Set("removeAttribute", removeAttribute)
		originalReady = js.Global().Get("Object").New()
		originalImage.Set("__listeners", originalReady)
		add := js.FuncOf(func(_ js.Value, args []js.Value) any {
			originalReady.Set(args[0].String(), args[1])
			return nil
		})
		remove := js.FuncOf(func(_ js.Value, args []js.Value) any {
			originalReady.Set(args[0].String(), js.Undefined())
			return nil
		})
		addClassOriginal := js.FuncOf(func(_ js.Value, args []js.Value) any {
			originalClassList.Set(args[0].String(), true)
			return nil
		})
		removeClassOriginal := js.FuncOf(func(_ js.Value, args []js.Value) any {
			originalClassList.Set(args[0].String(), false)
			return nil
		})
		setSource := js.FuncOf(func(js.Value, []js.Value) any {
			if load := originalReady.Get("load"); load.Truthy() {
				load.Invoke()
			}
			return nil
		})
		originalImage.Set("addEventListener", add)
		originalImage.Set("removeEventListener", remove)
		originalClassList.Set("add", addClassOriginal)
		originalClassList.Set("remove", removeClassOriginal)
		js.Global().Get("Object").Call("defineProperty", originalImage, "src", js.ValueOf(map[string]any{"set": setSource, "configurable": true}))
		t.Cleanup(func() {
			add.Release()
			remove.Release()
			addClassOriginal.Release()
			removeClassOriginal.Release()
			setSource.Release()
		})
		return originalImage
	}
	originalImage = newOriginalImage()
	fetchesBeforeBudgetCheck := fetchCalls
	button.Get("dataset").Set("mediaAnimated", "true")
	animatedDownload := false
	animatedCleanup := loadChatImageOriginal(button, originalImage, func(loaded, download bool) {
		animatedDownload = !loaded && download
	})
	animatedCleanup()
	if !animatedDownload || fetchCalls != fetchesBeforeBudgetCheck {
		t.Fatal("animated display original was fetched a second time")
	}
	button.Get("dataset").Set("mediaAnimated", "false")
	tooLargeCleanup := loadChatImageOriginal(button, originalImage, func(loaded, download bool) {
		downloadOnly = !loaded && download
	})
	tooLargeCleanup()
	if !downloadOnly || fetchCalls != fetchesBeforeBudgetCheck {
		t.Fatal("over-budget original was fetched instead of exposing its download option")
	}
	button.Get("dataset").Set("mediaWidth", "2400")
	button.Get("dataset").Set("mediaHeight", "1600")
	button.Get("dataset").Set("mediaBytes", strconv.FormatInt(2<<20, 10))
	succeedNext = true
	originalImage = newOriginalImage()
	originalDone := make(chan bool, 1)
	originalCleanup := loadChatImageOriginal(button, originalImage, func(loaded, download bool) {
		originalDone <- loaded && !download
	})
	select {
	case loaded := <-originalDone:
		if !loaded || !originalClassList.Get("chat-image-original-ready").Bool() {
			t.Fatal("byte-exact original was not revealed after decoding")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("original image did not finish decoding")
	}
	if fetchCalls != fetchesBeforeBudgetCheck+1 || fetchURL != button.Get("dataset").Get("mediaOriginal").String() {
		t.Fatal("viewer original fetch did not use the plain protected GET URL")
	}
	originalCleanup()
	cleanup()
}
