package chatui

// The preview's settings are radio groups made of buttons. A radio group is one
// Tab stop at the chosen option and the arrow keys move the choice and the
// focus inside it (#3991); Space on the focused option already presses it.

// chatcmd003RadioID is the id of one option of a preview setting, the place the
// focus returns to after an arrow key moved the choice.
func chatcmd003RadioID(group, option string) string {
	return "chatcmd003-" + group + "-" + option
}

// chatcmd003RadioStep is the option an arrow key moves to from current, or ""
// when the key is not one that moves a radio group. Down and Up follow the
// order of the options; Left and Right follow the way the row reads, so they
// turn round in a right-to-left language. Moving past either end wraps.
func chatcmd003RadioStep(pressed string, rtl bool, options []string, current string) string {
	if len(options) == 0 {
		return ""
	}
	at := 0
	for i, option := range options {
		if option == current {
			at = i
		}
	}
	switch pressed {
	case "ArrowDown":
		at++
	case "ArrowUp":
		at--
	case "ArrowRight":
		if rtl {
			at--
		} else {
			at++
		}
	case "ArrowLeft":
		if rtl {
			at++
		} else {
			at--
		}
	case "Home":
		at = 0
	case "End":
		at = len(options) - 1
	default:
		return ""
	}
	return options[(at+len(options))%len(options)]
}

// chatcmd003RadioKey answers a key pressed on the option current of the
// setting group, whose options are listed in order. A key that moves the group
// chooses the option it lands on and names the id to focus; any other key is
// not the group's.
func chatcmd003RadioKey(m Model, local localStore, pressed, group, current string, options []string) (focus string, ok bool) {
	next := chatcmd003RadioStep(pressed, agentReplyDirection(m.Locale) == "rtl", options, current)
	if next == "" {
		return "", false
	}
	if next != current {
		chatcmd003Action(m, local, "chatcmd003-set", group, next)
	}
	return chatcmd003RadioID(group, next), true
}
