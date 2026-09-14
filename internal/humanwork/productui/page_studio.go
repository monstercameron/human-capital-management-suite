package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

func studioPage(view View) ui.Node {
	return ui.CreateElement(StudioPage, StudioPageProps{
		Back: ActionLinkProps{Label: view.Locale.Text("studio.back_admin"), Href: statefulHref(view, PageAdmin), Class: "studio-back-link", Navigate: view.Navigate},
		State: EmptyStateProps{
			Badge: view.Locale.Text("studio.unavailable_badge"), Tone: "warning", Title: view.Locale.Text("studio.unavailable_title"),
			Description: view.Locale.Text("studio.unavailable_description"),
			Action:      &ActionLinkProps{Label: view.Locale.Text("studio.return_home"), Href: statefulHref(view, PageHome), Class: "button primary", Navigate: view.Navigate},
		},
	})
}
