//go:build js && wasm

package chatui

import (
	"sync"
	"syscall/js"
)

var selectReappendGuardOnce sync.Once

// InstallSelectReappendGuard keeps a select's chosen option chosen when the
// reconciler appends the select's own options to it again (see
// selectReappendKeepsChoice for why that otherwise moves the choice to the last
// option). It wraps `append` on the select and option-group prototypes only, so
// no other element pays for it, and it is installed once for the page: by the
// workspace client when it starts, and by Chat's field sync for a host that did
// not.
func InstallSelectReappendGuard() {
	selectReappendGuardOnce.Do(func() {
		for _, name := range []string{"HTMLSelectElement", "HTMLOptGroupElement"} {
			constructor := js.Global().Get(name)
			if constructor.Type() != js.TypeFunction {
				continue
			}
			prototype := constructor.Get("prototype")
			if prototype.Type() != js.TypeObject {
				continue
			}
			original := prototype.Get("append")
			if original.Type() != js.TypeFunction {
				continue
			}
			prototype.Set("append", selectAppendKeepingChoice(original))
		}
	})
}

// selectAppendKeepingChoice wraps one `append` implementation. The wrapper is
// called with the select (or an option group inside one) as its receiver.
func selectAppendKeepingChoice(original js.Value) js.Func {
	return js.FuncOf(func(this js.Value, args []js.Value) any {
		owner, chosen := selectChoiceToKeep(this, args)
		result := selectCallAppend(original, this, args)
		selectChooseAgain(owner, chosen)
		return result
	})
}

// selectChoiceToKeep returns the select that owns node and the option that must
// still be chosen after args are appended to node, or undefined values when the
// append needs no care. It never panics: an append must go through whatever the
// receiver turns out to be.
func selectChoiceToKeep(node js.Value, args []js.Value) (owner, chosen js.Value) {
	owner, chosen = js.Undefined(), js.Undefined()
	defer func() {
		if recover() != nil {
			owner, chosen = js.Undefined(), js.Undefined()
		}
	}()
	if node.Type() != js.TypeObject {
		return owner, chosen
	}
	selectEl := node
	if tag := node.Get("tagName"); tag.Type() != js.TypeString || tag.String() != "SELECT" {
		if node.Get("closest").Type() != js.TypeFunction {
			return owner, chosen
		}
		selectEl = node.Call("closest", "select")
	}
	if selectEl.Type() != js.TypeObject {
		return owner, chosen
	}
	moves, brings := false, false
	for _, arg := range args {
		if arg.Type() != js.TypeObject {
			continue
		}
		if arg.Get("parentNode").Equal(node) {
			moves = true
			continue
		}
		if selected := arg.Get("selected"); selected.Type() == js.TypeBoolean && selected.Bool() {
			brings = true
		}
	}
	index := -1
	if value := selectEl.Get("selectedIndex"); value.Type() == js.TypeNumber {
		index = value.Int()
	}
	if !selectReappendKeepsChoice(selectEl.Get("multiple").Truthy(), index, moves, brings) {
		return owner, chosen
	}
	return selectEl, selectEl.Get("options").Index(index)
}

// selectCallAppend runs the browser's own append. A failure there is reported
// to the console instead of ending the client: a panic inside a callback the
// browser invoked cannot be handed back to the caller as an exception.
func selectCallAppend(original, node js.Value, args []js.Value) (result js.Value) {
	result = js.Undefined()
	defer func() {
		if failure := recover(); failure != nil {
			if console := js.Global().Get("console"); console.Truthy() && console.Get("error").Type() == js.TypeFunction {
				message := "select append failed"
				if err, ok := failure.(error); ok {
					message += ": " + err.Error()
				}
				console.Call("error", message)
			}
		}
	}()
	list := make([]any, 0, len(args)+1)
	list = append(list, node)
	for _, arg := range args {
		list = append(list, arg)
	}
	return original.Call("call", list...)
}

// selectChooseAgain chooses the kept option again when the append left another
// one chosen and the option is still inside its select.
func selectChooseAgain(owner, chosen js.Value) {
	defer func() { _ = recover() }()
	if owner.Type() != js.TypeObject || chosen.Type() != js.TypeObject {
		return
	}
	if !owner.Call("contains", chosen).Truthy() || chosen.Get("selected").Truthy() {
		return
	}
	chosen.Set("selected", true)
}
