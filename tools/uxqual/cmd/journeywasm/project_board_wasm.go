//go:build js && wasm

package main

import (
	"strconv"
	"strings"
	"sync"
	"syscall/js"
	"time"

	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/journeyclient"
)

// Board interactions are document-level listeners over the markup projectui
// renders, the same pattern as the move selects: the board stays a plain
// server-renderable tree, and these read its data-* attributes.
//
// Drag and drop writes no component state while a drag is in progress. The
// highlight is DOM classes on the board; the single state change is the
// move committed on drop, through startProjectMove. The dragged card is
// dimmed with opacity only: changing pointer-events on it makes Chrome abort
// the drag without a dragend.

var projectBoardListenersInstalled bool

type projectDragState struct {
	active           bool
	taskID           string
	taskRevision     uint64
	workflowRevision uint64
	status           string
	lane             string
	targets          map[string]bool
	laneMovable      bool
	card             js.Value
	region           js.Value
	zone             js.Value
	lastSignal       time.Time
	watchdog         *time.Timer
}

var projectDrag projectDragState

// projectLaneConfig is the lane grouping of the board last loaded, used by
// the lane selects in the ticket modal, which live outside the board.
var projectLaneConfig struct {
	sync.Mutex
	kind, field string
}

func setProjectLaneConfig(kind, field string) {
	projectLaneConfig.Lock()
	projectLaneConfig.kind, projectLaneConfig.field = kind, field
	projectLaneConfig.Unlock()
}

func projectLaneConfigFor() (string, string) {
	projectLaneConfig.Lock()
	defer projectLaneConfig.Unlock()
	return projectLaneConfig.kind, projectLaneConfig.field
}

func bindProjectBoardInteractions(cfg journeyclient.Config, service projectv1.ProjectServiceClient, revalidate func()) {
	if projectBoardListenersInstalled {
		return
	}
	document := js.Global().Get("document")
	if !document.Truthy() {
		return
	}
	projectBoardListenersInstalled = true
	bindProjectPopovers()
	bindProjectBoardKeys()
	bindProjectShare(cfg)
	bindProjectWorkflows(cfg, service, revalidate)
	document.Call("addEventListener", "click", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 {
			handleProjectLaneClick(args[0])
		}
		return nil
	}))
	document.Call("addEventListener", "dragstart", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 {
			projectDragStart(args[0])
		}
		return nil
	}))
	document.Call("addEventListener", "dragover", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 {
			projectDragOver(args[0])
		}
		return nil
	}))
	document.Call("addEventListener", "drop", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 {
			projectDrop(args[0], cfg, service, revalidate)
		}
		return nil
	}))
	document.Call("addEventListener", "dragend", js.FuncOf(func(js.Value, []js.Value) any {
		projectDragCleanup()
		return nil
	}))
}

func projectClosest(target js.Value, selector string) js.Value {
	if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return js.Null()
	}
	return target.Call("closest", selector)
}

func projectAttr(node js.Value, name string) string {
	if !node.Truthy() {
		return ""
	}
	value := node.Call("getAttribute", name)
	if value.IsNull() || value.IsUndefined() {
		return ""
	}
	return value.String()
}

// ---- swimlane collapse -------------------------------------------------

// handleProjectPopoverClick closes a create/settings popover from its
// Cancel or close button, and copies a task link from the card menu.
func handleProjectPopoverClick(event js.Value) bool {
	target := event.Get("target")
	if closer := projectClosest(target, "[data-projectui-close-popover]"); closer.Truthy() {
		if details := projectClosest(closer, "details.project-page-disclosure"); details.Truthy() {
			details.Call("removeAttribute", "open")
			if summary := details.Call("querySelector", "summary"); summary.Truthy() {
				summary.Call("focus")
			}
		}
		return true
	}
	if copier := projectClosest(target, `[data-projectui-action="copy-link"]`); copier.Truthy() {
		href := projectShareCanonical(projectResolveShareHref(projectAttr(copier, "data-href")))
		location := js.Global().Get("location")
		absolute := location.Get("origin").String() + href
		if clipboard := js.Global().Get("navigator").Get("clipboard"); clipboard.Truthy() {
			func() {
				defer func() { _ = recover() }()
				clipboard.Call("writeText", absolute)
			}()
		}
		if label := copier.Call("querySelector", ".projectui-menu-item-label"); label.Truthy() {
			label.Set("textContent", projectAttr(copier, "data-copied"))
		}
		copier.Call("setAttribute", "data-copied-state", "true")
		return true
	}
	return false
}

