package productui

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type OrganizationPageProps struct {
	I18nProps
	Title       string
	Description string
	ViewLabel   string
	FlatAction  ActionLinkProps
	TreeAction  ActionLinkProps
	TreeActive  bool
	TreeLocked  bool
	Metadata    BusinessMetadataProps
	Summary     OrganizationSummaryProps
	Density     string
	Groups      []OrganizationGroupProps
	Tree        []OwnershipNodeProps
	TreeLabel   string
	Search      OrganizationSearchProps
	Empty       EmptyStateProps
}

// OrganizationSummaryProps is deliberately derived from the admitted
// projection. It gives people an at-a-glance orientation without claiming
// totals for records outside the viewer's server-authorized scope.
type OrganizationSummaryProps struct {
	VisiblePeople    int
	Units            int
	Scope            string
	CompactLabel     string
	ComfortableLabel string
	SpaciousLabel    string
	ExpandAllLabel   string
	CollapseAllLabel string
}

// OrganizationSearchProps is an SSR-first, authorized organization search.
// The input only changes the view over an already admitted projection; it
// never asks the browser to discover records on its own.
type OrganizationSearchProps struct {
	I18nProps
	Query     string
	Action    string
	ClearHref string
	Summary   string
	// HiddenInputs preserves addressable view state through a native GET
	// submit. Browsers replace an action URL's query with successful form
	// controls, so query state must be represented as controls explicitly.
	HiddenInputs map[string]string
	Navigate     func(string)
	OnFilter     func(string)
}

// BusinessMetadataProps is the narrow, reusable contract for an authorized
// organization summary. It intentionally carries display facts rather than a
// page View so future company-record services can populate it without changing
// the component composition.
type BusinessMetadataProps struct {
	Title          string
	Description    string
	Items          []BusinessMetadataItemProps
	FootprintLabel string
	Footprint      []string
	Boundary       string
}

type BusinessMetadataItemProps struct {
	Label string
	Value string
}

type OrganizationGroupProps struct {
	Name       string
	Count      int
	CountLabel string
	Members    []OwnershipNodeProps
}

// OrganizationRelationshipState describes the server-admitted relationship
// for a worker. Empty manager references are deliberately not treated as
// edges: a root has no manager, while a withheld relationship is explained to
// the viewer without inventing a parent.
type OrganizationRelationshipState string

const (
	OrganizationRelationshipRoot     OrganizationRelationshipState = "root"
	OrganizationRelationshipVisible  OrganizationRelationshipState = "visible"
	OrganizationRelationshipOrphan   OrganizationRelationshipState = "orphan"
	OrganizationRelationshipWithheld OrganizationRelationshipState = "withheld"
)

// OrganizationRelationshipProjection is the one authorized relationship
// projection shared by the flat directory, reporting tree, and Myself
// subtree. The maps are keyed by the worker's stable product ID.
type OrganizationRelationshipProjection struct {
	ManagerByWorker      map[string]string
	ManagerLabelByWorker map[string]string
	StateByWorker        map[string]OrganizationRelationshipState
	ExplanationByWorker  map[string]string
}

// OwnershipNodeProps is the recursive, presentation-only reporting-line
// contract. Every person has already passed the page's visibility boundary.
// UXAUDIT-004: this is the ONE organization-node contract the flat
// organization list, the organization tree, and the Myself subtree all
// render through -- organizationPersonCard is their shared leaf renderer, and
// every field here comes from the one authorized relationship projection
// (see organization_relationships.go), never from a name match.
type OwnershipNodeProps struct {
	I18nProps
	Manager, ManagerRef, Location              string
	RelationshipState, RelationshipExplanation string
	Open                                       bool
	// ID is the person's public routing reference (Person.ID), used to find
	// and re-root a subtree (Myself) and to compare against View.SelectedPerson.
	ID                                         string
	Name, Role, Team, Initials, PhotoURL, Href string
	WorkerNumber, ReportsLabel                 string
	Navigate                                   func(string)
	Reports                                    []OwnershipNodeProps
	Current                                    bool
	// Level is the node's 1-based rendered tree depth. A root -- whether a
	// genuine top of the organization or an explained root that could not be
	// honestly nested -- is always 1; a nested node is always its parent's
	// Level+1. aria-level is read directly from this field, so accessibility
	// and visual nesting can never disagree.
	Level int
	// Selected mirrors View.SelectedPerson so flat and tree modes highlight
	// the same node without duplicating the comparison in each renderer.
	Selected bool
	// Explanation is non-empty only when this node renders as a root that is
	// NOT a genuine top of the organization: a withheld, not-visible,
	// ambiguous, stale, cyclical, or otherwise undetermined manager
	// relationship. It is never populated for a nested node, and never for a
	// genuine no-manager root, which needs no explanation at all -- see
	// UXAUDIT-004's "never invent or flatten hierarchy" clause.
	Explanation string
	// ManagerSummary is the localized "Reports to: <name>" line for a nested
	// node. It exists so the flat, non-hierarchical list conveys the same
	// manager relationship the tree conveys through nesting -- UXAUDIT-004's
	// flat/tree parity clause -- without a second lookup.
	ManagerSummary string
}

