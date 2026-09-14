package productui

import (
	"fmt"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// UtilityDrawerItem is one contextual destination: a related page or an
// authorized start, both resolved from the registry and the view.
type UtilityDrawerItem struct {
	ID          string
	Label       string
	Description string
	Href        string
	Icon        string
	Page        PageID
}

// UtilityDrawerSection groups drawer items by their derivation: registry
// relations ("related") or contextual authorized starts ("actions").
type UtilityDrawerSection struct {
	ID    string
	Title string
	Items []UtilityDrawerItem
}

// UtilityDrawerProps keeps the drawer independently composable without the
// page-wide View.
type UtilityDrawerProps struct {
	I18nProps
	Sections []UtilityDrawerSection
	Navigate func(string)
}

// utilityDrawerSections derives the drawer's sections from the current page
// context. Relations come from the canonical PageDefinitions ParentNav
// chain; actions come from the already-resolved workflow projection behind
// the Journeys create grant. Anything unauthorized stays unlisted, and an
// empty context yields no sections.
func utilityDrawerSections(view View) []UtilityDrawerSection {
	sections := make([]UtilityDrawerSection, 0, 2)
	if related := utilityDrawerRelated(view); len(related.Items) > 0 {
		sections = append(sections, related)
	}
	if actions := utilityDrawerActions(view); len(actions.Items) > 0 {
		sections = append(sections, actions)
	}
	return sections
}

func utilityDrawerRelated(view View) UtilityDrawerSection {
	section := UtilityDrawerSection{ID: "related", Title: view.Locale.Text("utility_drawer.related")}
	definition, ok := LookupPage(view.Page)
	if !ok {
		return section
	}
	if parent, ok := LookupPage(definition.ParentNav); ok && definition.ParentNav != "" {
		if navigationDestinationAuthorized(view, parent.ID) {
			section.Items = append(section.Items, UtilityDrawerItem{
				ID: "related:" + string(parent.ID), Label: view.Locale.Text(parent.LabelKey),
				Description: view.Locale.Text(parent.TitleKey), Href: statefulHref(view, parent.ID), Icon: parent.Icon, Page: parent.ID,
			})
		}
	}
	for _, sibling := range registeredPages() {
		if sibling.ID == definition.ID || sibling.ParentNav != definition.ParentNav || definition.ParentNav == "" {
			continue
		}
		if !navigationDestinationAuthorized(view, sibling.ID) {
			continue
		}
		section.Items = append(section.Items, UtilityDrawerItem{
			ID: "related:" + string(sibling.ID), Label: view.Locale.Text(sibling.LabelKey),
			Description: view.Locale.Text(sibling.TitleKey), Href: statefulHref(view, sibling.ID), Icon: sibling.Icon, Page: sibling.ID,
		})
	}
	return section
}

func utilityDrawerActions(view View) UtilityDrawerSection {
	section := UtilityDrawerSection{ID: "actions", Title: view.Locale.Text("utility_drawer.actions")}
	if view.Page != PagePerson {
		return section
	}
	person, ok := exactPerson(view)
	if !ok {
		return section
	}
	if len(view.EffectivePermissions) > 0 && !view.Can(PageJourneys, "create") {
		return section
	}
	active, hasActiveJourney := activePromotionWorkItem(view, person.ID)
	for _, workflow := range filteredPersonWorkflows(view) {
		href := workflow.Href
		if workflow.LaunchHref != nil {
			href = workflow.LaunchHref(person.ID)
		}
		if workflow.ID == "promotion" {
			// PROMOUX-012: the drawer follows the profile's own guard. An
			// active promotion replaces Start with a link to it, and a worker
			// the server did not make eligible is offered no start at all.
			switch {
			case hasActiveJourney:
				section.Items = append(section.Items, UtilityDrawerItem{
					ID: "action:" + workflow.ID, Label: view.Locale.Text("people.open_active_promotion"), Description: workflow.Category,
					Href: JourneyDetailHref(view, active.ID), Icon: "journeys", Page: PageJourneys,
				})
				continue
			case person.PromotionAvailability == PromotionActiveConflict || !personPromotionEligible(person):
				continue
			}
		}
		section.Items = append(section.Items, UtilityDrawerItem{
			ID: "action:" + workflow.ID, Label: workflow.Name, Description: workflow.Category,
			Href: href, Icon: "journeys", Page: PageJourneys,
		})
	}
	return section
}

func utilityDrawerProps(view View) UtilityDrawerProps {
	return UtilityDrawerProps{
		I18nProps: I18nProps{Locale: view.Locale}, Sections: utilityDrawerSections(view), Navigate: view.Navigate,
	}
}

// UtilityDrawer renders the contextual trigger plus its dialog. With no
// sections the context offers nothing, so the drawer renders as a fragment
// instead of an empty control. The closed dialog stays in the document with
// its destinations and accessible name resolving without client state.
func UtilityDrawer(props UtilityDrawerProps) ui.Node {
	if len(props.Sections) == 0 {
		return html.Fragment()
	}
	open := ui.UseState(false)
	trigger := html.Button(html.Props{
		ID: "utility-drawer-trigger", Class: "utility-drawer-trigger", Type: "button",
		Aria: map[string]string{
			"label": props.Text("utility_drawer.trigger"), "haspopup": "dialog",
			"expanded": fmt.Sprint(open.Get()), "controls": "utility-drawer-dialog",
		},
		OnClick: ui.UseEvent(func(ui.MouseEvent) { open.Set(!open.Get()) }),
	}, navIcon("expand"), html.Span(html.Props{Class: "utility-drawer-label"}, ui.Text(props.Text("utility_drawer.trigger"))))
	dialogProps := html.Props{
		ID: "utility-drawer-dialog", Class: "utility-drawer utility-drawer-dialog",
		Raw: map[string]any{"role": "dialog", "aria-modal": "true", "aria-label": props.Text("utility_drawer.dialog_title")},
		OnKeyDown: ui.UseEvent(func(event ui.KeyboardEvent) {
			if drawerEscapeCloses(event.GetKey()) {
				open.Set(false)
			}
		}),
	}
	if !open.Get() {
		dialogProps.Class += " utility-drawer-dialog-hidden"
		dialogProps.Raw["hidden"] = "hidden"
		dialogProps.Raw["aria-hidden"] = "true"
	}
	sections := make([]ui.Node, 0, len(props.Sections))
	for _, section := range props.Sections {
		items := make([]ui.Node, 0, len(section.Items))
		for _, item := range section.Items {
			item := item
			link := softwareLink(props.Navigate, html.Props{}, item.Href,
				navIcon(item.Icon), html.Span(html.Props{Class: "utility-drawer-item-text"},
					html.Span(html.Props{Class: "utility-drawer-item-label"}, ui.Text(item.Label)),
					html.Span(html.Props{Class: "utility-drawer-item-description"}, ui.Text(item.Description))))
			items = append(items, html.Li(html.Props{Class: "utility-drawer-item"}, link))
		}
		sections = append(sections, html.Section(html.Props{Class: "utility-drawer-section", Aria: map[string]string{"label": section.Title}},
			html.H2(html.Props{Class: "utility-drawer-section-title"}, ui.Text(section.Title)),
			html.Ul(html.Props{Class: "utility-drawer-list"}, items...)))
	}
	closeLabel := props.Text("utility_drawer.close")
	children := append([]ui.Node{html.Button(html.Props{
		ID: "utility-drawer-close", Class: "utility-drawer-close", Type: "button",
		Aria:    map[string]string{"label": closeLabel},
		OnClick: ui.UseEvent(func(ui.MouseEvent) { open.Set(false) }),
	}, ui.Text(closeLabel))}, sections...)
	dialog := html.Div(dialogProps, children...)
	return html.Fragment(trigger, dialog)
}

func utilityDrawer(view View) ui.Node {
	return ui.CreateElement(UtilityDrawer, utilityDrawerProps(view))
}