func bindProjectPopovers() {
	document := js.Global().Get("document")
	// toggle does not bubble; capture it for every popover on the page.
	document.Call("addEventListener", "toggle", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		details := args[0].Get("target")
		if !details.Truthy() || details.Get("matches").Type() != js.TypeFunction {
			return nil
		}
		isPopover := details.Call("matches", "details.project-page-disclosure").Bool()
		isMenu := details.Call("matches", "details.projectui-card-menu").Bool()
		if (!isPopover && !isMenu) || !details.Get("open").Bool() {
			return nil
		}
		// One popover or card menu open at a time.
		others := document.Call("querySelectorAll", "details.project-page-disclosure[open], details.projectui-card-menu[open]")
		for index := 0; index < others.Get("length").Int(); index++ {
			if other := others.Index(index); !other.Equal(details) {
				other.Call("removeAttribute", "open")
			}
		}
		if isPopover {
			if field := details.Call("querySelector", "[data-autofocus]:not([disabled])"); field.Truthy() {
				field.Call("focus")
			}
		}
		return nil
	}), true)
	document.Call("addEventListener", "keydown", js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 0 {
			return nil
		}
		event := args[0]
		key := event.Get("key").String()
		target := event.Get("target")
		if key == "Escape" {
			details := projectClosest(target, "details.project-page-disclosure[open], details.projectui-card-menu[open]")
			if !details.Truthy() {
				return nil
			}
			event.Call("preventDefault")
			details.Call("removeAttribute", "open")
			if summary := details.Call("querySelector", "summary"); summary.Truthy() {
				summary.Call("focus")
			}
			return nil
		}
		// Arrow keys move between a card menu's items, as in any menu.
		if key == "ArrowDown" || key == "ArrowUp" || key == "Home" || key == "End" {
			menu := projectClosest(target, "details.projectui-card-menu[open]")
			if !menu.Truthy() {
				return nil
			}
			items := menu.Call("querySelectorAll", `[role^="menuitem"]:not(:disabled)`)
			count := items.Get("length").Int()
			if count == 0 {
				return nil
			}
			current := -1
			for index := 0; index < count; index++ {
				if items.Index(index).Equal(target) {
					current = index
				}
			}
			next := 0
			switch {
			case key == "End":
				next = count - 1
			case key == "Home":
				next = 0
			case key == "ArrowUp" && current <= 0:
				next = count - 1
			case key == "ArrowUp":
				next = current - 1
			case current >= 0:
				next = (current + 1) % count
			}
			event.Call("preventDefault")
			items.Index(next).Call("focus")
			return nil
		}
		// Ctrl/Cmd+Enter submits a create form from any field.
		if key == "Enter" && (event.Get("ctrlKey").Bool() || event.Get("metaKey").Bool()) {
			if form := projectClosest(target, "form.project-page-create-form"); form.Truthy() {
				event.Call("preventDefault")
				form.Call("requestSubmit")
			}
		}
		return nil
	}))
}

