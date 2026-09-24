//go:build !(js && wasm)

package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// Outside the browser there are no clicks to read and no files to take:
// the server renders the reader and the editor without them.

func docsMediaEventTarget(ui.Event) (action, id string) { return "", "" }

func docsEditorMediaListen(func(string, []byte)) func() { return func() {} }
