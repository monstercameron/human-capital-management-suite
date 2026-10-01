package productui

import (
	"net/url"
	"strings"

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
			ui.CreateElement(EmptyState, EmptyStateProps{Title: props.Text("myself.unavailable_title"), Description: props.Text("myself.unavailable_detail"), Role: "status"}),
		)
	}
	profile := myselfProfileWithoutSelfPromotion(*props.Profile, props.Text("workflow.promotion_self_subject"))
	profile = myselfProfileWithTopOrganizationFact(profile, props.OrganizationTree, props.Text("organization.top_of_organization"))
	return html.Div(html.Props{Class: "myself-page"},
		ui.CreateElement(SelfServiceBoundary, SelfServiceBoundaryProps{I18nProps: props.I18nProps}),
		ui.CreateElement(PersonProfileComposition, PersonProfileCompositionProps{I18nProps: props.I18nProps, Profile: profile}),
		ui.CreateElement(Panel, PanelProps{Title: props.OrganizationTitle, Body: html.Div(html.Props{Class: "myself-organization"},
			html.P(html.Props{Class: "muted"}, ui.Text(props.OrganizationDescription)),
			ui.CreateElement(OrganizationOwnershipTree, OrganizationOwnershipTreeProps{Nodes: props.OrganizationTree, Label: props.OrganizationTreeLabel}),
		)}),
	)
}

// myselfProfileWithTopOrganizationFact replaces an absent manager value only
// when the authorized self-service subtree proves that the viewer is a
// genuine organization root. An explained root (for example, a withheld or
// unresolved relationship) remains unavailable rather than being mislabeled
// as the top of the organization.
func myselfProfileWithTopOrganizationFact(profile PersonProfileProps, tree []OwnershipNodeProps, topLabel string) PersonProfileProps {
	if len(tree) == 0 || strings.TrimSpace(topLabel) == "" {
		return profile
	}
	root := tree[0]
	if root.Explanation != "" || root.Manager != "" {
		return profile
	}
	managerLabel := profile.Organization.Text("person.manager")
	for index := range profile.Organization.Facts {
		fact := &profile.Organization.Facts[index]
		if fact.Label == managerLabel && fact.Status == WorkerFactMissing {
			fact.Value = topLabel
			fact.Status = WorkerFactPresent
			break
		}
	}
	return profile
}

// myselfProfileWithoutSelfPromotion removes a promotion launcher whose
// worker query points back to the self-service record. The server applies the
// same separation-of-duties rule at Propose, so the entry point must not
// invite a request it will reject. Other workflow links remain untouched.
func myselfProfileWithoutSelfPromotion(profile PersonProfileProps, reason string) PersonProfileProps {
	filtered := profile.Workflows.Workflows[:0]
	removed := false
	for _, card := range profile.Workflows.Workflows {
		query, err := url.Parse(card.Href)
		if err == nil && query.Query().Get("mode") == "new" {
			removed = true
			continue
		}
		filtered = append(filtered, card)
	}
	if !removed {
		return profile
	}
	profile.Workflows.Workflows = filtered
	profile.Workflows.TotalCount = len(filtered)
	// The removed card is the viewer's own promotion request. Its refusal is
	// more specific and more useful than a stale generic server-side launcher
	// explanation, so it must replace that explanation even when one arrived.
	profile.Workflows.UnavailableDetail = reason
	return profile
}

type SelfServiceBoundaryProps struct{ I18nProps }

// SelfServiceBoundary makes the action model unmistakable before a user
// reaches sensitive or payroll facts: records are view-only and changes are
// requests executed by governed workflows.
func SelfServiceBoundary(props SelfServiceBoundaryProps) ui.Node {
	return html.Section(html.Props{Class: "surface self-service-boundary", Raw: map[string]any{"role": "note"}},
		html.Div(html.Props{Class: "self-service-boundary-icon"}, productIcon("info", "self-service-boundary-glyph")),
		html.Div(html.Props{Class: "self-service-boundary-copy"},
			html.H2(html.Props{}, ui.Text(props.Text("myself.read_only_title"))),
			html.P(html.Props{Class: "muted"}, ui.Text(props.Text("myself.read_only_detail"))),
		),
		html.Span(html.Props{Class: "status self-service-badge"}, ui.Text(props.Text("myself.read_only_badge"))),
	)
}
