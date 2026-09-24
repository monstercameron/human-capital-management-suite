package productui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type positionObjectPageProps struct{ View View }

const positionOccupancyFactSeparator = " · "

func PositionObjectPage(props positionObjectPageProps) ui.Node {
	view := props.View
	projection := view.PositionObject
	children := []ui.Node{positionOptionSelector(view)}
	if projection == nil || projection.PositionID == "" {
		children = append(children, ui.CreateElement(EmptyState, EmptyStateProps{
			Title: view.Locale.Text("position_object.select_title"), Description: view.Locale.Text("position_object.select_detail"), Role: "status",
		}))
		return html.Section(html.Props{Class: "position-object-page", Aria: map[string]string{"label": view.Locale.Text("position_object.details")}}, children...)
	}
	compatible := view.Locale.Text("position_object.incompatible")
	if projection.Compatible {
		compatible = view.Locale.Text("position_object.compatible")
	}
	children = append(children,
		html.H2(html.Props{Class: "position-object-heading"}, ui.Text(view.Locale.Text("position_object.details"))),
		positionFact(view.Locale.Text("position_object.position"), projection.PositionID),
		positionFact(view.Locale.Text("position_object.revision"), projection.Revision),
		positionFact(view.Locale.Text("position_object.job"), projection.JobCode),
		positionFact(view.Locale.Text("position_object.organization_unit"), projection.OrgUnit),
		positionFact(view.Locale.Text("position_object.lifecycle"), projection.Lifecycle),
		positionFact(view.Locale.Text("position_object.compatibility"), compatible),
	)
	return html.Section(html.Props{Class: "position-object-page", Aria: map[string]string{"label": view.Locale.Text("position_object.details")}}, children...)
}

func positionFact(label, value string) ui.Node {
	return html.P(html.Props{Class: "position-object-fact"}, html.Strong(html.Props{}, ui.Text(label+": ")), ui.Text(value))
}

type positionOccupancyPageProps struct{ View View }

func PositionOccupancyPage(props positionOccupancyPageProps) ui.Node {
	view := props.View
	projection := view.PositionOccupancy
	children := []ui.Node{positionOptionSelector(view)}
	if projection == nil || projection.PositionID == "" {
		children = append(children, ui.CreateElement(EmptyState, EmptyStateProps{
			Title: view.Locale.Text("position_occupancy.select_title"), Description: view.Locale.Text("position_occupancy.select_detail"), Role: "status",
		}))
		return html.Section(html.Props{Class: "position-occupancy-page", Aria: map[string]string{"label": view.Locale.Text("position_occupancy.details")}}, children...)
	}
	occupants := make([]ui.Node, 0, len(projection.Occupants))
	for _, occupant := range projection.Occupants {
		occupants = append(occupants, html.Li(html.Props{Class: "position-occupancy-occupant"},
			html.Strong(html.Props{}, ui.Text(occupant.WorkerID)),
			ui.Text(positionOccupancyFactSeparator+view.Locale.Text("position_occupancy.fte")+": "+positionLocalizedDecimal(view, occupant.FTE)),
		))
	}
	if len(occupants) == 0 {
		occupants = append(occupants, html.Li(html.Props{Class: "position-occupancy-empty"}, ui.Text(view.Locale.Text("position_occupancy.no_occupants"))))
	}
	children = append(children,
		html.H2(html.Props{Class: "position-occupancy-heading"}, ui.Text(view.Locale.Text("position_occupancy.details"))),
		positionOccupancyFact(view.Locale.Text("position_occupancy.position"), projection.PositionID),
		positionOccupancyFact(view.Locale.Text("position_occupancy.capacity"), view.Locale.FormatNumber(strconv.FormatInt(projection.CapacityHeads, 10), 0)+" · "+positionLocalizedDecimal(view, projection.CapacityFTE)),
		positionOccupancyFact(view.Locale.Text("position_occupancy.consumed"), view.Locale.FormatNumber(strconv.FormatInt(projection.ConsumedHeads, 10), 0)+" · "+positionLocalizedDecimal(view, projection.ConsumedFTE)),
		positionOccupancyFact(view.Locale.Text("position_occupancy.vacancies"), view.Locale.FormatNumber(strconv.FormatInt(projection.AvailableHeads, 10), 0)+" · "+positionLocalizedDecimal(view, projection.AvailableFTE)),
		html.H3(html.Props{}, ui.Text(view.Locale.Text("position_occupancy.occupants"))),
		html.Ul(html.Props{Class: "position-occupancy-occupants", Raw: map[string]any{"role": "list"}}, occupants...),
	)
	return html.Section(html.Props{Class: "position-occupancy-page", Aria: map[string]string{"label": view.Locale.Text("position_occupancy.details")}}, children...)
}

