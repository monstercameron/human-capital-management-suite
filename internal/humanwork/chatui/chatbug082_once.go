package chatui

// CHATBUG-082: a widget failure is said once. Conversation details says it in
// About, where the purpose it could not read would be. The bar above the
// messages says it too (it is the only place while details is closed), so with
// details open the same sentence and the same Try again stood on the page
// twice. chatbug082DetailsSaysFailure reports whether details is drawing it.
func chatbug082DetailsSaysFailure(m Model) bool {
	if m.ChannelWidgetsError == "" || !m.ShowDetails || m.searching() || m.ShowPerson {
		return false
	}
	c := m.selected()
	if m.ShowThread && !c.Agent {
		// The thread pane has the side column; details is not on the page.
		return false
	}
	return c.Kind == PublicChannel || c.Kind == PrivateChannel
}
