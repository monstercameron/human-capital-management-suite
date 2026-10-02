//go:build js && wasm

package main

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// The Moderation page is a page in the main area of Chat: the conversation list
// stays where it is, the page takes the place of the conversation, and its close
// control returns to the conversation that was open. A dialog (remove, report,
// restore, message the author) is a small centred layer over whatever is
// showing. Neither depends on a timer or on the window having focus: they open
// on a click, close on a click or Escape, and place themselves from what is on
// screen when they open and when the window is resized.

func chatmodIsDialog(href string) bool {
	u, err := url.Parse(href)
	return err == nil && u.Query().Get("action") != ""
}

// chatmodWithZone tells the server the reader's time zone, so the times in the
// queue read as the conversation's do.
func chatmodWithZone(href string) string {
	u, err := url.Parse(href)
	if err != nil {
		return href
	}
	q := u.Query()
	offset := js.Global().Get("Date").New().Call("getTimezoneOffset").Float()
	q.Set("tz", strconv.Itoa(-int(offset)))
	u.RawQuery = q.Encode()
	return u.String()
}

func (b *chatremoveBrowser) overlay(kind string) js.Value {
	return js.Global().Get("document").Call("querySelector", "[data-chatremove-overlay="+kind+"]")
}

// placePage fits the page to the main column: below the application header,
// beside the conversation list, down to the bottom of the window.
func (b *chatremoveBrowser) placePage() {
	page := b.overlay("page")
	if !page.Truthy() {
		return
	}
	document := js.Global().Get("document")
	layout := document.Call("querySelector", ".chat-layout")
	if !layout.Truthy() {
		return
	}
	rect := layout.Call("getBoundingClientRect")
	top, bottom, left, right := rect.Get("top").Float(), rect.Get("bottom").Float(), rect.Get("left").Float(), rect.Get("right").Float()
	width, height := js.Global().Get("innerWidth").Float(), js.Global().Get("innerHeight").Float()
	if rail := document.Call("querySelector", ".chat-rail"); rail.Truthy() && width > 760 {
		edge := rail.Call("getBoundingClientRect")
		if js.Global().Call("getComputedStyle", layout).Get("direction").String() == "rtl" {
			right = edge.Get("left").Float()
		} else {
			left = edge.Get("right").Float()
		}
	}
	style := page.Get("style")
	for name, value := range map[string]float64{"--chatmod-top": top, "--chatmod-bottom": height - bottom, "--chatmod-left": left, "--chatmod-right": width - right} {
		style.Call("setProperty", name, strconv.FormatFloat(value, 'f', 2, 64)+"px")
	}
}

// openOverlay shows the page or the dialog an address names.
func (b *chatremoveBrowser) openOverlay(href string, opener js.Value) {
	if b.disposed {
		return
	}
	kind := "page"
	if chatmodIsDialog(href) {
		kind = "dialog"
	}
	document := js.Global().Get("document")
	workspace := document.Call("querySelector", ".chat-workspace")
	if !workspace.Truthy() {
		return
	}
	href = chatmodWithZone(href)
	b.generation++
	generation := b.generation
	overlay := b.overlay(kind)
	if !overlay.Truthy() {
		overlay = document.Call("createElement", "div")
		overlay.Call("setAttribute", "data-chatremove-overlay", kind)
		overlay.Set("className", "chatremove-overlay chatremove-overlay-"+kind)
		overlay.Set("tabIndex", -1)
		if kind == "dialog" {
			overlay.Call("setAttribute", "role", "dialog")
			overlay.Call("setAttribute", "aria-modal", "true")
			overlay.Call("setAttribute", "aria-label", chatui.ModerationText(b.locale, "moderation"))
			children := workspace.Get("children")
			for i := 0; i < children.Length(); i++ {
				child := children.Index(i)
				if !child.Get("inert").Bool() {
					child.Set("inert", true)
					child.Call("setAttribute", "data-chatremove-inert", "")
				}
			}
		} else {
			overlay.Call("setAttribute", "role", "region")
			overlay.Call("setAttribute", "aria-label", chatui.ModerationText(b.locale, "moderation"))
		}
		workspace.Call("appendChild", overlay)
	}
	if kind == "page" {
		b.pageHref = href
		if opener.Truthy() {
			b.pageOpener = opener
		}
		b.placePage()
	} else if opener.Truthy() {
		b.dialogOpener = opener
	}
	// Keep what is on screen while the next answer is on its way; the first open
	// has nothing yet, so it says it is loading.
	if !overlay.Call("querySelector", ".chatremove").Truthy() {
		loading := document.Call("createElement", "p")
		loading.Call("setAttribute", "role", "status")
		loading.Set("textContent", chatui.ModerationText(b.locale, "loading"))
		overlay.Set("textContent", "")
		overlay.Call("appendChild", loading)
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		markup, err := chatremovePageRequest(ctx, http.DefaultClient, b.config, href)
		ui.PostAsync(func() {
			if b.disposed || b.generation != generation || !overlay.Get("isConnected").Bool() {
				return
			}
			if err != nil {
				overlay.Set("textContent", "")
				failed := document.Call("createElement", "p")
				failed.Call("setAttribute", "role", "alert")
				failed.Set("textContent", chatui.ModerationText(b.locale, chatremoveErrorKey(err)))
				overlay.Call("appendChild", failed)
				closer := document.Call("createElement", "button")
				closer.Set("type", "button")
				closer.Call("setAttribute", "data-chatremove-close", "")
				closer.Set("className", "chatremove-link")
				closer.Set("textContent", chatui.ModerationText(b.locale, "close"))
				overlay.Call("appendChild", closer)
				closer.Call("focus")
				return
			}
			overlay.Set("innerHTML", markup)
			fields := overlay.Call("querySelectorAll", "[data-chat-value]")
			for i := 0; i < fields.Length(); i++ {
				field := fields.Index(i)
				field.Set("value", field.Call("getAttribute", "data-chat-value").String())
			}
			if kind == "dialog" {
				// The layer is the dialog; the section inside is its content.
				if inner := overlay.Call("querySelector", "[role=dialog]"); inner.Truthy() {
					inner.Call("removeAttribute", "role")
					inner.Call("removeAttribute", "aria-modal")
				}
				if field := overlay.Call("querySelector", "input,textarea,select,button"); field.Truthy() {
					field.Call("focus")
				}
				return
			}
			b.placePage()
			if focus := overlay.Call("querySelector", ".chatmod005-tab[aria-selected=true],[data-chatremove-close]"); focus.Truthy() {
				focus.Call("focus")
			}
		})
	}()
}

