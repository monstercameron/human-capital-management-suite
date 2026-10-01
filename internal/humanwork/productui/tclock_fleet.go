package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// PageClockDevices identifies the clock device fleet.
const PageClockDevices PageID = "time-devices"

// ClockDeviceRevoker retires one enrolled device through the authenticated
// transport. Implementations authorize the caller for the device's site.
type ClockDeviceRevoker interface {
	RevokeDevice(id string) error
}

// ClockFleetDevice is one enrolled time clock and its health, already
// localized: what it is called, where it is, and what is wrong with it.
type ClockFleetDevice struct {
	ID            string
	Name          string
	SiteLabel     string
	StatusLabel   string
	StatusTone    string
	LastSeenLabel string
	VersionLabel  string
	DriftLabel    string
	// Issues name each problem in words a supervisor can act on.
	Issues []string
	// Fix is the one step that clears the issue, such as updating the app.
	Fix     *ActionLinkProps
	Revoked bool
}

// ClockFleetProjection is the administrator's view of every enrolled device.
type ClockFleetProjection struct {
	State        TimeSurfaceState
	SummaryLabel string
	SummaryTone  string
	Devices      []ClockFleetDevice
	Enroll       *ActionLinkProps
	Revoker      ClockDeviceRevoker
	Notice       string
	Error        string
}

// ClockFleetPage lists the devices with the unhealthy ones first, and makes
// retiring a device a named, confirmed act.
func ClockFleetPage(view View, projection ClockFleetProjection) ui.Node {
	locale := view.Locale
	title := timeText(locale, "fleet.title")
	if projection.State != TimeSurfaceReady {
		return timeUnavailable(view, PageClockDevices, "fleet-title", title, timeText(locale, "fleet.intro"), timeText(locale, "fleet.unavailable"), timeText(locale, "fleet.unavailable_help"))
	}
	head := html.Div(html.Props{Class: "page-head time-head fleet-head", Data: map[string]string{"hcm-page": string(PageClockDevices)}},
		html.Div(html.Props{},
			html.H1(html.Props{ID: "fleet-title"}, ui.Text(title)),
			html.P(html.Props{Class: "subtitle"}, ui.Text(timeText(locale, "fleet.intro"))),
		),
		html.Div(html.Props{Class: "fleet-head-action"}, timeNodes(timeLink(view, projection.Enroll, "button primary"))...),
	)
	children := timeNodes(head, timeNotice("error", projection.Error), timeNotice("success", projection.Notice))
	if strings.TrimSpace(projection.SummaryLabel) != "" {
		children = append(children, html.P(html.Props{Class: "fleet-summary", Role: "status"}, timePill(projection.SummaryTone, projection.SummaryLabel)))
	}
	if len(projection.Devices) == 0 {
		children = append(children, html.Div(html.Props{Class: "surface time-empty"},
			html.H2(html.Props{}, ui.Text(timeText(locale, "fleet.empty"))),
			html.P(html.Props{Class: "muted"}, ui.Text(timeText(locale, "fleet.empty_help"))),
		))
		return html.Section(html.Props{Class: "time-page fleet-page", Raw: map[string]any{"aria-labelledby": "fleet-title"}}, children...)
	}
	cards := make([]ui.Node, 0, len(projection.Devices))
	for _, device := range projection.Devices {
		cards = append(cards, html.WithKey(ui.CreateElement(clockFleetCard, clockFleetCardProps{View: view, Device: device, Revoker: projection.Revoker}), "device-"+safeID(device.ID)))
	}
	children = append(children, html.Div(html.Props{Class: "fleet-cards"}, cards...))
	return html.Section(html.Props{Class: "time-page fleet-page", Raw: map[string]any{"aria-labelledby": "fleet-title"}}, children...)
}

type clockFleetCardProps struct {
	View    View
	Device  ClockFleetDevice
	Revoker ClockDeviceRevoker
}

type clockFleetCardState struct {
	confirming bool
	busy       bool
	revoked    bool
	err        string
}

