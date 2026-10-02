package chatui

import "testing"

// ComposerMarkupForTest renders the whole chat composer. With commandQuery set
// the "/" command list is open on that query, so a test outside this package can
// render it against the product's own catalog.
func ComposerMarkupForTest(t *testing.T, m Model, commandQuery string, commandsOpen bool) string {
	t.Helper()
	h := handlers{}
	if commandsOpen {
		h.local.commandMenu = composerCommandMenu{Target: "chat-composer", Query: commandQuery, Open: true}
	}
	return renderNode(t, composer(m, h))
}
