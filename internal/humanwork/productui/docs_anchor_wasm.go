//go:build js && wasm

package productui

import (
	"strconv"
	"strings"
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// docsAnchorRootID is the rendered Markdown element comments anchor into.
const docsAnchorRootID = "docs-markdown"

// docsTextIndex is the reader's text flattened in document order, with each
// text node's starting offset, so a DOM range and a character span can be
// converted either way.
type docsTextIndex struct {
	nodes  []js.Value
	starts []int
	text   string
}

func docsIndexText() (docsTextIndex, bool) {
	doc := js.Global().Get("document")
	root := doc.Call("getElementById", docsAnchorRootID)
	if !root.Truthy() {
		return docsTextIndex{}, false
	}
	walker := doc.Call("createTreeWalker", root, 4) // NodeFilter.SHOW_TEXT
	var index docsTextIndex
	var b strings.Builder
	runes := 0
	for node := walker.Call("nextNode"); node.Truthy(); node = walker.Call("nextNode") {
		value := node.Get("data").String()
		index.nodes = append(index.nodes, node)
		index.starts = append(index.starts, runes)
		runes += len([]rune(value))
		b.WriteString(value)
	}
	index.text = b.String()
	return index, true
}

// offsetOf maps a (text node, offset) boundary to a character offset.
func (index docsTextIndex) offsetOf(node js.Value, offset int) (int, bool) {
	for i, candidate := range index.nodes {
		if candidate.Equal(node) {
			return index.starts[i] + offset, true
		}
	}
	return 0, false
}

// rangeFor maps a character span back to a DOM Range.
func (index docsTextIndex) rangeFor(start, end int) (js.Value, bool) {
	if start < 0 || end <= start || len(index.nodes) == 0 {
		return js.Value{}, false
	}
	locate := func(at int) (js.Value, int, bool) {
		for i := len(index.nodes) - 1; i >= 0; i-- {
			if index.starts[i] <= at {
				length := len([]rune(index.nodes[i].Get("data").String()))
				if at-index.starts[i] <= length {
					return index.nodes[i], at - index.starts[i], true
				}
				return js.Value{}, 0, false
			}
		}
		return js.Value{}, 0, false
	}
	startNode, startOffset, ok := locate(start)
	if !ok {
		return js.Value{}, false
	}
	endNode, endOffset, ok := locate(end)
	if !ok {
		return js.Value{}, false
	}
	r := js.Global().Get("document").Call("createRange")
	r.Call("setStart", startNode, startOffset)
	r.Call("setEnd", endNode, endOffset)
	return r, true
}

// docsCurrentSelection returns the selected passage inside the reader with
// up to 48 characters of context either side, or ok=false.
func docsCurrentSelection() (quote, prefix, suffix string, ok bool) {
	selection := js.Global().Call("getSelection")
	if !selection.Truthy() || selection.Get("isCollapsed").Bool() || selection.Get("rangeCount").Int() == 0 {
		return "", "", "", false
	}
	r := selection.Call("getRangeAt", 0)
	index, found := docsIndexText()
	if !found {
		return "", "", "", false
	}
	root := js.Global().Get("document").Call("getElementById", docsAnchorRootID)
	if !root.Call("contains", r.Get("commonAncestorContainer")).Bool() {
		return "", "", "", false
	}
	start, okStart := index.offsetOf(r.Get("startContainer"), r.Get("startOffset").Int())
	end, okEnd := index.offsetOf(r.Get("endContainer"), r.Get("endOffset").Int())
	if !okStart || !okEnd || end <= start {
		return "", "", "", false
	}
	runes := []rune(index.text)
	quote = strings.TrimSpace(string(runes[start:end]))
	if quote == "" || len([]rune(quote)) > 500 {
		return "", "", "", false
	}
	prefix = string(runes[max(0, start-48):start])
	suffix = string(runes[end:min(len(runes), end+48)])
	return quote, prefix, suffix, true
}

// docsSelectionFromChatProjection reports whether the selected range touches
// live Chat content. That content is expanded from a reference and is not in
// the document version's Markdown, which the comment service validates quoted
// anchors against.
func docsSelectionFromChatProjection() bool {
	selection := js.Global().Call("getSelection")
	if !selection.Truthy() || selection.Get("rangeCount").Int() == 0 {
		return false
	}
	r := selection.Call("getRangeAt", 0)
	projections := js.Global().Get("document").Call("querySelectorAll", "#docs-markdown .docs-chat-quote,#docs-markdown .docs-chat-chip")
	for i := 0; i < projections.Get("length").Int(); i++ {
		if r.Call("intersectsNode", projections.Index(i)).Bool() {
			return true
		}
	}
	return false
}

// docsLocateQuote finds the best occurrence of quote using its context.
func docsLocateQuote(text []rune, quote, prefix, suffix string) (int, int, bool) {
	q := []rune(quote)
	if len(q) == 0 {
		return 0, 0, false
	}
	best, bestScore := -1, -1
	for i := 0; i+len(q) <= len(text); i++ {
		// Compare runes in place: building a string at every offset made
		// painting the anchors allocate once per character per comment.
		if text[i] != q[0] || !docsRunesEqual(text[i:i+len(q)], q) {
			continue
		}
		score := 0
		before := string(text[max(0, i-len([]rune(prefix))):i])
		after := string(text[i+len(q) : min(len(text), i+len(q)+len([]rune(suffix)))])
		if prefix != "" && strings.HasSuffix(before, strings.TrimLeft(prefix, " ")) {
			score += 2
		}
		if suffix != "" && strings.HasPrefix(after, strings.TrimRight(suffix, " ")) {
			score += 2
		}
		if score > bestScore {
			best, bestScore = i, score
		}
	}
	if best < 0 {
		return 0, 0, false
	}
	return best, best + len(q), true
}

func docsRunesEqual(a, b []rune) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

var docsAnchorRanges = map[string]js.Value{}

// docsPaintAnchors highlights every anchored passage with the CSS Custom
// Highlight API. Nothing is inserted into the rendered Markdown, so the
// reconciler's DOM stays exactly as it rendered it.
func docsPaintAnchors(anchors []DocumentComment, active string) {
	css := js.Global().Get("CSS")
	if !css.Truthy() || !css.Get("highlights").Truthy() || !js.Global().Get("Highlight").Truthy() {
		return
	}
	index, ok := docsIndexText()
	if !ok {
		return
	}
	text := []rune(index.text)
	docsAnchorRanges = map[string]js.Value{}
	all := js.Global().Get("Highlight").New()
	current := js.Global().Get("Highlight").New()
	for _, comment := range anchors {
		if comment.Quote == "" || comment.Resolved {
			continue
		}
		start, end, found := docsLocateQuote(text, comment.Quote, comment.Prefix, comment.Suffix)
		if !found {
			continue
		}
		r, ok := index.rangeFor(start, end)
		if !ok {
			continue
		}
		docsAnchorRanges[comment.ID] = r
		all.Call("add", r)
		if comment.ID == active {
			current.Call("add", r)
		}
	}
	css.Get("highlights").Call("set", "docs-anchor", all)
	css.Get("highlights").Call("set", "docs-anchor-active", current)
}

// docsRevealAnchor scrolls a comment's passage into view.
func docsRevealAnchor(id string) bool {
	r, ok := docsAnchorRanges[id]
	if !ok {
		return false
	}
	node := r.Get("startContainer")
	if node.Get("nodeType").Int() == 3 {
		node = node.Get("parentElement")
	}
	if node.Truthy() {
		node.Call("scrollIntoView", map[string]any{"block": "center", "behavior": "smooth"})
		return true
	}
	return false
}

// docsAnchorAt names the comment whose passage contains the clicked point.
func docsAnchorAt(event ui.Event) string {
	native := event.JSValue()
	x, y := native.Get("clientX").Float(), native.Get("clientY").Float()
	for id, r := range docsAnchorRanges {
		rects := r.Call("getClientRects")
		for i := 0; i < rects.Get("length").Int(); i++ {
			rect := rects.Index(i)
			if x >= rect.Get("left").Float() && x <= rect.Get("right").Float() && y >= rect.Get("top").Float() && y <= rect.Get("bottom").Float() {
				return id
			}
		}
	}
	return ""
}

// docsWatchSelection shows the floating Comment button beside a selection
// in the reader and hides it otherwise. The button is positioned through
// the CSSOM, which the content security policy allows.
func docsWatchSelection() func() {
	doc := js.Global().Get("document")
	handler := js.FuncOf(func(js.Value, []js.Value) any {
		button := doc.Call("getElementById", "docs-select-comment")
		if !button.Truthy() {
			return nil
		}
		_, _, _, ok := docsCurrentSelection()
		if !ok {
			button.Get("classList").Call("remove", "is-visible")
			return nil
		}
		r := js.Global().Call("getSelection").Call("getRangeAt", 0)
		top, left := docsSelectButtonPosition(button, r.Call("getBoundingClientRect"))
		style := button.Get("style")
		style.Call("setProperty", "top", strconv.FormatFloat(top, 'f', 1, 64)+"px")
		style.Call("setProperty", "left", strconv.FormatFloat(left, 'f', 1, 64)+"px")
		button.Get("classList").Call("add", "is-visible")
		return nil
	})
	// Keyboard readers select with Shift and the arrows; Ctrl+Alt+M (or
	// Cmd+Alt+M) presses the Comment button for them, since the button
	// itself stays out of the tab order.
	shortcut := js.FuncOf(func(_ js.Value, args []js.Value) any {
		event := args[0]
		if !event.Get("altKey").Bool() || !(event.Get("ctrlKey").Bool() || event.Get("metaKey").Bool()) || event.Get("code").String() != "KeyM" {
			return nil
		}
		button := doc.Call("getElementById", "docs-select-comment")
		if !button.Truthy() || !button.Get("classList").Call("contains", "is-visible").Bool() {
			return nil
		}
		event.Call("preventDefault")
		button.Call("click")
		return nil
	})
	doc.Call("addEventListener", "selectionchange", handler)
	doc.Call("addEventListener", "keydown", shortcut)
	return func() {
		doc.Call("removeEventListener", "selectionchange", handler)
		doc.Call("removeEventListener", "keydown", shortcut)
		handler.Release()
		shortcut.Release()
	}
}

// docsSelectButtonPosition places the Comment button centred 8 px above the
// selection (below it when there is no room above), kept inside the reader.
// The button is absolutely positioned, so the result is in its offset
// parent's coordinates: the shell's scrolling main region, not the window.
func docsSelectButtonPosition(button, rect js.Value) (top, left float64) {
	height := button.Get("offsetHeight").Float()
	width := button.Get("offsetWidth").Float()
	y := rect.Get("top").Float() - height - 8
	visibleTop := 8.0
	if scroller := js.Global().Get("document").Call("querySelector", ".main-scroll"); scroller.Truthy() {
		visibleTop = max(visibleTop, scroller.Call("getBoundingClientRect").Get("top").Float()+8)
	}
	if y < visibleTop {
		y = rect.Get("bottom").Float() + 8
	}
	x := rect.Get("left").Float() + rect.Get("width").Float()/2
	if reader := js.Global().Get("document").Call("getElementById", "docs-reader-box"); reader.Truthy() {
		box := reader.Call("getBoundingClientRect")
		low, high := box.Get("left").Float()+width/2+4, box.Get("right").Float()-width/2-4
		if low <= high {
			x = min(max(x, low), high)
		}
	}
	parent := button.Get("offsetParent")
	if !parent.Truthy() || parent.Equal(js.Global().Get("document").Get("body")) {
		return y + js.Global().Get("scrollY").Float(), x + js.Global().Get("scrollX").Float()
	}
	origin := parent.Call("getBoundingClientRect")
	top = y - origin.Get("top").Float() - parent.Get("clientTop").Float() + parent.Get("scrollTop").Float()
	left = x - origin.Get("left").Float() - parent.Get("clientLeft").Float() + parent.Get("scrollLeft").Float()
	return top, left
}

func docsClearSelection() {
	if selection := js.Global().Call("getSelection"); selection.Truthy() {
		selection.Call("removeAllRanges")
	}
}

func docsFocusElement(id string) {
	if el := js.Global().Get("document").Call("getElementById", id); el.Truthy() {
		el.Call("focus", map[string]any{"preventScroll": false})
	}
}

// docsLinkState is what the scroll and resize listener redraws: the numbered
// pins in the reader's margin and the connector from the linked thread's
// quote to its passage.
// The listeners themselves belong to docsWatchLayout, which the document
// page installs and removes with its own lifetime.
var docsLinkState struct {
	ids    []string
	linked string
}

// docsLayoutAnchors places each pin beside the first line of its passage.
// Pins that would overlap are nudged down so every number stays readable.
func docsLayoutAnchors(ids []string, linked string) {
	docsLinkState.ids, docsLinkState.linked = ids, linked
	docsPlacePins()
	docsDrawConnector()
}

// docsPlaceGutter moves the pin column to the end of the text column, so a
// pin sits beside the line it numbers instead of at the far edge of a wide
// reader. It is clamped inside the reader.
func docsPlaceGutter(box js.Value) {
	doc := js.Global().Get("document")
	gutter := box.Call("querySelector", ".docs-anchor-gutter")
	column := doc.Call("querySelector", "#docs-markdown > p")
	if !column.Truthy() {
		column = doc.Call("getElementById", docsAnchorRootID)
	}
	if !gutter.Truthy() || !column.Truthy() {
		return
	}
	// The text's own direction decides which side it ends on (an English
	// document in the Arabic interface ends on the right); the reader's
	// direction decides which edge inset-inline-start is measured from.
	outer, text := box.Call("getBoundingClientRect"), column.Call("getBoundingClientRect")
	width, border := gutter.Get("offsetWidth").Float(), box.Get("clientLeft").Float()
	inner, innerEnd := outer.Get("left").Float()+border, outer.Get("right").Float()-border
	x := text.Get("right").Float() + 4
	if js.Global().Call("getComputedStyle", column).Get("direction").String() == "rtl" {
		x = text.Get("left").Float() - 4 - width
	}
	x = max(inner+4, min(x, innerEnd-width-4))
	start := x - inner
	if js.Global().Call("getComputedStyle", box).Get("direction").String() == "rtl" {
		start = innerEnd - (x + width)
	}
	gutter.Get("style").Call("setProperty", "inset-inline-start", strconv.FormatFloat(max(start, 0), 'f', 1, 64)+"px")
}

func docsPlacePins() {
	doc := js.Global().Get("document")
	box := doc.Call("getElementById", "docs-reader-box")
	if !box.Truthy() {
		return
	}
	docsPlaceGutter(box)
	origin := box.Call("getBoundingClientRect").Get("top").Float()
	last := -1e9
	// On a narrow reader the pin column runs under the Copy text button;
	// pins start below it there.
	if copyButton, gutter := box.Call("querySelector", ".docs-copy-text"), box.Call("querySelector", ".docs-anchor-gutter"); copyButton.Truthy() && gutter.Truthy() {
		c, g := copyButton.Call("getBoundingClientRect"), gutter.Call("getBoundingClientRect")
		if c.Get("left").Float() < g.Get("right").Float() && c.Get("right").Float() > g.Get("left").Float() {
			last = c.Get("bottom").Float() - origin + 4 - 28
		}
	}
	for _, id := range docsLinkState.ids {
		pin := doc.Call("getElementById", "docs-pin-"+id)
		if !pin.Truthy() {
			continue
		}
		r, ok := docsAnchorRanges[id]
		if !ok {
			pin.Get("classList").Call("add", "is-unplaced")
			continue
		}
		rects := r.Call("getClientRects")
		if rects.Get("length").Int() == 0 {
			continue
		}
		top := rects.Index(0).Get("top").Float() - origin
		if top < last+28 {
			top = last + 28
		}
		last = top
		pin.Get("style").Call("setProperty", "top", strconv.FormatFloat(top, 'f', 1, 64)+"px")
		pin.Get("classList").Call("remove", "is-unplaced")
	}
}

// docsDrawConnector draws a curve from the linked thread's quote to its
// passage when both are on screen side by side; in the stacked layout, or
// when either is scrolled away, the curve is hidden and the pin and
// highlight carry the link alone.
func docsDrawConnector() {
	doc := js.Global().Get("document")
	path := doc.Call("getElementById", "docs-connector-path")
	if !path.Truthy() {
		return
	}
	hide := func() { path.Call("setAttribute", "d", "") }
	id := docsLinkState.linked
	r, ok := docsAnchorRanges[id]
	quote := doc.Call("querySelector", "#docs-thread-"+id+" .docs-thread-quote")
	if id == "" || !ok || !quote.Truthy() {
		hide()
		return
	}
	rects := r.Call("getClientRects")
	if rects.Get("length").Int() == 0 {
		hide()
		return
	}
	passage := rects.Index(0)
	card := quote.Call("getBoundingClientRect")
	height := js.Global().Get("innerHeight").Float()
	rtl := doc.Get("documentElement").Call("getAttribute", "dir").String() == "rtl"
	x1 := passage.Get("right").Float()
	x2 := card.Get("left").Float()
	if rtl {
		x1, x2 = passage.Get("left").Float(), card.Get("right").Float()
	}
	y1 := passage.Get("top").Float() + passage.Get("height").Float()/2
	y2 := card.Get("top").Float() + card.Get("height").Float()/2
	sideBySide := (!rtl && x2 > x1+24) || (rtl && x1 > x2+24)
	if !sideBySide || y1 < 0 || y1 > height || y2 < 0 || y2 > height {
		hide()
		return
	}
	mid := (x1 + x2) / 2
	f := func(v float64) string { return strconv.FormatFloat(v, 'f', 1, 64) }
	path.Call("setAttribute", "d", "M"+f(x1)+" "+f(y1)+" C"+f(mid)+" "+f(y1)+" "+f(mid)+" "+f(y2)+" "+f(x2)+" "+f(y2))
}

// docsFlashAnchor briefly emphasises a passage after a jump so the eye
// lands on it.
func docsFlashAnchor(id string) {
	css := js.Global().Get("CSS")
	r, ok := docsAnchorRanges[id]
	if !ok || !css.Truthy() || !css.Get("highlights").Truthy() {
		return
	}
	flash := js.Global().Get("Highlight").New(r)
	css.Get("highlights").Call("set", "docs-anchor-flash", flash)
	var clear js.Func
	clear = js.FuncOf(func(js.Value, []js.Value) any {
		css.Get("highlights").Call("delete", "docs-anchor-flash")
		clear.Release()
		return nil
	})
	js.Global().Call("setTimeout", clear, 1200)
}

// docsLinkedAt reads which comment a pointer is over: a thread card in the
// rail, a pin, or a highlighted passage in the reader.
func docsLinkedAt(event ui.Event) string {
	target := event.JSValue().Get("target")
	if target.Truthy() && !target.Get("closest").IsUndefined() {
		if el := target.Call("closest", "[data-comment-id],[data-pin-id]"); el.Truthy() {
			if id := el.Get("dataset").Get("commentId"); id.Truthy() {
				return id.String()
			}
			if id := el.Get("dataset").Get("pinId"); id.Truthy() {
				return id.String()
			}
		}
	}
	return docsAnchorAt(event)
}

func docsScrollIntoView(id string) {
	if el := js.Global().Get("document").Call("getElementById", id); el.Truthy() {
		el.Call("scrollIntoView", map[string]any{"block": "nearest", "behavior": "smooth"})
		el.Call("focus", map[string]any{"preventScroll": true})
	}
}
