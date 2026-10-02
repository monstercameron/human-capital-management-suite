//go:build js && wasm

package main

import "syscall/js"

// domAttribute reads a DOM attribute without converting null or another
// non-string JavaScript value into the literal text "<null>".
func domAttribute(node js.Value, name string) string {
	if !node.Truthy() || name == "" || node.Get("getAttribute").Type() != js.TypeFunction {
		return ""
	}
	value := node.Call("getAttribute", name)
	return normalizedDOMAttribute(value.String(), value.Type() == js.TypeString)
}

// domDataset reads one optional data-* value without stringifying undefined.
func domDataset(node js.Value, key string) string {
	if !node.Truthy() || key == "" {
		return ""
	}
	dataset := node.Get("dataset")
	if dataset.Type() != js.TypeObject {
		return ""
	}
	value := dataset.Get(key)
	return normalizedDOMAttribute(value.String(), value.Type() == js.TypeString)
}
