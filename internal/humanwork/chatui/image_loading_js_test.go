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

	constructor := js.Global().Get("Function").New("callback", "opts", "this.callback=callback;this.opts=opts;this.seen=[];this.observe=function(v){this.seen.push(v)};this.unobserve=function(){};this.disconnect=function(){};if(opts.rootMargin==='400px 0px')globalThis.__chatTestObserver=this")
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

func TestChatImageWarmPrefetchFailureRetriesOnceWhenVisible(t *testing.T) {
	globals := []string{"IntersectionObserver", "MutationObserver", "hcmChatMediaFetch", "__chatTestObservers"}
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

	constructor := js.Global().Get("Function").New("callback", "opts", `
		this.callback=callback; this.opts=opts; this.seen=[];
		this.observe=function(v){this.seen.push(v)};
		this.unobserve=function(){}; this.disconnect=function(){};
		globalThis.__chatTestObservers.push(this);
	`)
	mutationConstructor := js.Global().Get("Function").New("callback", "this.observe=function(){};this.disconnect=function(){}")
	js.Global().Set("__chatTestObservers", js.Global().Get("Array").New())
	js.Global().Set("IntersectionObserver", constructor)
	js.Global().Set("MutationObserver", mutationConstructor)

	fetchCalls := 0
	var fetchMock js.Func
	fetchMock = js.FuncOf(func(js.Value, []js.Value) any {
		fetchCalls++
		if fetchCalls == 1 {
			return js.Global().Get("Promise").Call("reject", "warm prefetch failed")
		}
		return js.Global().Get("Promise").Call("resolve", "blob:retry-image")
	})
	defer fetchMock.Release()
	js.Global().Set("hcmChatMediaFetch", fetchMock)

	button, image := js.Global().Get("Object").New(), js.Global().Get("Object").New()
	button.Set("isConnected", true)
	button.Set("dataset", js.ValueOf(map[string]any{
		"mediaId": "old-image", "mediaThumb": "thumbnail", "mediaDisplay": "display",
	}))
	queryImage := js.FuncOf(func(js.Value, []js.Value) any { return image })
	button.Set("querySelector", queryImage)
	defer queryImage.Release()
	removeImageSource := js.FuncOf(func(js.Value, []js.Value) any { return nil })
	image.Set("removeAttribute", removeImageSource)
	defer removeImageSource.Release()

	root := js.Global().Get("Object").New()
	root.Set("isConnected", true)
	root.Set("dataset", js.ValueOf(map[string]any{"selectedId": "room", "principal": "viewer"}))
	buttons := js.Global().Get("Array").New()
	buttons.Call("push", button)
	queryButtons := js.FuncOf(func(js.Value, []js.Value) any { return buttons })
	root.Set("querySelectorAll", queryButtons)
	defer queryButtons.Release()

	initChatImageLoading(root)
	observers := js.Global().Get("__chatTestObservers")
	if observers.Get("length").Int() != 2 || observers.Index(0).Get("opts").Get("rootMargin").String() != "400px 0px" || observers.Index(1).Get("opts").Get("rootMargin").String() != "0px" {
		t.Fatal("near-prefetch and viewport-retry observers were not installed")
	}
	entry := func() js.Value { return js.ValueOf(map[string]any{"target": button, "isIntersecting": true}) }
	entries := js.Global().Get("Array").New()
	entries.Call("push", entry())
	observers.Index(0).Get("callback").Invoke(entries)
	time.Sleep(25 * time.Millisecond)
	if fetchCalls != 1 || observers.Index(1).Get("seen").Get("length").Int() != 1 {
		t.Fatal("failed warm prefetch did not arm one viewport retry")
	}

	observers.Index(1).Get("callback").Invoke(entries)
	time.Sleep(25 * time.Millisecond)
	if fetchCalls != 2 || image.Get("src").String() != "blob:retry-image" {
		t.Fatal("visible image did not retry its failed warm prefetch")
	}
	observers.Index(1).Get("callback").Invoke(entries)
	if fetchCalls != 2 {
		t.Fatal("viewport retry was not bounded to one attempt")
	}
}

