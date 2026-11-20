package chatui

// keepPrivateAnswer sets aside the Private answer choice of the conversation
// being left, so it is still there when its draft is open again. Only an
// "on" choice is kept; a sent or turned-off draft leaves nothing behind.
func (u *localUI) keepPrivateAnswer(room string) {
	if room == "" {
		return
	}
	if !u.privateAnswer {
		delete(u.privateByRoom, room)
		return
	}
	if u.privateByRoom == nil {
		u.privateByRoom = map[string]bool{}
	}
	u.privateByRoom[room] = true
}

// privateAnswerForRoom is the choice kept for a conversation's draft: off unless
// that same draft had it on.
func (u *localUI) privateAnswerForRoom(room string) bool {
	return u.privateByRoom[room]
}