func handleProjectLaneClick(event js.Value) {
	if handleProjectPopoverClick(event) {
		return
	}
	target := event.Get("target")
	button := projectClosest(target, `[data-projectui-action="toggle-lane"], [data-projectui-action="collapse-lanes"], [data-projectui-action="expand-lanes"]`)
	if !button.Truthy() {
		return
	}
	region := js.Global().Get("document").Call("querySelector", `.projectui-columns[data-lanes="true"]`)
	if !region.Truthy() {
		return
	}
	switch projectAttr(button, "data-projectui-action") {
	case "toggle-lane":
		lane := projectClosest(button, ".projectui-lane")
		setProjectLaneCollapsed(lane, projectAttr(lane, "data-collapsed") != "true")
	case "collapse-lanes", "expand-lanes":
		collapse := projectAttr(button, "data-projectui-action") == "collapse-lanes"
		lanes := region.Call("querySelectorAll", ".projectui-lane")
		for index := 0; index < lanes.Get("length").Int(); index++ {
			setProjectLaneCollapsed(lanes.Index(index), collapse)
		}
	}
	storeProjectLaneState(region)
	syncProjectLaneTool(region)
}

// syncProjectLaneTool flips the single Collapse all / Expand all button to
// whichever action is still possible.
func syncProjectLaneTool(region js.Value) {
	tool := js.Global().Get("document").Call("querySelector", ".projectui-lane-tool")
	if !tool.Truthy() {
		return
	}
	open := region.Call("querySelector", `.projectui-lane[data-collapsed="false"]`).Truthy()
	action, label, dir := "collapse-lanes", projectAttr(tool, "data-label-collapse"), "collapse"
	if !open {
		action, label, dir = "expand-lanes", projectAttr(tool, "data-label-expand"), "expand"
	}
	tool.Call("setAttribute", "data-projectui-action", action)
	if text := tool.Call("querySelector", ".projectui-lane-tool-label"); text.Truthy() {
		text.Set("textContent", label)
	}
	if icon := tool.Call("querySelector", ".projectui-lane-tool-icon"); icon.Truthy() {
		icon.Call("setAttribute", "data-dir", dir)
	}
}

func setProjectLaneCollapsed(lane js.Value, collapsed bool) {
	if !lane.Truthy() {
		return
	}
	lane.Call("setAttribute", "data-collapsed", strconv.FormatBool(collapsed))
	if toggle := lane.Call("querySelector", `[data-projectui-action="toggle-lane"]`); toggle.Truthy() {
		toggle.Call("setAttribute", "aria-expanded", strconv.FormatBool(!collapsed))
	}
}

// storeProjectLaneState remembers which lanes this viewer collapsed on this
// board view. Storage is a convenience: when it is unavailable or throws,
// the board still works and simply forgets on reload.
func storeProjectLaneState(region js.Value) {
	key := projectAttr(region, "data-lane-store")
	if key == "" {
		return
	}
	states := []string{}
	lanes := region.Call("querySelectorAll", ".projectui-lane")
	for index := 0; index < lanes.Get("length").Int(); index++ {
		lane := lanes.Index(index)
		state := "0"
		if projectAttr(lane, "data-collapsed") == "true" {
			state = "1"
		}
		states = append(states, projectAttr(lane, "data-lane-id")+"="+state)
	}
	projectStorageSet(key, strings.Join(states, "\n"))
}

// projectCollapsedLanes reads the remembered lane states for a board: true
// is collapsed, false is open, and a lane missing from the map has no
// remembered choice. A bare ID (the earlier format) means collapsed.
func projectCollapsedLanes(key string) map[string]bool {
	out := map[string]bool{}
	if key == "" {
		return out
	}
	for _, line := range strings.Split(projectStorageGet(key), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		id, state, found := strings.Cut(line, "=")
		out[id] = !found || state == "1"
	}
	return out
}

func projectStorageGet(key string) (value string) {
	defer func() {
		if recover() != nil {
			value = ""
		}
	}()
	storage := js.Global().Get("localStorage")
	if !storage.Truthy() {
		return ""
	}
	item := storage.Call("getItem", key)
	if item.IsNull() || item.IsUndefined() {
		return ""
	}
	return item.String()
}

func projectStorageSet(key, value string) {
	defer func() { _ = recover() }()
	storage := js.Global().Get("localStorage")
	if !storage.Truthy() {
		return
	}
	if value == "" {
		storage.Call("removeItem", key)
		return
	}
	storage.Call("setItem", key, value)
}

