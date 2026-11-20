package productui

import "strings"

// Issue 900003: a portrait that fails to load is replaced by the person's
// initials on every page, including a page whose client never mounts the
// component that owns the image. The server-rendered <img> carries the label
// it falls back to, and a page-wide guard (issue900003_portrait_guard_wasm.go)
// swaps any failed avatar portrait for it, whether the failure happened before
// the client started or after.

// portraitInitialsAttr names the attribute that carries the fallback label.
const portraitInitialsAttr = "data-portrait-initials"

// PortraitGuardSelector selects the avatar portraits the guard watches.
const PortraitGuardSelector = "img.avatar[" + portraitInitialsAttr + "]"

// portraitFallbackLabel is the label an avatar portrait falls back to: the
// label the caller supplied, else the two letters of the name.
func portraitFallbackLabel(name, initials string) string {
	if label := strings.TrimSpace(initials); label != "" {
		return label
	}
	return viewerInitials(name)
}
