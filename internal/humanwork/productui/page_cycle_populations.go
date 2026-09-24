package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// cyclePopulationsPage renders the admitted subjects in the active cycle.
func cyclePopulationsPage(view View) ui.Node {
	return managerCompensationPage(view, "cycle_populations", "")
}
