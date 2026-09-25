package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

type projectsPageModuleRenderer struct{}

func (projectsPageModuleRenderer) Render(view View) ui.Node { return projectsPage(view) }

type projectPageModuleRenderer struct{}

func (projectPageModuleRenderer) Render(view View) ui.Node { return projectPage(view) }
