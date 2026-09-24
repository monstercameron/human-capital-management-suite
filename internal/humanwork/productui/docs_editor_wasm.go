//go:build js && wasm

package productui

import (
	"fmt"
	"strings"
	"syscall/js"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// The browser half of the split editor. Every listener sits on the
// document and finds the editor's elements by id each time, so a render
// that replaces an element never leaves a listener attached to a stale
// one. Text moves between the panes after a 150 ms pause in typing.

const (
	docsEditorSourceID = "docs-editor-source"
	docsEditorRichID   = "docs-editor-rich"
	docsEditorDebounce = 150
)

type docsEditorBrowser struct {
	c                          *docsEditorController
	sourceTimer, richTimer     js.Value
	sourcePending, richPending bool
	sourceTick, richTick       js.Func
	listeners                  []docsEditorListener
	rendered                   string   // the Markdown the formatted pane was last rendered from
	richElement                js.Value // the element that rendering went into
	lastRange                  js.Value // the formatted pane's last selection
	scrollQuiet                map[string]time.Time
	formatsQueued              bool
}

type docsEditorListener struct {
	name    string
	fn      js.Func
	capture bool
}

var docsEditorBrowsers = map[*docsEditorController]*docsEditorBrowser{}

func docsEditorElement(id string) js.Value {
	return js.Global().Get("document").Call("getElementById", id)
}

func docsEditorFocused(el js.Value) bool {
	return el.Truthy() && js.Global().Get("document").Get("activeElement").Equal(el)
}

func docsEditorWithin(el, target js.Value) bool {
	return el.Truthy() && target.Truthy() && el.Call("contains", target).Bool()
}

func docsEditorMount(c *docsEditorController) func() {
	b := &docsEditorBrowser{c: c, scrollQuiet: map[string]time.Time{}}
	docsEditorBrowsers[c] = b
	if title := docsEditorElement("docs-editor-title"); title.Truthy() {
		title.Set("value", c.title)
	}
	if source := docsEditorElement(docsEditorSourceID); source.Truthy() {
		source.Set("value", c.markdown)
	}
	b.renderRich(c.markdown, -1, -1, false)
	// New paragraphs typed in the formatted pane are <p>, not <div>.
	js.Global().Get("document").Call("execCommand", "defaultParagraphSeparator", false, "p")
	b.sourceTick = js.FuncOf(func(js.Value, []js.Value) any {
		defer docsEditorContain()
		b.syncSource()
		return nil
	})
	b.richTick = js.FuncOf(func(js.Value, []js.Value) any {
		defer docsEditorContain()
		b.syncRich()
		return nil
	})
	c.flush = b.flush

	b.listen("input", false, b.onInput)
	b.listen("change", false, b.onInput)
	b.listen("keydown", false, b.onKeyDown)
	b.listen("focusin", false, b.onFocusIn)
	b.listen("paste", false, b.onPaste)
	b.listen("drop", false, b.onDrop)
	b.listen("scroll", true, b.onScroll)
	b.listen("selectionchange", false, func(js.Value) { b.queueFormats() })
	return func() {
		doc := js.Global().Get("document")
		for _, l := range b.listeners {
			doc.Call("removeEventListener", l.name, l.fn, l.capture)
			l.fn.Release()
		}
		js.Global().Call("clearTimeout", b.sourceTimer)
		js.Global().Call("clearTimeout", b.richTimer)
		b.sourceTick.Release()
		b.richTick.Release()
		if c.flush != nil {
			c.flush = nil
		}
		delete(docsEditorBrowsers, c)
	}
}

// docsEditorContain keeps a fault in an editor listener from ending the
// whole application: the WASM program exits on an unrecovered panic, which
// would take every other page down with the editor. The fault is logged.
func docsEditorContain() {
	if fault := recover(); fault != nil {
		js.Global().Get("console").Call("error", "docs editor:", fmt.Sprint(fault))
	}
}

func (b *docsEditorBrowser) listen(name string, capture bool, handler func(js.Value)) {
	fn := js.FuncOf(func(_ js.Value, args []js.Value) any {
		defer docsEditorContain()
		if len(args) > 0 {
			handler(args[0])
		}
		return nil
	})
	js.Global().Get("document").Call("addEventListener", name, fn, capture)
	b.listeners = append(b.listeners, docsEditorListener{name: name, fn: fn, capture: capture})
}

// docsEditorEnsure runs after every render. GWC can rebuild an element's
// children when its siblings change; if that replaced a field or emptied
// the formatted pane, the text goes back in (never into a focused field,
// which holds the person's newest text).
func docsEditorEnsure(c *docsEditorController) {
	defer docsEditorContain()
	b := docsEditorBrowsers[c]
	if b == nil {
		return
	}
	if title := docsEditorElement("docs-editor-title"); title.Truthy() && !docsEditorFocused(title) && title.Get("value").String() != c.title {
		title.Set("value", c.title)
	}
	if source := docsEditorElement(docsEditorSourceID); source.Truthy() && !docsEditorFocused(source) && !b.sourcePending && source.Get("value").String() != c.markdown {
		source.Set("value", c.markdown)
	}
	rich := docsEditorElement(docsEditorRichID)
	if !rich.Truthy() || docsEditorWithin(rich, js.Global().Get("document").Get("activeElement")) {
		return
	}
	if !rich.Equal(b.richElement) || (rich.Get("innerHTML").String() == "" && strings.TrimSpace(c.markdown) != "") {
		b.renderRich(c.markdown, -1, -1, false)
	}
}

func docsEditorNarrow() bool {
	width := js.Global().Get("innerWidth").Float()
	if editor := docsEditorElement("docs-editor"); editor.Truthy() && editor.Get("clientWidth").Float() > 0 {
		width = editor.Get("clientWidth").Float()
	}
	return width < 900
}

// docsEditorFocus returns focus to a pane, with the formatted pane's last
// selection if it had one.
func docsEditorFocus(pane string) {
	defer docsEditorContain()
	id := docsEditorSourceID
	if pane == "rich" {
		id = docsEditorRichID
	}
	el := docsEditorElement(id)
	if !el.Truthy() {
		return
	}
	el.Call("focus", map[string]any{"preventScroll": true})
	if pane != "rich" {
		return
	}
	for _, b := range docsEditorBrowsers {
		if b.lastRange.Truthy() && docsEditorWithin(el, b.lastRange.Get("startContainer")) {
			selection := js.Global().Call("getSelection")
			selection.Call("removeAllRanges")
			selection.Call("addRange", b.lastRange)
		}
	}
}

func docsEditorCommandAt(event ui.Event) string {
	defer docsEditorContain()
	target := event.JSValue().Get("target")
	if !target.Truthy() || target.Get("closest").IsUndefined() {
		return ""
	}
	el := target.Call("closest", "[data-editor-cmd]")
	if !el.Truthy() || el.Get("disabled").Truthy() {
		return ""
	}
	return el.Get("dataset").Get("editorCmd").String()
}

// docsEditorToolbarKey moves focus along the toolbar with the arrow keys,
// Home and End, mirrored in a right-to-left layout.
func docsEditorToolbarKey(event ui.Event) {
	defer docsEditorContain()
	native := event.JSValue()
	key := native.Get("key").String()
	target := native.Get("target")
	if !target.Truthy() || target.Get("closest").IsUndefined() {
		return
	}
	toolbar := target.Call("closest", ".docs-editor-toolbar")
	if !toolbar.Truthy() {
		return
	}
	buttons := toolbar.Call("querySelectorAll", ".docs-editor-tool")
	count := buttons.Get("length").Int()
	current := -1
	for i := 0; i < count; i++ {
		if buttons.Index(i).Equal(target) {
			current = i
		}
	}
	if current < 0 || count == 0 {
		return
	}
	rtl := js.Global().Call("getComputedStyle", toolbar).Get("direction").String() == "rtl"
	next := current
	switch key {
	case "ArrowRight":
		next = current + 1
		if rtl {
			next = current - 1
		}
	case "ArrowLeft":
		next = current - 1
		if rtl {
			next = current + 1
		}
	case "Home":
		next = 0
	case "End":
		next = count - 1
	default:
		return
	}
	native.Call("preventDefault")
	next = (next + count) % count
	buttons.Index(next).Call("focus")
}

// --- sync --------------------------------------------------------------

func (b *docsEditorBrowser) schedule(pane string) {
	if pane == "source" {
		b.sourcePending = true
		js.Global().Call("clearTimeout", b.sourceTimer)
		b.sourceTimer = js.Global().Call("setTimeout", b.sourceTick, docsEditorDebounce)
		return
	}
	b.richPending = true
	js.Global().Call("clearTimeout", b.richTimer)
	b.richTimer = js.Global().Call("setTimeout", b.richTick, docsEditorDebounce)
}

func (b *docsEditorBrowser) flush() {
	if b.sourcePending {
		js.Global().Call("clearTimeout", b.sourceTimer)
		b.syncSource()
	}
	if b.richPending {
		js.Global().Call("clearTimeout", b.richTimer)
		b.syncRich()
	}
}

// syncSource carries the Markdown pane's text into the formatted pane.
func (b *docsEditorBrowser) syncSource() {
	b.sourcePending = false
	source := docsEditorElement(docsEditorSourceID)
	if !source.Truthy() {
		return
	}
	markdown := source.Get("value").String()
	if markdown == b.c.markdown {
		return
	}
	b.c.setMarkdown(markdown, true, false)
	rich := docsEditorElement(docsEditorRichID)
	if !docsEditorWithin(rich, js.Global().Get("document").Get("activeElement")) {
		b.renderRich(markdown, -1, -1, false)
	}
	b.queueFormats()
}

// syncRich carries the formatted pane's text into the Markdown pane. The
// formatted pane is not re-rendered: it holds the caret.
func (b *docsEditorBrowser) syncRich() {
	b.richPending = false
	rich := docsEditorElement(docsEditorRichID)
	if !rich.Truthy() {
		return
	}
	markdown := docsEditorStripMarks(docsEditorMarkdown(docsEditorTreeOf(rich)))
	if markdown == b.c.markdown {
		return
	}
	b.c.setMarkdown(markdown, true, false)
	b.rendered = ""
	b.writeSource(markdown)
	b.queueFormats()
}

// writeSource replaces the textarea's text, keeping its selection and
// scroll position where they were.
func (b *docsEditorBrowser) writeSource(markdown string) {
	source := docsEditorElement(docsEditorSourceID)
	if !source.Truthy() || source.Get("value").String() == markdown {
		return
	}
	start, end := source.Get("selectionStart").Int(), source.Get("selectionEnd").Int()
	top := source.Get("scrollTop")
	source.Set("value", markdown)
	length := docsEditorByteToUTF16(markdown, len(markdown))
	source.Call("setSelectionRange", min(start, length), min(end, length))
	source.Set("scrollTop", top)
}

// renderRich replaces the formatted pane's content. With a selection it
// renders marks at the selection's ends, then removes them and selects
// the text between.
func (b *docsEditorBrowser) renderRich(markdown string, start, end int, focus bool) {
	rich := docsEditorElement(docsEditorRichID)
	if !rich.Truthy() {
		return
	}
	source := markdown
	if start >= 0 {
		source = docsEditorInsertMarks(markdown, start, end)
	}
	top := rich.Get("scrollTop")
	rich.Set("innerHTML", docsEditorHTML(b.c.locale, source))
	rich.Set("scrollTop", top)
	b.rendered = markdown
	b.richElement = rich
	if start < 0 {
		return
	}
	doc := js.Global().Get("document")
	walker := doc.Call("createTreeWalker", rich, 4)
	var startNode, endNode js.Value
	startOffset, endOffset := 0, 0
	var emptied []js.Value
	for node := walker.Call("nextNode"); node.Truthy(); node = walker.Call("nextNode") {
		data := node.Get("data").String()
		if !strings.ContainsAny(data, string([]rune{docsEditorMarkStart, docsEditorMarkEnd})) {
			continue
		}
		var clean strings.Builder
		units := 0
		for _, r := range data {
			switch r {
			case docsEditorMarkStart:
				startNode, startOffset = node, units
			case docsEditorMarkEnd:
				endNode, endOffset = node, units
			default:
				clean.WriteRune(r)
				units++
				if r >= 0x10000 {
					units++
				}
			}
		}
		node.Set("data", clean.String())
		if clean.Len() == 0 {
			emptied = append(emptied, node)
		}
	}
	if focus {
		rich.Call("focus", map[string]any{"preventScroll": true})
	}
	if !startNode.Truthy() && !endNode.Truthy() {
		return
	}
	if !startNode.Truthy() {
		startNode, startOffset = endNode, endOffset
	}
	if !endNode.Truthy() {
		endNode, endOffset = startNode, startOffset
	}
	r := doc.Call("createRange")
	// A paragraph left holding only the marks would collapse to nothing;
	// a line break gives the caret a line to sit on.
	for _, node := range emptied {
		parent := node.Get("parentNode")
		if parent.Truthy() && strings.TrimSpace(parent.Get("textContent").String()) == "" && parent.Get("childNodes").Get("length").Int() == 1 {
			parent.Call("appendChild", doc.Call("createElement", "br"))
		}
	}
	r.Call("setStart", startNode, startOffset)
	r.Call("setEnd", endNode, endOffset)
	selection := js.Global().Call("getSelection")
	selection.Call("removeAllRanges")
	selection.Call("addRange", r)
	b.lastRange = r.Call("cloneRange")
	if parent := startNode.Get("parentElement"); parent.Truthy() {
		parent.Call("scrollIntoView", map[string]any{"block": "nearest"})
	}
}

// --- selection across the panes -----------------------------------------

func (b *docsEditorBrowser) activePane() string {
	switch b.c.mode {
	case "source":
		return "source"
	case "rich":
		return "rich"
	}
	return b.c.pane
}

func docsEditorCaptureSelection(c *docsEditorController) (out docsEditorCapture) {
	b := docsEditorBrowsers[c]
	fallback := docsEditorCapture{Markdown: c.markdown, Start: len(c.markdown), End: len(c.markdown), Pane: c.pane}
	defer func() {
		if fault := recover(); fault != nil {
			js.Global().Get("console").Call("error", "docs editor:", fmt.Sprint(fault))
			out = fallback
		}
	}()
	if b == nil {
		return fallback
	}
	b.flush()
	pane := b.activePane()
	fallback.Pane = pane
	if pane == "source" {
		source := docsEditorElement(docsEditorSourceID)
		if !source.Truthy() {
			return fallback
		}
		value := source.Get("value").String()
		start := docsEditorUTF16ToByte(value, source.Get("selectionStart").Int())
		end := docsEditorUTF16ToByte(value, source.Get("selectionEnd").Int())
		return docsEditorCapture{Markdown: value, Start: start, End: end, Pane: "source"}
	}
	rich := docsEditorElement(docsEditorRichID)
	if !rich.Truthy() {
		return fallback
	}
	var r js.Value
	selection := js.Global().Call("getSelection")
	if selection.Truthy() && selection.Get("rangeCount").Int() > 0 && docsEditorWithin(rich, selection.Call("getRangeAt", 0).Get("commonAncestorContainer")) {
		r = selection.Call("getRangeAt", 0).Call("cloneRange")
	} else if b.lastRange.Truthy() && docsEditorWithin(rich, b.lastRange.Get("startContainer")) {
		r = b.lastRange.Call("cloneRange")
	} else {
		return fallback
	}
	doc := js.Global().Get("document")
	docsEditorSnapToText(rich, r)
	endMark := doc.Call("createTextNode", string(docsEditorMarkEnd))
	startMark := doc.Call("createTextNode", string(docsEditorMarkStart))
	endAt := r.Call("cloneRange")
	endAt.Call("collapse", false)
	endAt.Call("insertNode", endMark)
	startAt := r.Call("cloneRange")
	startAt.Call("collapse", true)
	startAt.Call("insertNode", startMark)
	marked := docsEditorMarkdown(docsEditorTreeOf(rich))
	startMark.Call("remove")
	endMark.Call("remove")
	rich.Call("normalize")
	clean, start, end := docsEditorExtractMarks(marked)
	return docsEditorCapture{Markdown: clean, Start: start, End: end, Pane: "rich"}
}

// docsEditorStructural are elements whose children are rows or items, not
// text: a mark placed directly inside one would become an item of its own.
var docsEditorStructural = map[string]bool{"ul": true, "ol": true, "table": true, "thead": true, "tbody": true, "tfoot": true, "tr": true}

// docsEditorSnapToText moves a range end that sits between list items or
// table rows into the nearest text: a caret goes to the end of the text
// before it, a range's start to the text after it and its end to the text
// before it. Ends inside paragraphs, even empty ones, stay where they are.
func docsEditorSnapToText(root, r js.Value) {
	collapsed := r.Get("collapsed").Bool()
	snap := func(which string, preferAfter bool) {
		container := r.Get(which + "Container")
		if container.Get("nodeType").Int() != 1 || !docsEditorStructural[strings.ToLower(container.Get("nodeName").String())] {
			return
		}
		point := js.Global().Get("document").Call("createRange")
		point.Call("setStart", container, r.Get(which+"Offset"))
		walker := js.Global().Get("document").Call("createTreeWalker", root, 4)
		var before, after js.Value
		for node := walker.Call("nextNode"); node.Truthy(); node = walker.Call("nextNode") {
			if point.Call("comparePoint", node, 0).Int() >= 0 {
				after = node
				break
			}
			before = node
		}
		set := "setStart"
		if which == "end" {
			set = "setEnd"
		}
		switch {
		case preferAfter && after.Truthy():
			r.Call(set, after, 0)
		case before.Truthy():
			r.Call(set, before, before.Get("length"))
		case after.Truthy():
			r.Call(set, after, 0)
		}
	}
	if collapsed {
		snap("start", false)
		r.Call("collapse", true)
		return
	}
	snap("start", true)
	snap("end", false)
}

func docsEditorCommit(c *docsEditorController, markdown string, start, end int, pane string) {
	docsEditorApplyText(c, markdown, start, end, pane, false)
}

func docsEditorCommitHistory(c *docsEditorController, markdown string, at int) {
	docsEditorApplyText(c, markdown, at, at, c.pane, true)
}

// docsEditorApplyText writes a new text into both panes after a command,
// a paste or an undo, and puts the selection back in the pane in use.
func docsEditorApplyText(c *docsEditorController, markdown string, start, end int, pane string, fromHistory bool) {
	c.setMarkdown(markdown, false, fromHistory)
	defer docsEditorContain()
	b := docsEditorBrowsers[c]
	if b == nil {
		return
	}
	if b.c.mode == "source" {
		pane = "source"
	} else if b.c.mode == "rich" {
		pane = "rich"
	}
	source := docsEditorElement(docsEditorSourceID)
	if source.Truthy() && source.Get("value").String() != markdown {
		top := source.Get("scrollTop")
		source.Set("value", markdown)
		source.Set("scrollTop", top)
	}
	if pane == "source" {
		b.renderRich(markdown, -1, -1, false)
		if source.Truthy() {
			source.Call("focus", map[string]any{"preventScroll": true})
			source.Call("setSelectionRange", docsEditorByteToUTF16(markdown, start), docsEditorByteToUTF16(markdown, end))
		}
	} else {
		b.renderRich(markdown, start, end, true)
	}
	c.pane = pane
	b.queueFormats()
}

// --- listeners -----------------------------------------------------------

func (b *docsEditorBrowser) onInput(event js.Value) {
	target := event.Get("target")
	if !target.Truthy() {
		return
	}
	if target.Get("id").String() == docsEditorSourceID {
		b.c.pane = "source"
		b.schedule("source")
		return
	}
	if docsEditorWithin(docsEditorElement(docsEditorRichID), target) {
		b.c.pane = "rich"
		b.schedule("rich")
	}
}

func (b *docsEditorBrowser) onFocusIn(event js.Value) {
	target := event.Get("target")
	switch {
	case target.Get("id").String() == docsEditorSourceID:
		b.c.pane = "source"
	case docsEditorWithin(docsEditorElement(docsEditorRichID), target):
		b.c.pane = "rich"
	default:
		return
	}
	b.queueFormats()
}

func (b *docsEditorBrowser) onKeyDown(event js.Value) {
	target := event.Get("target")
	editor := docsEditorElement("docs-editor")
	if !docsEditorWithin(editor, target) {
		return
	}
	key := strings.ToLower(event.Get("key").String())
	if key == "escape" {
		if b.c.onEscape != nil {
			b.c.onEscape()
		}
		return
	}
	if !(event.Get("ctrlKey").Bool() || event.Get("metaKey").Bool()) || event.Get("altKey").Bool() {
		return
	}
	inPane := target.Get("id").String() == docsEditorSourceID || docsEditorWithin(docsEditorElement(docsEditorRichID), target)
	shift := event.Get("shiftKey").Bool()
	command := ""
	switch {
	case key == "s":
		event.Call("preventDefault")
		if b.c.onSave != nil {
			b.c.onSave()
		}
		return
	case !inPane:
		return
	case key == "b" && !shift:
		command = "bold"
	case key == "i" && !shift:
		command = "italic"
	case key == "k" && !shift:
		event.Call("preventDefault")
		if b.c.onLink != nil {
			b.c.onLink()
		}
		return
	case key == "z":
		event.Call("preventDefault")
		if shift {
			b.c.undo(1)
		} else {
			b.c.undo(-1)
		}
		return
	case key == "y" && !shift:
		event.Call("preventDefault")
		b.c.undo(1)
		return
	default:
		return
	}
	event.Call("preventDefault")
	b.c.apply(command, "")
}

// onPaste turns whatever is pasted into the formatted pane into Markdown
// first, so styles, scripts and stray markup never enter the document.
// Pasted HTML is parsed into an inert document that runs and loads nothing.
func (b *docsEditorBrowser) onPaste(event js.Value) {
	rich := docsEditorElement(docsEditorRichID)
	if !docsEditorWithin(rich, event.Get("target")) {
		return
	}
	data := event.Get("clipboardData")
	if !data.Truthy() {
		return
	}
	event.Call("preventDefault")
	b.insertTransfer(data)
}

func (b *docsEditorBrowser) onDrop(event js.Value) {
	rich := docsEditorElement(docsEditorRichID)
	if !docsEditorWithin(rich, event.Get("target")) {
		return
	}
	data := event.Get("dataTransfer")
	if !data.Truthy() {
		return
	}
	event.Call("preventDefault")
	doc := js.Global().Get("document")
	if doc.Get("caretRangeFromPoint").Truthy() {
		if r := doc.Call("caretRangeFromPoint", event.Get("clientX"), event.Get("clientY")); r.Truthy() && docsEditorWithin(rich, r.Get("startContainer")) {
			selection := js.Global().Call("getSelection")
			selection.Call("removeAllRanges")
			selection.Call("addRange", r)
		}
	}
	b.insertTransfer(data)
}

func (b *docsEditorBrowser) insertTransfer(data js.Value) {
	fragment := ""
	if markup := data.Call("getData", "text/html").String(); strings.TrimSpace(markup) != "" {
		parsed := js.Global().Get("DOMParser").New().Call("parseFromString", markup, "text/html")
		if body := parsed.Get("body"); body.Truthy() {
			fragment = docsEditorStripMarks(docsEditorMarkdown(docsEditorTreeOf(body)))
		}
	}
	if strings.TrimSpace(fragment) == "" {
		fragment = docsEditorPlainMarkdown(docsEditorStripMarks(data.Call("getData", "text/plain").String()))
	}
	if fragment == "" {
		return
	}
	b.c.pane = "rich"
	capture := docsEditorCaptureSelection(b.c)
	next, start, end := docsEditorInsertFragment(capture.Markdown, capture.Start, capture.End, fragment)
	docsEditorCommit(b.c, next, start, end, capture.Pane)
}

// onScroll keeps the other pane at the same proportion of its length.
// The pane moved in response is ignored for a moment so the two do not
// chase each other.
func (b *docsEditorBrowser) onScroll(event js.Value) {
	if b.c.mode != "split" {
		return
	}
	target := event.Get("target")
	if !target.Truthy() || target.Get("id").IsUndefined() {
		return
	}
	id := target.Get("id").String()
	other := ""
	switch id {
	case docsEditorSourceID:
		other = docsEditorRichID
	case docsEditorRichID:
		other = docsEditorSourceID
	default:
		return
	}
	if time.Now().Before(b.scrollQuiet[id]) {
		return
	}
	peer := docsEditorElement(other)
	if !peer.Truthy() {
		return
	}
	span := target.Get("scrollHeight").Float() - target.Get("clientHeight").Float()
	peerSpan := peer.Get("scrollHeight").Float() - peer.Get("clientHeight").Float()
	if span <= 0 || peerSpan <= 0 {
		return
	}
	next := target.Get("scrollTop").Float() / span * peerSpan
	if diff := next - peer.Get("scrollTop").Float(); diff > 1 || diff < -1 {
		b.scrollQuiet[other] = time.Now().Add(120 * time.Millisecond)
		peer.Set("scrollTop", next)
	}
}

// --- pressed states --------------------------------------------------------

func (b *docsEditorBrowser) queueFormats() {
	if b.formatsQueued {
		return
	}
	b.formatsQueued = true
	var tick js.Func
	tick = js.FuncOf(func(js.Value, []js.Value) any {
		defer docsEditorContain()
		tick.Release()
		b.formatsQueued = false
		b.updateFormats()
		return nil
	})
	js.Global().Call("requestAnimationFrame", tick)
}

func (b *docsEditorBrowser) updateFormats() {
	if b.c.onFormats == nil {
		return
	}
	doc := js.Global().Get("document")
	active := doc.Get("activeElement")
	source := docsEditorElement(docsEditorSourceID)
	rich := docsEditorElement(docsEditorRichID)
	switch {
	case docsEditorFocused(source) || (b.c.pane == "source" && !docsEditorWithin(rich, active)):
		if !source.Truthy() {
			return
		}
		value := source.Get("value").String()
		at := docsEditorUTF16ToByte(value, source.Get("selectionStart").Int())
		b.c.onFormats(docsEditorFormatsKey(docsEditorFormatsAt(value, at)))
	default:
		selection := js.Global().Call("getSelection")
		if !selection.Truthy() || selection.Get("rangeCount").Int() == 0 {
			return
		}
		r := selection.Call("getRangeAt", 0)
		if !docsEditorWithin(rich, r.Get("startContainer")) {
			return
		}
		b.lastRange = r.Call("cloneRange")
		var ancestors []*docsEditorNode
		for node := r.Get("startContainer"); node.Truthy() && !node.Equal(rich); node = node.Get("parentNode") {
			if node.Get("nodeType").Int() != 1 {
				continue
			}
			n := &docsEditorNode{Tag: strings.ToLower(node.Get("nodeName").String()), Attrs: map[string]string{"class": node.Get("className").String()}}
			if n.Tag == "li" && node.Call("querySelector", ":scope > input[type=checkbox], :scope > p > input[type=checkbox]").Truthy() {
				n.Attrs["class"] += " docs-editor-task"
			}
			ancestors = append(ancestors, n)
		}
		b.c.onFormats(docsEditorFormatsKey(docsEditorFormatsOfAncestors(ancestors)))
	}
}

// --- DOM → tree ------------------------------------------------------------

var docsEditorKeptAttributes = []string{"class", "href", "title", "start", "type", "align", "src", "alt", "data-md-raw", "data-md-image", "data-md-title"}

// docsEditorTreeOf copies an element's children into the serializer's
// node tree. Only the attributes the serializer reads are copied; a
// checkbox's live state is read from its property, not its attribute.
func docsEditorTreeOf(el js.Value) *docsEditorNode {
	root := &docsEditorNode{Tag: "div"}
	root.Children = docsEditorChildNodes(el, 0)
	return root
}

func docsEditorChildNodes(el js.Value, depth int) []*docsEditorNode {
	if depth > 64 {
		return nil
	}
	children := el.Get("childNodes")
	count := children.Get("length").Int()
	out := make([]*docsEditorNode, 0, count)
	for i := 0; i < count; i++ {
		child := children.Index(i)
		switch child.Get("nodeType").Int() {
		case 3:
			out = append(out, &docsEditorNode{Text: child.Get("data").String()})
		case 1:
			node := &docsEditorNode{Tag: strings.ToLower(child.Get("nodeName").String())}
			for _, name := range docsEditorKeptAttributes {
				if child.Call("hasAttribute", name).Bool() {
					if node.Attrs == nil {
						node.Attrs = map[string]string{}
					}
					node.Attrs[name] = child.Call("getAttribute", name).String()
				}
			}
			if node.Tag == "input" {
				if node.Attrs == nil {
					node.Attrs = map[string]string{}
				}
				delete(node.Attrs, "checked")
				if child.Get("checked").Bool() {
					node.Attrs["checked"] = ""
				}
			}
			if !docsEditorSkipTags[node.Tag] {
				node.Children = docsEditorChildNodes(child, depth+1)
			}
			out = append(out, node)
		}
	}
	return out
}
