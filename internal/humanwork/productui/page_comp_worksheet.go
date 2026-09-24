package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// compWorksheetPage renders scoped worksheet rows and submits governed intents.
func compWorksheetPage(view View) ui.Node {
	return managerCompensationPage(view, "comp_worksheet", "worksheet")
}
