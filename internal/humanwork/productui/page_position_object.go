package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

func positionObjectPage(view View) ui.Node {
	return ui.CreateElement(PositionObjectPage, positionObjectPageProps{View: view})
}
