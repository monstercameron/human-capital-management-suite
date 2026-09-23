//go:build js && wasm

package chatui

import (
	"strconv"
	"strings"
	"syscall/js"
	"testing"
	"time"
)

func TestGiphyPostEmbedLoadsOnlyNearViewport(t *testing.T) {
	globalNames := []string{"IntersectionObserver", "__giphyPostEmbedObserver"}
	previous := make(map[string]js.Value, len(globalNames))
	for _, name := range globalNames {
		previous[name] = js.Global().Get(name)
	}
	defer func() {
		for _, name := range globalNames {
			js.Global().Set(name, previous[name])
		}
	}()

	constructor := js.Global().Get("Function").New("callback", "opts", "this.callback=callback;this.opts=opts;this.seen=[];this.unseen=[];this.observe=function(v){this.seen.push(v)};this.unobserve=function(v){this.unseen.push(v)};this.disconnect=function(){};globalThis.__giphyPostEmbedObserver=this")
	js.Global().Set("IntersectionObserver", constructor)

	mediaURL := "https://media.giphy.com/media/abc123/giphy.gif?cid=approved"
	image := js.Global().Get("Object").New()
	image.Set("dataURL", mediaURL)
	getAttribute := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 1 && args[0].String() == "data-giphy-embed-src" {
			return image.Get("dataURL")
		}
		return js.Null()
	})
	removeAttribute := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 1 && args[0].String() == "data-giphy-embed-src" {
			image.Set("dataURL", js.Null())
		}
		return nil
	})
	image.Set("getAttribute", getAttribute)
	image.Set("removeAttribute", removeAttribute)
	defer getAttribute.Release()
	defer removeAttribute.Release()

	images := js.Global().Get("Array").New()
	images.Call("push", image)
	root := js.Global().Get("Object").New()
	querySelectorAll := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) != 1 || args[0].String() != giphyPostEmbedSelector {
			t.Errorf("unexpected query selector: %v", args)
		}
		return images
	})
	root.Set("querySelectorAll", querySelectorAll)
	defer querySelectorAll.Release()

	cleanup := observeGiphyPostEmbeds(root)
	defer cleanup()
	observer := js.Global().Get("__giphyPostEmbedObserver")
	if observer.Get("opts").Get("rootMargin").String() != "400px 0px" || observer.Get("seen").Get("length").Int() != 1 {
		t.Fatal("posted GIF image was not registered with the near-viewport observer")
	}
	if got := image.Get("src"); got.Type() != js.TypeUndefined && got.Type() != js.TypeNull {
		t.Fatalf("offscreen posted GIF loaded early: %q", got.String())
	}

	entry := js.ValueOf(map[string]any{"target": image, "isIntersecting": false, "intersectionRatio": 0})
	entries := js.Global().Get("Array").New()
	entries.Call("push", entry)
	observer.Get("callback").Invoke(entries)
	if image.Get("src").Truthy() {
		t.Fatal("non-intersecting posted GIF started loading")
	}

	entry = js.ValueOf(map[string]any{"target": image, "isIntersecting": true, "intersectionRatio": 0.1})
	entries = js.Global().Get("Array").New()
	entries.Call("push", entry)
	observer.Get("callback").Invoke(entries)
	if image.Get("src").String() != mediaURL || image.Get("loading").String() != "lazy" || image.Get("decoding").String() != "async" {
		t.Fatal("near-viewport posted GIF did not use the direct provider URL with lazy async decoding")
	}
	if !image.Get("dataURL").IsNull() || observer.Get("unseen").Get("length").Int() != 1 {
		t.Fatal("loaded GIF URL was retained in data attributes or remained observed")
	}
}

func TestGiphyPostEmbedRejectsUntrustedMediaURL(t *testing.T) {
	image := js.Global().Get("Object").New()
	image.Set("dataURL", "https://evil.example/giphy.gif")
	getAttribute := js.FuncOf(func(js.Value, []js.Value) any { return image.Get("dataURL") })
	removeAttribute := js.FuncOf(func(js.Value, []js.Value) any { image.Set("dataURL", js.Null()); return nil })
	image.Set("getAttribute", getAttribute)
	image.Set("removeAttribute", removeAttribute)
	defer getAttribute.Release()
	defer removeAttribute.Release()
	if giphyLoadPostEmbed(image) {
		t.Fatal("untrusted host was accepted as a GIF rendition")
	}
	if image.Get("src").Truthy() {
		t.Fatal("untrusted GIF media was assigned to src")
	}
}

