//go:build js && wasm

package main

import (
	"net/url"
	"strings"
	"syscall/js"

	projectclient "github.com/monstercameron/human-capital-management-suite/tools/uxqual/projectclient"
)

// Board interaction craft: the quick-filter search and toggle, keyboard
// shortcuts, the first-run "create" button, and the drop settle. Everything
// here reads and writes DOM attributes and classes only; board state stays
// in the URL and the loader, so no GWC state is written during a drag.

var (
	projectNavigate        func(string)
	projectKeysInstalled   bool
	projectSearchTimer     js.Value
	projectSearchTimerFunc js.Func
)

func bindProjectBoardKeys() {
	if projectKeysInstalled {
		return
	}
	document := js.Global().Get("document")
	if !document.Truthy() {
		return
	}
	projectKeysInstalled = true
	document.Call("addEventListener", "input", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 {
			if input := args[0].Get("target"); projectAttr(input, "id") == "projectui-board-search" {
				scheduleProjectSearch(input)
			}
		}
		return nil
	}))
	// A rows-per-page select's options are addresses.
	document.Call("addEventListener", "change", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		picker := args[0].Get("target")
		if projectAttr(picker, "data-projectui-action") != "page-size" || projectNavigate == nil {
			return nil
		}
		if href := picker.Get("value").String(); strings.HasPrefix(href, "/workspace/app/") {
			projectNavigate(href)
		}
		return nil
	}))
	document.Call("addEventListener", "submit", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		form := args[0].Get("target")
		if projectAttr(form, "data-projectui-action") != "board-search" {
			return nil
		}
		args[0].Call("preventDefault")
		if input := form.Call("querySelector", "input"); input.Truthy() {
			runProjectSearch(input)
		}
		return nil
	}))
	document.Call("addEventListener", "click", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		target := args[0].Get("target")
		switch {
		case projectClosest(target, `[data-projectui-action="toggle-filters"]`).Truthy():
			toggleProjectFilters(false)
		case projectClosest(target, `[data-projectui-action="open-create-task"]`).Truthy():
			openProjectCreateTask()
		case projectClosest(target, `[data-projectui-action="close-shortcuts"]`).Truthy():
			setProjectShortcutsOpen(false)
		case projectClosest(target, `[data-projectui-action="scroll-columns"]`).Truthy():
			scrollProjectColumns(projectClosest(target, `[data-projectui-action="scroll-columns"]`))
		}
		return nil
	}))
	document.Call("addEventListener", "keydown", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 {
			handleProjectBoardKey(args[0])
		}
		return nil
	}))
	// The edge scroll buttons follow the board's scroll position; a light
	// poll also catches boards that just rendered or resized.
	document.Call("addEventListener", "scroll", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 {
			if region := args[0].Get("target"); projectAttr(region, "class") != "" && region.Get("classList").Call("contains", "projectui-columns").Bool() {
				syncProjectScrollButtons(region)
			}
		}
		return nil
	}), true)
	js.Global().Call("setInterval", js.FuncOf(func(js.Value, []js.Value) any {
		frames := js.Global().Get("document").Call("querySelectorAll", ".projectui-columns-frame > .projectui-columns")
		for index := 0; index < frames.Get("length").Int(); index++ {
			syncProjectScrollButtons(frames.Index(index))
		}
		return nil
	}), 400)
}

// syncProjectScrollButtons shows each edge button only while the board can
// scroll further that way. In RTL scrollLeft runs from 0 to negative.
func syncProjectScrollButtons(region js.Value) {
	frame := region.Get("parentElement")
	if !frame.Truthy() {
		return
	}
	width := region.Get("scrollWidth").Float() - region.Get("clientWidth").Float()
	position := region.Get("scrollLeft").Float()
	if position < 0 {
		position = -position
	}
	canPrev, canNext := width > 4 && position > 4, width > 4 && position < width-4
	if projectAttr(frame, "data-can-prev") != boolText(canPrev) {
		frame.Call("setAttribute", "data-can-prev", boolText(canPrev))
	}
	if projectAttr(frame, "data-can-next") != boolText(canNext) {
		frame.Call("setAttribute", "data-can-next", boolText(canNext))
	}
}