func OrganizationPage(props OrganizationPageProps) ui.Node {
	props.Search.I18nProps = props.I18nProps
	sections := make([]ui.Node, 0, 2)
	if props.Metadata.Title != "" {
		sections = append(sections, ui.CreateElement(BusinessMetadata, props.Metadata))
	}
	if len(props.Groups) == 0 && len(props.Tree) == 0 {
		sections = append(sections, ui.CreateElement(EmptyState, props.Empty))
		return html.Div(html.Props{Class: "organization-page"}, sections...)
	}
	content := organizationFlat(props.Groups)
	if props.TreeActive {
		content = organizationTree(props.Tree, props.TreeLabel)
	}
	viewHead := []ui.Node{html.P(html.Props{Class: "definition"}, ui.Text(props.Description))}
	if !props.TreeLocked {
		viewHead = append(viewHead, html.Div(html.Props{Class: "organization-view-toggle", Raw: map[string]any{"role": "group", "aria-label": props.ViewLabel}},
			organizationViewAction(props.FlatAction, !props.TreeActive),
			organizationViewAction(props.TreeAction, props.TreeActive),
		))
	}
	density := normalizeOrganizationDensity(props.Density)
	body := html.Div(html.Props{Class: "org organization-density-" + density},
		organizationBrowseSummary(props),
		ui.CreateElement(OrganizationSearch, props.Search),
		html.Div(html.Props{Class: "organization-view-head"}, viewHead...),
		content,
	)
	// Exploration is the primary task; business context remains available below it.
	sections = append([]ui.Node{ui.CreateElement(Panel, PanelProps{Title: props.Title, Body: body})}, sections...)
	return html.Div(html.Props{Class: "organization-page"}, sections...)
}

func normalizeOrganizationDensity(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "compact", "comfortable", "spacious":
		return strings.ToLower(strings.TrimSpace(value))
	default:
		return "comfortable"
	}
}

// organizationBrowseSummary keeps the high-value orientation and controls
// before the potentially long list. Native details/summary remain the source
// of truth for disclosure; data hooks allow a progressive client enhancer to
// operate on these same nodes without inventing records or replacing links.
func organizationBrowseSummary(props OrganizationPageProps) ui.Node {
	count := props.Text("organization.visible_workforce")
	units := props.Text("organization.units")
	scope := strings.TrimSpace(props.Summary.Scope)
	if scope == "" {
		scope = props.Text("common.not_reported")
	}
	return html.Div(html.Props{Class: "organization-browse-summary", Raw: map[string]any{"aria-label": props.Text("organization.structure_title"), "data-organization-summary": "true"}},
		html.Div(html.Props{Class: "organization-summary-facts"},
			html.Span(html.Props{Class: "organization-summary-fact"}, html.Strong(html.Props{}, ui.Text(props.Locale.FormatNumber(strconv.Itoa(props.Summary.VisiblePeople), 0))), html.Small(html.Props{}, ui.Text(count))),
			html.Span(html.Props{Class: "organization-summary-fact"}, html.Strong(html.Props{}, ui.Text(props.Locale.FormatNumber(strconv.Itoa(props.Summary.Units), 0))), html.Small(html.Props{}, ui.Text(units))),
			html.Span(html.Props{Class: "organization-summary-scope"}, html.Small(html.Props{}, ui.Text(props.Text("organization.access_scope"))), html.Span(html.Props{}, ui.Text(scope))),
		),
	)
}

