//go:build js && wasm

package productui

import (
	"strings"
	"syscall/js"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// These tests drive the Docs library in a real browser DOM (a headless
// Chromium page running the wasm test binary). Under node, which has no
// document, they skip.

type docsDOM struct {
	t   *testing.T
	doc js.Value
}

func newDocsDOM(t *testing.T) docsDOM {
	t.Helper()
	doc := js.Global().Get("document")
	if doc.IsUndefined() || doc.Get("createElement").Type() != js.TypeFunction {
		t.Skip("no browser DOM")
	}
	return docsDOM{t: t, doc: doc}
}

func (d docsDOM) q(selector string) js.Value { return d.doc.Call("querySelector", selector) }

func (d docsDOM) count(selector string) int {
	return d.doc.Call("querySelectorAll", selector).Length()
}

func (d docsDOM) waitFor(what string, cond func() bool) {
	d.t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	d.t.Fatalf("timed out waiting for %s; focus on %s", what, d.activeDesc())
}

func (d docsDOM) activeDesc() string {
	el := d.doc.Get("activeElement")
	if !el.Truthy() {
		return "<none>"
	}
	return el.Get("tagName").String() + "#" + el.Get("id").String() + "." + el.Get("className").String()
}

func (d docsDOM) activeIs(selector string) bool {
	el := d.q(selector)
	return el.Truthy() && d.doc.Get("activeElement").Equal(el)
}

func (d docsDOM) click(selector string) {
	d.t.Helper()
	el := d.q(selector)
	if !el.Truthy() {
		d.t.Fatalf("no element %s", selector)
	}
	if el.Get("focus").Type() == js.TypeFunction {
		el.Call("focus")
	}
	el.Call("click")
}

func (d docsDOM) key(key string, shift bool) {
	target := d.doc.Get("activeElement")
	if !target.Truthy() {
		target = d.doc.Get("body")
	}
	init := js.Global().Get("Object").New()
	init.Set("key", key)
	init.Set("bubbles", true)
	init.Set("cancelable", true)
	init.Set("shiftKey", shift)
	target.Call("dispatchEvent", js.Global().Get("KeyboardEvent").New("keydown", init))
}

func mountDocs(t *testing.T, id string, node ui.Node) docsDOM {
	d := newDocsDOM(t)
	style := d.doc.Call("createElement", "style")
	style.Set("textContent", docsStylesheet())
	d.doc.Get("head").Call("appendChild", style)
	host := d.doc.Call("createElement", "div")
	host.Set("id", id)
	d.doc.Get("body").Call("appendChild", host)
	ui.Render(node, "#"+id)
	d.waitFor("mount", func() bool { return host.Get("childElementCount").Int() > 0 })
	return d
}

func docsBrowserView() View {
	view := docsFocusView()
	view.CreateDocument = func(DocumentCreateRequest, func(error)) {}
	view.RenameDocumentFolder = func(_ string, _ string, done func(error)) { done(nil) }
	view.DeleteDocumentFolder = func(_ string, done func(error)) { done(nil) }
	view.People = []Person{{ID: "p-1", Name: "Maya Chen", LifecycleStatus: "active"}, {ID: "p-2", Name: "Mateo Ruiz", LifecycleStatus: "active"}}
	return view
}

func TestDocsFocusBrowserLibrary(t *testing.T) {
	d := mountDocs(t, "docs-harness-library", ui.CreateElement(docsLibrary, docsLibraryProps{View: docsBrowserView()}))

	t.Run("M9 arrow keys move through a row menu", func(t *testing.T) {
		d.t = t
		trigger := d.q(".docs-row:not(.docs-row-head) .docs-row-menu-trigger")
		trigger.Call("focus")
		d.key("ArrowDown", false)
		d.waitFor("first menu item", func() bool { return d.activeIs(".docs-row-menu[open] .docs-menu-item") })
		items := d.doc.Call("querySelectorAll", ".docs-row-menu[open] .docs-menu-item")
		d.key("End", false)
		if !d.doc.Get("activeElement").Equal(items.Index(items.Length() - 1)) {
			t.Fatalf("End did not reach the last item: %s", d.activeDesc())
		}
		d.key("ArrowDown", false)
		if !d.doc.Get("activeElement").Equal(items.Index(0)) {
			t.Fatalf("ArrowDown from the last item did not wrap: %s", d.activeDesc())
		}
		d.q(".docs-row-menu[open]").Call("removeAttribute", "open")
	})

	t.Run("H3 move dialog is modal and restores focus to the menu trigger", func(t *testing.T) {
		d.t = t
		menu := d.q(".docs-row:not(.docs-row-head) .docs-row-menu")
		menu.Set("open", true)
		d.click(".docs-row-menu[open] [data-docs-action=\"move\"]")
		d.waitFor("move dialog", func() bool { return d.q("#docs-move-dialog").Truthy() })
		d.waitFor("focus in move dialog", func() bool { return d.q("#docs-move-dialog").Call("contains", d.doc.Get("activeElement")).Bool() })
		if d.count("details[data-hcm-transient-popover][open]") != 0 {
			t.Fatal("row menu left open behind the dialog")
		}
		if !d.q(".docs-main").Call("closest", "[inert]").Truthy() || !d.q(".docs-nav").Call("closest", "[inert]").Truthy() {
			t.Fatal("list behind the dialog is not inert")
		}
		if d.q(".docs-live").Call("closest", "[inert]").Truthy() {
			t.Fatal("the notice live region was made inert, so a closing dialog's notice is lost")
		}
		// Tab from the last control wraps to the first; Shift+Tab back.
		items := docsFocusablesForTest(d.q("#docs-move-dialog"))
		items[len(items)-1].Call("focus")
		d.key("Tab", false)
		if !d.doc.Get("activeElement").Equal(items[0]) {
			t.Fatalf("Tab from the last control left the dialog: %s", d.activeDesc())
		}
		d.key("Tab", true)
		if !d.doc.Get("activeElement").Equal(items[len(items)-1]) {
			t.Fatalf("Shift+Tab from the first control left the dialog: %s", d.activeDesc())
		}
		d.key("Escape", false)
		d.waitFor("move dialog closed", func() bool { return !d.q("#docs-move-dialog").Truthy() })
		d.waitFor("focus on the row menu trigger", func() bool { return d.activeIs(".docs-row:not(.docs-row-head) .docs-row-menu-trigger") })
		if d.count("[inert]") != 0 {
			t.Fatal("inert left behind after the dialog closed")
		}
	})

	t.Run("H4 L11 share dialog keeps focus in the people field", func(t *testing.T) {
		d.t = t
		row := d.q(`.docs-row[data-document-id="doc-2"] .docs-row-menu`)
		row.Set("open", true)
		d.click(`.docs-row[data-document-id="doc-2"] [data-docs-action="share"]`)
		d.waitFor("share field focused", func() bool { return d.activeIs("#docs-share-people") })
		field := d.q("#docs-share-people")
		field.Set("value", "ma")
		field.Call("dispatchEvent", js.Global().Get("Event").New("input", map[string]any{"bubbles": true}))
		d.waitFor("suggestions", func() bool { return d.count(".docs-share-option") > 0 })
		d.key("Enter", false)
		d.waitFor("a chip", func() bool { return d.count(".docs-pick-chip") == 1 })
		d.waitFor("focus back in the field after picking", func() bool { return d.activeIs("#docs-share-people") })
		d.click(".docs-pick-chip .docs-pick-remove")
		d.waitFor("chip removed", func() bool { return d.count(".docs-pick-chip") == 0 })
		d.waitFor("focus back in the field after removing", func() bool { return d.activeIs("#docs-share-people") })
		field = d.q("#docs-share-people")
		field.Set("value", "ma")
		field.Call("dispatchEvent", js.Global().Get("Event").New("input", map[string]any{"bubbles": true}))
		d.waitFor("suggestions again", func() bool { return d.count(".docs-share-option") > 0 })
		d.key("Escape", false)
		d.waitFor("query cleared", func() bool { return d.count(".docs-share-option") == 0 })
		if !d.q("#docs-share-dialog").Truthy() {
			t.Fatal("Escape with suggestions open closed the whole dialog")
		}
		d.key("Escape", false)
		d.waitFor("share dialog closed", func() bool { return !d.q("#docs-share-dialog").Truthy() })
		d.waitFor("focus on doc-2's menu trigger", func() bool {
			return d.activeIs(`.docs-row[data-document-id="doc-2"] .docs-row-menu-trigger`)
		})
	})

	t.Run("M12 folder forms take focus and Escape backs out", func(t *testing.T) {
		d.t = t
		d.click(".docs-nav-add")
		d.waitFor("new folder field focused", func() bool { return d.activeIs("#docs-folder-new") })
		d.key("Escape", false)
		d.waitFor("new folder form closed, focus on +", func() bool { return !d.q("#docs-folder-new").Truthy() && d.activeIs(".docs-nav-add") })

		d.q(".docs-folder-menu").Set("open", true)
		d.click(`.docs-folder-menu [data-docs-action="folder-rename"]`)
		d.waitFor("rename field focused", func() bool { return d.activeIs("#docs-folder-rename") })
		d.waitFor("rename text selected", func() bool {
			field := d.q("#docs-folder-rename")
			ok := field.Get("value").String() == "Payroll" && field.Get("selectionStart").Int() == 0 && field.Get("selectionEnd").Int() == len("Payroll")
			if !ok {
				t.Logf("rename field value=%q selection=%d-%d", field.Get("value").String(), field.Get("selectionStart").Int(), field.Get("selectionEnd").Int())
			}
			return ok
		})
		d.key("Escape", false)
		d.waitFor("rename closed, focus on the folder link", func() bool {
			return !d.q("#docs-folder-rename").Truthy() && d.activeIs("#"+docsFolderLinkID("f-1"))
		})

		d.q(".docs-folder-menu").Set("open", true)
		d.click(`.docs-folder-menu [data-docs-action="folder-delete"]`)
		d.waitFor("delete confirm focused", func() bool { return d.activeIs("#" + docsFolderDeleteConfirmID) })
		d.key("Escape", false)
		d.waitFor("delete question closed, focus on the folder link", func() bool {
			return !d.q(".docs-folder-confirm").Truthy() && d.activeIs("#"+docsFolderLinkID("f-1"))
		})
	})

	t.Run("H3 L9 create dialog traps focus and asks before discarding", func(t *testing.T) {
		d.t = t
		d.click(".docs-create-trigger button")
		d.waitFor("title focused", func() bool { return d.activeIs("#docs-create-title") })
		if !d.q(".docs-nav").Call("closest", "[inert]").Truthy() {
			t.Fatal("nav behind the create dialog is not inert")
		}
		// Escape from the dialog frame (not a field) closes an empty draft.
		d.q("#docs-create-dialog").Call("focus")
		d.key("Escape", false)
		d.waitFor("create dialog closed, focus on the trigger", func() bool {
			return !d.q("#docs-create-dialog").Truthy() && d.activeIs(".docs-create-trigger button")
		})
		d.click(".docs-create-trigger button")
		d.waitFor("title focused again", func() bool { return d.activeIs("#docs-create-title") })
		title := d.q("#docs-create-title")
		title.Set("value", "Draft")
		title.Call("dispatchEvent", js.Global().Get("Event").New("input", map[string]any{"bubbles": true}))
		time.Sleep(100 * time.Millisecond)
		d.key("Escape", false)
		d.waitFor("discard question on Keep editing", func() bool { return d.activeIs("#docs-create-keep") })
		d.key("Escape", false)
		d.waitFor("back to the title with the draft", func() bool {
			return d.activeIs("#docs-create-title") && !d.q(".docs-create-discard").Truthy() && d.q("#docs-create-title").Get("value").String() == "Draft"
		})
		d.click(".docs-dialog-close")
		d.waitFor("question again", func() bool { return d.q(".docs-create-discard").Truthy() })
		d.click(".docs-create-discard .docs-danger")
		d.waitFor("create dialog discarded", func() bool { return !d.q("#docs-create-dialog").Truthy() })
	})

	t.Run("M3 M4 a repeated notice replays and a failed copy says so", func(t *testing.T) {
		d.t = t
		copyLink := func() {
			d.q(`.docs-row[data-document-id="doc-1"] .docs-row-menu`).Set("open", true)
			d.click(`.docs-row[data-document-id="doc-1"] [data-docs-action="copy-link"]`)
		}
		// Headless pages without a user gesture grant can refuse the write;
		// either way the toast must say what happened, and a second copy must
		// mount a new toast.
		copyLink()
		d.waitFor("first toast", func() bool { return d.q(".docs-toast").Truthy() })
		first := d.q(".docs-toast")
		text := first.Get("textContent").String()
		if !strings.Contains(text, "copied") && !strings.Contains(text, "Couldn't copy") {
			t.Fatalf("unexpected toast %q", text)
		}
		copyLink()
		d.waitFor("a new toast element", func() bool {
			toast := d.q(".docs-toast")
			return toast.Truthy() && !toast.Equal(first)
		})
		live := d.q(".docs-live").Get("textContent").String()
		if live == "" {
			t.Fatal("live region is empty while the toast shows")
		}
		deadline := time.Now().Add(6 * time.Second)
		for time.Now().Before(deadline) && d.q(".docs-toast").Truthy() {
			time.Sleep(100 * time.Millisecond)
		}
		if d.q(".docs-toast").Truthy() || d.q(".docs-live").Get("textContent").String() != "" {
			t.Fatal("the notice did not clear after the toast faded")
		}
	})

	t.Run("M4 a refused clipboard write is reported as a failure", func(t *testing.T) {
		d.t = t
		js.Global().Call("eval", `Object.defineProperty(navigator, 'clipboard', {configurable: true, value: {writeText: () => Promise.reject(new DOMException('Write permission denied.', 'NotAllowedError'))}})`)
		d.q(`.docs-row[data-document-id="doc-1"] .docs-row-menu`).Set("open", true)
		d.click(`.docs-row[data-document-id="doc-1"] [data-docs-action="copy-link"]`)
		d.waitFor("failure toast", func() bool {
			toast := d.q(".docs-toast")
			return toast.Truthy() && strings.Contains(toast.Get("textContent").String(), "Couldn't copy")
		})
	})
}

func docsFocusablesForTest(root js.Value) []js.Value { return docsFocusables(root) }

func TestDocsFocusBrowserDetail(t *testing.T) {
	view := docsBrowserView()
	view.AddDocumentComment = func(_ DocumentCommentCreateRequest, done func(error)) { done(nil) }
	view.ResolveDocumentComment = func(_, _ string, _ bool, done func(error)) { done(nil) }
	view.Document = &DocumentDetail{
		Summary:    DocumentSummary{ID: "doc-2", Title: "Payroll close checklist", OwnerID: "hc-050-rafael-torres", CanManageAccess: true, CanComment: true},
		Markdown:   "First paragraph.\n\nSecond paragraph.",
		CanComment: true,
		Comments: []DocumentComment{
			{ID: "c-1", Body: "One", CreatedAt: "2026-09-01T10:00:00Z", Start: -1},
			{ID: "c-2", Body: "Two", CreatedAt: "2026-09-02T10:00:00Z", Start: -1},
		},
	}
	d := mountDocs(t, "docs-harness-detail", ui.CreateElement(docsDetail, docsDetailProps{View: view}))

	t.Run("H4 Reply and Cancel keep focus in the thread", func(t *testing.T) {
		d.t = t
		d.click(`[data-docs-action="comment-reply"][data-docs-id="c-1"]`)
		d.waitFor("reply field focused", func() bool { return d.activeIs("#docs-reply-c-1") })
		d.click(`[data-docs-action="comment-reply-cancel"][data-docs-id="c-1"]`)
		d.waitFor("Reply button focused", func() bool { return d.activeIs("#" + docsReplyOpenID("c-1")) })
	})

	t.Run("H4 Resolve moves focus to the next thread", func(t *testing.T) {
		d.t = t
		d.click(`[data-docs-action="comment-resolve"][data-docs-id="c-1"]`)
		d.waitFor("next thread focused", func() bool { return d.activeIs("#docs-thread-c-2") })
	})

	t.Run("M9 the document menu has no menu roles", func(t *testing.T) {
		d.t = t
		if d.count(`.docs-detail [role="menu"], .docs-detail [role="menuitem"]`) != 0 {
			t.Fatal("document menu still declares menu roles")
		}
	})
}
