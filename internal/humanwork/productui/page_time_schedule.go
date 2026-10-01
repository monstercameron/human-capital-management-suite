package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// crewSchedulePage is the route adapter for the manager's server projection.
func crewSchedulePage(view View) ui.Node {
	return CrewSchedulePage(view, view.CrewScheduleProjection)
}

func crewSchedulePageModule() PageModule {
	return pageModule(PageDefinition{
		ID: PageCrewSchedule, Route: "/workspace/app/time/schedule", Label: "Crew schedule", Icon: "calendar",
		Title: "Crew schedule", Subtitle: "Plan shifts and publish changes to your crew.",
		LabelKey: "page.time_schedule.label", TitleKey: "page.time_schedule.title", SubtitleKey: "page.time_schedule.subtitle",
		SearchTerms: []string{"schedule", "shifts", "crew", "roster", "publish"},
		ParentNav:   PageClock, Admitted: true, NavigationPublished: true, OwnsHeading: true, RenderOrder: 179,
	}, crewSchedulePageModuleRenderer{}, PageAccessPolicy{Audience: PageAudienceManager}, routeProfileFor("/workspace/app/time/schedule"), dataProfileFor("/workspace/app/time/schedule"))
}

type crewSchedulePageModuleRenderer struct{}

func (crewSchedulePageModuleRenderer) Render(view View) ui.Node { return crewSchedulePage(view) }
