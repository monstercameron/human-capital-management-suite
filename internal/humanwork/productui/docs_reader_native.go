//go:build !(js && wasm)

package productui

// Outside the browser there is nothing to scroll, hover or highlight.
func docsWatchLayout(func(string)) func() { return func() {} }
func docsSetLinked(string)                {}
func docsPaintDraft(_, _, _ string)       {}
func docsRevealComposer()                 {}
