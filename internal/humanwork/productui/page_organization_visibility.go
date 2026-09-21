package productui

import (
	"sort"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func organizationVisibilityPage(view View) ui.Node {
	units := make([]string, 0)
	seen := map[string]bool{}
	for _, person := range view.People {
		unit := strings.TrimSpace(person.Team)
		if unit != "" && !seen[strings.ToLower(unit)] {
			seen[strings.ToLower(unit)] = true
			units = append(units, unit)
		}
	}
	// Keep configured units visible even when they currently contain no
	// workers; otherwise an administrator could not review or remove a stale
	// qualification after the last employee transferred out.
	for _, policy := range view.RoleVisibilityPolicies {
		for _, configured := range policy.OrganizationUnits {
			unit := strings.TrimSpace(configured)
			if unit != "" && !seen[strings.ToLower(unit)] {
				seen[strings.ToLower(unit)] = true
				units = append(units, unit)
			}
		}
	}
	sort.Slice(units, func(i, j int) bool { return strings.ToLower(units[i]) < strings.ToLower(units[j]) })
	return ui.CreateElement(OrganizationVisibilityPage, OrganizationVisibilityPageProps{
		I18nProps: I18nProps{Locale: view.Locale}, Roles: view.AccessRoles, Policies: view.RoleVisibilityPolicies, AvailableUnits: units, AvailableDomains: AvailableDataDomains(),
		Back:      ActionLinkProps{Label: "← " + view.Locale.Text("page.admin.title"), Href: statefulHref(view, PageAdmin), Class: "button secondary", Navigate: view.Navigate},
		RolesLink: ActionLinkProps{Label: "Manage roles", Href: statefulHref(view, PageRoles), Class: "button secondary", Navigate: view.Navigate},
		Editable:  len(view.EffectivePermissions) == 0 || view.Can(PageOrganizationVisibility, "update"),
		OnSave:    view.SaveRoleVisibility,
		OnPreview: view.PreviewRoleVisibility,
	})
}