func TestGiphyPostViewFencesRoomAndPrincipalAndIgnoresOwnCardMutation(t *testing.T) {
	previous := activeGiphyPostView
	defer func() { activeGiphyPostView = previous }()
	root := js.Global().Get("Object").New()
	root.Set("isConnected", true)
	root.Set("dataset", js.ValueOf(map[string]any{"selectedId": "room-a", "principal": "person-a"}))
	view := &giphyPostView{root: root, room: "room-a", principal: "person-a"}
	activeGiphyPostView = view
	if !view.current() {
		t.Fatal("current room/principal failed lifecycle fence")
	}
	root.Get("dataset").Set("selectedId", "room-b")
	if view.current() {
		t.Fatal("view remained current after selected room changed")
	}
	root.Get("dataset").Set("selectedId", "room-a")
	root.Get("dataset").Set("principal", "person-b")
	if view.current() {
		t.Fatal("view remained current after authenticated principal changed")
	}
	root.Get("dataset").Set("principal", "person-a")

	classList := js.Global().Get("Object").New()
	contains := js.FuncOf(func(_ js.Value, args []js.Value) any { return len(args) == 1 && args[0].String() == "giphy-post-embed" })
	classList.Set("contains", contains)
	defer contains.Release()
	card := js.Global().Get("Object").New()
	card.Set("classList", classList)
	nodes := js.Global().Get("Array").New()
	nodes.Call("push", card)
	record := js.ValueOf(map[string]any{"type": "childList", "addedNodes": nodes, "removedNodes": js.Global().Get("Array").New()})
	records := js.Global().Get("Array").New()
	records.Call("push", record)
	if !giphyOnlyEmbedMutations(records) {
		t.Fatal("the manager did not recognize its own card append mutation")
	}
	textNode := js.Global().Get("Object").New()
	textNode.Set("classList", js.Null())
	nodes = js.Global().Get("Array").New()
	nodes.Call("push", textNode)
	record = js.ValueOf(map[string]any{"type": "childList", "addedNodes": nodes, "removedNodes": js.Global().Get("Array").New()})
	records = js.Global().Get("Array").New()
	records.Call("push", record)
	if giphyOnlyEmbedMutations(records) {
		t.Fatal("ordinary DOM changes were mistaken for manager-owned card mutations")
	}
}

