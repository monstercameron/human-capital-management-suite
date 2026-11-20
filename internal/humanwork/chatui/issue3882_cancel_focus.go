package chatui

// Issue 3882: closing a Manage channel editor with its Cancel button returns
// keyboard focus to the control that opened it (the Status or Project and
// milestones row, or the Add milestone button).

// issue3882MilestoneToggleID is the id of the Add milestone button.
const issue3882MilestoneToggleID = "chat-manage-milestone-toggle"

// issue3882ReturnFocusID is the control focus goes back to after a Manage
// channel row (id) is pressed while it shows state "1" (open), that is, when
// the press closes it. It is empty when the press opens the row.
func issue3882ReturnFocusID(id, state string) string {
	switch {
	case state != "1" || id == "":
		return ""
	case id == "milestone":
		return issue3882MilestoneToggleID
	}
	return "chat-manage-" + id + "-row"
}
