package chatui

import "strings"

// pickSuggestOpen is whether the recipient search has its suggestion list
// open: the viewer typed a name, or the add-people dialog lists people before
// any typing, and Escape has not dismissed the list since the last keystroke.
// Escape closes this list before it closes the dialog around it (issue 3876).
func pickSuggestOpen(m Model, local localUI, kind ConversationKind) bool {
	if !m.ShowCreate && !m.ShowAddMembers {
		return false
	}
	if kind == DirectMessage && len(local.picked) >= 1 {
		return false
	}
	if local.pickDismissed {
		return false
	}
	return strings.TrimSpace(local.pickQuery) != "" || m.ShowAddMembers
}