func positionLocalizedDecimal(view View, decimal string) string {
	fraction := 0
	if point := strings.IndexByte(decimal, '.'); point >= 0 {
		fraction = len(decimal) - point - 1
	}
	return view.Locale.FormatNumber(decimal, fraction)
}

func positionOccupancyFact(label, value string) ui.Node {
	return html.P(html.Props{Class: "position-occupancy-fact"}, html.Strong(html.Props{}, ui.Text(label+": ")), ui.Text(value))
}

type reviewParticipantsPageProps struct{ View View }

func ReviewParticipantsPage(props reviewParticipantsPageProps) ui.Node {
	view := props.View
	projection := view.ReviewParticipants
	if projection == nil {
		return reviewParticipantsEmptyState(view, "review_participants.unavailable_title", "review_participants.unavailable_detail", true)
	}
	if len(projection.Cycles) == 0 {
		return reviewParticipantsEmptyState(view, "review_participants.empty_title", "review_participants.empty_detail", true)
	}
	cycles := make([]ui.Node, 0, len(projection.Cycles))
	for _, cycle := range projection.Cycles {
		assignments := make([]ui.Node, 0, len(cycle.Assignments))
		for _, assignment := range cycle.Assignments {
			assignments = append(assignments, html.Li(html.Props{Class: "review-participant-assignment"},
				html.Span(html.Props{Class: "review-participant-person"}, ui.Text(assignment.ParticipantID)),
				html.Span(html.Props{Class: "review-participant-relationship"}, ui.Text(view.Locale.Text("review_participants.relationship."+assignment.Relationship))),
				html.Span(html.Props{Class: "review-participant-reviewer"}, ui.Text(assignment.ReviewerID)),
			))
		}
		if len(assignments) == 0 {
			continue
		}
		cycles = append(cycles, html.Section(html.Props{Class: "review-participants-cycle", Raw: map[string]any{"aria-labelledby": fmt.Sprintf("review-cycle-%s-title", cycle.CycleID)}},
			html.H2(html.Props{ID: fmt.Sprintf("review-cycle-%s-title", cycle.CycleID)}, ui.Text(view.Locale.Text("review_participants.cycle_heading", map[string]string{"revision": fmt.Sprint(cycle.CycleRevision)}))),
			html.Ul(html.Props{Class: "review-participants-assignments", Raw: map[string]any{"role": "list"}}, assignments...),
		))
	}
	if len(cycles) == 0 {
		return reviewParticipantsEmptyState(view, "review_participants.empty_title", "review_participants.empty_detail", false)
	}
	return html.Section(html.Props{Class: "review-participants-page", Aria: map[string]string{"label": view.Locale.Text("page.review_participants.title")}}, html.Div(html.Props{Class: "review-participants-cycles"}, cycles...))
}

func reviewParticipantsEmptyState(view View, titleKey, detailKey string, homeAction bool) ui.Node {
	props := EmptyStateProps{Title: view.Locale.Text(titleKey), Description: view.Locale.Text(detailKey), Role: "status"}
	if homeAction {
		props.Action = &ActionLinkProps{Label: view.Locale.Text("review_participants.return_home"), Href: statefulHref(view, PageHome), Class: "button primary", Navigate: view.Navigate}
	}
	return ui.CreateElement(EmptyState, props)
}