// ---- drag and drop -------------------------------------------------------

func projectDragStart(event js.Value) {
	card := projectClosest(event.Get("target"), `.projectui-card[draggable="true"]`)
	if !card.Truthy() {
		return
	}
	region := projectClosest(card, ".projectui-columns")
	taskRevision, errTask := strconv.ParseUint(projectAttr(card, "data-task-revision"), 10, 64)
	workflowRevision, errFlow := strconv.ParseUint(projectAttr(card, "data-workflow-revision"), 10, 64)
	if !region.Truthy() || errTask != nil || errFlow != nil {
		return
	}
	projectDragCleanup()
	targets := map[string]bool{}
	for _, id := range strings.Fields(projectAttr(card, "data-move-targets")) {
		targets[id] = true
	}
	projectDrag = projectDragState{
		active: true, taskID: projectAttr(card, "data-task-id"), taskRevision: taskRevision, workflowRevision: workflowRevision,
		status: projectAttr(card, "data-status-id"), lane: projectAttr(card, "data-lane-id"), targets: targets,
		laneMovable: projectAttr(card, "data-lane-movable") == "true", card: card, region: region, zone: js.Null(), lastSignal: time.Now(),
	}
	if transfer := event.Get("dataTransfer"); transfer.Truthy() {
		transfer.Set("effectAllowed", "move")
		transfer.Call("setData", "text/plain", projectDrag.taskID)
	}
	height := card.Get("offsetHeight").Int()
	region.Get("style").Call("setProperty", "--pu-drop-h", strconv.Itoa(height)+"px")
	card.Get("classList").Call("add", "is-drag-source")
	region.Get("classList").Call("add", "is-dragging")
	zones := region.Call("querySelectorAll", "[data-drop-status]")
	for index := 0; index < zones.Get("length").Int(); index++ {
		zone := zones.Index(index)
		state := "invalid"
		switch {
		case projectDropAllowed(zone):
			state = "valid"
		case projectZoneHoldsCard(zone):
			// The card's own column and lane is where it is, not a refusal.
			state = "source"
		}
		zone.Call("setAttribute", "data-drop-state", state)
	}
	armProjectDragWatchdog()
}

// armProjectDragWatchdog clears the drag if the browser stops reporting it
// (an aborted drag can end without a dragend), so no highlight or dimmed
// card is left behind.
func armProjectDragWatchdog() {
	if projectDrag.watchdog != nil {
		projectDrag.watchdog.Stop()
	}
	// Browsers fire dragover every ~50 ms for as long as a drag is alive,
	// even with the pointer still; four seconds of silence means it died.
	projectDrag.watchdog = time.AfterFunc(2*time.Second, func() {
		if !projectDrag.active {
			return
		}
		if time.Since(projectDrag.lastSignal) > 4*time.Second {
			projectDragCleanup()
			return
		}
		armProjectDragWatchdog()
	})
}

// projectDropAllowed reports whether the dragged card may land in zone: a
// status the card can move to (or its own), and a lane that accepts moves
// when the drop would change the lane.
func projectDropAllowed(zone js.Value) bool {
	if !projectDrag.active || !zone.Truthy() {
		return false
	}
	statuses := strings.Fields(projectAttr(zone, "data-drop-statuses"))
	statusOK := false
	sameColumn := false
	for _, id := range statuses {
		if id == projectDrag.status {
			sameColumn = true
		}
	}
	if sameColumn {
		statusOK = true
	} else if projectDrag.targets[projectAttr(zone, "data-drop-status")] {
		statusOK = true
	}
	lane := projectAttr(zone, "data-drop-lane")
	laneChange := lane != "" && lane != projectDrag.lane
	if laneChange && (!projectDrag.laneMovable || projectAttr(zone, "data-lane-editable") != "true") {
		return false
	}
	if sameColumn && !laneChange {
		return false
	}
	return statusOK
}