// scrollProjectColumns scrolls the board by one column toward the button's
// side, mirrored in RTL.
func scrollProjectColumns(button js.Value) {
	frame := projectClosest(button, ".projectui-columns-frame")
	region := frame.Call("querySelector", ".projectui-columns")
	column := region.Call("querySelector", ".projectui-column")
	if !region.Truthy() || !column.Truthy() {
		return
	}
	step := column.Get("offsetWidth").Float() + 16
	if projectAttr(button, "data-dir") == "prev" {
		step = -step
	}
	if js.Global().Get("document").Get("documentElement").Get("dir").String() == "rtl" {
		step = -step
	}
	region.Call("scrollBy", map[string]any{"left": step, "behavior": "smooth"})
}

// scheduleProjectSearch debounces typing into the board search: the
// address updates after a short pause, so the loader reruns once.
func scheduleProjectSearch(input js.Value) {
	window := js.Global()
	if projectSearchTimer.Truthy() {
		window.Call("clearTimeout", projectSearchTimer)
	}
	if projectSearchTimerFunc.Truthy() {
		projectSearchTimerFunc.Release()
	}
	projectSearchTimerFunc = js.FuncOf(func(js.Value, []js.Value) any {
		projectSearchTimer = js.Undefined()
		runProjectSearch(input)
		return nil
	})
	projectSearchTimer = window.Call("setTimeout", projectSearchTimerFunc, 280)
}

func runProjectSearch(input js.Value) {
	form := projectClosest(input, `form[data-projectui-action="board-search"]`)
	base := projectAttr(form, "data-href")
	if base == "" || projectNavigate == nil || !input.Get("isConnected").Bool() {
		return
	}
	href := base
	if query := strings.TrimSpace(input.Get("value").String()); query != "" {
		separator := "&"
		if !strings.Contains(base, "?") {
			separator = "?"
		}
		href += separator + "q=" + url.QueryEscape(query)
	}
	if href != currentProductHref() {
		focused := js.Global().Get("document").Get("activeElement").Equal(input)
		projectNavigate(href)
		if focused {
			refocusProjectSearch(0)
		}
	}
}

// refocusProjectSearch keeps typing in the board search across the
// navigation its own text causes: if focus fell to the page while the
// board redrew, it goes back to the field with the caret at the end.
func refocusProjectSearch(attempt int) {
	if attempt > 45 {
		return
	}
	var callback js.Func
	callback = js.FuncOf(func(js.Value, []js.Value) any {
		defer callback.Release()
		document := js.Global().Get("document")
		input := document.Call("getElementById", "projectui-board-search")
		active := document.Get("activeElement")
		if input.Truthy() && !active.Equal(input) && (!active.Truthy() || active.Equal(document.Get("body"))) {
			input.Call("focus", map[string]any{"preventScroll": true})
			length := input.Get("value").Get("length").Int()
			input.Call("setSelectionRange", length, length)
		}
		refocusProjectSearch(attempt + 1)
		return nil
	})
	js.Global().Call("requestAnimationFrame", callback)
}

func projectPhone() bool {
	match := js.Global().Call("matchMedia", "(max-width:40rem)")
	return match.Truthy() && match.Get("matches").Bool()
}

// toggleProjectFilters flips the bar: on desktop it hides and shows the
// visible bar (F), on phone it opens and closes the bottom sheet. forceOpen
// only ever opens it.
func toggleProjectFilters(forceOpen bool) {
	section := js.Global().Get("document").Call("querySelector", ".projectui-filters")
	if !section.Truthy() {
		return
	}
	toggled := projectAttr(section, "data-filters-toggled") == "true"
	phone := projectPhone()
	// Shown now: on a phone only while toggled, on a desktop unless toggled.
	open := toggled
	if !phone {
		open = !toggled
	}
	if forceOpen && open {
		return
	}
	toggled = !toggled
	section.Call("setAttribute", "data-filters-toggled", boolText(toggled))
	expanded := toggled
	if !phone {
		expanded = !toggled
	}
	if button := section.Call("querySelector", ".projectui-filter-toggle"); button.Truthy() {
		button.Call("setAttribute", "aria-expanded", boolText(expanded))
		if !expanded {
			button.Call("focus", map[string]any{"preventScroll": true})
		}
	}
	if expanded {
		if input := section.Call("querySelector", "#projectui-board-search"); input.Truthy() && phone {
			input.Call("focus", map[string]any{"preventScroll": true})
		}
	}
}

