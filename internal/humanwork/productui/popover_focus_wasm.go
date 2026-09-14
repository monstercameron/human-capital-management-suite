//go:build js && wasm

package productui

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// usePopoverFocusDismissal keeps stateful popovers open during internal focus
// travel, but dismisses them when keyboard focus or a pointer moves outside.
func usePopoverFocusDismissal(rootID, triggerID string, open bool, dismiss func()) {
	ui.UseEffectOf(func() func() {
		if !open {
			return nil
		}
		return bindPopoverFocusDismissal(rootID, triggerID, dismiss)
	}, struct {
		Root, Trigger string
		Open          bool
	}{rootID, triggerID, open})
}

func bindPopoverFocusDismissal(rootID, triggerID string, dismiss func()) func() {
	doc := js.Global().Get("document")
	root := doc.Call("getElementById", rootID)
	if !root.Truthy() {
		return nil
	}
	var timer js.Value
	pending := false
	cancelPending := func() {
		if pending {
			js.Global().Call("clearTimeout", timer)
			pending = false
		}
	}
	check := js.FuncOf(func(js.Value, []js.Value) any {
		defer func() { _ = recover() }()
		pending = false
		active := doc.Get("activeElement")
		if !active.Truthy() || !root.Call("contains", active).Bool() {
			dismiss()
		}
		return nil
	})
	listener := js.FuncOf(func(_ js.Value, args []js.Value) any {
		defer func() { _ = recover() }()
		if len(args) == 0 {
			return nil
		}
		event := args[0]
		target := event.Get("target")
		inside := target.Truthy() && root.Call("contains", target).Bool()
		switch event.Get("type").String() {
		case "pointerdown":
			if !inside {
				// A pointer move commonly follows focusout. Cancel its delayed
				// check so it cannot steal focus back from the clicked control.
				cancelPending()
				dismiss()
			}
		case "focusout":
			if !inside {
				return nil
			}
			related := event.Get("relatedTarget")
			if related.Truthy() && root.Call("contains", related).Bool() {
				return nil
			}
			if pending {
				js.Global().Call("clearTimeout", timer)
			}
			pending = true
			timer = js.Global().Call("setTimeout", check, 180)
		case "keydown":
			if inside && event.Get("key").String() == "Escape" {
				event.Call("preventDefault")
				cancelPending()
				dismiss()
				focusElementByID(doc, triggerID)
			}
		}
		return nil
	})
	for _, kind := range []string{"focusout", "pointerdown", "keydown"} {
		doc.Call("addEventListener", kind, listener)
	}
	return func() {
		for _, kind := range []string{"focusout", "pointerdown", "keydown"} {
			doc.Call("removeEventListener", kind, listener)
		}
		cancelPending()
		listener.Release()
		check.Release()
	}
}

// focusElementByID is deliberately defensive: a route transition can remove
// a trigger between the event and the focus restoration callback. Browser
// focus recovery must never turn that normal race into a panic.
func focusElementByID(doc js.Value, id string) {
	if !doc.Truthy() || id == "" {
		return
	}
	defer func() { _ = recover() }()
	if element := doc.Call("getElementById", id); element.Truthy() {
		element.Call("focus")
	}
}

func focusPopoverElement(id string) {
	focusElementByID(js.Global().Get("document"), id)
}

// useMobileNavigationDrawer progressively enhances the server-rendered aside
// into a viewport-bound drawer. It lives beside the popover focus controller
// because both controls need the same defensive DOM/focus behavior, while the
// native build deliberately retains the plain document-order baseline.
func useMobileNavigationDrawer(rootID, triggerID, backdropID string) {
	ui.UseEffectOf(func() func() {
		return bindMobileNavigationDrawer(rootID, triggerID, backdropID)
	}, struct {
		Root, Trigger, Backdrop string
	}{rootID, triggerID, backdropID})
}

