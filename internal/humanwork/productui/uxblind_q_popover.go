package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// PopoverControllerProps mounts the one document-level controller used by
// transient menus. The component is intentionally transparent: it adds no
// wrapper element, so adopting it cannot change layout or landmark structure.
type PopoverControllerProps struct {
	Children []ui.Node
}

// PopoverController installs the shared menu lifecycle for a product surface
// and returns its children unchanged. Native rendering keeps the details /
// summary baseline; the WASM build enhances it with dismissal and focus
// recovery.
func PopoverController(props PopoverControllerProps) ui.Node {
	useUXBlindQPopoverController()
	return html.Fragment(props.Children...)
}

const (
	uxblindQEventEscape  = "escape"
	uxblindQEventOutside = "outside"
	uxblindQEventSelect  = "select"
	uxblindQDismissEvent = "hcm-popover-dismiss"
)

// uxblindQObservedAttributes are the attribute changes that can reveal a
// modal dialog over an open menu.
var uxblindQObservedAttributes = []string{"aria-hidden", "class", "hidden", "open", "role"}

// uxblindQObserverOptions is the MutationObserver init dictionary. It crosses
// into JavaScript through js.ValueOf, which accepts only nil, bool, numbers,
// string, js.Value, map[string]any and []any; a []string panics. That panic
// ran inside the shell's first effect, aborted the hydration commit and left
// every workspace page on its loading skeleton (UXBLIND-083), so the filter
// is built as []any here where a native test can pin its shape.
func uxblindQObserverOptions() map[string]any {
	filter := make([]any, 0, len(uxblindQObservedAttributes))
	for _, name := range uxblindQObservedAttributes {
		filter = append(filter, name)
	}
	return map[string]any{
		"subtree": true, "childList": true, "attributes": true,
		"attributeFilter": filter,
	}
}

// uxblindQShouldDismiss is the platform-independent decision table shared by
// the browser controller and its native contract tests.
func uxblindQShouldDismiss(event string, inside, selectable bool) bool {
	switch event {
	case uxblindQEventEscape:
		return true
	case uxblindQEventOutside:
		return !inside
	case uxblindQEventSelect:
		return inside && selectable
	default:
		return false
	}
}