func TestGiphyPostResolveUsesBoundedDirectIDLookup(t *testing.T) {
	previousFetch := js.Global().Get("fetch")
	defer js.Global().Set("fetch", previousFetch)
	var requestURL string
	var requestOptions js.Value
	var jsonFunc js.Func
	fetch := js.FuncOf(func(_ js.Value, args []js.Value) any {
		requestURL = args[0].String()
		requestOptions = args[1]
		payload := js.ValueOf(map[string]any{
			"data": []any{map[string]any{
				"id": "abc123", "url": "https://giphy.com/gifs/wave-abc123", "rating": "g",
				"images": map[string]any{
					"fixed_width_small":       map[string]any{"url": "https://media.giphy.com/media/abc123/100w.gif"},
					"fixed_width_small_still": map[string]any{"url": "https://media.giphy.com/media/abc123/100w_still.jpg"},
					"original":                map[string]any{"url": "https://media.giphy.com/media/abc123/giphy.gif?cid=api"},
				},
			}},
			"pagination": map[string]any{"count": 1, "total_count": 1},
		})
		response := js.Global().Get("Object").New()
		response.Set("ok", true)
		jsonFunc = js.FuncOf(func(js.Value, []js.Value) any { return js.Global().Get("Promise").Call("resolve", payload) })
		response.Set("json", jsonFunc)
		return js.Global().Get("Promise").Call("resolve", response)
	})
	defer fetch.Release()
	defer func() {
		if jsonFunc.Value.Truthy() {
			jsonFunc.Release()
		}
	}()
	js.Global().Set("fetch", fetch)

	resolved := make(chan []GiphyResult, 1)
	picker := NewGiphyPicker(GiphyPickerConfig{APIKey: "test-key"}, GiphyPickerCallbacks{
		OnResults: func(results []GiphyResult, appendPage, hasMore bool) {
			if !appendPage && !hasMore {
				resolved <- results
			}
		},
	})
	ids := []string{"abc123", "abc123"}
	for i := 0; i < GiphyPageSize+2; i++ {
		ids = append(ids, "gifid"+string(rune('a'+i)))
	}
	picker.Resolve(ids)
	defer picker.Close()
	params := js.Global().Get("URLSearchParams").New(strings.SplitN(requestURL, "?", 2)[1])
	limit, _ := strconv.Atoi(params.Call("get", "limit").String())
	if !strings.HasPrefix(requestURL, giphyAPIBase+"?") || params.Call("get", "ids").String() == "" ||
		params.Call("get", "rating").String() != GiphyRating || limit != GiphyPageSize ||
		requestOptions.Get("cache").String() != "no-store" {
		t.Fatalf("Resolve request was not a bounded direct no-store ID lookup: url=%q options=%v", requestURL, requestOptions)
	}
	requestedIDs := strings.Split(params.Call("get", "ids").String(), ",")
	if len(requestedIDs) != GiphyPageSize || requestedIDs[0] != "abc123" || requestedIDs[1] != "gifida" {
		t.Fatalf("Resolve IDs were not deduplicated and capped: %v", requestedIDs)
	}
	select {
	case results := <-resolved:
		if len(results) != 1 || results[0].ID != "abc123" || results[0].EmbedURL != "https://media.giphy.com/media/abc123/giphy.gif?cid=api" || results[0].Rating != GiphyRating {
			t.Fatalf("Resolve returned unexpected validated embed metadata: %#v", results)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Resolve did not return the API's original animated media URL")
	}
}

func TestGiphyPostViewReattachesAfterBodyReplacementWithoutRefetch(t *testing.T) {
	previousDocument, previousFetch, previousObserver, previousView := js.Global().Get("document"), js.Global().Get("fetch"), js.Global().Get("IntersectionObserver"), activeGiphyPostView
	defer func() {
		js.Global().Set("document", previousDocument)
		js.Global().Set("fetch", previousFetch)
		js.Global().Set("IntersectionObserver", previousObserver)
		activeGiphyPostView = previousView
	}()
	created := 0
	document := js.Global().Get("Object").New()
	createElement := js.FuncOf(func(_ js.Value, args []js.Value) any {
		created++
		return newGiphyEmbedTestElement(args[0].String())
	})
	defer createElement.Release()
	document.Set("createElement", createElement)
	js.Global().Set("document", document)
	js.Global().Set("IntersectionObserver", js.Undefined())

	var fetchCalls int
	var jsonFunc js.Func
	fetch := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		fetchCalls++
		payload := js.ValueOf(map[string]any{
			"data": []any{map[string]any{
				"id": "abc123", "url": "https://giphy.com/gifs/wave-abc123", "rating": "g",
				"images": map[string]any{
					"fixed_width_small":       map[string]any{"url": "https://media.giphy.com/media/abc123/100w.gif"},
					"fixed_width_small_still": map[string]any{"url": "https://media.giphy.com/media/abc123/100w_still.jpg"},
					"original":                map[string]any{"url": "https://media.giphy.com/media/abc123/giphy.gif"},
				},
			}},
			"pagination": map[string]any{"count": 1, "total_count": 1},
		})
		response := js.Global().Get("Object").New()
		response.Set("ok", true)
		jsonFunc = js.FuncOf(func(js.Value, []js.Value) any { return js.Global().Get("Promise").Call("resolve", payload) })
		response.Set("json", jsonFunc)
		return js.Global().Get("Promise").Call("resolve", response)
	})
	defer fetch.Release()
	defer func() {
		if jsonFunc.Value.Truthy() {
			jsonFunc.Release()
		}
	}()
	js.Global().Set("fetch", fetch)

	firstBody := newGiphyEmbedTestBody("https://giphy.com/gifs/wave-abc123")
	root := newGiphyEmbedTestRoot(firstBody)
	view := &giphyPostView{root: root, room: "room-a", principal: "person-a", key: "test-key", results: map[string]GiphyResult{}, desired: map[string]bool{}}
	activeGiphyPostView = view
	resolved := make(chan struct{}, 1)
	view.picker = NewGiphyPicker(GiphyPickerConfig{APIKey: view.key}, GiphyPickerCallbacks{
		OnResults: func(items []GiphyResult, _, _ bool) {
			if !view.current() {
				return
			}
			for _, item := range items {
				if view.desired[item.ID] {
					view.results[item.ID] = item
				}
			}
			view.decorate()
			select {
			case resolved <- struct{}{}:
			default:
			}
		},
	})
	defer view.close()
	view.scan()
	select {
	case <-resolved:
	case <-time.After(2 * time.Second):
		t.Fatal("first visible GIPHY link did not resolve")
	}
	if fetchCalls != 1 || fakeGiphyCards(firstBody).Length() != 1 {
		t.Fatalf("initial resolution produced fetches=%d cards=%d", fetchCalls, fakeGiphyCards(firstBody).Length())
	}

	// Simulate GoWebComponents replacing a message-body subtree after an
	// unrelated chat render. The visible ID set is unchanged, so the cached
	// transient result reattaches the card without another provider request.
	secondBody := newGiphyEmbedTestBody("https://giphy.com/gifs/wave-abc123")
	root.Set("bodies", js.Global().Get("Array").New(secondBody))
	view.scan()
	if fetchCalls != 1 || fakeGiphyCards(secondBody).Length() != 1 {
		t.Fatalf("replacement body did not reuse current-window result: fetches=%d cards=%d", fetchCalls, fakeGiphyCards(secondBody).Length())
	}
	view.scan()
	if created != 6 || fakeGiphyCards(secondBody).Length() != 1 {
		t.Fatalf("routine rescan duplicated cards or churned DOM nodes: created=%d cards=%d", created, fakeGiphyCards(secondBody).Length())
	}
}

