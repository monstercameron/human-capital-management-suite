package chatui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// openDocReference sends a plain click on a document link in a message
// through the application's history instead of a full page load. A click
// with a modifier (new tab, new window) keeps the browser's own behaviour.
func openDocReference(e ui.Event, m Model, id string) {
	if docReferenceNavigate(m, id, eventPlainClick(e)) {
		e.PreventDefault()
	}
}

// docReferenceNavigate reports whether it navigated in-app.
func docReferenceNavigate(m Model, id string, plain bool) bool {
	if !plain || m.Callbacks.Navigate == nil || !validDocReferenceID(id) {
		return false
	}
	m.Callbacks.Navigate(DocReferenceURL(id))
	return true
}
