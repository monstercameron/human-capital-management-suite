package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// timecardPage is the route adapter for the worker's own server projection.
// The page owns its heading, while the projection owns all displayed facts.
func timecardPage(view View) ui.Node {
	return TimecardPage(view, view.TimecardProjection)
}

func timecardPageModule() PageModule {
	return pageModule(PageDefinition{
		ID: PageTimecard, Route: "/workspace/app/time/timecard", Label: "My timecard", Icon: "clock",
		Title: "My timecard", Subtitle: "Review and submit your recorded hours.",
		LabelKey: "page.timecard.label", TitleKey: "page.timecard.title", SubtitleKey: "page.timecard.subtitle",
		SearchTerms: []string{"timecard", "hours", "timesheet", "punches", "submit"},
		ParentNav:   PageClock, Admitted: true, NavigationPublished: true, OwnsHeading: true, RenderOrder: 178,
	}, timecardPageModuleRenderer{}, PageAccessPolicy{Audience: PageAudienceWorker}, routeProfileFor("/workspace/app/time/timecard"), dataProfileFor("/workspace/app/time/timecard"))
}

type timecardPageModuleRenderer struct{}

func (timecardPageModuleRenderer) Render(view View) ui.Node { return timecardPage(view) }