func projectDragOver(event js.Value) {
	if !projectDrag.active {
		return
	}
	projectDrag.lastSignal = time.Now()
	zone := projectClosest(event.Get("target"), "[data-drop-status]")
	if zone.Truthy() && !projectDrag.region.Call("contains", zone).Bool() {
		zone = js.Null()
	}
	if !zone.Truthy() || !projectDropAllowed(zone) {
		setProjectDropZone(js.Null())
		return
	}
	event.Call("preventDefault")
	if transfer := event.Get("dataTransfer"); transfer.Truthy() {
		transfer.Set("dropEffect", "move")
	}
	setProjectDropZone(zone)
}

func setProjectDropZone(zone js.Value) {
	if projectDrag.zone.Truthy() && (!zone.Truthy() || !projectDrag.zone.Equal(zone)) {
		projectDrag.zone.Get("classList").Call("remove", "is-drop-target")
	}
	if zone.Truthy() {
		zone.Get("classList").Call("add", "is-drop-target")
	}
	projectDrag.zone = zone
}

func projectDrop(event js.Value, cfg journeyclient.Config, service projectv1.ProjectServiceClient, revalidate func()) {
	if !projectDrag.active {
		return
	}
	zone := projectClosest(event.Get("target"), "[data-drop-status]")
	if !zone.Truthy() || !projectDropAllowed(zone) {
		projectDragCleanup()
		return
	}
	event.Call("preventDefault")
	request := projectMoveRequest{TaskID: projectDrag.taskID, TaskRevision: projectDrag.taskRevision, WorkflowRevision: projectDrag.workflowRevision, Action: "status"}
	sameColumn := false
	for _, id := range strings.Fields(projectAttr(zone, "data-drop-statuses")) {
		if id == projectDrag.status {
			sameColumn = true
		}
	}
	if !sameColumn {
		request.ToStatus = projectAttr(zone, "data-drop-status")
	}
	if lane := projectAttr(zone, "data-drop-lane"); lane != "" && lane != projectDrag.lane {
		request.LaneValue = lane
		request.LaneKind = projectAttr(projectDrag.region, "data-lane-kind")
		request.LaneField = projectAttr(projectDrag.region, "data-lane-field")
		if request.ToStatus == "" {
			request.Action = "lane"
		}
	}
	projectDragCleanup()
	startProjectMove(cfg, service, revalidate, request)
}

func projectDragCleanup() {
	if projectDrag.watchdog != nil {
		projectDrag.watchdog.Stop()
	}
	if projectDrag.card.Truthy() {
		projectDrag.card.Get("classList").Call("remove", "is-drag-source")
	}
	if projectDrag.zone.Truthy() {
		projectDrag.zone.Get("classList").Call("remove", "is-drop-target")
	}
	if projectDrag.region.Truthy() {
		region := projectDrag.region
		region.Get("classList").Call("remove", "is-dragging")
		zones := region.Call("querySelectorAll", "[data-drop-state]")
		for index := 0; index < zones.Get("length").Int(); index++ {
			zones.Index(index).Call("removeAttribute", "data-drop-state")
		}
	}
	// Any other board left in a drag state (for example after a re-render
	// replaced the region mid-drag) is cleared too.
	if document := js.Global().Get("document"); document.Truthy() {
		stale := document.Call("querySelectorAll", ".projectui-columns.is-dragging, .projectui-card.is-drag-source, .is-drop-target")
		for index := 0; index < stale.Get("length").Int(); index++ {
			node := stale.Index(index)
			node.Get("classList").Call("remove", "is-dragging", "is-drag-source", "is-drop-target")
		}
	}
	projectDrag = projectDragState{card: js.Null(), region: js.Null(), zone: js.Null()}
}

// projectZoneHoldsCard reports whether zone is the dragged card's own place.
func projectZoneHoldsCard(zone js.Value) bool {
	for _, id := range strings.Fields(projectAttr(zone, "data-drop-statuses")) {
		if id == projectDrag.status {
			lane := projectAttr(zone, "data-drop-lane")
			return lane == "" || lane == projectDrag.lane
		}
	}
	return false
}
