//go:build !(js && wasm)

package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// Outside the browser there is no selection, highlight registry or scroll;
// server rendering shows comments and their quotes as plain content.
func docsCurrentSelection() (quote, prefix, suffix string, ok bool) { return "", "", "", false }
func docsSelectionFromChatProjection() bool                         { return false }
func docsPaintAnchors([]DocumentComment, string)                    {}
func docsRevealAnchor(string) bool                                  { return false }
func docsAnchorAt(ui.Event) string                                  { return "" }
func docsWatchSelection() func()                                    { return func() {} }
func docsClearSelection()                                           {}
func docsFocusElement(string)                                       {}

func docsLayoutAnchors([]string, string) {}
func docsFlashAnchor(string)             {}
func docsLinkedAt(ui.Event) string       { return "" }
func docsScrollIntoView(string)          {}
