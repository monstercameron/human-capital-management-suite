package chatui

// ClearRefusedChannel returns the page to its ordinary Chat state after the
// address stopped naming a conversation the reader was refused. A refused or
// unavailable link leaves the selection on that room and the page in
// StateError; with the fragment gone nothing else resets it, so the whole
// workspace kept showing the refusal. It reports whether it changed anything.
// A room that is still listed, or one being previewed, is not a refusal and is
// left alone.
func ClearRefusedChannel(m *Model) bool {
	if m == nil || m.State != StateError || m.SelectedID == "" || m.PreviewConversation != nil {
		return false
	}
	for _, room := range m.Conversations {
		if room.ID == m.SelectedID {
			return false
		}
	}
	m.SelectedID = ""
	m.State, m.Error = StateReady, ""
	m.Messages, m.HasOlder = nil, false
	return true
}
