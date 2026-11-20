package chatui

// Issues #3901 and #3895. The reply box of a thread holds two things the page
// must not lose or leak: the choice "Also send to the conversation", which
// belongs to one thread's reply, and the text of the draft as it is in the
// field right now. Both used to live only in the browser's field, so they
// followed the pane to the next thread, vanished when the thread was reopened or
// the person came back from another page, and fell back to an older copy of the
// draft when the toolbar or the preview redrew the form.

// threadChoiceUI is the reply boxes' own state, kept in localUI.
type threadChoiceUI struct {
	// also is the checkbox by root message, written the moment it changes.
	also map[string]bool
	// live is the field's text captured just before a redraw that the model
	// cannot yet follow. It counts only while the model still holds what it held
	// then (base), so any newer draft the model learns of wins over it.
	live threadLiveDraft
}

type threadLiveDraft struct {
	parent, base, value string
	ok                  bool
}

// readThreadField reads the open reply field. The browser build replaces it.
var readThreadField = func() (string, bool) { return "", false }

// threadDraftNow is the reply box's text for the open thread: what the field
// held when it was last captured, else the model's draft.
func threadDraftNow(m Model, local localUI) string {
	draft := m.ThreadDrafts[m.ThreadParentID]
	if l := local.threadUI.live; l.ok && l.parent == m.ThreadParentID && l.base == draft {
		return l.value
	}
	return draft
}

// captureThreadDraft keeps the field's text where the next redraw will find it
// and tells the model, before a toolbar or preview change redraws the form.
func captureThreadDraft(m Model, local localStore) {
	parent := m.ThreadParentID
	if parent == "" {
		return
	}
	value, ok := readThreadField()
	if !ok {
		return
	}
	local.box.threadUI.live = threadLiveDraft{parent: parent, base: m.ThreadDrafts[parent], value: value, ok: true}
	if m.Callbacks.ThreadDraftChanged != nil {
		m.Callbacks.ThreadDraftChanged(parent, value)
	}
}

// threadAlsoChecked is whether the open thread's reply is also to be sent to
// the conversation: this page's own choice first, else the one the model kept.
func threadAlsoChecked(m Model, local localUI) bool {
	if m.ThreadParentID == "" {
		return false
	}
	if v, ok := local.threadUI.also[m.ThreadParentID]; ok {
		return v
	}
	return m.ThreadAlso[m.ThreadParentID]
}

// setThreadAlso records the choice for one thread's reply, here and in the model.
func setThreadAlso(m Model, local localStore, parent string, checked bool) {
	if parent == "" {
		return
	}
	local.update(func(u *localUI) {
		next := make(map[string]bool, len(u.threadUI.also)+1)
		for k, v := range u.threadUI.also {
			next[k] = v
		}
		next[parent] = checked
		u.threadUI.also = next
	})
	if m.Callbacks.ThreadAlsoChanged != nil {
		m.Callbacks.ThreadAlsoChanged(parent, checked)
	}
}
