package productui

import (
	"fmt"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

type WorkPageProps struct {
	I18nProps
	Collection    WorkCollectionProps
	Preview       WorkPreviewProps
	HidePreview   bool
	Drafts        WorkCollectionProps
	Tracked       TrackedRequestsProps
	ShowSecondary bool
}

type WorkCollectionProps struct {
	I18nProps
	// EmptyTitle and EmptyDetail are the localized empty state for the
	// current view; empty falls back to the action-queue copy.
	Title       string
	Description string
	EmptyTitle  string
	EmptyDetail string
	Kind        string
	CountLabel  string
	Tabs        []WorkTabProps
	Rows        []WorkRowProps
	Footer      WorkCollectionFooterProps
}

type WorkTabProps struct {
	Label    string
	Href     string
	Active   bool
	Navigate func(string)
}

type WorkRowProps struct {
	I18nProps
	ID       string
	Initials string
	PhotoURL string
	Title    string
	Person   string
	Summary  string
	Due      string
	// JourneyStage is a server-projected workflow label, not a canonical
	// BusinessIntent lifecycle tuple. Never derive StatusProjection from it.
	JourneyStage     string
	StatusProjection StatusProjection
	// NextStep ("Next step: Manager decision") and WaitingOn ("Waiting on
	// the manager") are UXAUDIT-017's localized stage dimension. Empty
	// renders nothing.
	NextStep  string
	WaitingOn string
	// Assignment ("Assigned to you"), WorkDue ("Due 2026-10-01") and
	// NextAction ("Your next action: Decide approval") are the localized
	// server work item summary. Empty renders nothing; a row with Assignment
	// does not repeat the stage-derived WaitingOn.
	Assignment string
	WorkDue    string
	NextAction string
	// Tracking ("No action needed from you") marks a tracked request whose
	// next step belongs to someone else or the workflow (PROMOUX-012).
	Tracking string
	// Disposition is PROMOUX-003's approval verdict for this item, already
	// localized by approvalDispositionCardProps. Show is false when the item
	// carries no disposition (not an approval, or none was resolved), and
	// the row renders exactly as it did before this field existed.
	Disposition ApprovalDispositionCardProps
	Href        string
	Selected    bool
	Navigate    func(string)
}

// ApprovalDispositionCardProps is PROMOUX-003's rendered approval
// disposition: the five GREEN facts, already localized by
// approvalDispositionCardProps from a [workitem.ApprovalDisposition]. Every
// field here is presentation-ready text; this package's render functions
// never resolve locale copy from Reason codes or references themselves.
type ApprovalDispositionCardProps struct {
	// Show is false when the owning work item carries no disposition at
	// all -- render nothing, not an empty or default-valued card.
	Show bool
	// WaitingFor states the authority class this item awaits, e.g.
	// "Waiting for Finance Partner".
	WaitingFor string
	// AssignedTo names the assigned person or protected-group label, e.g.
	// "Assigned to Jane Smith" or "Assigned to a protected group of
	// approvers".
	AssignedTo      string
	AssignedIsGroup bool
	// Due states the item's own deadline, e.g. "Due September 12, 2026".
	Due string
	// ViewerAuthority states the viewer's own acting authority against this
	// item, e.g. "You are acting as Finance Partner" or "You hold no acting
	// authority for this approval".
	ViewerAuthority string
	Available       bool
	// Reason explains why the action is, or is not, available. It is always
	// populated when Show is true, and never names the identity of a
	// conflicting sibling's completer -- only the stable rule that applies.
	Reason string
}

type WorkCollectionFooterProps struct {
	Label string
	// Note, when set, is one collection-level sentence stating what the rows
	// deliberately do not carry (UXAUDIT-017: named assignees and action-by
	// dates live on each journey's work items, not on the list).
	Note   string
	Action ActionLinkProps
}

type WorkPreviewProps struct {
	I18nProps
	Empty            bool
	ID               string
	Initials         string
	PhotoURL         string
	Title            string
	Person           string
	Summary          string
	JourneyStage     string
	StatusProjection StatusProjection
	Provenance       ProvenanceProjection
	// Disposition is PROMOUX-003's approval verdict for this item; see
	// WorkRowProps.Disposition.
	Disposition ApprovalDispositionCardProps
	FactsTitle  string
	Facts       []FactProps
	// Diagnostics is PROMOUX-008's authorized-only journey id disclosure.
	// The raw identifier no longer travels in Facts (GREEN: an ordinary
	// reviewer sees no work-item UUID); it lives only here, gated by
	// Diagnostics.Available.
	Diagnostics TechnicalDetailsProps
	Action      ActionLinkProps
	EmptyTitle  string
	EmptyDetail string
}

func WorkPage(props WorkPageProps) ui.Node {
	props.Collection.I18nProps = props.I18nProps
	props.Preview.I18nProps = props.I18nProps
	var primary ui.Node
	if props.HidePreview {
		primary = html.Div(html.Props{Class: "page-stack"}, ui.CreateElement(WorkCollection, props.Collection))
	} else {
		primary = html.Div(html.Props{Class: "workbench"},
			ui.CreateElement(WorkCollection, props.Collection),
			ui.CreateElement(WorkPreview, props.Preview),
		)
	}
	if !props.ShowSecondary {
		return primary
	}
	secondary := make([]ui.Node, 0, 2)
	if len(props.Drafts.Rows) > 0 {
		secondary = append(secondary, ui.CreateElement(WorkCollection, props.Drafts))
	}
	if len(props.Tracked.Items) > 0 {
		secondary = append(secondary, ui.CreateElement(TrackedRequests, props.Tracked))
	}
	return html.Div(html.Props{Class: "page-stack"}, primary, html.Div(html.Props{Class: "work-secondary"}, secondary...))
}

func WorkCollection(props WorkCollectionProps) ui.Node {
	tabs := make([]ui.Node, 0, len(props.Tabs))
	for _, item := range props.Tabs {
		tabs = append(tabs, ui.CreateElement(WorkTab, item))
	}
	rows := make([]ui.Node, 0, len(props.Rows))
	for _, item := range props.Rows {
		item.I18nProps = props.I18nProps
		rows = append(rows, ui.CreateElement(WorkRow, item))
	}
	if len(rows) == 0 {
		emptyTitle, emptyDetail := props.EmptyTitle, props.EmptyDetail
		if emptyTitle == "" {
			emptyTitle = props.Text("work.empty_title")
		}
		if emptyDetail == "" {
			emptyDetail = props.Text("work.empty_detail")
		}
		rows = append(rows, html.Li(html.Props{Class: "collection-empty"},
			html.Strong(html.Props{}, ui.Text(emptyTitle)),
			html.Small(html.Props{}, ui.Text(emptyDetail)),
		))
	}
	foot := []ui.Node{}
	if props.Footer.Label != "" {
		foot = append(foot, html.Span(html.Props{}, ui.Text(props.Footer.Label)))
	}
	if props.Footer.Note != "" {
		foot = append(foot, html.Small(html.Props{Class: "work-list-note"}, ui.Text(props.Footer.Note)))
	}
	if props.Footer.Action.Href != "" {
		foot = append(foot, ui.CreateElement(ActionLink, props.Footer.Action))
	}
	sectionProps := html.Props{Class: "surface work-list", Aria: map[string]string{"label": props.Text("work.collection_label")}}
	if props.Kind != "" {
		sectionProps.DataAttr = html.DataAttribute{Name: "work-kind", Value: props.Kind}
	}
	title := ui.Node(html.H2(html.Props{}, ui.Text(props.Title)))
	if props.Description != "" {
		title = html.Div(html.Props{},
			html.H2(html.Props{}, ui.Text(props.Title)),
			html.P(html.Props{Class: "muted"}, ui.Text(props.Description)),
		)
	}
	heading := []ui.Node{title, html.Span(html.Props{Class: "count"}, ui.Text(props.CountLabel))}
	children := []ui.Node{html.Div(html.Props{Class: "section-head"}, heading...)}
	if len(tabs) > 0 {
		children = append(children, html.Nav(html.Props{Class: "tabs", Aria: map[string]string{"label": props.Text("work.filter_label")}}, tabs...))
	}
	children = append(children, html.Ul(html.Props{Class: "work-rows", Raw: map[string]any{"role": "list"}}, rows...))
	if len(foot) > 0 {
		children = append(children, html.Div(html.Props{Class: "panel-foot"}, foot...))
	}
	return html.Section(sectionProps, children...)
}

func WorkTab(props WorkTabProps) ui.Node {
	class := "tab"
	action := ActionLinkProps{Label: props.Label, Href: props.Href, Class: class, Navigate: props.Navigate}
	if props.Active {
		class += " active"
		return softwareLink(props.Navigate, html.Props{Class: class, Aria: map[string]string{"current": "page"}}, props.Href, ui.Text(props.Label))
	}
	action.Class = class
	return ui.CreateElement(ActionLink, action)
}

func WorkRow(props WorkRowProps) ui.Node {
	class := "work-row"
	if props.Selected {
		class += " selected"
	}
	linkProps := html.Props{Class: class}
	if props.Selected {
		linkProps.Aria = map[string]string{"current": "true"}
	}
	status := workStatus(props.I18nProps, "work-row-status-"+props.ID, props.StatusProjection, props.JourneyStage)
	main := []ui.Node{
		html.Strong(html.Props{}, ui.Text(props.Title)),
		html.Small(html.Props{}, ui.Text(props.Person)),
		html.Small(html.Props{Class: "row-summary"}, ui.Text(props.Summary)),
	}
	// PROMOUX-003: the row's own compact disposition summary -- "Waiting
	// for <role>" -- so an approver scanning the list learns whose turn it
	// is without opening the preview. It renders only what
	// approvalDispositionCardProps already localized.
	// UXAUDIT-017: an action queue row leads with the single next step. The
	// stage-derived waiting-on class yields to PROMOUX-003's disposition when
	// one exists, so a row never states whose turn it is twice.
	if props.NextAction != "" {
		main = append(main, html.Small(html.Props{Class: "row-next-action"}, ui.Text(props.NextAction)))
	}
	if props.Tracking != "" {
		main = append(main, html.Small(html.Props{Class: "row-tracking"}, ui.Text(props.Tracking)))
	}
	if props.NextStep != "" {
		main = append(main, html.Small(html.Props{Class: "row-next-step"}, ui.Text(props.NextStep)))
	}
	switch {
	case props.Disposition.Show:
		main = append(main, html.Small(html.Props{Class: "row-disposition"}, ui.Text(props.Disposition.WaitingFor)))
	case props.Assignment != "":
		main = append(main, html.Small(html.Props{Class: "row-assignment"}, ui.Text(props.Assignment)))
	case props.WaitingOn != "":
		main = append(main, html.Small(html.Props{Class: "row-waiting-on"}, ui.Text(props.WaitingOn)))
	}
	if props.WorkDue != "" {
		main = append(main, html.Small(html.Props{Class: "row-work-due"}, ui.Text(props.WorkDue)))
	}
	return html.Li(html.Props{Class: "work-row-item"}, softwareLink(props.Navigate, linkProps, props.Href,
		personAvatar(props.Person, props.Initials, props.PhotoURL, ""),
		html.Span(html.Props{Class: "row-main"}, main...),
		html.Span(html.Props{Class: "row-end"}, status, rowEffectiveDate(props.I18nProps, props.Due)),
		productIcon("expand", "work-row-chevron"),
	))
}

// rowEffectiveDate renders the queue row's own date, labeled for what it
// actually is. UXAUDIT-017: the wire's only date on a journey summary is its
// effective date -- the promotion's planned start, not a deadline by which
// the viewer must act -- and the server carries no separate action-by/due
// field on that summary (see productclient's projectJourneys). Showing it
// bare read as a due date to every reader who scanned the row; labeling it
// "Effective" is honest about what the server actually said without
// fabricating an action-by date it never sent. Renders nothing for a row
// with no date rather than an empty label.
func rowEffectiveDate(i18n I18nProps, date string) ui.Node {
	if date == "" {
		return nil
	}
	return html.Small(html.Props{Class: "row-effective-date"}, ui.Text(i18n.Text("work.row_effective_date", map[string]string{"date": date})))
}

// ApprovalDispositionCard renders PROMOUX-003's five GREEN facts -- waiting
// for, assigned to, due date, the viewer's acting authority and why the
// action is or is not available -- exactly as approvalDispositionCardProps
// already localized them. It renders nothing when props.Show is false, and
// it never resolves locale copy or recomputes availability itself: every
// string here is presentation-ready input.
func ApprovalDispositionCard(props ApprovalDispositionCardProps) ui.Node {
	if !props.Show {
		return nil
	}
	tone := "disposition-available"
	if !props.Available {
		tone = "disposition-unavailable"
	}
	assignedClass := "disposition-assigned"
	if props.AssignedIsGroup {
		assignedClass += " disposition-assigned-group"
	}
	return html.Div(html.Props{Class: "approval-disposition " + tone, Raw: map[string]any{"role": "group"}},
		html.P(html.Props{Class: "disposition-waiting-for"}, ui.Text(props.WaitingFor)),
		html.P(html.Props{Class: assignedClass}, ui.Text(props.AssignedTo)),
		html.P(html.Props{Class: "disposition-due"}, ui.Text(props.Due)),
		html.P(html.Props{Class: "disposition-viewer-authority"}, ui.Text(props.ViewerAuthority)),
		html.P(html.Props{Class: "disposition-reason"}, ui.Text(props.Reason)),
	)
}

func workStatus(i18n I18nProps, id string, projection StatusProjection, stage string) ui.Node {
	if stage != "" && !projection.Available {
		return html.Span(html.Props{Class: "workflow-stage"}, ui.Text(stage))
	}
	return ui.CreateElement(StatusPresentation, StatusPresentationProps{I18nProps: i18n, IDSeed: id, Projection: projection})
}

func WorkPreview(props WorkPreviewProps) ui.Node {
	if props.Empty {
		return ui.CreateElement(EmptyState, EmptyStateProps{
			Title: props.EmptyTitle, Description: props.EmptyDetail, Class: "work-preview", Action: &props.Action,
		})
	}
	facts := append([]ui.Node{html.H3(html.Props{}, ui.Text(props.FactsTitle))}, factRows(props.Facts)...)
	status := workStatus(props.I18nProps, "work-preview-status-"+props.ID, props.StatusProjection, props.JourneyStage)
	provenance := ui.CreateElement(ProvenancePresentation, ProvenancePresentationProps{I18nProps: props.I18nProps, IDSeed: "work-preview-provenance-" + props.ID, Projection: props.Provenance})
	children := []ui.Node{
		html.Div(html.Props{Class: "preview-head"},
			personAvatar(props.Person, props.Initials, props.PhotoURL, ""),
			html.Div(html.Props{}, html.Small(html.Props{}, ui.Text(props.Text("work.selected_label"))), html.H2(html.Props{}, ui.Text(props.Title)), html.P(html.Props{Class: "muted"}, ui.Text(fmt.Sprintf("%s · %s", props.Person, props.Summary)))),
			status,
		),
	}
	// PROMOUX-003: the full disposition card -- rendered before the facts
	// and the action, since "why the action is or is not available" is what
	// an approver needs to read before deciding whether to open the journey
	// at all.
	if props.Disposition.Show {
		children = append(children, ui.CreateElement(ApprovalDispositionCard, props.Disposition))
	}
	diagnostics := props.Diagnostics
	diagnostics.I18nProps = props.I18nProps
	children = append(children,
		html.Div(html.Props{Class: "facts"}, facts...),
		ui.CreateElement(TechnicalDetails, diagnostics),
		provenance, ui.CreateElement(ActionLink, props.Action),
	)
	return html.Aside(html.Props{Class: "surface work-preview", Aria: map[string]string{"label": props.Text("work.selected_summary")}}, children...)
}
