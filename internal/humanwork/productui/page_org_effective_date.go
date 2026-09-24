package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// orgEffectiveDatePage adapts the route view to the effective-date feature
// component. The route adapter carries state, while the feature owns markup.
func orgEffectiveDatePage(view View) ui.Node {
	return ui.CreateElement(OrgEffectiveDatePage, orgEffectiveDatePageProps{View: view})
}
