package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

func reviewParticipantsPage(view View) ui.Node {
	return ui.CreateElement(ReviewParticipantsPage, reviewParticipantsPageProps{View: view})
}
