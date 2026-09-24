package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

func positionOccupancyPage(view View) ui.Node {
	return ui.CreateElement(PositionOccupancyPage, positionOccupancyPageProps{View: view})
}
