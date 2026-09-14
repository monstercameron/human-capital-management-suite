package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// JourneysPageProps is the focused Journeys landing projection. The route
// owns workflow discovery and continuity; the service still owns which
// actions and records are authorized.
type JourneysPageProps struct {
	I18nProps
	Launch  *ActionLinkProps
	Tracker TrackedRequestsProps
}

// JourneysPage renders a compact workflow-first overview. Person directory
// and employee creation deliberately stay on People; this page only links to
// an already-authorized promotion launcher and to the shared active/history
// work projection.
func JourneysPage(props JourneysPageProps) ui.Node {
	return html.Div(html.Props{Class: "page-stack journeys-page"},
		html.Section(html.Props{Class: "surface journeys-overview", Aria: map[string]string{"labelledby": "journeys-page-title"}},
			ui.CreateElement(SectionHeading, SectionHeadingProps{
				ID: "journeys-page-title", Title: props.Text("page.journeys.title"), Description: props.Text("page.journeys.subtitle"),
				Trailing: typedJourneyLaunch(props.Launch),
			}),
		),
		ui.CreateElement(TrackedRequests, props.Tracker),
	)
}

func typedJourneyLaunch(launch *ActionLinkProps) ui.Node {
	if launch == nil || strings.TrimSpace(launch.Href) == "" {
		return nil
	}
	copy := *launch
	if strings.TrimSpace(copy.Class) == "" {
		copy.Class = "button primary"
	}
	return ui.CreateElement(ActionLink, copy)
}
