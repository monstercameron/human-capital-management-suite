package productui

import (
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// docsFocusPatience bounds how long a focus request waits for its target to
// render. A request that outlives it is dropped, so a late render can never
// pull focus away from wherever the person has moved since.
const docsFocusPatience = 4 * time.Second

// docsToastLifetime matches the docs-toast animation: once it has faded the
// notice is cleared, so the same message can play again and be announced.
const docsToastLifetime = 4200 * time.Millisecond

// docsActionToastLifetime gives an actionable toast (one with an Undo
// button, e.g. DOCS-07's remove flow) longer to be seen and used than a
// plain confirmation gets.
const docsActionToastLifetime = 8000 * time.Millisecond

// docsFocusRequest names where keyboard focus goes after the next render.
// Each target is an element id written "id:<id>" or a CSS selector; the
// first one present wins.
type docsFocusRequest struct {
	targets    []string
	selectText bool
	until      time.Time
}

// useDocsFocus returns a function that moves keyboard focus once its target
// is in the DOM. Controls that disappear on click (Cancel, Resolve, a chip's
// remove button, a folder form) would otherwise drop focus to <body>; the
// caller names where it belongs instead. The move happens in an effect
// after the render that shows the target, never by guessing a delay: a
// passive effect, so a child field's own layout effect (seeding a rename
// box with the current name) has already run and the text can be selected.
func useDocsFocus() func(selectText bool, targets ...string) {
	pending := ui.UseRef(docsFocusRequest{})
	tick := ui.UseState(0)
	ui.UseEffect(func() func() {
		request := pending.Get()
		if len(request.targets) == 0 {
			return nil
		}
		if docsFocusFirst(request.targets, request.selectText) || time.Now().After(request.until) {
			pending.Set(docsFocusRequest{})
		}
		return nil
	})
	return func(selectText bool, targets ...string) {
		pending.Set(docsFocusRequest{targets: targets, selectText: selectText, until: time.Now().Add(docsFocusPatience)})
		tick.Update(func(n int) int { return n + 1 })
	}
}

// useDocsModal makes an open Docs dialog modal: open row menus close, the
// page behind it turns inert, focus starts inside it and Tab stays there, and
// closing returns focus to what opened it (or the first fallback present).
func useDocsModal(open bool, dialogID, initial string, fallbacks ...string) {
	ui.UseEffectOf(func() func() {
		if !open {
			return nil
		}
		return docsTrapModal(dialogID, initial, fallbacks)
	}, open)
}

// useDocsMenuKeys gives the row and folder "⋯" disclosures arrow-key
// travel: ArrowDown / ArrowUp move between the items (opening the menu from
// its trigger), Home and End jump to the ends. Escape already closes them and
// returns focus to the trigger through the shared popover controller.
func useDocsMenuKeys() {
	ui.UseEffectOf(func() func() { return docsBindMenuKeys() }, "docs-menu-keys")
}

// docsMenuNext is the item an arrow key lands on in a menu of count items
// when the item at current (-1: the trigger) has focus. The ends wrap.
func docsMenuNext(key string, current, count int) int {
	if count <= 0 {
		return -1
	}
	switch key {
	case "Home":
		return 0
	case "End":
		return count - 1
	case "ArrowUp":
		if current <= 0 {
			return count - 1
		}
		return current - 1
	default:
		if current < 0 || current >= count-1 {
			return 0
		}
		return current + 1
	}
}

type docsNoticeValue struct {
	text string
	seq  int
	// actionLabel, action and id describe an optional Undo-shaped control
	// the toast carries: a docs-action/docs-id pair routed through the
	// page's own delegated click handler, exactly like every other
	// docs-action button (docsEventAction). A plain notice leaves all
	// three empty.
	actionLabel, action, id string
}

// docsNotice is the Docs page's one-line confirmation ("Link copied"). It has
// the Get / Set shape of a string state, but every Set is a new notice with
// its own sequence number, and it clears itself once the toast has faded.
// Keyed by that number, a repeated message remounts the toast and changes the
// live region, so the second "Link copied" is seen and announced too.
type docsNotice struct {
	state ui.State[docsNoticeValue]
	seq   ui.Ref[int]
}

func useDocsNotice() docsNotice {
	state := ui.UseState(docsNoticeValue{})
	seq := ui.UseRef(0)
	current := state.Get()
	ui.UseEffectOf(func() func() {
		if current.text == "" {
			return nil
		}
		shown := current.seq
		lifetime := docsToastLifetime
		if current.actionLabel != "" {
			lifetime = docsActionToastLifetime
		}
		return docsAfter(lifetime, func() {
			ui.PostAsync(func() {
				state.Update(func(value docsNoticeValue) docsNoticeValue {
					if value.seq == shown {
						value.text = ""
					}
					return value
				})
			})
		})
	}, current.seq)
	return docsNotice{state: state, seq: seq}
}

func (notice docsNotice) Get() string { return notice.state.Get().text }

func (notice docsNotice) Seq() int { return notice.state.Get().seq }

func (notice docsNotice) Set(text string) {
	next := notice.seq.Get() + 1
	notice.seq.Set(next)
	notice.state.Set(docsNoticeValue{text: text, seq: next})
}

// SetAction is Set plus one Undo-shaped control: action and id are the
// docs-action/docs-id pair the toast's button carries, caught by the same
// delegated click handler every other docs-action button already uses, so
// no new event wiring is needed here.
func (notice docsNotice) SetAction(text, actionLabel, action, id string) {
	next := notice.seq.Get() + 1
	notice.seq.Set(next)
	notice.state.Set(docsNoticeValue{text: text, seq: next, actionLabel: actionLabel, action: action, id: id})
}

// Live is the polite status line that reads the notice. The text sits in a
// span keyed by the notice's sequence, so repeating a message replaces the
// node and screen readers announce it again.
func (notice docsNotice) Live(class string) ui.Node {
	value := notice.state.Get()
	var text ui.Node
	if value.text != "" {
		text = html.Span(html.Props{Key: "notice:" + strconv.Itoa(value.seq)}, ui.Text(value.text))
	}
	return html.P(html.Props{Class: strings.TrimSpace("sr-only " + class), Raw: map[string]any{"role": "status", "aria-live": "polite"}}, text)
}

// Toast is the visual echo of the notice. Its key carries the sequence, so
// the same message twice mounts a fresh toast and its animation plays
// again. A toast carrying an action (SetAction) is not aria-hidden: its
// button is a real, keyboard-reachable control, and the toast itself
// becomes its own polite live region so the action is announced too.
func (notice docsNotice) Toast() ui.Node { return docsNoticeToastNode(notice.state.Get()) }

// docsNoticeToastNode is Toast's pure rendering step, split out so it can
// be exercised directly with a literal docsNoticeValue in tests, without
// standing up a hook fiber.
func docsNoticeToastNode(value docsNoticeValue) ui.Node {
	if value.text == "" {
		return nil
	}
	if value.actionLabel == "" {
		return html.Div(html.Props{Key: "toast:" + strconv.Itoa(value.seq), Class: "docs-toast", Raw: map[string]any{"aria-hidden": "true"}}, ui.Text(value.text))
	}
	return html.Div(html.Props{Key: "toast:" + strconv.Itoa(value.seq), Class: "docs-toast docs-toast-action", Raw: map[string]any{"role": "status", "aria-live": "polite"}},
		html.Span(html.Props{}, ui.Text(value.text)),
		html.Button(html.Props{Class: "docs-toast-undo", Type: "button", Data: map[string]string{"docs-action": value.action, "docs-id": value.id}}, ui.Text(value.actionLabel)),
	)
}

// docsCopyOutcome turns a clipboard result into the notice to show: the
// success message, or an honest failure.
func docsCopyOutcome(locale, success string, err error) string {
	if err != nil {
		return docsText(locale, "copy_failed")
	}
	return docsText(locale, success)
}

// docsDialogReturnFallbacks is where focus lands when the control that
// opened a dialog is gone by the time it closes (a row moved out of the
// folder being viewed): the list's search box, or the open document's title.
var docsDialogReturnFallbacks = []string{"#docs-browse-query", "id:page-title"}

// Focus targets in the Docs library nav and list.
const (
	docsFolderAddTarget       = ".docs-nav-add"
	docsNavFirstTarget        = ".docs-nav-list .docs-nav-link"
	docsSelectAllTarget       = `[data-docs-action="select-all"]`
	docsFolderDeleteConfirmID = "docs-folder-delete-confirm"
)

// docsFolderLinkID is the id of a folder's link in the nav, where focus
// returns after renaming or cancelling.
func docsFolderLinkID(folderID string) string { return "docs-folder-" + folderID }

// docsReplyOpenID is the id of a thread's Reply button, where focus returns
// when the reply form closes.
func docsReplyOpenID(threadID string) string { return "docs-reply-open-" + threadID }

// docsThreadNeighbour is the thread that takes focus when id leaves the list
// being shown (open or resolved threads): the next one, else the one before,
// else "".
func docsThreadNeighbour(threads []DocumentComment, showingResolved bool, id string) string {
	shown := make([]string, 0, len(threads))
	for _, thread := range threads {
		if thread.Resolved == showingResolved {
			shown = append(shown, thread.ID)
		}
	}
	for index, threadID := range shown {
		if threadID != id {
			continue
		}
		if index+1 < len(shown) {
			return shown[index+1]
		}
		if index > 0 {
			return shown[index-1]
		}
	}
	return ""
}

// docsThreadFocusTargets is where focus goes after a thread leaves the
// list: its neighbour, or else the composer or the open/resolved toggle.
func docsThreadFocusTargets(neighbour string) []string {
	targets := []string{}
	if neighbour != "" {
		targets = append(targets, "id:docs-thread-"+neighbour)
	}
	return append(targets, "#docs-comment-body", ".docs-comments-toggle")
}

// routeAnnouncementTitle is the name the route announcer reads for the page
// just shown: an open document by its own title ("Leave policy page
// loaded"), every other page by the shell's title.
func routeAnnouncementTitle(view View) string {
	if view.Page == PageDocs && view.Document != nil {
		if title := strings.TrimSpace(view.Document.Summary.Title); title != "" {
			return title
		}
	}
	return view.Title
}
