package chatui

// purposeKept is a channel's unsaved purpose: the editor was open when the
// viewer moved to another conversation (#3909).
type purposeKept struct{ draft string }

// keepPurposeEdit sets aside the open purpose editor of the conversation being
// left, so coming back finds it as it was. Nothing is saved.
func (u *localUI) keepPurposeEdit(room string) {
	if room == "" || !u.purposeEditing {
		return
	}
	if u.purposeByRoom == nil {
		u.purposeByRoom = map[string]purposeKept{}
	}
	u.purposeByRoom[room] = purposeKept{draft: u.purposeDraft}
}

// restorePurposeEdit reopens the editor of a conversation that was left with
// unsaved text, and forgets it.
func (u *localUI) restorePurposeEdit(room string) {
	kept, ok := u.purposeByRoom[room]
	if !ok {
		return
	}
	delete(u.purposeByRoom, room)
	u.purposeEditing, u.purposeDraft = true, kept.draft
}

// issue3909RememberPurpose records the purpose box's text as it is typed. It
// writes without asking for a render: the box already shows what was typed.
func issue3909RememberPurpose(local localStore, form, value string) {
	if form == "purpose" && local.box.purposeEditing {
		local.box.purposeDraft = value
	}
}
