package productui

import (
	"sort"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/organization"
)

type orgEffectiveDatePageProps struct {
	View View
}

// OrgEffectiveDatePage renders effective-date organization navigation. The
// governed organization service is not published to this UI yet, so the
// surface keeps an honest unavailable state instead of simulating records.
func OrgEffectiveDatePage(props orgEffectiveDatePageProps) ui.Node {
	view := props.View
	graph := readOrganizationGraph(view)
	if graph.Err != nil {
		return orgEffectiveDateUnavailable(view)
	}
	formValues := currentPageAddressState(view, view.NavCollapsed)
	formValues.Del("as_of")
	formValues.Del("unit")
	formValues.Set("unit", view.SelectedOrganizationUnit)
	action := pageHref(PageOrgEffectiveDate)
	if encoded := formValues.Encode(); encoded != "" {
		action += "?" + encoded
	}
	dateForm := html.Form(html.Props{Class: "organization-effective-date-form", Method: "get", Action: action},
		html.Label(html.Props{For: "organization-as-of"}, ui.Text(view.Locale.Text("org_effective_date.date_label"))),
		html.Tag("input", html.Props{ID: "organization-as-of", Name: "as_of", Value: view.OrganizationAsOf, Required: true, Raw: map[string]any{"type": "date"}}),
		html.Button(html.Props{Type: "submit", Class: "button primary"}, ui.Text(view.Locale.Text("org_effective_date.apply"))),
	)
	units := make([]ui.Node, 0, len(graph.Units))
	for _, unit := range graph.Units {
		href := statefulHref(view, PageOrgEffectiveDate, "as_of", view.OrganizationAsOf, "unit", unit.ID)
		units = append(units, html.Li(html.Props{}, softwareLink(view.Navigate, html.Props{}, href, ui.Text(unit.Name))))
	}
	children := []ui.Node{html.H1(html.Props{}, ui.Text(view.Title)), dateForm}
	if len(units) == 0 {
		children = append(children, html.P(html.Props{Raw: map[string]any{"role": "status"}}, ui.Text(view.Locale.Text("org_effective_date.no_units"))))
	} else {
		children = append(children, html.H2(html.Props{}, ui.Text(view.Locale.Text("org_effective_date.units_label"))), html.Ul(html.Props{}, units...))
	}
	if view.SelectedOrganizationUnit != "" {
		unit, ok := graph.UnitByID[view.SelectedOrganizationUnit]
		if !ok {
			children = append(children, html.P(html.Props{Raw: map[string]any{"role": "status"}}, ui.Text(view.Locale.Text("org_effective_date.unit_unavailable"))))
		} else {
			request := organization.ReadRequest{Tenant: view.OrganizationTenant, Root: unit.ID, AsOf: graph.AsOf, Authorize: view.AuthorizeOrganizationUnit}
			ancestors, ancestryErr := organization.Ancestry(*view.OrganizationGraph, request)
			descendants, descendencyErr := organization.Descendency(*view.OrganizationGraph, request)
			if ancestryErr != nil || descendencyErr != nil {
				children = append(children, html.P(html.Props{Raw: map[string]any{"role": "status"}}, ui.Text(view.Locale.Text("org_effective_date.unit_unavailable"))))
			} else {
				children = append(children,
					html.H2(html.Props{}, ui.Text(unit.Name)),
					effectiveDateUnitList(view, "org_effective_date.ancestry", ancestors.Units),
					effectiveDateUnitList(view, "org_effective_date.descendency", descendants.Units),
				)
			}
		}
	}
	return html.Section(html.Props{Class: "surface organization-effective-date", Raw: map[string]any{"aria-label": view.Title}}, children...)
}

func orgEffectiveDateUnavailable(view View) ui.Node {
	return ui.CreateElement(EmptyState, EmptyStateProps{
		Title:       view.Locale.Text("org_effective_date.unavailable_title"),
		Description: view.Locale.Text("org_effective_date.unavailable_detail"), Role: "status",
		Action: &ActionLinkProps{Label: view.Locale.Text("org_effective_date.return_home"), Href: statefulHref(view, PageHome), Class: "button primary", Navigate: view.Navigate},
	})
}

func effectiveDateUnitList(view View, labelKey string, units []organization.OrganizationUnit) ui.Node {
	ordered := append([]organization.OrganizationUnit(nil), units...)
	sort.Slice(ordered, func(i, j int) bool { return strings.ToLower(ordered[i].Name) < strings.ToLower(ordered[j].Name) })
	items := make([]ui.Node, 0, len(ordered))
	for _, unit := range ordered {
		items = append(items, html.Li(html.Props{}, ui.Text(unit.Name)))
	}
	return html.Section(html.Props{}, html.H3(html.Props{}, ui.Text(view.Locale.Text(labelKey))), html.Ul(html.Props{}, items...))
}
