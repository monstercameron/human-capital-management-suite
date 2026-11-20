package chatui

// chatissue3865Select changes the create dialog's conversation type and keeps
// each type's draft: leaving a group sets aside its recipients and name, a
// direct message holds at most one recipient, and returning to a group puts the
// set-aside draft back (ISSUE-3865). Channels have their own name box, so a
// private group name is never carried into one.
func chatissue3865Select(m Model, local localStore, next ConversationKind) {
	current := createKindOf(m, local.get())
	local.update(func(u *localUI) {
		if current == GroupChat && next != GroupChat {
			u.groupPicked = append([]mentionCandidate(nil), u.picked...)
		}
		u.createKind = next
		switch {
		case next == DirectMessage && len(u.picked) > 1:
			u.picked = u.picked[:1]
		case next == GroupChat && current != GroupChat && len(u.groupPicked) > 0:
			merged := append([]mentionCandidate(nil), u.picked...)
			seen := map[string]bool{}
			for _, p := range merged {
				seen[p.ID] = true
			}
			for _, p := range u.groupPicked {
				if !seen[p.ID] {
					merged = append(merged, p)
				}
			}
			u.picked = merged
		}
		if next == GroupChat {
			u.groupPicked = nil
		}
	})
}