func OrganizationSearch(props OrganizationSearchProps) ui.Node {
	query := props.Query
	input := html.Props{ID: "organization-search", Name: "q", Value: props.Query,
		Raw: map[string]any{"type": "search", "placeholder": props.Text("people.filter_placeholder"), "aria-label": props.Text("people.filter_aria")}}
	form := html.Props{Class: "organization-search", Action: props.Action, Method: "get", Raw: map[string]any{"role": "search"}}
	if props.OnFilter != nil {
		input.OnInput = ui.UseEvent(func(event ui.InputEvent) { query = event.GetValue() })
		onFilter := props.OnFilter
		form.OnSubmit = ui.UseEvent(func(event ui.FormEvent) {
			event.PreventDefault()
			onFilter(strings.TrimSpace(query))
		})
	}
	hiddenNames := make([]string, 0, len(props.HiddenInputs))
	for name := range props.HiddenInputs {
		hiddenNames = append(hiddenNames, name)
	}
	sort.Strings(hiddenNames)
	children := make([]ui.Node, 0, len(hiddenNames)+4)
	for _, name := range hiddenNames {
		children = append(children, html.Tag("input", html.Props{Name: name, Value: props.HiddenInputs[name], Raw: map[string]any{"type": "hidden"}}))
	}
	children = append(children,
		html.Label(html.Props{For: "organization-search"}, ui.Text(props.Text("people.find"))),
		html.Div(html.Props{Class: "organization-search-control"},
			html.Tag("input", input),
			html.Button(html.Props{Class: "button primary", Type: "submit"}, ui.Text(props.Text("people.filter"))),
		),
	)
	status := make([]ui.Node, 0, 2)
	if props.Query != "" {
		status = append(status, softwareLink(props.Navigate, html.Props{Class: "button secondary compact"}, props.ClearHref, ui.Text(props.Text("people.clear"))))
	}
	if props.Summary != "" {
		status = append(status, html.Span(html.Props{Class: "muted organization-search-summary", Raw: map[string]any{"aria-live": "polite"}}, ui.Text(props.Summary)))
	}
	if len(status) != 0 {
		children = append(children, html.Div(html.Props{Class: "organization-search-status"}, status...))
	}
	return html.Form(form, children...)
}

func organizationViewAction(action ActionLinkProps, active bool) ui.Node {
	props := html.Props{Aria: map[string]string{}}
	if active {
		action.Class += " active"
		props.Aria["current"] = "true"
	}
	props.Class = action.Class
	return softwareLink(action.Navigate, props, action.Href, ui.Text(action.Label))
}

func organizationFlat(values []OrganizationGroupProps) ui.Node {
	groups := make([]ui.Node, 0, len(values))
	for _, group := range values {
		members := make([]ui.Node, 0, len(group.Members))
		for _, member := range group.Members {
			members = append(members, html.Li(html.Props{}, organizationPersonCard(member)))
		}
		groups = append(groups, html.Li(html.Props{Class: "organization-unit"},
			html.Details(html.Props{Class: "organization-unit-disclosure"},
				html.Summary(html.Props{Class: "org-node manager"},
					html.Span(html.Props{Class: "organization-unit-glyph", Aria: map[string]string{"hidden": "true"}}, productIcon("expand", "organization-unit-chevron")),
					html.Span(html.Props{Class: "row-main"}, html.Strong(html.Props{}, ui.Text(group.Name)), html.Small(html.Props{}, ui.Text(group.CountLabel))),
				),
				html.Ul(html.Props{Class: "organization-unit-members", Raw: map[string]any{"role": "list"}}, members...),
			),
		))
	}
	return html.Ul(html.Props{Class: "org-branches", Raw: map[string]any{"role": "list", "data-organization-view": "flat"}}, groups...)
}

func organizationTree(values []OwnershipNodeProps, label string) ui.Node {
	return ui.CreateElement(OrganizationOwnershipTree, OrganizationOwnershipTreeProps{Nodes: values, Label: label})
}

type OrganizationOwnershipTreeProps struct {
	Nodes []OwnershipNodeProps
	Label string
}

