package chatui

// CHATSIDE-001: dragging a row onto a section moves it there. The sidebar marks
// each row with the kind of conversation it is and each section with the kind
// it takes, so the browser half can tell where a drop is allowed without asking
// the application. Keyboard and touch use the row menu's Move to.

// chatside001KindOf is the kind a row carries for the drag: "direct" for direct
// messages and groups, "channel" for everything else.
func chatside001KindOf(c Conversation) string {
	if c.Kind == DirectMessage || c.Kind == GroupChat {
		return "direct"
	}
	return "channel"
}

// chatside001Accepts is what a section takes: the Channels section channels,
// Direct messages its own kind, and Favorites and the person's sections any.
func chatside001Accepts(sectionID string) string {
	switch sectionID {
	case "channels":
		return "channel"
	case "direct":
		return "direct"
	}
	return "any"
}

const (
	// chatside001LongPressMs is how long a finger rests on a row before the row
	// menu opens, and chatside001LongPressSlop how far it may wander meanwhile
	// before the press becomes a scroll instead.
	chatside001LongPressMs   = 550
	chatside001LongPressSlop = 8.0
)

// chatside001Gesture is what a press on a sidebar row can become, by the kind of
// pointer: a mouse or a pen drags the row onto a section, and a finger does not
// drag (it scrolls the list) but a long press opens the row menu, whose Move to
// does the same move.
func chatside001Gesture(pointerType string) (drag, longPress bool) {
	if pointerType == "touch" {
		return false, true
	}
	return true, false
}
