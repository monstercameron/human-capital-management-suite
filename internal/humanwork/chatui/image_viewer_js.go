//go:build js && wasm

package chatui

import (
	"strconv"
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

var imageViewerRoom string
var imageViewerPrincipal string
var imageViewerPost string
var imageViewerClose func(bool)
var imageViewerDisplayCleanup func()
var imageViewerOriginalCleanup func()

// openImageViewer opens the viewer on the thumbnail that was pressed.
func openImageViewer(event ui.Event, title, closeLabel, downloadLabel, actualSizeLabel, fitScreenLabel string) {
	target := event.JSValue().Get("target")
	if !target.Truthy() {
		return
	}
	button := target.Call("closest", ".attachment-image-open")
	if !button.Truthy() {
		return
	}
	locale := ""
	if workspace := button.Call("closest", ".chat-workspace"); workspace.Truthy() && workspace.Get("lang").Type() == js.TypeString {
		locale = workspace.Get("lang").String()
	}
	showImageViewer(button, chatbug083ViewerLabels(locale, title, closeLabel, downloadLabel, actualSizeLabel, fitScreenLabel), "")
}

// imageViewerButtons is the thumbnails the viewer steps through: the images of
// the list the opened one is in (the conversation, or the open thread) that
// have their picture, in the order they are on the page.
func imageViewerButtons(button js.Value) []js.Value {
	list := button.Call("closest", ".thread-pane,.message-list")
	if !list.Truthy() {
		return []js.Value{button}
	}
	found := list.Call("querySelectorAll", ".attachment-image-open")
	buttons := make([]js.Value, 0, found.Length())
	for i := 0; i < found.Length(); i++ {
		candidate := found.Index(i)
		if img := candidate.Call("querySelector", "img"); candidate.Equal(button) || (img.Truthy() && img.Get("src").String() != "") {
			buttons = append(buttons, candidate)
		}
	}
	return buttons
}

// imageViewerElement makes one of the viewer's own elements.
func imageViewerElement(doc js.Value, tag, class, text string) js.Value {
	el := doc.Call("createElement", tag)
	el.Set("className", class)
	if tag == "button" {
		el.Set("type", "button")
	}
	if text != "" {
		el.Set("textContent", text)
	}
	return el
}

// The viewer lives on document.body so a timeline render cannot replace it or
// move the scroll position. It begins with the visible thumbnail and upgrades
// through the same protected grant when the optimized display image is ready.
// focusOn names the control that takes focus ("prev" or "next" after a step
// made with that button); Close takes it otherwise.
func showImageViewer(button js.Value, labels chatbug083Labels, focusOn string) {
	thumbnail := button.Call("querySelector", "img")
	if !thumbnail.Truthy() || thumbnail.Get("src").String() == "" {
		return
	}
	if imageViewerClose != nil {
		imageViewerClose(false)
	}
	doc := js.Global().Get("document")
	workspace := button.Call("closest", ".chat-workspace")
	dataset := button.Get("dataset")
	text := func(name string) string {
		if v := dataset.Get(name); v.Type() == js.TypeString {
			return v.String()
		}
		return ""
	}
	backdrop := imageViewerElement(doc, "div", "chat-image-viewer", "")
	backdrop.Call("setAttribute", "role", "dialog")
	backdrop.Call("setAttribute", "aria-modal", "true")
	backdrop.Call("setAttribute", "aria-label", labels.Title)
	backdrop.Call("setAttribute", "lang", workspace.Get("lang"))
	backdrop.Call("setAttribute", "dir", workspace.Get("dir"))
	rtl := workspace.Get("dir").String() == "rtl"

	// What the image is: its name, who sent it and when, and its place among
	// the images of the conversation.
	siblings := imageViewerButtons(button)
	index := 0
	for i, sibling := range siblings {
		if sibling.Equal(button) {
			index = i
		}
	}
	caption := imageViewerElement(doc, "div", "chat-image-viewer-caption", "")
	caption.Call("appendChild", imageViewerElement(doc, "span", "chat-image-viewer-name", text("mediaName")))
	from := text("mediaAuthor")
	if sent := text("mediaSent"); sent != "" {
		if from != "" {
			from += " · "
		}
		from += sent
	}
	if len(siblings) > 1 {
		if from != "" {
			from += " · "
		}
		from += labels.position(index+1, len(siblings))
	}
	if from != "" {
		caption.Call("appendChild", imageViewerElement(doc, "span", "chat-image-viewer-from", from))
	}

	closeButton := imageViewerElement(doc, "button", "chat-image-viewer-close", "×")
	closeButton.Call("setAttribute", "aria-label", labels.Close)
	closeButton.Set("title", labels.Close)
	tools := imageViewerElement(doc, "div", "chat-image-viewer-tools", "")
	zoomButton := imageViewerElement(doc, "button", "chat-image-viewer-zoom", "1:1")
	zoomButton.Call("setAttribute", "aria-label", labels.ActualSize)
	zoomButton.Call("setAttribute", "aria-pressed", "false")
	zoomButton.Set("disabled", true)
	zoomButton.Call("setAttribute", "hidden", "")
	openButton := imageViewerElement(doc, "button", "chat-image-viewer-open", labels.OpenOriginal)
	openButton.Set("disabled", true)
	download := imageViewerElement(doc, "button", "chat-image-viewer-download", labels.Download)
	download.Call("setAttribute", "aria-label", labels.Download+": "+text("mediaName"))
	canDownload := js.Global().Get("hcmChatMediaDownload").Type() == js.TypeFunction
	if !canDownload {
		download.Call("setAttribute", "hidden", "")
	}
	for _, tool := range []js.Value{zoomButton, openButton, download} {
		tools.Call("appendChild", tool)
	}
	previous := imageViewerElement(doc, "button", "chat-image-viewer-step chat-image-viewer-prev", "‹")
	previous.Call("setAttribute", "aria-label", labels.Previous)
	previous.Set("title", labels.Previous)
	next := imageViewerElement(doc, "button", "chat-image-viewer-step chat-image-viewer-next", "›")
	next.Call("setAttribute", "aria-label", labels.Next)
	next.Set("title", labels.Next)
	_, hasPrevious := chatImageViewerNeighbour(index, -1, len(siblings))
	_, hasNext := chatImageViewerNeighbour(index, 1, len(siblings))
	previous.Set("disabled", !hasPrevious)
	next.Set("disabled", !hasNext)
	if len(siblings) < 2 {
		previous.Call("setAttribute", "hidden", "")
		next.Call("setAttribute", "hidden", "")
	}

	media := imageViewerElement(doc, "div", "chat-image-viewer-media", "")
	preview := imageViewerElement(doc, "img", "chat-image-viewer-preview", "")
	preview.Set("src", thumbnail.Get("src"))
	preview.Set("alt", thumbnail.Get("alt"))
	full := imageViewerElement(doc, "img", "chat-image-viewer-full", "")
	full.Set("src", thumbnail.Get("src"))
	full.Set("alt", "")
	full.Call("setAttribute", "aria-hidden", "true")
	original := imageViewerElement(doc, "img", "chat-image-viewer-original", "")
	original.Set("alt", "")
	original.Call("setAttribute", "aria-hidden", "true")
	for _, img := range []js.Value{preview, full, original} {
		media.Call("appendChild", img)
	}
	for _, part := range []js.Value{caption, tools, closeButton, previous, media, next} {
		backdrop.Call("appendChild", part)
	}
	doc.Get("body").Call("appendChild", backdrop)
	imageViewerRoom = workspace.Get("dataset").Get("selectedId").String()
	imageViewerPrincipal = workspace.Get("dataset").Get("principal").String()
	displayURL := text("mediaDisplay")
	if row := button.Call("closest", "[data-message-id]"); row.Truthy() {
		imageViewerPost = row.Get("dataset").Get("messageId").String()
	}
	post := imageViewerPost

	// The picture's own size is what the message recorded for it; the thumbnail
	// is a smaller rendition and says nothing about it.
	ownWidth, _ := strconv.Atoi(text("mediaWidth"))
	ownHeight, _ := strconv.Atoi(text("mediaHeight"))
	syncSize := func() {
		width, height := chatImageViewerSize(ownWidth, ownHeight, media.Get("clientWidth").Int(), media.Get("clientHeight").Int())
		style := media.Get("style")
		if width == 0 {
			style.Call("removeProperty", "--chat-viewer-w")
			style.Call("removeProperty", "--chat-viewer-h")
			return
		}
		style.Call("setProperty", "--chat-viewer-w", strconv.Itoa(width)+"px")
		style.Call("setProperty", "--chat-viewer-h", strconv.Itoa(height)+"px")
	}
	// What Open original opens: the original once it has loaded here, or the
	// display rendition of an animated image, which is the original's bytes.
	originalSource := func() string {
		if original.Get("classList").Call("contains", "chat-image-original-ready").Bool() {
			return original.Get("src").String()
		}
		if text("mediaAnimated") == "true" && full.Get("classList").Call("contains", "chat-image-display-ready").Bool() {
			return full.Get("src").String()
		}
		return ""
	}
	var onClick, onKey, onMutation, onResize js.Func
	var observer js.Value
	syncControls := func() {
		ready := original.Get("classList").Call("contains", "chat-image-original-ready").Bool()
		zoomed := media.Get("classList").Call("contains", "chat-image-viewer-zoomed").Bool()
		needsActualSize := ready && (zoomed || chatImageViewerNeedsActualSize(
			original.Get("naturalWidth").Int(), original.Get("naturalHeight").Int(), media.Get("clientWidth").Int(), media.Get("clientHeight").Int(),
		))
		zoomButton.Set("disabled", !ready)
		if needsActualSize {
			zoomButton.Call("removeAttribute", "hidden")
		} else {
			zoomButton.Call("setAttribute", "hidden", "")
		}
		openButton.Set("disabled", originalSource() == "")
	}
	imageViewerClose = func(restore bool) {
		if imageViewerClose == nil {
			return
		}
		imageViewerClose = nil
		imageViewerRoom = ""
		imageViewerPrincipal = ""
		imageViewerPost = ""
		if imageViewerDisplayCleanup != nil {
			imageViewerDisplayCleanup()
			imageViewerDisplayCleanup = nil
		}
		if imageViewerOriginalCleanup != nil {
			imageViewerOriginalCleanup()
			imageViewerOriginalCleanup = nil
		}
		backdrop.Call("removeEventListener", "click", onClick)
		doc.Call("removeEventListener", "keydown", onKey, true)
		js.Global().Get("window").Call("removeEventListener", "resize", onResize)
		observer.Call("disconnect")
		backdrop.Call("remove")
		onClick.Release()
		onKey.Release()
		onMutation.Release()
		onResize.Release()
		if restore {
			focus := button
			if !focus.Get("isConnected").Bool() {
				buttons := doc.Call("querySelectorAll", ".attachment-image-open")
				for i := 0; i < buttons.Get("length").Int(); i++ {
					candidate := buttons.Index(i)
					if candidate.Get("dataset").Get("id").String() == button.Get("dataset").Get("id").String() {
						focus = candidate
						break
					}
				}
			}
			if focus.Get("isConnected").Bool() {
				focus.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
			}
		}
	}
	// step shows the image before or after this one. The list is read again:
	// messages may have arrived, or left, while the viewer was open.
	step := func(by int, focusOn string) {
		current := imageViewerButtons(button)
		at := -1
		for i, sibling := range current {
			if sibling.Equal(button) {
				at = i
			}
		}
		if to, ok := chatImageViewerNeighbour(at, by, len(current)); ok {
			showImageViewer(current[to], labels, focusOn)
		}
	}
	onClick = js.FuncOf(func(_ js.Value, args []js.Value) any {
		clicked := args[0].Get("target")
		switch {
		case clicked.Equal(download):
			if bridge := js.Global().Get("hcmChatMediaDownload"); bridge.Type() == js.TypeFunction {
				bridge.Invoke(post, text("mediaId"))
			}
		case clicked.Equal(openButton):
			if source := originalSource(); source != "" {
				js.Global().Call("open", source, "_blank", "noopener")
			}
		case clicked.Equal(previous):
			step(-1, "prev")
		case clicked.Equal(next):
			step(1, "next")
		case clicked.Equal(zoomButton):
			zoomed := media.Get("classList").Call("toggle", "chat-image-viewer-zoomed").Bool()
			zoomButton.Call("setAttribute", "aria-pressed", strconv.FormatBool(zoomed))
			if zoomed {
				zoomButton.Call("setAttribute", "aria-label", labels.Fit)
			} else {
				zoomButton.Call("setAttribute", "aria-label", labels.ActualSize)
			}
			syncControls()
		case clicked.Equal(backdrop), clicked.Equal(media), clicked.Equal(closeButton):
			// The media box fills the viewer; a press on it beside the picture is
			// a press outside the picture.
			imageViewerClose(true)
		}
		return nil
	})
	onKey = js.FuncOf(func(_ js.Value, args []js.Value) any {
		key := args[0].Get("key").String()
		switch {
		case key == "Escape":
			args[0].Call("preventDefault")
			args[0].Call("stopPropagation")
			imageViewerClose(true)
		case key == "ArrowLeft" || key == "ArrowRight":
			// At actual size the arrows scroll the picture.
			if media.Get("classList").Call("contains", "chat-image-viewer-zoomed").Bool() {
				return nil
			}
			args[0].Call("preventDefault")
			args[0].Call("stopPropagation")
			step(chatImageViewerStep(key, rtl), "")
		case key == "Tab":
			var focusables []js.Value
			for _, control := range []js.Value{closeButton, previous, next, zoomButton, openButton, download} {
				if !control.Get("disabled").Bool() && !control.Get("hidden").Bool() {
					focusables = append(focusables, control)
				}
			}
			active := js.Global().Get("document").Get("activeElement")
			at := 0
			for i, focusable := range focusables {
				if active.Equal(focusable) {
					at = i
					break
				}
			}
			by := 1
			if args[0].Get("shiftKey").Truthy() {
				by = -1
			}
			args[0].Call("preventDefault")
			focusables[(at+by+len(focusables))%len(focusables)].Call("focus")
		}
		return nil
	})
	onResize = js.FuncOf(func(js.Value, []js.Value) any {
		syncSize()
		syncControls()
		return nil
	})
	onMutation = js.FuncOf(func(js.Value, []js.Value) any {
		if !workspace.Get("isConnected").Bool() ||
			workspace.Get("dataset").Get("selectedId").String() != imageViewerRoom ||
			workspace.Get("dataset").Get("principal").String() != imageViewerPrincipal ||
			!button.Get("isConnected").Bool() || button.Get("dataset").Get("mediaDisplay").String() != displayURL {
			imageViewerClose(false)
		}
		return nil
	})
	observer = js.Global().Get("MutationObserver").New(onMutation)
	observer.Call("observe", doc.Get("body"), js.ValueOf(map[string]any{"childList": true, "subtree": true, "attributes": true, "attributeFilter": []any{"data-selected-id", "data-principal", "data-media-display"}}))
	backdrop.Call("addEventListener", "click", onClick)
	doc.Call("addEventListener", "keydown", onKey, true)
	js.Global().Get("window").Call("addEventListener", "resize", onResize)
	syncSize()
	first := closeButton
	if focusOn == "prev" && hasPrevious {
		first = previous
	} else if focusOn == "next" && hasNext {
		first = next
	}
	first.Call("focus", js.ValueOf(map[string]any{"preventScroll": true}))
	imageViewerDisplayCleanup = loadChatImageDisplay(button, full, func(ok bool) {
		if !ok {
			return
		}
		syncControls()
		imageViewerOriginalCleanup = loadChatImageOriginal(button, original, func(loaded, _ bool) {
			syncControls()
			if loaded && imageViewerDisplayCleanup != nil {
				imageViewerDisplayCleanup()
				imageViewerDisplayCleanup = nil
			}
		})
	})
}

func syncImageViewer(selectedID, principal string) {
	if imageViewerClose != nil && (selectedID != imageViewerRoom || principal != imageViewerPrincipal) {
		imageViewerClose(false)
	}
}
