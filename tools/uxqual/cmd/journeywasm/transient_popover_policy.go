package main

import "strconv"

const (
	transientPopoverSelector                 = `[data-hcm-transient-popover]`
	defaultTransientPopoverGraceMilliseconds = 180
)

type transientPopoverAction uint8

const (
	transientPopoverKeep transientPopoverAction = iota
	transientPopoverCancelPendingClose
	transientPopoverScheduleClose
	transientPopoverCloseNow
)

// shouldDismissTransientPopover is the DOM-independent interaction policy.
// Pointer travel between descendants is not a leave, and pointer movement
// must not hide a control that currently owns keyboard focus. Focus travel
// inside the disclosure similarly keeps it open.
func shouldDismissTransientPopover(eventType, key string, relatedInside, focusInPopover bool) bool {
	switch eventType {
	case "mouseout":
		return !relatedInside && !focusInPopover
	case "focusout":
		return !relatedInside
	case "keydown":
		return key == "Escape"
	default:
		return false
	}
}

// transientPopoverEventAction makes the delayed-close state transition
// independently testable without a browser runtime.
func transientPopoverEventAction(eventType, key string, relatedInside, focusInPopover bool) transientPopoverAction {
	switch eventType {
	case "mouseover", "focusin":
		return transientPopoverCancelPendingClose
	case "mouseout", "focusout":
		if shouldDismissTransientPopover(eventType, key, relatedInside, focusInPopover) {
			return transientPopoverScheduleClose
		}
	case "keydown":
		if key == "Escape" {
			return transientPopoverCloseNow
		}
	}
	return transientPopoverKeep
}

// Navigation links inside a transient menu close it after activation. Native
// details otherwise remains open when a software route reuses the same shell.
func transientPopoverLinkActivated(eventType string, isLink bool) bool {
	return eventType == "click" && isLink
}

func normalizedTransientPopoverGraceMilliseconds(raw string) int {
	value, err := strconv.Atoi(raw)
	if err != nil || value < 80 || value > 500 {
		return defaultTransientPopoverGraceMilliseconds
	}
	return value
}
