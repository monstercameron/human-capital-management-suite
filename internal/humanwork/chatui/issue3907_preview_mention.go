package chatui

// Issue 3907. Preview shows the draft the way a sent message will read, but a
// mention was drawn as plain words because the preview was given no references:
// a sent message resolves "@Name" against the references the service stored,
// and a draft's references live beside it (the composer's mention store).
//
// issue3907PreviewMentions gives the preview those references, so a person the
// draft really mentions is the same chip a sent message shows, with the same
// accessible name and the same way to open their details. Text that only
// resembles a mention has no reference and stays text, and so does a person who
// is not in the conversation, because nothing will notify them.
func issue3907PreviewMentions(m Model, refs []ChatReference) Model {
	m.renderReferences = refs
	return m
}