func boolText(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func openProjectCreateTask() {
	document := js.Global().Get("document")
	forms := document.Call("querySelectorAll", "details.project-page-create-disclosure")
	for index := 0; index < forms.Get("length").Int(); index++ {
		details := forms.Index(index)
		if details.Call("querySelector", ".project-page-task-form").Truthy() {
			details.Set("open", true)
			details.Call("scrollIntoView", map[string]any{"block": "nearest"})
			return
		}
	}
}

func setProjectShortcutsOpen(open bool) {
	sheet := js.Global().Get("document").Call("getElementById", "projectui-shortcuts")
	if !sheet.Truthy() {
		return
	}
	sheet.Set("hidden", !open)
	if open {
		sheet.Call("focus", map[string]any{"preventScroll": true})
	}
}

// handleProjectTabKey moves between the Projects home tabs with the arrow
// keys, Home and End (mirrored in RTL); a tab activates when focused.
func handleProjectTabKey(event js.Value) bool {
	tab := projectClosest(event.Get("target"), `.project-page-tabs [role="tab"]`)
	if !tab.Truthy() {
		return false
	}
	tabs := projectClosest(tab, `[role="tablist"]`).Call("querySelectorAll", `[role="tab"]`)
	count := tabs.Get("length").Int()
	current := 0
	for index := 0; index < count; index++ {
		if tabs.Index(index).Equal(tab) {
			current = index
		}
	}
	step := 0
	switch event.Get("key").String() {
	case "ArrowRight":
		step = 1
	case "ArrowLeft":
		step = -1
	case "Home":
		step = -current
	case "End":
		step = count - 1 - current
	default:
		return false
	}
	if js.Global().Get("document").Get("documentElement").Get("dir").String() == "rtl" && (event.Get("key").String() == "ArrowRight" || event.Get("key").String() == "ArrowLeft") {
		step = -step
	}
	next := ((current+step)%count + count) % count
	event.Call("preventDefault")
	target := tabs.Index(next)
	target.Call("focus")
	if !target.Equal(tab) {
		target.Call("click")
	}
	return true
}

func handleProjectBoardKey(event js.Value) {
	if handleProjectTabKey(event) {
		return
	}
	document := js.Global().Get("document")
	board := document.Call("querySelector", ".projectui-board[data-projectui], .projectui-list[data-projectui]")
	if !board.Truthy() || document.Call("querySelector", "dialog[open]").Truthy() {
		return
	}
	key := event.Get("key").String()
	target := event.Get("target")
	sheet := document.Call("getElementById", "projectui-shortcuts")
	sheetOpen := sheet.Truthy() && !sheet.Get("hidden").Bool()
	if key == "Escape" {
		if sheetOpen {
			event.Call("preventDefault")
			setProjectShortcutsOpen(false)
			return
		}
		if section := document.Call("querySelector", ".projectui-filters"); section.Truthy() && projectPhone() && projectAttr(section, "data-filters-toggled") == "true" {
			event.Call("preventDefault")
			toggleProjectFilters(false)
		}
		return
	}
	if key == "?" && sheetOpen {
		event.Call("preventDefault")
		setProjectShortcutsOpen(false)
		return
	}
	tag := ""
	if target.Truthy() && target.Get("tagName").Type() == js.TypeString {
		tag = target.Get("tagName").String()
	}
	press := projectclient.KeyPress{
		Key: key, Ctrl: event.Get("ctrlKey").Bool(), Meta: event.Get("metaKey").Bool(), Alt: event.Get("altKey").Bool(),
		Repeat: event.Get("repeat").Bool(), IsComposing: event.Get("isComposing").Bool(),
		Target: projectclient.KeyTarget{
			Tag: tag, Editable: target.Truthy() && target.Get("isContentEditable").Truthy(),
			InOverlay: projectClosest(target, "details[open], [role=menu], .projectui-shortcuts").Truthy(),
		},
	}
	action := projectclient.BoardShortcut(press)
	if action == "" {
		return
	}
	switch action {
	case projectclient.ShortcutNewTask:
		event.Call("preventDefault")
		openProjectCreateTask()
	case projectclient.ShortcutSearch:
		event.Call("preventDefault")
		toggleProjectFilters(true)
		if input := document.Call("getElementById", "projectui-board-search"); input.Truthy() {
			input.Call("focus")
			input.Call("select")
		}
	case projectclient.ShortcutFilters:
		event.Call("preventDefault")
		toggleProjectFilters(false)
	case projectclient.ShortcutHelp:
		event.Call("preventDefault")
		setProjectShortcutsOpen(true)
	default:
		// Arrow keys keep scrolling the page unless focus is on a card.
		if strings.HasPrefix(key, "Arrow") && !projectClosest(target, ".projectui-card-title").Truthy() {
			return
		}
		if moveProjectCardFocus(target, action) {
			event.Call("preventDefault")
		}
	}
}

// moveProjectCardFocus walks card title links: up and down within the
// reading order, left and right across columns (mirrored in RTL).
func moveProjectCardFocus(target js.Value, action string) bool {
	document := js.Global().Get("document")
	all := document.Call("querySelectorAll", ".projectui-card .projectui-card-title")
	links := []js.Value{}
	current := -1
	for index := 0; index < all.Get("length").Int(); index++ {
		link := all.Index(index)
		if link.Get("offsetParent").IsNull() {
			continue
		}
		if link.Equal(target) {
			current = len(links)
		}
		links = append(links, link)
	}
	if len(links) == 0 {
		return false
	}
	focus := func(link js.Value) bool {
		link.Call("focus", map[string]any{"preventScroll": true})
		link.Call("scrollIntoView", map[string]any{"block": "nearest", "inline": "nearest"})
		return true
	}
	if current < 0 {
		return focus(links[0])
	}
	switch action {
	case projectclient.ShortcutNext:
		if current+1 < len(links) {
			return focus(links[current+1])
		}
		return true
	case projectclient.ShortcutPrev:
		if current > 0 {
			return focus(links[current-1])
		}
		return true
	}
	// Across columns: the group is a column body or a lane cell.
	group := projectClosest(target, ".projectui-column-body, .projectui-cell")
	if !group.Truthy() {
		return false
	}
	groups := document.Call("querySelectorAll", ".projectui-column-body, .projectui-cell")
	indexOf := -1
	for index := 0; index < groups.Get("length").Int(); index++ {
		if groups.Index(index).Equal(group) {
			indexOf = index
		}
	}
	step := 1
	if action == projectclient.ShortcutLeft {
		step = -1
	}
	if document.Get("documentElement").Get("dir").String() == "rtl" {
		step = -step
	}
	position := 0
	inGroup := group.Call("querySelectorAll", ".projectui-card-title")
	for index := 0; index < inGroup.Get("length").Int(); index++ {
		if inGroup.Index(index).Equal(target) {
			position = index
		}
	}
	for next := indexOf + step; next >= 0 && next < groups.Get("length").Int(); next += step {
		cards := groups.Index(next).Call("querySelectorAll", ".projectui-card-title")
		if count := cards.Get("length").Int(); count > 0 {
			return focus(cards.Index(min(position, count-1)))
		}
	}
	return true
}

// settleProjectCard plays the drop settle (or the rollback) on a card once
// the board has redrawn it out of its pending state.
func settleProjectCard(taskID, class string, attempt int) {
	if attempt > 40 {
		return
	}
	var callback js.Func
	callback = js.FuncOf(func(js.Value, []js.Value) any {
		defer callback.Release()
		card := js.Global().Get("document").Call("querySelector", `.projectui-card[data-task-id="`+taskID+`"]`)
		if !card.Truthy() || projectAttr(card, "data-state") == "pending" {
			settleProjectCard(taskID, class, attempt+1)
			return nil
		}
		list := card.Get("classList")
		list.Call("remove", "is-settling", "is-returning")
		card.Get("offsetWidth") // restart the animation
		list.Call("add", class)
		var done js.Func
		done = js.FuncOf(func(js.Value, []js.Value) any {
			defer done.Release()
			list.Call("remove", class)
			return nil
		})
		js.Global().Call("setTimeout", done, 1000)
		return nil
	})
	js.Global().Call("requestAnimationFrame", callback)
}
