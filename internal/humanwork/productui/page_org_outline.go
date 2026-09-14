package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// orgOutlinePage is the accessible outline route adapter. It shares the
// admitted organization graph and selects the semantic tree view.
func orgOutlinePage(view View) ui.Node {
	return organizationRoutePage(view, true)
}
