package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// orgResponsivePage is the responsive route adapter. It reuses the same
// admitted projection and semantic disclosures as the desktop surface.
func orgResponsivePage(view View) ui.Node {
	return organizationRoutePage(view, false)
}