// OrganizationOwnershipTree is shared by the organization page's tree view
// and the Myself subtree so reporting semantics and accessibility cannot
// drift between them (UXAUDIT-004 REFACTOR). It renders the real WAI-ARIA
// tree structure the live audit found missing: role="tree" on the container,
// role="treeitem" on every node, aria-level tied to OwnershipNodeProps.Level
// (which is itself tied to the authorized relationship projection's depth,
// not a hardcoded number), role="group" wrapping each child set, and
// aria-expanded present only on a node that actually has children.
func OrganizationOwnershipTree(props OrganizationOwnershipTreeProps) ui.Node {
	nodes := make([]ui.Node, 0, len(props.Nodes))
	for _, value := range props.Nodes {

		nodes = append(nodes, ui.CreateElement(organizationTreeItem, value))
	}
	treeProps := html.Props{Class: "ownership-tree", Raw: map[string]any{"role": "tree", "data-organization-view": "tree"}}
	if props.Label != "" {
		treeProps.Aria = map[string]string{"label": props.Label}
	}
	return html.Ul(treeProps, nodes...)
}

// organizationTreeItem renders one treeitem. Expand/collapse is real,
// client-interactive state (ui.UseState, the same idiom NavigationDrawerScope
// and the global search popover already use), defaulting to expanded so a
// server-rendered document exposes every level without a click -- what the
// PRIMARY, ACCESSIBILITY and PROPERTY tests parse.
func organizationTreeItem(props OwnershipNodeProps) ui.Node {
	expanded := ui.UseState(true)
	hasChildren := len(props.Reports) > 0
	aria := map[string]string{"level": strconv.Itoa(props.Level), "selected": fmt.Sprint(props.Selected)}
	if hasChildren {
		aria["expanded"] = fmt.Sprint(expanded.Get())
	}
	content := []ui.Node{organizationPersonCard(props)}
	if hasChildren {
		// The visible label IS the accessible name (WCAG 2.5.3 Label in
		// Name): the button reads "Direct reports: N" either way, and
		// aria-expanded on the enclosing treeitem carries the open/closed
		// state, so there is no separate "Collapse/Expand" phrase to drift
		// out of sync with what a sighted user actually sees.
		content = append(content, html.Button(html.Props{
			Type: "button", Class: "ownership-toggle",
			OnClick: ui.UseEvent(func(ui.MouseEvent) { expanded.Set(!expanded.Get()) }),
		},
			html.Span(html.Props{Aria: map[string]string{"hidden": "true"}}, ui.Text(organizationToggleGlyph(expanded.Get()))),
			ui.Text(props.ReportsLabel),
		))
		children := make([]ui.Node, 0, len(props.Reports))
		for _, report := range props.Reports {
			children = append(children, ui.CreateElement(organizationTreeItem, report))
		}
		groupProps := html.Props{Class: "ownership-reports", Raw: map[string]any{"role": "group"}}
		if !expanded.Get() {
			groupProps.Hidden = true
		}
		content = append(content, html.Ul(groupProps, children...))
	}

	liProps := html.Props{Raw: map[string]any{"role": "treeitem"}, Aria: aria}
	if props.RelationshipState != "" {
		liProps.Raw["data-relationship-state"] = props.RelationshipState
	}
	return html.Li(liProps, content...)
}

func organizationToggleGlyph(expanded bool) string {
	if expanded {
		return "⌄"
	}
	return "›"
}

