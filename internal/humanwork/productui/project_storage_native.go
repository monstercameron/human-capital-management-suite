//go:build !(js && wasm)

package productui

// Outside the browser there is no per-viewer storage.
func projectStorageRead(string) string { return "" }

func projectStorageWrite(string, string) {}
