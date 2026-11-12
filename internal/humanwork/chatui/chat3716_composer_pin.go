package chatui

// CHAT-3716: a message list counts as on its newest message when no more than
// this many pixels of it lie below the viewport. The composer pin uses it to
// decide whether the list follows a growing composer.
const chat3716NewestSlack = 48

// chat3716AtNewest reports whether a message list is on its newest message.
func chat3716AtNewest(scrollHeight, scrollTop, clientHeight float64) bool {
	return scrollHeight-scrollTop-clientHeight < chat3716NewestSlack
}
