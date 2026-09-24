package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// compProposalsPage renders the manager's authorized compensation projection.
func compProposalsPage(view View) ui.Node {
	return managerCompensationPage(view, "comp_proposals", "")
}
