package chatui

// issue3883CloseLayerKind is the layer whose opener takes focus back when the
// action closes it. Escape already did this; the Close details button did not,
// so focus fell to the page (issue 3883).
func issue3883CloseLayerKind(action string) string {
	if action == "close-details" {
		return "details"
	}
	// Issue 3722: the Close thread button restores focus the way Escape does.
	if action == "close-thread" {
		return "thread"
	}
	return ""
}
