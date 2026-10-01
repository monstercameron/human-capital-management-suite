package productui

import "github.com/monstercameron/GoWebComponents/v5/ui"

// clockDevicesPage is the route adapter for the administrator's fleet
// projection. Device state and actions remain server-owned.
func clockDevicesPage(view View) ui.Node {
	return ClockFleetPage(view, view.ClockFleetProjection)
}

func clockDevicesPageModule() PageModule {
	return pageModule(PageDefinition{
		ID: PageClockDevices, Route: "/workspace/app/admin/time/devices", Label: "Time clock devices", Icon: "admin",
		Title: "Time clock devices", Subtitle: "Manage enrolled devices that record punches.",
		LabelKey: "page.time_devices.label", TitleKey: "page.time_devices.title", SubtitleKey: "page.time_devices.subtitle",
		SearchTerms: []string{"time clock", "devices", "fleet", "tablets", "readers"},
		ParentNav:   PageAdmin, Admitted: true, NavigationPublished: true, OwnsHeading: true, RenderOrder: 180,
	}, clockDevicesPageModuleRenderer{}, PageAccessPolicy{Audience: PageAudienceDenied}, routeProfileFor("/workspace/app/admin/time/devices"), dataProfileFor("/workspace/app/admin/time/devices"))
}

type clockDevicesPageModuleRenderer struct{}

func (clockDevicesPageModuleRenderer) Render(view View) ui.Node { return clockDevicesPage(view) }
