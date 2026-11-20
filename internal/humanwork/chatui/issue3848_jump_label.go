package chatui

// issue3848JumpLabel names the floating shortcut. While a newer page is still
// unloaded, the list also offers "Show newer messages" (one page, in place), so
// the shortcut says it skips ahead to the last message instead.
func issue3848JumpLabel(m Model) string {
	if m.HasNewer {
		return m.t(KeyJumpNewestSkip)
	}
	return m.t(KeyJumpNewest)
}