// organizationPersonCard is the single organization-node leaf renderer:
// UXAUDIT-004's flat organization list, organization tree, and Myself
// subtree all render every person through this one function.
func organizationPersonCard(props OwnershipNodeProps) ui.Node {
	class := "ownership-card"
	if props.Current {
		class += " current-person"
	}
	if props.Selected {

		class += " selected selected-person"
	}
	metadata := []ui.Node{html.Strong(html.Props{}, ui.Text(props.Name))}
	if props.Current {
		metadata = append(metadata, html.Span(html.Props{Class: "sr-only"}, ui.Text(props.Text("organization.current_you"))))
	}
	if props.Selected {
		metadata = append(metadata, html.Span(html.Props{Class: "sr-only"}, ui.Text(props.Text("organization.selected_person"))))
	}
	for _, value := range []string{props.Role, props.Team} {
		if value != "" {
			metadata = append(metadata, html.Small(html.Props{}, ui.Text(value)))
		}
	}
	if props.ManagerSummary != "" {
		metadata = append(metadata, html.Small(html.Props{Class: "ownership-manager-summary"}, ui.Text(props.ManagerSummary)))
	}
	if props.Explanation != "" {
		metadata = append(metadata, html.Small(html.Props{Class: "ownership-explanation", Raw: map[string]any{"role": "note"}}, ui.Text(props.Explanation)))
	}
	children := []ui.Node{
		personAvatar(props.Name, props.Initials, props.PhotoURL, "small"),
		html.Span(html.Props{Class: "row-main"}, metadata...),
	}
	linkProps := html.Props{Class: class}
	if props.Current || props.Selected {
		linkProps.Raw = map[string]any{}
		if props.Current {
			linkProps.Raw["data-current-viewer"] = "true"
		}
		if props.Selected {
			linkProps.Raw["data-selected-person"] = "true"
		}
	}

	if props.Selected {
		if linkProps.Data == nil {
			linkProps.Data = map[string]string{}
		}
		linkProps.Data["selected"] = "true"
	}
	var card ui.Node
	if props.Href == "" {
		card = html.Div(linkProps, children...)
	} else {
		card = softwareLink(props.Navigate, linkProps, props.Href, children...)
	}
	// Keep the compact card scannable while making secondary employee facts
	// available on demand. The disclosure is separate from the profile link,
	// so keyboard users never encounter nested interactive controls in summary.
	details := make([]ui.Node, 0, 2)
	if props.WorkerNumber != "" {
		details = append(details, html.Div(html.Props{Class: "ownership-person-detail"}, html.Small(html.Props{}, ui.Text(props.Text("person.worker_number"))), html.Span(html.Props{}, ui.Text(props.WorkerNumber))))
	}
	if props.Manager != "" {
		details = append(details, html.Div(html.Props{Class: "ownership-person-detail"}, html.Small(html.Props{}, ui.Text(props.Text("person.manager"))), html.Span(html.Props{}, ui.Text(props.Manager))))
	}
	if props.Location != "" {
		details = append(details, html.Div(html.Props{Class: "ownership-person-detail"}, html.Small(html.Props{}, ui.Text(props.Text("person.work_location"))), html.Span(html.Props{}, ui.Text(props.Location))))
	}
	nodes := []ui.Node{card}
	if props.RelationshipExplanation != "" {
		nodes = append(nodes, html.P(html.Props{Class: "relationship-explanation", Raw: map[string]any{"role": "note"}}, ui.Text(props.RelationshipExplanation)))
	}
	if len(details) == 0 && len(nodes) == 1 {
		return card
	}
	if len(details) > 0 {
		nodes = append(nodes, html.Details(html.Props{Class: "ownership-person-disclosure"},
			html.Summary(html.Props{Aria: map[string]string{"label": props.Text("organization.employee_details_for", map[string]string{"name": props.Name})}}, ui.Text(props.Text("organization.employee_details"))),
			html.Div(html.Props{Class: "ownership-person-details"}, details...),
		))
	}
	return html.Div(html.Props{Class: "ownership-person"}, nodes...)
}

func BusinessMetadata(props BusinessMetadataProps) ui.Node {
	items := make([]ui.Node, 0, len(props.Items))
	for _, item := range props.Items {
		items = append(items, html.Div(html.Props{Class: "business-metadata-item"},
			html.Tag("dt", html.Props{}, ui.Text(item.Label)),
			html.Tag("dd", html.Props{}, ui.Text(item.Value)),
		))
	}
	body := []ui.Node{html.Tag("dl", html.Props{Class: "business-metadata-grid"}, items...)}
	if len(props.Footprint) > 0 {
		locations := make([]ui.Node, 0, len(props.Footprint))
		for _, location := range props.Footprint {
			locations = append(locations, html.Li(html.Props{}, ui.Text(location)))
		}
		body = append(body, html.Div(html.Props{Class: "business-footprint"},
			html.H3(html.Props{}, ui.Text(props.FootprintLabel)),
			html.Ul(html.Props{Raw: map[string]any{"role": "list"}}, locations...),
		))
	}
	if props.Boundary != "" {
		body = append(body, html.P(html.Props{Class: "business-metadata-boundary"}, ui.Text(props.Boundary)))
	}
	return html.Section(html.Props{Class: "surface organization-metadata", Raw: map[string]any{"aria-labelledby": "business-metadata-title"}},
		html.Div(html.Props{Class: "section-head"}, html.Div(html.Props{},
			html.H2(html.Props{ID: "business-metadata-title"}, ui.Text(props.Title)),
			html.P(html.Props{Class: "muted"}, ui.Text(props.Description)),
		)),
		html.Div(html.Props{Class: "business-metadata-body"}, body...),
	)
}
