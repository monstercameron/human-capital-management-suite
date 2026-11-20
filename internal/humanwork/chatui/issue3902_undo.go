package chatui

import "unicode/utf16"

// composerMinimalEdit finds the smallest UTF-16 range of old that, replaced by
// insert, gives updated. A toolbar action computes a whole new draft; the
// browser's undo history only records an edit made through the editing
// commands, so the change is handed to the field as this one replacement
// (Issue #3902). Offsets match a textarea's selectionStart/End, and neither
// edge is left between the two halves of a surrogate pair.
func composerMinimalEdit(old, updated string) (start, end int, insert string) {
	a, b := utf16.Encode([]rune(old)), utf16.Encode([]rune(updated))
	limit := min(len(a), len(b))
	for start < limit && a[start] == b[start] {
		start++
	}
	if start > 0 && utf16.IsSurrogate(rune(a[start-1])) && a[start-1] < 0xDC00 {
		start--
	}
	tail := 0
	for tail < limit-start && a[len(a)-1-tail] == b[len(b)-1-tail] {
		tail++
	}
	if tail > 0 && a[len(a)-tail] >= 0xDC00 && a[len(a)-tail] <= 0xDFFF {
		tail--
	}
	return start, len(a) - tail, string(utf16.Decode(b[start : len(b)-tail]))
}
