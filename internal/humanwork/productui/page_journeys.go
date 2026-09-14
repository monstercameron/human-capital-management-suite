package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// journeysPage is the non-browser fallback for the registered route. The
// WASM composition replaces this body with the live journey state machine;
// keeping an honest fallback means SSR and architecture checks never invent
// workflow data. When the authorized projection is already available, the
// fallback remains useful: it is a workflow-first landing page with an
// explicit start action and a path to terminal history.
func journeysPage(view View) ui.Node {
	if view.Loading {
		return ui.CreateElement(EmptyState, EmptyStateProps{
			Title:       view.Locale.Text("page.journeys.title"),
			Description: view.Locale.Text("shell.loading_authorized"),
			Role:        "status",
		})
	}
	return ui.CreateElement(JourneysPage, journeysPageProps(view))
}

func journeysPageProps(view View) JourneysPageProps {
	var launch *ActionLinkProps
	canCreate := len(view.EffectivePermissions) == 0 || view.Can(PageJourneys, "create")
	for _, workflow := range view.PersonWorkflows {
		if !canCreate {
			break
		}
		if strings.ToLower(strings.TrimSpace(workflow.ID)) != "promotion" {
			continue
		}
		href := strings.TrimSpace(workflow.Href)
		if href == "" && workflow.LaunchHref != nil {
			// The launcher callback is supplied by the authorized adapter. It
			// receives only the already-admitted person reference.
			for _, person := range view.People {
				if strings.TrimSpace(person.ID) != "" {
					href = strings.TrimSpace(workflow.LaunchHref(person.ID))
					break
				}
			}
		}
		if href == "" {
			continue
		}
		label := strings.TrimSpace(workflow.Name)
		if label == "" || strings.EqualFold(label, "promotion") {
			// There is no dedicated localized verb label in the catalog yet;
			// use the reviewed, localized promotion journey label rather than
			// inventing an English action string.
			label = view.Locale.Text("work.promotion_journeys")
		}
		launch = &ActionLinkProps{Label: label, Href: href, Class: "button primary", Navigate: view.Navigate}
		break
	}
	scoped := view
	scoped.Work = admittedWork(view)
	tracker := trackedRequestsFor(scoped, scoped.Work)
	tracker.Title = view.Locale.Text("work.promotion_journeys")
	tracker.Description = view.Locale.Text("page.journeys.subtitle")
	if len(view.EffectivePermissions) == 0 || view.Can(PageHistory, "view") {
		tracker.More = ActionLinkProps{Label: view.Locale.Text("work.past"), Href: statefulHref(view, PageHistory), Navigate: view.Navigate}
	}
	return JourneysPageProps{I18nProps: I18nProps{Locale: view.Locale}, Launch: launch, Tracker: tracker}
}
