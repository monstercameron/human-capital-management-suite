package chatui

// Browse channels (#3921): Open and Join keep their short visible label, and
// the accessible name adds the channel, so a list of buttons reads "Open
// announcements", "Join random". The button is also described by its row's
// kind and member facts, which tells apart channels with alike names.

func issue3921MetaID(c Conversation) string { return "browse-meta-" + c.ID }

func issue3921ActionAria(label string, c Conversation) map[string]string {
	return map[string]string{"label": label + " " + c.Name, "describedby": issue3921MetaID(c)}
}

// issue3921JoinAria names a Join button. A gated channel's visible label is a
// whole sentence ("Answer 3 questions to join"), so the channel follows after a
// comma instead of running on from it.
func issue3921JoinAria(m Model, c Conversation) map[string]string {
	label := chatgateJoinLabel(m, c)
	aria := issue3921ActionAria(label, c)
	if _, gated := chatgateJoin(m, c); gated {
		aria["label"] = label + ", " + c.Name
	}
	return aria
}
