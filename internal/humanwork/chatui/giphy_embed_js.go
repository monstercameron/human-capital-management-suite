//go:build js && wasm

package chatui

import "syscall/js"

const giphyPostEmbedSelector = "img[data-giphy-embed-src]"

// observeGiphyPostEmbeds loads posted GIF media only as it approaches the
// viewport. The API-provided URL is used verbatim; it is never proxied, cached,
// or rewritten. The caller owns the cleanup function for the rendered root.
func observeGiphyPostEmbeds(root js.Value) func() {
	if !root.Truthy() {
		return func() {}
	}
	elements := root.Call("querySelectorAll", giphyPostEmbedSelector)
	if elements.Length() == 0 {
		return func() {}
	}
	observerType := js.Global().Get("IntersectionObserver")
	if observerType.Type() != js.TypeFunction {
		for i := 0; i < elements.Length(); i++ {
			giphyLoadPostEmbed(elements.Index(i))
		}
		return func() {}
	}

	var callback js.Func
	var observer js.Value
	callback = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		entries := args[0]
		for i := 0; i < entries.Length(); i++ {
			entry := entries.Index(i)
			ratio := entry.Get("intersectionRatio")
			if !entry.Get("isIntersecting").Truthy() && (ratio.Type() != js.TypeNumber || ratio.Float() <= 0) {
				continue
			}
			target := entry.Get("target")
			if giphyLoadPostEmbed(target) {
				observer.Call("unobserve", target)
			}
		}
		return nil
	})
	options := js.Global().Get("Object").New()
	options.Set("rootMargin", "400px 0px")
	observer = observerType.New(callback, options)
	for i := 0; i < elements.Length(); i++ {
		observer.Call("observe", elements.Index(i))
	}
	return func() {
		observer.Call("disconnect")
		callback.Release()
	}
}

func giphyLoadPostEmbed(image js.Value) bool {
	if !image.Truthy() {
		return false
	}
	url := image.Call("getAttribute", "data-giphy-embed-src").String()
	if !validGiphyGIFURL(url) {
		image.Call("removeAttribute", "data-giphy-embed-src")
		return false
	}
	image.Set("loading", "lazy")
	image.Set("decoding", "async")
	image.Set("src", url)
	image.Call("removeAttribute", "data-giphy-embed-src")
	return true
}
