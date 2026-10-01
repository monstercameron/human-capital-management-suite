package productui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// PageCrewSchedule identifies the crew schedule.
const PageCrewSchedule PageID = "time-schedule"

// CrewSchedulePublisher publishes the week's draft shifts and notifies the
// affected workers. Implementations check eligibility, overlap and notice.
type CrewSchedulePublisher interface {
	PublishWeek() error
}

// CrewShift is one planned shift in a cell. It is a plan, never proof of
// attendance; the punch record stays on the timecard.
type CrewShift struct {
	TimeLabel     string
	SiteLabel     string
	Draft         bool
	ConflictLabel string
	Edit          *ActionLinkProps
}

// CrewScheduleRow is one worker across the week; Cells lines up with Days.
type CrewScheduleRow struct {
	ID     string
	Worker string
	Role   string
	Cells  [][]CrewShift
}

// CrewScheduleProjection is one week of planned shifts.
type CrewScheduleProjection struct {
	State       TimeSurfaceState
	WeekLabel   string
	StatusLabel string
	StatusTone  string
	Previous    *ActionLinkProps
	Next        *ActionLinkProps
	Days        []string
	Rows        []CrewScheduleRow
	AddShift    *ActionLinkProps
	// Publisher is present only when the week has unpublished changes the
	// caller may publish. PublishBlocked explains why publishing is off.
	Publisher        CrewSchedulePublisher
	PublishBlocked   string
	UnpublishedCount int
	Notice           string
	Error            string
}

// CrewSchedulePage lays the week out as a grid on wide screens and as one
// list per worker on narrow ones, and makes publishing a confirmed act.
func CrewSchedulePage(view View, projection CrewScheduleProjection) ui.Node {
	locale := view.Locale
	title := timeText(locale, "schedule.title")
	if projection.State != TimeSurfaceReady {
		return timeUnavailable(view, PageCrewSchedule, "schedule-title", title, timeText(locale, "schedule.intro"), timeText(locale, "schedule.unavailable"), timeText(locale, "schedule.unavailable_help"))
	}
	head := html.Div(html.Props{Class: "page-head time-head schedule-head", Data: map[string]string{"hcm-page": string(PageCrewSchedule)}},
		html.Div(html.Props{},
			html.H1(html.Props{ID: "schedule-title"}, ui.Text(title)),
			html.P(html.Props{Class: "subtitle"}, ui.Text(timeText(locale, "schedule.intro"))),
		),
		html.Div(html.Props{Class: "fleet-head-action"}, timeNodes(timeLink(view, projection.AddShift, "button secondary"))...),
	)
	week := html.Nav(html.Props{Class: "timecard-bar schedule-bar", Raw: map[string]any{"aria-label": timeText(locale, "schedule.week_nav")}}, timeNodes(
		html.Div(html.Props{Class: "timecard-period"},
			html.H2(html.Props{Class: "timecard-period-label"}, ui.Text(projection.WeekLabel)),
			timePill(projection.StatusTone, projection.StatusLabel),
		),
		timeLink(view, projection.Previous, "button secondary timecard-step"),
		timeLink(view, projection.Next, "button secondary timecard-step"),
	)...)
	children := timeNodes(head, timeNotice("error", projection.Error), timeNotice("success", projection.Notice), week)
	if len(projection.Rows) == 0 {
		children = append(children, html.Div(html.Props{Class: "surface time-empty"},
			html.H2(html.Props{}, ui.Text(timeText(locale, "schedule.empty"))),
			html.P(html.Props{Class: "muted"}, ui.Text(timeText(locale, "schedule.empty_help"))),
		))
		return html.Section(html.Props{Class: "time-page schedule-page", Raw: map[string]any{"aria-labelledby": "schedule-title"}}, children...)
	}
	if projection.Publisher != nil || strings.TrimSpace(projection.PublishBlocked) != "" {
		children = append(children, ui.CreateElement(crewSchedulePublish, crewSchedulePublishProps{View: view, Projection: projection}))
	}
	children = append(children, crewScheduleLegend(locale), crewScheduleGrid(view, projection))
	return html.Section(html.Props{Class: "time-page schedule-page", Raw: map[string]any{"aria-labelledby": "schedule-title"}}, children...)
}

func crewScheduleGrid(view View, projection CrewScheduleProjection) ui.Node {
	locale := view.Locale
	heads := []ui.Node{html.Th(html.Props{Class: "schedule-corner", Raw: map[string]any{"scope": "col"}}, html.Span(html.Props{Class: "sr-only"}, ui.Text(timeText(locale, "schedule.worker"))))}
	for _, day := range projection.Days {
		heads = append(heads, html.Th(html.Props{Raw: map[string]any{"scope": "col"}}, ui.Text(day)))
	}
	rows := make([]ui.Node, 0, len(projection.Rows))
	for _, row := range projection.Rows {
		cells := []ui.Node{html.Th(html.Props{Class: "schedule-worker", Raw: map[string]any{"scope": "row"}},
			html.Strong(html.Props{}, ui.Text(row.Worker)),
			html.Span(html.Props{Class: "muted"}, ui.Text(row.Role)),
		)}
		for index, day := range projection.Days {
			var shifts []CrewShift
			if index < len(row.Cells) {
				shifts = row.Cells[index]
			}
			cells = append(cells, html.Td(html.Props{Data: map[string]string{"day": day}, Class: "schedule-cell"}, crewShiftNodes(view, shifts)...))
		}
		rows = append(rows, html.WithKey(html.Tr(html.Props{}, cells...), "row-"+safeID(row.ID)))
	}
	return html.Div(html.Props{Class: "surface schedule-scroll", Role: "region", Raw: map[string]any{"aria-label": timeText(locale, "schedule.grid"), "tabindex": "0"}},
		html.Table(html.Props{Class: "schedule-grid"},
			html.Caption(html.Props{Class: "sr-only"}, ui.Text(projection.WeekLabel)),
			html.Thead(html.Props{}, html.Tr(html.Props{}, heads...)),
			html.Tbody(html.Props{}, rows...),
		),
	)
}

