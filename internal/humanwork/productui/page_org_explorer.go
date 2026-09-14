package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// orgExplorerPage is the route adapter for the organization explorer. It
// reuses the live admitted organization projection so the specialized route
// cannot drift into a second hierarchy or disclosure boundary.
func orgExplorerPage(view View) ui.Node {
	return organizationRoutePage(view, false)
}
