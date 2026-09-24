//go:build js && wasm

package productui

import (
	"syscall/js"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// docsMediaEventTarget reads the data-media-action (and the enclosing
// data-attachment-id) of a delegated click.
func docsMediaEventTarget(event ui.Event) (action, id string) {
	defer func() { _ = recover() }()
	target := event.JSValue().Get("target")
	if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
		return "", ""
	}
	button := target.Call("closest", "[data-media-action]")
	if !button.Truthy() || button.Get("disabled").Truthy() {
		return "", ""
	}
	action = button.Get("dataset").Get("mediaAction").String()
	if holder := button.Call("closest", "[data-attachment-id]"); holder.Truthy() {
		id = holder.Get("dataset").Get("attachmentId").String()
	}
	return action, id
}

// docsEditorMediaListen takes files dropped on or pasted into the editor's
// panes and hands each one's bytes to upload. It listens on the document in
// the capture phase, so a file never reaches the editor's own text paste
// and drop handling (or the browser's default of opening the file).
func docsEditorMediaListen(upload func(name string, content []byte)) func() {
	doc := js.Global().Get("document")
	inEditor := func(event js.Value) bool {
		target := event.Get("target")
		if !target.Truthy() || target.Get("closest").Type() != js.TypeFunction {
			return false
		}
		return target.Call("closest", "#docs-editor .docs-editor-panes").Truthy()
	}
	hasFiles := func(transfer js.Value) bool {
		if !transfer.Truthy() {
			return false
		}
		types := transfer.Get("types")
		for i := 0; types.Truthy() && i < types.Length(); i++ {
			if types.Index(i).String() == "Files" {
				return true
			}
		}
		return false
	}
	dropping := func(on bool) {
		if editor := doc.Call("getElementById", "docs-editor"); editor.Truthy() {
			editor.Get("classList").Call("toggle", "is-dropping", on)
		}
	}
	take := func(files js.Value) {
		for i := 0; i < files.Length(); i++ {
			file := files.Index(i)
			name := file.Get("name").String()
			var onLoad, onFail js.Func
			release := func() { onLoad.Release(); onFail.Release() }
			onLoad = js.FuncOf(func(_ js.Value, args []js.Value) any {
				defer release()
				array := js.Global().Get("Uint8Array").New(args[0])
				content := make([]byte, array.Length())
				js.CopyBytesToGo(content, array)
				ui.PostAsync(func() { upload(name, content) })
				return nil
			})
			onFail = js.FuncOf(func(js.Value, []js.Value) any { release(); return nil })
			file.Call("arrayBuffer").Call("then", onLoad, onFail)
		}
	}
	type listener struct {
		name string
		fn   js.Func
	}
	var listeners []listener
	listen := func(name string, handler func(js.Value)) {
		fn := js.FuncOf(func(_ js.Value, args []js.Value) any {
			defer func() { _ = recover() }()
			if len(args) > 0 {
				handler(args[0])
			}
			return nil
		})
		doc.Call("addEventListener", name, fn, true)
		listeners = append(listeners, listener{name, fn})
	}
	listen("dragover", func(event js.Value) {
		if inEditor(event) && hasFiles(event.Get("dataTransfer")) {
			event.Call("preventDefault")
			event.Get("dataTransfer").Set("dropEffect", "copy")
			dropping(true)
		}
	})
	listen("dragleave", func(event js.Value) {
		if inEditor(event) {
			dropping(false)
		}
	})
	listen("drop", func(event js.Value) {
		dropping(false)
		transfer := event.Get("dataTransfer")
		if !inEditor(event) || !hasFiles(transfer) || transfer.Get("files").Length() == 0 {
			return
		}
		event.Call("preventDefault")
		event.Call("stopPropagation")
		take(transfer.Get("files"))
	})
	listen("paste", func(event js.Value) {
		data := event.Get("clipboardData")
		if !inEditor(event) || !data.Truthy() || !data.Get("files").Truthy() || data.Get("files").Length() == 0 {
			return
		}
		event.Call("preventDefault")
		event.Call("stopPropagation")
		take(data.Get("files"))
	})
	return func() {
		for _, l := range listeners {
			doc.Call("removeEventListener", l.name, l.fn, true)
			l.fn.Release()
		}
		dropping(false)
	}
}