func clockFleetCard(props clockFleetCardProps) ui.Node {
	view, device := props.View, props.Device
	locale := view.Locale
	state := ui.UseState(clockFleetCardState{})
	current := state.Get()
	edit := func(update func(*clockFleetCardState)) {
		next := state.Get()
		update(&next)
		state.Set(next)
	}
	id := "fleet-" + safeID(device.ID)
	statusLabel, tone := device.StatusLabel, device.StatusTone
	revoked := device.Revoked || current.revoked
	if current.revoked {
		statusLabel, tone = timeText(locale, "fleet.revoked"), TimeToneNeutral
	}
	head := html.Div(html.Props{Class: "fleet-card-head"},
		html.Div(html.Props{},
			html.H2(html.Props{ID: id + "-name", Class: "fleet-name"}, ui.Text(device.Name)),
			html.P(html.Props{Class: "muted fleet-site"}, ui.Text(device.SiteLabel)),
		),
		timePill(tone, statusLabel),
	)
	facts := timeFacts("fleet-facts", []TimeFact{
		{Label: timeText(locale, "fleet.last_seen"), Value: device.LastSeenLabel},
		{Label: timeText(locale, "fleet.version"), Value: device.VersionLabel},
		{Label: timeText(locale, "fleet.drift"), Value: device.DriftLabel},
	})
	body := timeNodes(head, facts)
	if len(device.Issues) > 0 && !revoked {
		issues := make([]ui.Node, 0, len(device.Issues))
		for _, issue := range device.Issues {
			issues = append(issues, html.Li(html.Props{}, ui.Text(issue)))
		}
		body = append(body, html.Ul(html.Props{Class: "fleet-issues"}, issues...))
	}
	if current.err != "" {
		body = append(body, timeNotice("error", current.err))
	}
	fix := timeLink(view, device.Fix, "button primary fleet-fix")
	if !revoked && (props.Revoker != nil || fix != nil) {
		body = append(body, clockFleetActions(view, device, props.Revoker, current, edit, id, fix))
	}
	class := "surface fleet-card"
	if revoked {
		class += " fleet-card-revoked"
	}
	return html.Article(html.Props{Class: class, Data: map[string]string{"tone": tone}, Raw: map[string]any{"aria-labelledby": id + "-name"}}, body...)
}

func clockFleetActions(view View, device ClockFleetDevice, revoker ClockDeviceRevoker, current clockFleetCardState, edit func(func(*clockFleetCardState)), id string, fix ui.Node) ui.Node {
	locale := view.Locale
	if revoker == nil {
		return html.Div(html.Props{Class: "fleet-actions"}, fix)
	}
	if !current.confirming {
		return html.Div(html.Props{Class: "fleet-actions"}, timeNodes(fix,
			html.Button(html.Props{Class: "button secondary fleet-revoke", Type: "button", Aria: map[string]string{"describedby": id + "-name"}, OnClick: ui.UseEvent(func(ui.MouseEvent) {
				edit(func(s *clockFleetCardState) { s.confirming, s.err = true, "" })
			})}, ui.Text(timeText(locale, "fleet.revoke"))))...)
	}
	yesLabel := timeText(locale, "fleet.revoke_yes")
	if current.busy {
		yesLabel = timeText(locale, "fleet.revoking")
	}
	yes := html.Button(html.Props{Class: "button destructive", Type: "button", Disabled: current.busy, OnClick: ui.UseEvent(func(ui.MouseEvent) {
		if current.busy {
			return
		}
		edit(func(s *clockFleetCardState) { s.busy, s.err = true, "" })
		err := revoker.RevokeDevice(device.ID)
		edit(func(s *clockFleetCardState) {
			s.busy = false
			if err != nil {
				s.err = timeText(locale, "fleet.error")
				return
			}
			s.revoked, s.confirming = true, false
		})
	})}, ui.Text(yesLabel))
	no := html.Button(html.Props{Class: "button secondary", Type: "button", Disabled: current.busy, OnClick: ui.UseEvent(func(ui.MouseEvent) {
		edit(func(s *clockFleetCardState) { s.confirming = false })
	})}, ui.Text(timeText(locale, "fleet.revoke_no")))
	title := strings.ReplaceAll(timeText(locale, "fleet.revoke_title"), "{name}", device.Name)
	return timeConfirmPanel(id+"-confirm", title, timeText(locale, "fleet.revoke_body"), true, yes, no)
}