func crewShiftNodes(view View, shifts []CrewShift) []ui.Node {
	locale := view.Locale
	if len(shifts) == 0 {
		return []ui.Node{html.Span(html.Props{Class: "schedule-off"}, ui.Text(timeText(locale, "schedule.off")))}
	}
	nodes := make([]ui.Node, 0, len(shifts))
	for _, shift := range shifts {
		class := "schedule-shift"
		if shift.Draft {
			class += " schedule-shift-draft"
		}
		if shift.ConflictLabel != "" {
			class += " schedule-shift-conflict"
		}
		children := []ui.Node{
			html.Span(html.Props{Class: "schedule-shift-time"}, html.Tag("bdi", html.Props{}, ui.Text(shift.TimeLabel))),
			html.Span(html.Props{Class: "schedule-shift-site"}, ui.Text(shift.SiteLabel)),
		}
		if shift.Draft {
			children = append(children, html.Span(html.Props{Class: "schedule-shift-tag"}, ui.Text(timeText(locale, "schedule.draft"))))
		}
		if shift.ConflictLabel != "" {
			children = append(children, timePill(TimeToneBad, shift.ConflictLabel))
		}
		if edit := timeLink(view, shift.Edit, "schedule-shift-edit"); edit != nil {
			children = append(children, edit)
		}
		nodes = append(nodes, html.Div(html.Props{Class: class}, children...))
	}
	return nodes
}

func crewScheduleLegend(locale LocaleContext) ui.Node {
	return html.P(html.Props{Class: "schedule-legend muted"},
		html.Span(html.Props{Class: "schedule-legend-swatch"}), ui.Text(timeText(locale, "schedule.legend_published")),
		html.Span(html.Props{Class: "schedule-legend-swatch schedule-legend-draft"}), ui.Text(timeText(locale, "schedule.legend_draft")),
	)
}

type crewSchedulePublishProps struct {
	View       View
	Projection CrewScheduleProjection
}

type crewSchedulePublishState struct {
	confirming bool
	busy       bool
	done       bool
	err        string
}

func crewSchedulePublish(props crewSchedulePublishProps) ui.Node {
	view, projection := props.View, props.Projection
	locale := view.Locale
	state := ui.UseState(crewSchedulePublishState{})
	current := state.Get()
	edit := func(update func(*crewSchedulePublishState)) {
		next := state.Get()
		update(&next)
		state.Set(next)
	}
	if current.done {
		return html.Div(html.Props{Class: "surface schedule-publish"}, timeNotice("success", timeText(locale, "schedule.published")))
	}
	if projection.Publisher == nil {
		return html.Div(html.Props{Class: "surface schedule-publish"},
			html.P(html.Props{Class: "muted"}, ui.Text(projection.PublishBlocked)),
		)
	}
	if current.confirming {
		yesLabel := timeText(locale, "schedule.publish_yes")
		if current.busy {
			yesLabel = timeText(locale, "schedule.publishing")
		}
		yes := html.Button(html.Props{Class: "button primary", Type: "button", Disabled: current.busy, OnClick: ui.UseEvent(func(ui.MouseEvent) {
			if current.busy {
				return
			}
			edit(func(s *crewSchedulePublishState) { s.busy, s.err = true, "" })
			err := projection.Publisher.PublishWeek()
			edit(func(s *crewSchedulePublishState) {
				s.busy = false
				if err != nil {
					s.err = timeText(locale, "schedule.error")
					return
				}
				s.done = true
			})
		})}, ui.Text(yesLabel))
		no := html.Button(html.Props{Class: "button secondary", Type: "button", Disabled: current.busy, OnClick: ui.UseEvent(func(ui.MouseEvent) {
			edit(func(s *crewSchedulePublishState) { s.confirming = false })
		})}, ui.Text(timeText(locale, "schedule.publish_no")))
		return html.Div(html.Props{Class: "surface schedule-publish"}, timeNodes(
			timeNotice("error", current.err),
			timeConfirmPanel("schedule-confirm-title", timeText(locale, "schedule.confirm_title"), timeCountText(locale, "schedule.confirm_body", projection.UnpublishedCount), false, yes, no),
		)...)
	}
	return html.Div(html.Props{Class: "surface schedule-publish"},
		html.P(html.Props{Class: "schedule-publish-note"}, ui.Text(timeCountText(locale, "schedule.unpublished", projection.UnpublishedCount))),
		html.Button(html.Props{Class: "button primary schedule-publish-button", Type: "button", OnClick: ui.UseEvent(func(ui.MouseEvent) {
			edit(func(s *crewSchedulePublishState) { s.confirming = true })
		})}, ui.Text(timeText(locale, "schedule.publish"))),
	)
}