// closeKind removes the page or the dialog and hands focus back to what opened it.
func (b *chatremoveBrowser) closeKind(kind string) {
	overlay := b.overlay(kind)
	if !overlay.Truthy() {
		return
	}
	b.generation++
	overlay.Call("remove")
	opener := b.pageOpener
	if kind == "dialog" {
		opener = b.dialogOpener
		children := js.Global().Get("document").Call("querySelectorAll", "[data-chatremove-inert]")
		for i := 0; i < children.Length(); i++ {
			child := children.Index(i)
			child.Set("inert", false)
			child.Call("removeAttribute", "data-chatremove-inert")
		}
	} else {
		b.pageHref = ""
	}
	if !b.disposed && opener.Truthy() && opener.Get("isConnected").Bool() {
		opener.Call("focus")
	}
}

// closeTop closes the dialog if one is open, otherwise the page.
func (b *chatremoveBrowser) closeTop() {
	if b.overlay("dialog").Truthy() {
		b.closeKind("dialog")
		return
	}
	b.closeKind("page")
}

// reloadPage reads the page again, for the tab that is showing.
func (b *chatremoveBrowser) reloadPage() {
	if b.pageHref != "" && b.overlay("page").Truthy() {
		b.openOverlay(b.pageHref, js.Undefined())
	}
}

// filterItems hides the items that do not contain what was typed, as it is
// typed, from the items already on the page.
func (b *chatremoveBrowser) filterItems(query string) {
	document := js.Global().Get("document")
	needle := strings.ToLower(strings.TrimSpace(query))
	items := document.Call("querySelectorAll", ".chatmod005-item")
	visible := 0
	for i := 0; i < items.Length(); i++ {
		item := items.Index(i)
		hide := needle != "" && !strings.Contains(strings.ToLower(item.Get("textContent").String()), needle)
		item.Set("hidden", hide)
		if !hide {
			visible++
		}
	}
	if none := document.Call("querySelector", ".chatmod005-nomatch"); none.Truthy() {
		none.Set("hidden", needle == "" || visible > 0)
	}
}

// act carries out Dismiss or Restore on one item at once; they ask for nothing.
func (b *chatremoveBrowser) act(button js.Value) {
	action := button.Call("getAttribute", "data-chatremove-act").String()
	caseID := button.Call("getAttribute", "data-case-id").String()
	if action == "" || caseID == "" {
		return
	}
	b.mu.Lock()
	if b.busy {
		b.mu.Unlock()
		return
	}
	b.busy = true
	b.mu.Unlock()
	button.Set("disabled", true)
	input := chatremoveClientInput{CaseID: caseID, Action: action}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := chatremoveRequest(ctx, http.DefaultClient, b.config, "resolve", "", input)
		ui.PostAsync(func() {
			b.mu.Lock()
			b.busy = false
			b.mu.Unlock()
			if b.disposed {
				return
			}
			if err != nil {
				button.Set("disabled", false)
				if item := button.Call("closest", ".chatmod005-item"); item.Truthy() {
					alert := item.Call("querySelector", "[role=alert]")
					if !alert.Truthy() {
						alert = js.Global().Get("document").Call("createElement", "p")
						alert.Call("setAttribute", "role", "alert")
						alert.Set("className", "chatmod005-error")
						item.Call("appendChild", alert)
					}
					alert.Set("textContent", chatui.ModerationText(b.locale, chatremoveErrorKey(err)))
				}
				return
			}
			b.reloadPage()
			go chatmod005Refresh(b.config, true)
		})
	}()
}
