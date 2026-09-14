package productui

import (
	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// PersonActiveWorkflowsProps is PROMOUX-012's in-progress promotion list for
// one worker's profile: every open journey for that worker with its next
// transition, who holds it, and a direct link that resumes it (when the
// server says the viewer must act) or opens it. Past workflows stay in the
// profile's WorkflowHistory section beside it.
type PersonActiveWorkflowsProps struct {
	I18nProps
	// Show is false on routes that do not carry this section (self-service).
	Show        bool
	Title       string
	Description string
	EmptyText   string
	Items       []PersonActiveWorkflowProps
}

// PersonActiveWorkflowProps is one open journey, already localized.
type PersonActiveWorkflowProps struct {
	ID         string
	Title      string
	Summary    string
	Status     string
	NextStep   string
	WaitingOn  string
	Action     string
	LinkLabel  string
	Href       string
	Navigate   func(string)
	Actionable bool
}

// personActiveWorkflowsProps builds the section from the admitted work
// population: open journeys whose subject is person, most urgent first. The
// link says Resume only when the server's projection asks the viewer to act.
func personActiveWorkflowsProps(view View, person Person, target PageID) PersonActiveWorkflowsProps {
	if target != PagePerson {
		return PersonActiveWorkflowsProps{}
	}
	text := view.Locale.Text
	props := PersonActiveWorkflowsProps{
		Show: true, Title: text("person.active_workflows"),
		Description: text("person.active_workflows_detail", map[string]string{"name": person.Name}),
		EmptyText:   text("person.active_workflows_empty"),
	}
	for _, item := range SortWorkByUrgency(personOpenWork(view, person.ID)) {
		action := text("person.workflow_open")
		if WorkNeedsViewerAction(item) {
			action = text("person.workflow_resume")
		}
		href := item.Href
		if href == "" {
			href = JourneyDetailHref(view, item.ID)
		}
		title := localizedWorkTitle(view.Locale, item)
		if title == "" {
			title = text("history.promotion")
		}
		props.Items = append(props.Items, PersonActiveWorkflowProps{
			ID: item.ID, Title: title, Summary: item.Summary, Status: localizedWorkStatus(view.Locale, item),
			NextStep: workNextStepText(view.Locale, item.NextStep), WaitingOn: workWaitingOnText(view.Locale, item.WaitingOn),
			Action: action, LinkLabel: text("person.workflow_link_label", map[string]string{"action": action, "name": person.Name}),
			Href: href, Navigate: view.Navigate, Actionable: WorkNeedsViewerAction(item),
		})
	}
	return props
}

// personOpenWork is every admitted open journey whose subject resolves to
// personID, in input order.
func personOpenWork(view View, personID string) []WorkItem {
	var open []WorkItem
	if personID == "" {
		return open
	}
	for _, item := range admittedWork(view) {
		if !item.Terminal && item.PersonRef != "" && stablePersonID(view.People, item.PersonRef) == personID {
			open = append(open, item)
		}
	}
	return open
}

// PersonActiveWorkflows renders the section. Each journey is one list item
// whose link carries an accessible name naming both the action and the
// worker, so a screen-reader user hears which promotion "Resume" resumes.
func PersonActiveWorkflows(props PersonActiveWorkflowsProps) ui.Node {
	if !props.Show {
		return nil
	}
	head := html.Div(html.Props{Class: "section-head"}, html.Div(html.Props{},
		html.H2(html.Props{ID: "person-active-workflows-title"}, ui.Text(props.Title)),
		html.P(html.Props{Class: "muted"}, ui.Text(props.Description)),
	))
	if len(props.Items) == 0 {
		return html.Section(html.Props{Class: "surface person-active-workflows", Raw: map[string]any{"aria-labelledby": "person-active-workflows-title"}},
			head, html.P(html.Props{Class: "muted person-active-empty"}, ui.Text(props.EmptyText)))
	}
	items := make([]ui.Node, 0, len(props.Items))
	for _, item := range props.Items {
		lines := []ui.Node{
			html.Strong(html.Props{}, ui.Text(item.Title)),
			html.Span(html.Props{Class: "person-active-status"}, ui.Text(item.Status)),
		}
		if item.Summary != "" {
			lines = append(lines, html.Small(html.Props{Class: "person-active-summary"}, ui.Text(item.Summary)))
		}
		if item.NextStep != "" {
			lines = append(lines, html.Small(html.Props{Class: "person-active-next-step"}, ui.Text(item.NextStep)))
		}
		if item.WaitingOn != "" {
			lines = append(lines, html.Small(html.Props{Class: "person-active-waiting-on"}, ui.Text(item.WaitingOn)))
		}
		linkClass := "button secondary"
		if item.Actionable {
			linkClass = "button primary"
		}
		items = append(items, html.Li(html.Props{Class: "person-active-item"},
			html.Div(html.Props{Class: "person-active-main"}, lines...),
			softwareLink(item.Navigate, html.Props{Class: linkClass, Aria: map[string]string{"label": item.LinkLabel}}, item.Href, ui.Text(item.Action)),
		))
	}
	return html.Section(html.Props{Class: "surface person-active-workflows", Raw: map[string]any{"aria-labelledby": "person-active-workflows-title"}},
		head, html.Ul(html.Props{Class: "person-active-list", Raw: map[string]any{"role": "list"}}, items...))
}