func TestChatImageSyncTracksEachButtonAndReplacedImage(t *testing.T) {
	globals := []string{"IntersectionObserver", "MutationObserver", "hcmChatMediaFetch", "__chatTestObservers"}
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

	constructor := js.Global().Get("Function").New("callback", "opts", `
		this.callback=callback; this.opts=opts; this.seen=[];
		this.observe=function(v){this.seen.push(v)};
		this.unobserve=function(){}; this.disconnect=function(){};
		globalThis.__chatTestObservers.push(this);
	`)
	mutationConstructor := js.Global().Get("Function").New("callback", "this.observe=function(){};this.disconnect=function(){}")
	js.Global().Set("__chatTestObservers", js.Global().Get("Array").New())
	js.Global().Set("IntersectionObserver", constructor)
	js.Global().Set("MutationObserver", mutationConstructor)

	fetchCalls := 0
	var fetchMock js.Func
	fetchMock = js.FuncOf(func(_ js.Value, args []js.Value) any {
		fetchCalls++
		return js.Global().Get("Promise").Call("resolve", "blob:"+args[0].String())
	})
	defer fetchMock.Release()
	js.Global().Set("hcmChatMediaFetch", fetchMock)

	makeImage := func() js.Value {
		image := js.Global().Get("Object").New()
		remove := js.FuncOf(func(js.Value, []js.Value) any { return nil })
		image.Set("removeAttribute", remove)
		t.Cleanup(remove.Release)
		t.Cleanup(remove.Release)
		return image
	}
	firstImage, secondImage := makeImage(), makeImage()
	images := []js.Value{firstImage, secondImage}
	buttons := make([]js.Value, 2)
	for i := range buttons {
		button := js.Global().Get("Object").New()
		button.Set("isConnected", true)
		button.Set("dataset", js.ValueOf(map[string]any{
			"mediaId": "image-" + strconv.Itoa(i+1), "mediaThumb": "thumbnail", "mediaDisplay": "display",
		}))
		index := i
		query := js.FuncOf(func(js.Value, []js.Value) any { return images[index] })
		button.Set("querySelector", query)
		t.Cleanup(query.Release)
		buttons[i] = button
	}

	root := js.Global().Get("Object").New()
	root.Set("isConnected", true)
	root.Set("dataset", js.ValueOf(map[string]any{"selectedId": "room", "principal": "viewer"}))
	domButtons := js.Global().Get("Array").New()
	for _, button := range buttons {
		domButtons.Call("push", button)
	}
	queryButtons := js.FuncOf(func(js.Value, []js.Value) any { return domButtons })
	root.Set("querySelectorAll", queryButtons)
	t.Cleanup(queryButtons.Release)

	initChatImageLoading(root)
	observer := js.Global().Get("__chatTestObservers").Index(0)
	if observer.Get("seen").Get("length").Int() != 2 || !observer.Get("seen").Index(0).Equal(buttons[0]) || !observer.Get("seen").Index(1).Equal(buttons[1]) {
		t.Fatal("each mounted attachment button was not independently observed")
	}
	if buttons[0].Get("__chatImageRecordKey").String() == buttons[1].Get("__chatImageRecordKey").String() {
		t.Fatal("attachment buttons shared an image record key")
	}

	newFirstImage := makeImage()
	images[0] = newFirstImage
	activeChatImageLoader.sync()
	if observer.Get("seen").Get("length").Int() != 3 || !observer.Get("seen").Index(2).Equal(buttons[0]) {
		t.Fatal("replacing a mounted button's img did not re-register that button")
	}

	entries := js.Global().Get("Array").New()
	entries.Call("push", js.ValueOf(map[string]any{"target": buttons[0], "isIntersecting": true}))
	entries.Call("push", js.ValueOf(map[string]any{"target": buttons[1], "isIntersecting": true}))
	observer.Get("callback").Invoke(entries)
	time.Sleep(25 * time.Millisecond)
	if fetchCalls != 2 || newFirstImage.Get("src").String() != "blob:image-1" || secondImage.Get("src").String() != "blob:image-2" {
		t.Fatal("visible current img children did not receive their own thumbnail URLs")
	}
}

