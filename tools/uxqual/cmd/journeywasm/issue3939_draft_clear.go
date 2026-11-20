package main

import "strings"

// Issue 3939. Typing is debounced, but emptying the composer (an ordinary
// clear, or Cancel on a /todo or /poll preview) is not: a reload inside the
// debounce window restored the text the person had just removed. An empty
// draft is therefore flushed at once; any other change joins the pending write.
func (w *chatDraftWriter) Changed(value string) {
	if strings.TrimSpace(value) == "" {
		w.Flush()
		return
	}
	w.Schedule()
}
