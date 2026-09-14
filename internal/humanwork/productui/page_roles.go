package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

func rolesPage(view View) ui.Node {
	people := admittedPeople(view)
	for index := range people {
		identity := ResolveWorkerIdentity(view.Locale, people[index], workerIdentityVerdicts(view))
		people[index].Name = identity.Name
		people[index].Role = identity.Role
		people[index].WorkerNumber = ""
		if identity.WorkerNumberStatus == WorkerFactPresent {
			people[index].WorkerNumber = identity.WorkerNumber
		}
	}
	pages := make([]RolePageOption, 0, len(PageDefinitions()))
	for _, definition := range PageDefinitions() {
		pages = append(pages, RolePageOption{ID: definition.ID, Label: view.Locale.Text(definition.LabelKey), Description: view.Locale.Text(definition.SubtitleKey), Published: definition.NavigationPublished})
	}
	return ui.CreateElement(RolesPage, RolesPageProps{
		I18nProps: I18nProps{Locale: view.Locale},
		Roles:     view.AccessRoles, Assignments: view.RoleAssignments, People: people, Query: view.Query, Page: view.RolePage,
		Policies:   view.RoleVisibilityPolicies,
		FilterHref: statefulHref(view, PageRoles), Navigate: view.Navigate,
		Back:            ActionLinkProps{Label: "← " + view.Locale.Text("page.admin.title"), Href: statefulHref(view, PageAdmin), Class: "button secondary", Navigate: view.Navigate},
		PagePermissions: view.RolePagePermissions, Pages: pages,
		CanCreate: view.Can(PageRoles, "create"), CanUpdate: view.Can(PageRoles, "update"),
		OnSaveRole: view.SaveAccessRole, OnAssign: view.SaveWorkerRoleAssignment, OnSavePermission: view.SaveRolePagePermission,
	})
}
