//go:build js && wasm

package main

import (
	"strconv"
	"syscall/js"
)

// browserTransientPopoverController adds progressive dismissal behavior to
// semantic details/summary popovers. Its document-level listener survives
// route reconciliation without rebinding every replacement DOM node.
type browserTransientPopoverController struct {
	listener    js.Func
	bound       bool
	nextID      uint64
	pendingByID map[string]*pendingPopoverClose
}

type pendingPopoverClose struct {
	timer    js.Value
	callback js.Func
}

func newBrowserTransientPopoverController() *browserTransientPopoverController {
	return &browserTransientPopoverController{pendingByID: map[string]*pendingPopoverClose{}}
}

func (c *browserTransientPopoverController) Bind() {
	if c == nil || c.bound {
		return
	}
	c.bound = true
	c.listener = js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		event := args[0]
		target := event.Get("target")
		if !target.Truthy() || target.Get("nodeType").Int() != 1 {
			return nil
		}
		eventType := event.Get("type").String()
		if eventType == "pointerdown" {
			c.closePopoversOutside(target)
			return nil
		}

		details := target.Call("closest", transientPopoverSelector)
		if !details.Truthy() || !details.Get("open").Bool() {
			return nil
		}
		if transientPopoverLinkActivated(eventType, target.Call("closest", "a[href]").Truthy()) {
			c.close(details)
			return nil
		}

		relatedInside := false
		if related := event.Get("relatedTarget"); related.Truthy() {
			relatedInside = details.Call("contains", related).Bool()
		}
		focusInPopover := false
		popover := details.Call("querySelector", ".popover-surface")
		active := js.Global().Get("document").Get("activeElement")
		if popover.Truthy() && active.Truthy() {
			focusInPopover = popover.Call("contains", active).Bool()
		}
		action := transientPopoverEventAction(eventType, event.Get("key").String(), relatedInside, focusInPopover)
		switch action {
		case transientPopoverCancelPendingClose:
			c.cancelClose(details)
		case transientPopoverScheduleClose:
			c.scheduleClose(details)
		case transientPopoverCloseNow:
			c.close(details)
			event.Call("preventDefault")
			summary := details.Get("firstElementChild")
			if summary.Truthy() && summary.Get("tagName").String() == "SUMMARY" {
				summary.Call("focus")
			}
		}
		return nil
	})
	document := js.Global().Get("document")
	for _, eventType := range []string{"mouseover", "mouseout", "focusin", "focusout", "keydown", "pointerdown", "click"} {
		document.Call("addEventListener", eventType, c.listener)
	}
}

func (c *browserTransientPopoverController) popoverID(details js.Value) string {
	if id := details.Call("getAttribute", "data-hcm-popover-runtime-id"); id.Truthy() {
		return id.String()
	}
	c.nextID++
	id := "hcm-popover-" + strconv.FormatUint(c.nextID, 10)
	details.Call("setAttribute", "data-hcm-popover-runtime-id", id)
	return id
}

func (c *browserTransientPopoverController) cancelClose(details js.Value) {
	if c == nil || !details.Truthy() {
		return
	}
	id := c.popoverID(details)
	pending, ok := c.pendingByID[id]
	if !ok {
		return
	}
	js.Global().Call("clearTimeout", pending.timer)
	pending.callback.Release()
	delete(c.pendingByID, id)
}

func (c *browserTransientPopoverController) scheduleClose(details js.Value) {
	if c == nil || !details.Truthy() {
		return
	}
	c.cancelClose(details)
	id := c.popoverID(details)
	grace := normalizedTransientPopoverGraceMilliseconds(details.Call("getAttribute", "data-hcm-popover-grace-ms").String())
	callback := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		pending, ok := c.pendingByID[id]
		if !ok {
			return nil
		}
		delete(c.pendingByID, id)
		defer pending.callback.Release()
		if !details.Get("open").Bool() || c.pointerOrFocusInside(details) {
			return nil
		}
		details.Call("removeAttribute", "open")
		return nil
	})
	timer := js.Global().Call("setTimeout", callback, grace)
	c.pendingByID[id] = &pendingPopoverClose{timer: timer, callback: callback}
}

func (c *browserTransientPopoverController) pointerOrFocusInside(details js.Value) bool {
	if details.Call("matches", ":hover").Bool() {
		return true
	}
	active := js.Global().Get("document").Get("activeElement")
	return active.Truthy() && details.Call("contains", active).Bool()
}

func (c *browserTransientPopoverController) close(details js.Value) {
	if !details.Truthy() {
		return
	}
	c.cancelClose(details)
	details.Call("removeAttribute", "open")
}

func (c *browserTransientPopoverController) closePopoversOutside(target js.Value) {
	document := js.Global().Get("document")
	open := document.Call("querySelectorAll", transientPopoverSelector+"[open]")
	for index := 0; index < open.Get("length").Int(); index++ {
		details := open.Index(index)
		if !details.Call("contains", target).Bool() {
			c.close(details)
		}
	}
}
