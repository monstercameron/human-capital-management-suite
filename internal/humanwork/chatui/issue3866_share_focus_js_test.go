//go:build js && wasm

package chatui

import (
	"syscall/js"
	"testing"
)

// issue3866World is a fake page: selector -> element, where an element only
// takes focus when it is focusable. Focus calls are recorded in order.
func issue3866World(t *testing.T, elements map[string]bool) (focused *[]string, active func() string) {
	t.Helper()
	global := js.Global()
	oldDoc, oldRAF := global.Get("document"), global.Get("requestAnimationFrame")
	log := []string{}
	current := ""
	doc := global.Get("Object").New()
	nodes := map[string]js.Value{}
	var funcs []js.Func
	keep := func(f js.Func) js.Func { funcs = append(funcs, f); return f }
	for selector, focusable := range elements {
		selector, focusable := selector, focusable
		node := global.Get("Object").New()
		node.Set("focus", keep(js.FuncOf(func(js.Value, []js.Value) any {
			log = append(log, selector)
			if focusable {
				current = selector
				doc.Set("activeElement", node)
			}
			return nil
		})))
		nodes[selector] = node
	}
	doc.Set("querySelector", keep(js.FuncOf(func(_ js.Value, args []js.Value) any {
		if node, ok := nodes[args[0].String()]; ok {
			return node
		}
		return js.Null()
	})))
	global.Set("document", doc)
	global.Set("requestAnimationFrame", keep(js.FuncOf(func(_ js.Value, args []js.Value) any {
		args[0].Invoke()
		return 1
	})))
	t.Cleanup(func() {
		global.Set("document", oldDoc)
		global.Set("requestAnimationFrame", oldRAF)
		for _, f := range funcs {
			f.Release()
		}
	})
	return &log, func() string { return current }
}

func TestIssue3866_CancelReturnsFocusToMessageWhenMenuButtonCannotTakeIt(t *testing.T) {
	menu := shareReturnSelectors("m1")[0]
	article := shareReturnSelectors("m1")[1]
	log, active := issue3866World(t, map[string]bool{menu: false, article: true, "#chat-composer": true})
	shareTriggerID = "m1"
	RestoreShareFocus()
	if active() != article {
		t.Fatalf("focus landed on %q after trying %v, want the source message", active(), *log)
	}
}

func TestIssue3866_CancelReturnsFocusToMoreActionsWhenAvailable(t *testing.T) {
	menu := shareReturnSelectors("m1")[0]
	_, active := issue3866World(t, map[string]bool{menu: true, shareReturnSelectors("m1")[1]: true, "#chat-composer": true})
	shareTriggerID = "m1"
	RestoreShareFocus()
	if active() != menu {
		t.Fatalf("focus landed on %q, want the More actions button", active())
	}
}

func TestIssue3866_ComposerOnlyWhenMessageIsGone(t *testing.T) {
	_, active := issue3866World(t, map[string]bool{"#chat-composer": true})
	shareTriggerID = "gone"
	RestoreShareFocus()
	if active() != "#chat-composer" {
		t.Fatalf("focus landed on %q", active())
	}
}