func TestChatImageUsesTimelineScrollRootAndLoadsAfterScroll(t *testing.T) {
	globals := []string{"IntersectionObserver", "MutationObserver", "hcmChatMediaFetch", "__chatTestObservers"}
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

	constructor := js.Global().Get("Function").New("callback", "opts", `
		this.callback=callback; this.opts=opts; this.seen=[];
		this.observe=function(v){this.seen.push(v)};
		this.unobserve=function(){}; this.disconnect=function(){};
		globalThis.__chatTestObservers.push(this);
	`)
	mutationConstructor := js.Global().Get("Function").New("callback", "this.observe=function(){};this.disconnect=function(){}")
	js.Global().Set("__chatTestObservers", js.Global().Get("Array").New())
	js.Global().Set("IntersectionObserver", constructor)
	js.Global().Set("MutationObserver", mutationConstructor)

	fetchCalls := 0
	unexpectedRequest := ""
	var fetchMock js.Func
	fetchMock = js.FuncOf(func(_ js.Value, args []js.Value) any {
		fetchCalls++
		if args[0].String() != "older-gif" || args[1].String() != "thumbnail" {
			unexpectedRequest = args[0].String() + ":" + args[1].String()
		}
		return js.Global().Get("Promise").Call("resolve", "blob:older-gif-thumbnail")
	})
	defer fetchMock.Release()
	js.Global().Set("hcmChatMediaFetch", fetchMock)

	scrollRoot := js.Global().Get("Object").New()
	scrollRoot.Set("scrollTop", 0)
	button := js.Global().Get("Object").New()
	button.Set("isConnected", true)
	button.Set("dataset", js.ValueOf(map[string]any{
		"mediaId": "older-gif", "mediaThumb": "thumbnail", "mediaDisplay": "display",
	}))
	image := js.Global().Get("Object").New()
	image.Set("loading", "lazy")
	image.Set("src", "")
	removeImageSource := js.FuncOf(func(js.Value, []js.Value) any { return nil })
	image.Set("removeAttribute", removeImageSource)
	queryImage := js.FuncOf(func(js.Value, []js.Value) any { return image })
	button.Set("querySelector", queryImage)
	defer removeImageSource.Release()
	defer queryImage.Release()

	buttons := js.Global().Get("Array").New()
	buttons.Call("push", button)
	queryButtons := js.FuncOf(func(js.Value, []js.Value) any { return buttons })
	queryScroller := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && args[0].String() == "#chat-main" {
			return scrollRoot
		}
		return js.Null()
	})
	root := js.Global().Get("Object").New()
	root.Set("isConnected", true)
	root.Set("dataset", js.ValueOf(map[string]any{"selectedId": "room", "principal": "viewer"}))
	root.Set("querySelectorAll", queryButtons)
	root.Set("querySelector", queryScroller)
	defer queryButtons.Release()
	defer queryScroller.Release()

	initChatImageLoading(root)
	observers := js.Global().Get("__chatTestObservers")
	observer := observers.Index(0)
	if !observer.Get("opts").Get("root").Equal(scrollRoot) {
		t.Fatal("older timeline images were observed against the page viewport instead of #chat-main")
	}
	entries := js.Global().Get("Array").New()
	entries.Call("push", js.ValueOf(map[string]any{"target": button, "isIntersecting": false}))
	observer.Get("callback").Invoke(entries)
	if fetchCalls != 0 || image.Get("src").String() != "" {
		t.Fatal("offscreen older GIF started loading before the timeline scroll")
	}

	// Simulate scrolling #chat-main until its older message intersects the
	// observer root. The mock delivers the transition that a nested scrollport
	// produces, and the actual loader must assign a usable object URL.
	scrollRoot.Set("scrollTop", 1200)
	entries = js.Global().Get("Array").New()
	entries.Call("push", js.ValueOf(map[string]any{"target": button, "isIntersecting": true}))
	observer.Get("callback").Invoke(entries)
	time.Sleep(25 * time.Millisecond)
	if fetchCalls != 1 || unexpectedRequest != "" || image.Get("src").String() != "blob:older-gif-thumbnail" {
		t.Fatal("visible older GIF did not receive its protected thumbnail source after scrolling")
	}
}

