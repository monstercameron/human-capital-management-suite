//go:build !js || !wasm

package productui

// Static rendering has no browser focus lifecycle.
func usePopoverFocusDismissal(rootID, triggerID string, open bool, dismiss func()) {}

func focusPopoverElement(_ string) {}

func scrollPopoverElementIntoView(_ string) {}

func useMobileNavigationDrawer(_, _, _ string) {}
