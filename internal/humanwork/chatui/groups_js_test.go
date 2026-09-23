//go:build js && wasm

package chatui

import (
	"syscall/js"
	"testing"
)

func TestTodo_CHAT_032_Browser(t *testing.T) {
	dataset := js.Global().Get("Object").New()
	dataset.Set("action", "rail-move-section")
	dataset.Set("id", "sales")
	dataset.Set("extra", "custom-projects")
	action, id, extra := railActionData(dataset)
	if action != "rail-move-section" || id != "sales" || extra != "custom-projects" {
		t.Fatalf("delegated group action = %q %q %q", action, id, extra)
	}
	dataset.Set("extra", js.Undefined())
	dataset.Set("emoji", "👍")
	_, _, extra = railActionData(dataset)
	if extra != "👍" {
		t.Fatalf("reaction fallback = %q", extra)
	}
}

func TestTodo_CHAT_032_GroupDisclosureFocusAndCancel(t *testing.T) {
	global := js.Global()
	oldDocument, oldFrame := global.Get("document"), global.Get("requestAnimationFrame")
	defer func() { global.Set("document", oldDocument); global.Set("requestAnimationFrame", oldFrame) }()
	focusedInput, focusedSummary := false, false
	focusInput := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) != 1 || !args[0].Get("preventScroll").Bool() {
			t.Error("input focus should preserve scroll")
		}
		focusedInput = true
		return nil
	})
	focusSummary := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) != 1 || !args[0].Get("preventScroll").Bool() {
			t.Error("summary focus should preserve scroll")
		}
		focusedSummary = true
		return nil
	})
	defer focusInput.Release()
	defer focusSummary.Release()
	input := js.ValueOf(map[string]any{"value": "Draft", "focus": focusInput})
	summary := js.ValueOf(map[string]any{"focus": focusSummary})
	formRect := js.FuncOf(func(js.Value, []js.Value) any { return js.ValueOf(map[string]any{"top": 20, "bottom": 80}) })
	scrollRect := js.FuncOf(func(js.Value, []js.Value) any { return js.ValueOf(map[string]any{"top": 0, "bottom": 100}) })
	defer formRect.Release()
	defer scrollRect.Release()
	form := js.ValueOf(map[string]any{"getBoundingClientRect": formRect})
	query := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if len(args) > 0 && args[0].String() == "summary" {
			return summary
		}
		return form
	})
	defer query.Release()
	var scroll js.Value
	closest := js.FuncOf(func(_ js.Value, args []js.Value) any { return scroll })
	defer closest.Release()
	scroll = js.ValueOf(map[string]any{"scrollTop": 0, "getBoundingClientRect": scrollRect})
	details := js.ValueOf(map[string]any{"open": true, "querySelector": query, "closest": closest})
	get := js.FuncOf(func(_ js.Value, args []js.Value) any {
		if args[0].String() == "chat-section-create" {
			return details
		}
		return input
	})
	defer get.Release()
	doc := js.ValueOf(map[string]any{"getElementById": get})
	global.Set("document", doc)
	frame := js.FuncOf(func(_ js.Value, args []js.Value) any { args[0].Invoke(); return nil })
	defer frame.Release()
	global.Set("requestAnimationFrame", frame)
	focusSectionCreate()
	if !focusedInput {
		t.Fatal("open disclosure did not focus its input")
	}
	closeSectionCreate(true)
	if details.Get("open").Bool() || !focusedSummary || input.Get("value").String() != "" {
		t.Fatalf("cancel state open=%v summary=%v value=%q", details.Get("open").Bool(), focusedSummary, input.Get("value").String())
	}
	details.Set("open", false)
	focusedInput = false
	focusSectionCreate()
	if focusedInput {
		t.Fatal("closed disclosure focused hidden input")
	}
}