func TestStartChatImageLoadingRegistersCommittedWorkspaceImages(t *testing.T) {
	globals := []string{"document", "IntersectionObserver", "MutationObserver", "__chatTestObservers"}
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

	constructor := js.Global().Get("Function").New("callback", "opts", `
		this.callback=callback; this.opts=opts; this.seen=[];
		this.observe=function(v){this.seen.push(v)};
		this.unobserve=function(){}; this.disconnect=function(){};
		globalThis.__chatTestObservers.push(this);
	`)
	mutationConstructor := js.Global().Get("Function").New("callback", "this.observe=function(){};this.disconnect=function(){}")
	js.Global().Set("__chatTestObservers", js.Global().Get("Array").New())
	js.Global().Set("IntersectionObserver", constructor)
	js.Global().Set("MutationObserver", mutationConstructor)

	button := js.Global().Get("Object").New()
	button.Set("isConnected", true)
	button.Set("dataset", js.ValueOf(map[string]any{"mediaId": "mounted-gif", "mediaThumb": "thumbnail", "mediaDisplay": "display"}))
	image := js.Global().Get("Object").New()
	removeImageSource := js.FuncOf(func(js.Value, []js.Value) any { return nil })
	queryImage := js.FuncOf(func(js.Value, []js.Value) any { return image })
	button.Set("querySelector", queryImage)
	image.Set("removeAttribute", removeImageSource)
	defer removeImageSource.Release()
	defer queryImage.Release()

	buttons := js.Global().Get("Array").New()
	buttons.Call("push", button)
	queryButtons := js.FuncOf(func(js.Value, []js.Value) any { return buttons })
	scrollRoot := js.Global().Get("Object").New()
	queryScroller := js.FuncOf(func(js.Value, []js.Value) any { return scrollRoot })
	workspace := js.Global().Get("Object").New()
	workspace.Set("isConnected", true)
	workspace.Set("dataset", js.ValueOf(map[string]any{"selectedId": "general", "principal": "viewer"}))
	workspace.Set("querySelectorAll", queryButtons)
	workspace.Set("querySelector", queryScroller)
	defer queryButtons.Release()
	defer queryScroller.Release()

	document := js.Global().Get("Object").New()
	findWorkspace := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && args[0].String() == ".chat-workspace" {
			return workspace
		}
		return js.Null()
	})
	document.Set("querySelector", findWorkspace)
	defer findWorkspace.Release()
	js.Global().Set("document", document)

	cleanup := startChatImageLoading()
	defer cleanup()
	if button.Get("__chatImageRecordKey").Type() != js.TypeString || button.Get("__chatImageRecordKey").String() == "" {
		t.Fatal("mounted workspace image was not registered by startChatImageLoading")
	}
	observers := js.Global().Get("__chatTestObservers")
	if observers.Get("length").Int() != 2 || observers.Index(0).Get("seen").Get("length").Int() != 1 || !observers.Index(0).Get("seen").Index(0).Equal(button) {
		t.Fatal("committed workspace image was not attached to the timeline observer")
	}
}

