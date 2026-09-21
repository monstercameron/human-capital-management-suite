//go:build js && wasm

package main

import (
	"syscall/js"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/journey"
)

// TestTodo_REV_095_01_Browser drives the production browser adapter
// (bindActionableNoticeFocus) that the journey client's refused edit
// proposal reaches: a new FocusInvalidRevision focuses the first
// aria-invalid control in the document without waiting for any server
// answer, and typing (no new revision) does not steal focus back.
func TestTodo_REV_095_01_Browser(t *testing.T) {
	object := js.Global().Get("Object")
	oldDocument := js.Global().Get("document")
	oldAnimationFrame := js.Global().Get("requestAnimationFrame")
	callbacks := make([]js.Func, 0, 6)
	bind := func(target js.Value, name string, fn func(js.Value, []js.Value) any) {
		callback := js.FuncOf(fn)
		callbacks = append(callbacks, callback)
		target.Set(name, callback)
	}
	grade := object.New()
	grade.Set("id", "edit-grade")
	focused := []string{}
	bind(grade, "focus", func(js.Value, []js.Value) any {
		focused = append(focused, "edit-grade")
		return nil
	})
	bind(grade, "scrollIntoView", func(js.Value, []js.Value) any { return nil })
	document := object.New()
	selectors := []string{}
	bind(document, "querySelector", func(_ js.Value, args []js.Value) any {
		selector := args[0].String()
		selectors = append(selectors, selector)
		if selector == `[aria-invalid="true"]` {
			return grade
		}
		return nil
	})
	animationFrame := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) == 1 {
			args[0].Invoke()
		}
		return 1
	})
	callbacks = append(callbacks, animationFrame)
	js.Global().Set("document", document)
	js.Global().Set("requestAnimationFrame", animationFrame)
	t.Cleanup(func() {
		js.Global().Set("document", oldDocument)
		js.Global().Set("requestAnimationFrame", oldAnimationFrame)
		for _, callback := range callbacks {
			callback.Release()
		}
	})

	store := journey.NewStore(journey.Page{})
	bindActionableNoticeFocus(store)
	refused := journey.Page{
		FocusInvalidRevision: 1,
		Notice:               &journey.Notice{Tone: "warning", Title: "Complete the required fields"},
		Detail: &journey.DetailView{Actions: []journey.Action{{ID: "edit-proposal", Fields: []journey.Field{
			{ID: "edit-grade", Name: "target_grade", Required: true, Error: "Complete this field."},
		}}}},
	}
	store.Set(refused)
	if len(focused) != 1 || focused[0] != "edit-grade" {
		t.Fatalf("refused edit focused %v, want the first invalid field", focused)
	}
	// Typing re-renders with the same revision: focus stays with the reader.
	refused.Values = map[string]string{"edit-grade": "M"}
	store.Set(refused)
	if len(focused) != 1 {
		t.Fatalf("typing moved focus again: %v", focused)
	}
	// A repeated refusal asks again.
	refused.FocusInvalidRevision = 2
	store.Set(refused)
	if len(focused) != 2 {
		t.Fatalf("a repeated refusal did not refocus: %v", focused)
	}
	for _, selector := range selectors {
		if selector != `[aria-invalid="true"]` {
			t.Fatalf("invalid-field focus consulted %q; it must target the invalid control, not the notice", selector)
		}
	}
}
