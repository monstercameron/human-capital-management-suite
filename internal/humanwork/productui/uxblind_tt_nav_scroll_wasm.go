//go:build js && wasm

package productui

import "syscall/js"

func scrollCurrentNavigationItem(key string) {
	if key == "" {
		return
	}
	document := js.Global().Get("document")
	if !document.Truthy() {
		return
	}
	navigation := document.Call("getElementById", "primary-nav")
	if !navigation.Truthy() {
		return
	}
	current := navigation.Call("querySelector", `[data-hcm-nav-current="true"]`)
	if current.Truthy() {
		current.Call("scrollIntoView", map[string]any{"block": "nearest", "behavior": "auto"})
	}
}

var fragmentScrollWatch struct {
	observer js.Value
	timer    js.Value
	funcs    []js.Func
	hashOnce bool
}

// scrollToURLFragment brings the element named by the URL fragment into view
// and moves focus to its heading. The page renders after load, so the
// browser's native fragment scroll found nothing; when the target is not in
// the DOM yet this watches for it for a few seconds.
func scrollToURLFragment() {
	global := js.Global()
	document, location := global.Get("document"), global.Get("location")
	if !document.Truthy() || !location.Truthy() {
		return
	}
	if !fragmentScrollWatch.hashOnce {
		fragmentScrollWatch.hashOnce = true
		global.Call("addEventListener", "hashchange", js.FuncOf(func(js.Value, []js.Value) any {
			scrollToURLFragment()
			return nil
		}))
	}
	stopFragmentWatch()
	id := fragmentTargetID(location.Get("hash").String())
	if id == "" {
		return
	}
	if revealFragmentTarget(id) {
		return
	}
	check := js.FuncOf(func(js.Value, []js.Value) any {
		if revealFragmentTarget(id) {
			stopFragmentWatch()
		}
		return nil
	})
	stop := js.FuncOf(func(js.Value, []js.Value) any {
		stopFragmentWatch()
		return nil
	})
	observer := global.Get("MutationObserver").New(check)
	observer.Call("observe", document.Get("body"), map[string]any{"childList": true, "subtree": true})
	fragmentScrollWatch.observer = observer
	fragmentScrollWatch.timer = global.Call("setTimeout", stop, 10000)
	fragmentScrollWatch.funcs = []js.Func{check, stop}
}

func stopFragmentWatch() {
	if fragmentScrollWatch.observer.Truthy() {
		fragmentScrollWatch.observer.Call("disconnect")
		js.Global().Call("clearTimeout", fragmentScrollWatch.timer)
	}
	fragmentScrollWatch.observer, fragmentScrollWatch.timer = js.Value{}, js.Value{}
	// The callback may be running right now; release it after it returns.
	funcs := fragmentScrollWatch.funcs
	fragmentScrollWatch.funcs = nil
	if len(funcs) > 0 {
		var release js.Func
		release = js.FuncOf(func(js.Value, []js.Value) any {
			for _, f := range funcs {
				f.Release()
			}
			release.Release()
			return nil
		})
		js.Global().Call("setTimeout", release, 0)
	}
}

func revealFragmentTarget(id string) bool {
	target := js.Global().Get("document").Call("getElementById", id)
	if !target.Truthy() {
		return false
	}
	target.Call("scrollIntoView", map[string]any{"block": "start", "behavior": "auto"})
	focus := target.Call("querySelector", "h1,h2,h3,h4,h5,h6")
	if !focus.Truthy() {
		focus = target
	}
	if focus.Call("getAttribute", "tabindex").IsNull() {
		focus.Call("setAttribute", "tabindex", "-1")
	}
	focus.Call("focus", map[string]any{"preventScroll": true})
	return true
}
