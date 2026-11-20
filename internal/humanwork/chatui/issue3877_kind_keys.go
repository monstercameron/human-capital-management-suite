package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// chatissue3877NextKind is the conversation type an arrow, Home or End key
// moves to inside the create dialog's radio group, and whether the key belongs
// to the group at all. Down and Right go forward, Up and Left go back, and the
// horizontal pair flips in a right-to-left language; the ends wrap.
func chatissue3877NextKind(current ConversationKind, key string, rtl bool) (ConversationKind, bool) {
	index := 0
	if current == KindUnchosen {
		// ISSUE-3849: nothing is chosen; forward keys land on the first type and
		// backward keys on the last.
		switch key {
		case "ArrowDown", "ArrowRight":
			if !rtl || key == "ArrowDown" {
				return createKinds[0].kind, true
			}
			return createKinds[len(createKinds)-1].kind, true
		case "ArrowUp", "ArrowLeft":
			if !rtl || key == "ArrowUp" {
				return createKinds[len(createKinds)-1].kind, true
			}
			return createKinds[0].kind, true
		}
	}
	for i, k := range createKinds {
		if k.kind == current {
			index = i
		}
	}
	step := 0
	switch key {
	case "ArrowDown":
		step = 1
	case "ArrowUp":
		step = -1
	case "ArrowRight":
		step = 1
		if rtl {
			step = -1
		}
	case "ArrowLeft":
		step = -1
		if rtl {
			step = 1
		}
	case "Home":
		return createKinds[0].kind, true
	case "End":
		return createKinds[len(createKinds)-1].kind, true
	default:
		return current, false
	}
	n := len(createKinds)
	return createKinds[((index+step)%n+n)%n].kind, true
}

// chatissue3877KindID is the DOM id of one type's radio, so focus can follow
// the arrow key to it.
func chatissue3877KindID(kind ConversationKind) string { return "create-kind-" + string(kind) }

// chatissue3877Key is the radio group's key handler: the arrow moves the
// selection and the focus together, and the hidden kind field follows on the
// re-render.
func chatissue3877Key(m Model, local localStore, e ui.KeyboardEvent) {
	current := createKindOf(m, local.get())
	next, ok := chatissue3877NextKind(current, e.GetKey(), chatux043RTL(m.Locale))
	if !ok {
		return
	}
	e.PreventDefault()
	e.StopPropagation()
	if next == current {
		return
	}
	chatissue3865Select(m, local, next)
	chatissue3877Focus(chatissue3877KindID(next))
}
