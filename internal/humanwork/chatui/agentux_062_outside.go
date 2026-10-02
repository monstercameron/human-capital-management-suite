package chatui

// AGENTUX-062. An outside press closes a layer, as Escape does, unless the
// layer holds something the person typed. Message menus, reaction pickers and
// the sidebar's panels already closed this way; the channel's to-do list and
// "more" card and the search layer stayed open until Escape or their own close
// button.
//
// A poll form is data entry whose fields are not mirrored in the model, so a
// stray press never closes it. A to-do list closes while its new-task field is
// empty, and the search layer while its query is.

// chatOutsidePress says which of the local layers a press outside every layer
// closes: the channel card (tray) and the search layer.
func chatOutsidePress(local localUI, m Model) (closeTray, closeSearch bool) {
	switch local.tray {
	case "todo":
		closeTray = m.ChannelTodoDraft == ""
	case chatcmd002TrayMore:
		closeTray = true
	}
	closeSearch = local.searchOpen && m.Search == ""
	return closeTray, closeSearch
}