func TestChatImageLoaderRebindsWhenWorkspaceNodeIsReplaced(t *testing.T) {
	globals := []string{"document", "IntersectionObserver", "MutationObserver", "hcmChatMediaFetch", "__chatTestObservers", "__chatTestMutation"}
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

	observerConstructor := js.Global().Get("Function").New("callback", "opts", `
		this.callback=callback; this.opts=opts; this.seen=[];
		this.observe=function(v){this.seen.push(v)};
		this.unobserve=function(){}; this.disconnect=function(){};
		globalThis.__chatTestObservers.push(this);
	`)
	mutationConstructor := js.Global().Get("Function").New("callback", `
		this.callback=callback;
		this.observe=function(target){this.target=target};
		this.disconnect=function(){};
		globalThis.__chatTestMutation=this;
	`)
	js.Global().Set("__chatTestObservers", js.Global().Get("Array").New())
	js.Global().Set("IntersectionObserver", observerConstructor)
	js.Global().Set("MutationObserver", mutationConstructor)
	fetchMock := js.FuncOf(func(js.Value, []js.Value) any {
		return js.Global().Get("Promise").Call("resolve", "blob:replacement-gif")
	})
	defer fetchMock.Release()
	js.Global().Set("hcmChatMediaFetch", fetchMock)

	makeWorkspace := func(id string) (js.Value, js.Value, js.Value, js.Value, js.Func, js.Func, js.Func) {
		button, image := js.Global().Get("Object").New(), js.Global().Get("Object").New()
		scrollRoot := js.Global().Get("Object").New()
		button.Set("isConnected", true)
		button.Set("dataset", js.ValueOf(map[string]any{"mediaId": id, "mediaThumb": "thumbnail", "mediaDisplay": "display"}))
		remove := js.FuncOf(func(js.Value, []js.Value) any { return nil })
		image.Set("removeAttribute", remove)
		queryImage := js.FuncOf(func(js.Value, []js.Value) any { return image })
		button.Set("querySelector", queryImage)
		buttons := js.Global().Get("Array").New()
		buttons.Call("push", button)
		queryButtons := js.FuncOf(func(js.Value, []js.Value) any { return buttons })
		root := js.Global().Get("Object").New()
		root.Set("isConnected", true)
		root.Set("dataset", js.ValueOf(map[string]any{"selectedId": "general", "principal": "viewer"}))
		root.Set("querySelectorAll", queryButtons)
		queryScroller := js.FuncOf(func(_ js.Value, args []js.Value) any {
			if len(args) > 0 && args[0].String() == "#chat-main" {
				return scrollRoot
			}
			return js.Null()
		})
		root.Set("querySelector", queryScroller)
		return root, button, image, scrollRoot, queryButtons, queryImage, queryScroller
	}
	oldRoot, _, _, oldScrollRoot, oldButtons, oldImage, oldScroller := makeWorkspace("previous-tree")
	newRoot, newButton, newImage, newScrollRoot, newButtons, newImageQuery, newScroller := makeWorkspace("current-tree")
	defer oldButtons.Release()
	defer oldImage.Release()
	defer oldScroller.Release()
	defer newButtons.Release()
	defer newImageQuery.Release()
	defer newScroller.Release()

	body := js.Global().Get("Object").New()
	currentRoot := oldRoot
	document := js.Global().Get("Object").New()
	findWorkspace := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && args[0].String() == ".chat-workspace" {
			return currentRoot
		}
		return js.Null()
	})
	document.Set("querySelector", findWorkspace)
	document.Set("body", body)
	defer findWorkspace.Release()
	js.Global().Set("document", document)

	initChatImageLoading(oldRoot)
	mutation := js.Global().Get("__chatTestMutation")
	if !mutation.Get("target").Equal(body) {
		t.Fatal("workspace mutations were observed on the replaceable node instead of the stable document body")
	}
	observers := js.Global().Get("__chatTestObservers")
	if !observers.Index(0).Get("opts").Get("root").Equal(oldScrollRoot) {
		t.Fatal("initial image observer did not use the initial timeline scroll root")
	}
	oldRoot.Set("isConnected", false)
	currentRoot = newRoot
	mutation.Get("callback").Invoke(js.Global().Get("Array").New())

	if observers.Get("length").Int() != 4 || !observers.Index(2).Get("opts").Get("root").Equal(newScrollRoot) ||
		!observers.Index(3).Get("opts").Get("root").Equal(newScrollRoot) {
		t.Fatal("replacement workspace did not receive fresh observers rooted at its current timeline")
	}
	observer := observers.Index(2)
	if !activeChatImageLoader.root.Equal(newRoot) || newButton.Get("__chatImageRecordKey").Type() != js.TypeString ||
		observer.Get("seen").Get("length").Int() != 1 || !observer.Get("seen").Index(0).Equal(newButton) {
		t.Fatal("loader remained bound to the detached workspace after the live tree was replaced")
	}
	entries := js.Global().Get("Array").New()
	entries.Call("push", js.ValueOf(map[string]any{"target": newButton, "isIntersecting": true}))
	observer.Get("callback").Invoke(entries)
	time.Sleep(25 * time.Millisecond)
	if newImage.Get("src").String() != "blob:replacement-gif" {
		t.Fatal("visible GIF in the replacement workspace never received a thumbnail source")
	}
}
