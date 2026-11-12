package productui

// uxblindQDialogAppeared reports whether a dialog has just come into view over
// the document. Only that change closes an open disclosure menu. A dialog that
// was already showing when the menu opened, and the menu's own open-attribute
// change, are not a reason to close it: the menu would otherwise close at the
// moment it opens (issue 3596).
func uxblindQDialogAppeared(wasVisible, nowVisible bool) bool {
	return nowVisible && !wasVisible
}