func bindMobileNavigationDrawer(rootID, triggerID, backdropID string) func() {
	doc := js.Global().Get("document")
	root := doc.Call("getElementById", rootID)
	trigger := doc.Call("getElementById", triggerID)
	backdrop := doc.Call("getElementById", backdropID)
	if !root.Truthy() || !trigger.Truthy() {
		return nil
	}
	createdBackdrop := false
	if !backdrop.Truthy() {
		backdrop = doc.Call("createElement", "button")
		backdrop.Set("id", backdropID)
		backdrop.Set("type", "button")
		backdrop.Set("aria-label", "Close navigation")
		backdrop.Set("tabIndex", -1)
		backdrop.Set("hidden", true)
		doc.Get("body").Call("appendChild", backdrop)
		createdBackdrop = true
	}
	nav := root.Call("querySelector", ".primary-nav")
	content := doc.Call("getElementById", "main-content")
	media := js.Global().Call("matchMedia", "(max-width: 760px)")
	open := false
	wasMobile := false
	var apply func(bool, bool)
	apply = func(next, focus bool) {
		defer func() { _ = recover() }()
		mobile := media.Get("matches").Bool()
		if !mobile {
			// Desktop has exactly two scroll owners: the primary navigation and
			// the content region. Inline rules here win over legacy cascade
			// layers during hydration and prevent the document/sidebar from
			// acquiring a third scrollbar.
			root.Get("style").Set("overflow", "hidden")
			if nav.Truthy() {
				nav.Get("style").Set("overflowY", "auto")
				nav.Get("style").Set("overflowX", "hidden")
			}
			if content.Truthy() {
				content.Get("style").Set("overflowY", "auto")
				content.Get("style").Set("overflowX", "hidden")
			}
			root.Get("style").Call("removeProperty", "position")
			root.Get("style").Call("removeProperty", "inset-block-start")
			root.Get("style").Call("removeProperty", "inset-inline-start")
			root.Get("style").Call("removeProperty", "width")
			root.Get("style").Call("removeProperty", "max-height")
			root.Get("style").Call("removeProperty", "z-index")
			root.Get("style").Call("removeProperty", "background")
			root.Get("style").Call("removeProperty", "box-shadow")
			root.Get("style").Call("removeProperty", "transform")
			root.Get("style").Call("removeProperty", "transition")
			backdrop.Set("hidden", true)
			backdrop.Get("style").Call("removeProperty", "position")
			backdrop.Get("style").Call("removeProperty", "inset")
			backdrop.Get("style").Call("removeProperty", "z-index")
			backdrop.Get("style").Call("removeProperty", "background")
			backdrop.Get("style").Call("removeProperty", "display")
			root.Call("removeAttribute", "data-hcm-mobile-open")
			return
		}
		style := root.Get("style")
		dir := doc.Get("documentElement").Get("dir").String()
		closedTransform := "translateX(-105%)"
		if dir == "rtl" {
			closedTransform = "translateX(105%)"
		}
		style.Set("position", "fixed")
		style.Set("insetBlockStart", "65px")
		style.Set("insetInlineStart", "0")
		style.Set("width", "min(88vw, 320px)")
		style.Set("maxHeight", "calc(100dvh - 65px)")
		style.Set("zIndex", "40")
		style.Set("background", "var(--surface, #fff)")
		style.Set("boxShadow", "0 18px 48px rgba(16,34,56,.22)")
		style.Set("overflow", "auto")
		style.Set("transition", "transform 180ms ease")
		style.Set("transform", closedTransform)
		if nav.Truthy() {
			nav.Get("style").Call("removeProperty", "overflow-y")
			nav.Get("style").Call("removeProperty", "overflow-x")
		}
		if content.Truthy() {
			content.Get("style").Call("removeProperty", "overflow-y")
			content.Get("style").Call("removeProperty", "overflow-x")
		}
		if next {
			style.Set("transform", "translateX(0)")
			root.Call("setAttribute", "data-hcm-mobile-open", "true")
			backdrop.Call("removeAttribute", "hidden")
			backdrop.Get("style").Set("position", "fixed")
			backdrop.Get("style").Set("inset", "0")
			backdrop.Get("style").Set("zIndex", "39")
			backdrop.Get("style").Set("background", "rgba(16,34,56,.32)")
			backdrop.Get("style").Set("display", "block")
			trigger.Set("aria-expanded", "true")
			if focus {
				if first := root.Call("querySelector", "input, a, button, summary"); first.Truthy() {
					first.Call("focus")
				}
			}
		} else {
			root.Call("removeAttribute", "data-hcm-mobile-open")
			backdrop.Set("hidden", true)
			trigger.Set("aria-expanded", "false")
			if focus {
				focusElementByID(doc, triggerID)
			}
		}
	}
	setOpen := func(next bool, restore bool) {
		open = next
		apply(open, restore)
	}
	click := js.FuncOf(func(_ js.Value, args []js.Value) any {
		defer func() { _ = recover() }()
		if len(args) == 0 || !media.Get("matches").Bool() {
			return nil
		}
		event := args[0]
		event.Call("preventDefault")
		event.Call("stopPropagation")
		setOpen(!open, true)
		return nil
	})
	backdropClick := js.FuncOf(func(_ js.Value, args []js.Value) any {
		defer func() { _ = recover() }()
		if len(args) > 0 {
			args[0].Call("preventDefault")
		}
		setOpen(false, true)
		return nil
	})
	insideClick := js.FuncOf(func(_ js.Value, args []js.Value) any {
		defer func() { _ = recover() }()
		if len(args) > 0 && args[0].Get("target").Truthy() && args[0].Get("target").Call("closest", "a").Truthy() {
			setOpen(false, false)
		}
		return nil
	})
	key := js.FuncOf(func(_ js.Value, args []js.Value) any {
		defer func() { _ = recover() }()
		if len(args) > 0 && open && media.Get("matches").Bool() && args[0].Get("key").String() == "Escape" {
			args[0].Call("preventDefault")
			setOpen(false, true)
		}
		return nil
	})
	resize := js.FuncOf(func(js.Value, []js.Value) any {
		defer func() { _ = recover() }()
		mobile := media.Get("matches").Bool()
		if mobile && !wasMobile {
			open = false
		}
		wasMobile = mobile
		apply(open, false)
		return nil
	})
	trigger.Call("addEventListener", "click", click, true)
	backdrop.Call("addEventListener", "click", backdropClick)
	root.Call("addEventListener", "click", insideClick)
	doc.Call("addEventListener", "keydown", key)
	js.Global().Call("addEventListener", "resize", resize)
	wasMobile = media.Get("matches").Bool()
	apply(false, false)
	return func() {
		defer func() { _ = recover() }()
		trigger.Call("removeEventListener", "click", click, true)
		backdrop.Call("removeEventListener", "click", backdropClick)
		root.Call("removeEventListener", "click", insideClick)
		doc.Call("removeEventListener", "keydown", key)
		js.Global().Call("removeEventListener", "resize", resize)
		root.Get("style").Call("removeProperty", "overflow")
		if nav.Truthy() {
			nav.Get("style").Call("removeProperty", "overflow-y")
			nav.Get("style").Call("removeProperty", "overflow-x")
		}
		if content.Truthy() {
			content.Get("style").Call("removeProperty", "overflow-y")
			content.Get("style").Call("removeProperty", "overflow-x")
		}
		if createdBackdrop {
			defer func() { _ = recover() }()
			doc.Get("body").Call("removeChild", backdrop)
		}
		click.Release()
		backdropClick.Release()
		insideClick.Release()
		key.Release()
		resize.Release()
	}
}