func newGiphyEmbedTestElement(tag string) js.Value {
	element := js.Global().Get("Object").New()
	element.Set("tag", tag)
	element.Set("attributes", js.Global().Get("Object").New())
	element.Set("children", js.Global().Get("Array").New())
	element.Set("dataset", js.Global().Get("Object").New())
	getAttribute := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) != 1 {
			return js.Null()
		}
		value := element.Get("attributes").Get(args[0].String())
		if value.Type() == js.TypeUndefined {
			return js.Null()
		}
		return value
	})
	setAttribute := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 2 {
			name, value := args[0].String(), args[1].String()
			element.Get("attributes").Set(name, value)
			if name == "data-giphy-post-embed-id" {
				element.Get("dataset").Set("giphyPostEmbedId", value)
			}
		}
		return nil
	})
	removeAttribute := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 1 {
			element.Get("attributes").Set(args[0].String(), js.Null())
		}
		return nil
	})
	appendChild := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 1 {
			element.Get("children").Call("push", args[0])
		}
		return nil
	})
	querySelector := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) != 1 {
			return js.Null()
		}
		selector := args[0].String()
		children := element.Get("children")
		if selector == "img" {
			for i := 0; i < children.Length(); i++ {
				if children.Index(i).Get("tag").String() == "img" {
					return children.Index(i)
				}
			}
			return js.Null()
		}
		if strings.HasPrefix(selector, "[data-giphy-post-embed-id='") {
			id := strings.TrimSuffix(strings.TrimPrefix(selector, "[data-giphy-post-embed-id='"), "']")
			for i := 0; i < children.Length(); i++ {
				if children.Index(i).Get("dataset").Get("giphyPostEmbedId").String() == id {
					return children.Index(i)
				}
			}
		}
		return js.Null()
	})
	querySelectorAll := js.FuncOf(func(_ js.Value, args []js.Value) any {
		result := js.Global().Get("Array").New()
		if len(args) != 1 {
			return result
		}
		selector := args[0].String()
		if selector == "a[href]" {
			return element.Get("anchors")
		}
		children := element.Get("children")
		if selector == ".giphy-post-embed" {
			for i := 0; i < children.Length(); i++ {
				child := children.Index(i)
				if child.Get("className").String() == "giphy-post-embed" {
					result.Call("push", child)
				}
			}
		}
		return result
	})
	contains := js.FuncOf(func(_ js.Value, args []js.Value) any {
		return len(args) == 1 && args[0].String() == element.Get("className").String()
	})
	classList := js.Global().Get("Object").New()
	classList.Set("contains", contains)
	element.Set("classList", classList)
	remove := js.FuncOf(func(js.Value, []js.Value) any { element.Set("removed", true); return nil })
	element.Set("getAttribute", getAttribute)
	element.Set("setAttribute", setAttribute)
	element.Set("removeAttribute", removeAttribute)
	element.Set("appendChild", appendChild)
	element.Set("querySelector", querySelector)
	element.Set("querySelectorAll", querySelectorAll)
	element.Set("remove", remove)
	return element
}

func newGiphyEmbedTestBody(url string) js.Value {
	body := newGiphyEmbedTestElement("body")
	anchors := js.Global().Get("Array").New()
	anchor := newGiphyEmbedTestElement("a")
	anchor.Call("setAttribute", "href", url)
	anchors.Call("push", anchor)
	body.Set("anchors", anchors)
	closest := js.FuncOf(func(js.Value, []js.Value) any { return js.Null() })
	body.Set("closest", closest)
	return body
}

func fakeGiphyCards(body js.Value) js.Value {
	return body.Call("querySelectorAll", giphyPostCardSelector)
}

func newGiphyEmbedTestRoot(body js.Value) js.Value {
	root := js.Global().Get("Object").New()
	root.Set("isConnected", true)
	root.Set("dataset", js.ValueOf(map[string]any{"selectedId": "room-a", "principal": "person-a"}))
	root.Set("bodies", js.Global().Get("Array").New(body))
	querySelectorAll := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 1 && args[0].String() == giphyMessageBodySelector {
			return root.Get("bodies")
		}
		return js.Global().Get("Array").New()
	})
	root.Set("querySelectorAll", querySelectorAll)
	return root
}
