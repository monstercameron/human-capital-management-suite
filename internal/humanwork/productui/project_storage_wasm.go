//go:build js && wasm

package productui

import "syscall/js"

// projectStorageRead and projectStorageWrite keep small per-viewer
// presentation choices (a collapsed section). Storage can be unavailable or
// throw; both then fall back to the default.
func projectStorageRead(key string) (value string) {
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

func projectStorageWrite(key, value string) {
	defer func() { _ = recover() }()
	if storage := js.Global().Get("localStorage"); storage.Truthy() {
		storage.Call("setItem", key, value)
	}
}
