//go:build !js || !wasm

package productui

// Static rendering has no browser focus lifecycle.
func usePopoverFocusDismissal(rootID, triggerID string, open bool, dismiss func()) {}

// focusElementByID is a browser-only operation. Keeping the native stub here
// lets components share their focus-restoration path across SSR and WASM.
func focusElementByID(_ any, _ string) {}

func focusPopoverElement(_ string) {}

func useMobileNavigationDrawer(_, _, _ string) {}
