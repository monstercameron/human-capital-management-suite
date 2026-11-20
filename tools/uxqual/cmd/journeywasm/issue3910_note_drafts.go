package main

// chatsaveNoteDrafts keeps what a person has typed into a Saved note field that
// is not on screen at the moment (issue 3910): changing the filter redraws the
// list, and the item may not be in the new filter. The key is the field's id.
type chatsaveNoteDrafts map[string]string

// remember stores the field's text when it differs from the note already saved.
func (d *chatsaveNoteDrafts) remember(field, value, saved string) {
	if value == saved {
		d.discard(field)
		return
	}
	if *d == nil {
		*d = chatsaveNoteDrafts{}
	}
	(*d)[field] = value
}

func (d chatsaveNoteDrafts) recall(field string) (string, bool) {
	value, ok := d[field]
	return value, ok
}

// discard drops one draft: Save and Cancel are explicit choices.
func (d chatsaveNoteDrafts) discard(field string) { delete(d, field) }

// reset drops every draft: the panel was closed.
func (d *chatsaveNoteDrafts) reset() { *d = nil }

// chatsaveTabFocusID is the element focus goes to after a press selects a tab
// from somewhere other than the tab strip, such as an empty state's link (issue 3878).
func chatsaveTabFocusID(tab string) string { return "chatsave-tab-" + tab }

// chatsaveRestoreNeedsKeep reports whether a field that render has just filled
// with restored text must be marked as typed. The page's field observer sweeps
// every [data-chat-value] box a tick after the redraw and writes the attribute
// (the saved note) over any box it did not see typed in; a restored draft that
// differs from the attribute would be wiped there (issue 3910, seen in the
// served build: the editor stayed open but came back empty).
func chatsaveRestoreNeedsKeep(restored, attribute string) bool { return restored != attribute }
