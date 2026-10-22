package chatui

import (
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// CHATUX-041: a form in Conversation details keeps its main button disabled
// until a field differs from what is saved. The fields stay uncontrolled (the
// reconciler never writes over what is typed); the only state is one flag per
// form in the panel's own local state, set by the form's input handler from the
// live field values. A flag's key carries what the field was compared with, so a
// save that changes the saved value ends the flag without anyone clearing it.

const chatux041InputAction = "details-input"

// chatux041DirtyKey is the local-state key of a form's flag.
func chatux041DirtyKey(m Model, form string) string {
	switch form {
	case "project":
		return "dirty:project:" + m.SelectedID + ":" + strconv.FormatUint(m.ChannelProject.Revision, 10)
	case "purpose":
		return "dirty:purpose:" + m.SelectedID + ":" + strings.TrimSpace(m.ChannelTeam.Purpose)
	case "milestone":
		return "dirty:milestone:" + m.SelectedID + ":" + strconv.Itoa(len(m.ChannelProject.Milestones))
	}
	return "dirty:" + form
}

// chatux041Dirty reports whether the person has changed the form's fields.
func chatux041Dirty(m Model, h handlers, form string) bool {
	return h.local.detailGroups[chatux041DirtyKey(m, form)]
}

// chatux041FormDirty compares a form's live fields with what is saved.
func chatux041FormDirty(m Model, form string) bool {
	switch form {
	case "project":
		return domValue("channel-project-title") != m.ChannelProject.Title || domValue("channel-project-summary") != m.ChannelProject.Summary
	case "purpose":
		return strings.TrimSpace(domValue(purposeFieldID)) != strings.TrimSpace(m.ChannelTeam.Purpose)
	case "milestone":
		return strings.TrimSpace(domValue("channel-milestone-new")) != ""
	}
	return false
}

// chatux041Input is the input handler of the panel's forms. A field names its
// form in data-id; a checkbox for a status change names the status.
func chatux041Input(e ui.InputEvent, m Model, local localStore) {
	action, id, _ := eventAction(e)
	if action != chatux041InputAction {
		return
	}
	key, value := chatux041DirtyKey(m, id), false
	if strings.HasPrefix(id, "ack:") {
		key, value = id, filterChecked("chatstate-ack")
	} else {
		value = chatux041FormDirty(m, id)
	}
	if local.get().detailGroups[key] != value {
		local.update(func(u *localUI) { u.setDetailGroup(key, value) })
	}
}
