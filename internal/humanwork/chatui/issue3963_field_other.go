//go:build !(js && wasm)

package chatui

// issue3963Field: the server-rendered tree has no box to read.
func issue3963Field(string) (string, bool) { return "", false }
