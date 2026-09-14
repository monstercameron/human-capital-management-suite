package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// myselfPage resolves self-service data exclusively through the worker
// identity bound to the admitted principal. Query parameters cannot select a
// different person on this route.
func myselfPage(view View) ui.Node {
	props := MyselfPageProps{I18nProps: I18nProps{Locale: view.Locale}}
	person, ok := viewerPerson(view)
	if !ok || !DiscoveryAdmitted(person.ID, view.RecordVerdicts) {
		return ui.CreateElement(MyselfPage, props)
	}
	profile := personProfileProps(view, person, PageMyself)
	profile.Compensation.Title = view.Locale.Text("myself.payroll_title")
	profile.Compensation.Description = view.Locale.Text("myself.payroll_detail")
	profile.Compensation.Notice = view.Locale.Text("myself.payroll_boundary")
	profile.History.Title = view.Locale.Text("myself.history_title")
	profile.History.Description = view.Locale.Text("myself.history_detail")
	props.Profile = &profile
	props.OrganizationTitle = view.Locale.Text("myself.organization_title")
	props.OrganizationDescription = view.Locale.Text("myself.organization_detail")
	props.OrganizationTree = myselfOwnershipSubtree(view, person.ID)
	props.OrganizationTreeLabel = view.Locale.Text("organization.tree_label")
	return ui.CreateElement(MyselfPage, props)
}

// myselfOwnershipSubtree scopes the reporting-line tree to personID and their
// own reports, rather than the whole visible organization -- UXAUDIT-004's
// REFACTOR names this explicitly as the "Myself subtree", and it consumes
// the exact same authorized relationship projection ownershipTree does
// (organization_relationships.go), never a second source of hierarchy truth.
// personID's own Level is renumbered to 1 so it displays as the root of its
// own subtree; a viewer with no visible reports simply gets one root node.
func myselfOwnershipSubtree(view View, personID string) []OwnershipNodeProps {
	node, ok := newOrganizationRelationshipIndex(view).findAndReroot(view, personID)
	if !ok {
		return nil
	}
	return []OwnershipNodeProps{node}
}

func viewerPerson(view View) (Person, bool) {
	for _, person := range view.People {
		if person.ID != "" && person.ID == view.Viewer.PersonID {
			return person, true
		}
	}
	return Person{}, false
}
