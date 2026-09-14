package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

func helpHubPage(view View) ui.Node {
	return ui.CreateElement(HelpHubPage, HelpHubPageProps{
		Title:        view.Locale.Text("help.hub_title"),
		Destinations: authorizedSupportDestinations(view),
		Empty: EmptyStateProps{
			Title:       view.Locale.Text("help_hub.unavailable_title"),
			Description: view.Locale.Text("help_hub.unavailable_detail"),
			Role:        "status",
			Action: &ActionLinkProps{
				Label: view.Locale.Text("help_hub.return_home"), Href: statefulHref(view, PageHome),
				Class: "button primary", Navigate: view.Navigate,
			},
		},
	})
}
