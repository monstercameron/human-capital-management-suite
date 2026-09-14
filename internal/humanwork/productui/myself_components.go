package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// MyselfPageProps is the route-independent self-service contract. Profile is
// nil unless the current principal has an authorized worker binding.
type MyselfPageProps struct {
	I18nProps
	Profile                 *PersonProfileProps
	OrganizationTitle       string
	OrganizationDescription string
	OrganizationTree        []OwnershipNodeProps
	OrganizationTreeLabel   string
}

// MyselfPage is read-only by construction. Every available change is exposed
// through the same governed workflow launcher used by other person surfaces.
func MyselfPage(props MyselfPageProps) ui.Node {
	if props.Profile == nil {
		return html.Div(html.Props{Class: "myself-page"},
			html.Section(html.Props{Class: "surface empty-state", Raw: map[string]any{"role": "status"}},
				html.H2(html.Props{}, ui.Text(props.Text("myself.unavailable_title"))),
				html.P(html.Props{Class: "muted"}, ui.Text(props.Text("myself.unavailable_detail"))),
			),
		)
	}
	return html.Div(html.Props{Class: "myself-page"},
		ui.CreateElement(SelfServiceBoundary, SelfServiceBoundaryProps{I18nProps: props.I18nProps}),
		ui.CreateElement(PersonProfileComposition, PersonProfileCompositionProps{I18nProps: props.I18nProps, Profile: *props.Profile}),
		ui.CreateElement(Panel, PanelProps{Title: props.OrganizationTitle, Body: html.Div(html.Props{Class: "myself-organization"},
			html.P(html.Props{Class: "muted"}, ui.Text(props.OrganizationDescription)),
			ui.CreateElement(OrganizationOwnershipTree, OrganizationOwnershipTreeProps{Nodes: props.OrganizationTree, Label: props.OrganizationTreeLabel}),
		)}),
	)
}

type SelfServiceBoundaryProps struct{ I18nProps }

// SelfServiceBoundary makes the action model unmistakable before a user
// reaches sensitive or payroll facts: records are view-only and changes are
// requests executed by governed workflows.
func SelfServiceBoundary(props SelfServiceBoundaryProps) ui.Node {
	return html.Section(html.Props{Class: "surface self-service-boundary", Raw: map[string]any{"role": "note"}},
		html.Div(html.Props{Class: "self-service-boundary-icon"}, productIcon("check", "self-service-boundary-glyph")),
		html.Div(html.Props{Class: "self-service-boundary-copy"},
			html.H2(html.Props{}, ui.Text(props.Text("myself.read_only_title"))),
			html.P(html.Props{Class: "muted"}, ui.Text(props.Text("myself.read_only_detail"))),
		),
		html.Span(html.Props{Class: "status self-service-badge"}, ui.Text(props.Text("myself.read_only_badge"))),
	)
}
