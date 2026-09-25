package projectui

import (
	"strings"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// Tickets linked to workflow runs (a Promotion's approval, say). The link
// itself is authorized by the Project service; what a row shows about the
// workflow comes from the Work projection the viewer can already read, and a
// link the viewer cannot see stays a neutral locked row.

// WorkflowLink is one linked workflow item on a ticket.
type WorkflowLink struct {
	LinkID, WorkItemID string
	// JourneyID is set for a linked journey (a Promotion run); From and To
	// are its role change ("SEC-SE3 · P4", "SEC-DIR · M4").
	JourneyID, From, To string
	// Workflow and Step name the run and the human step ("Promotion",
	// "Manager approval"); Person is the run's subject when visible.
	Workflow, Step, Person string
	Status, StatusTone     string
	Due                    string
	// Href opens the item in My Work.
	Href  string
	State ReferenceState
}

// WorkflowOption is one work item the "Link workflow" picker offers.
type WorkflowOption struct {
	WorkItemID, Workflow, Step, Person string
	// JourneyID, From and To describe a journey option; Group is its stage
	// group heading ("In progress", "Blocked", "Done").
	JourneyID, From, To, Group string
	Status, StatusTone, Due    string
	// Open is true while the item still needs someone; Linked marks items
	// this ticket already links.
	Open, Linked bool
}

// WorkflowCopy holds the workflow strings.
type WorkflowCopy struct {
	Workflows, LinkWorkflow, NoWorkflows, Open, Unlink             string
	Restricted, Unavailable, Loading                               string
	PickerTitle, PickerSearch, PickerOpenOnly, PickerAll           string
	PickerEmpty, PickerLoading, Linked, FilterLinked, LinkFailed   string
	StatusOpen, StatusActive, StatusDone, StatusClosed, UnlinkFrom string
	BoardTitle, BoardHint                                          string
}

func localizedWorkflowCopy(copy WorkflowCopy) WorkflowCopy {
	defaults := WorkflowCopy{
		Workflows: "Workflows", LinkWorkflow: "Link workflow…", NoWorkflows: "No workflows linked yet. Link a promotion or another request this work belongs to.",
		Open: "Open", Unlink: "Unlink", Restricted: "Workflow item you can't see", Unavailable: "Workflow item unavailable", Loading: "Loading workflow…",
		PickerTitle: "Link a workflow", PickerSearch: "Search by person or step", PickerOpenOnly: "Open", PickerAll: "All",
		PickerEmpty: "No workflow items you can see match.", PickerLoading: "Loading workflow items…", Linked: "Linked",
		FilterLinked: "Linked to workflow", LinkFailed: "The workflow could not be linked. Reload and try again.",
		BoardTitle: "Workflows", BoardHint: "Promotions and other requests linked from this project's tickets.",
		StatusOpen: "Open", StatusActive: "In progress", StatusDone: "Done", StatusClosed: "Closed", UnlinkFrom: "Unlink from this ticket",
	}
	for _, pair := range []struct {
		value    *string
		fallback string
	}{
		{&copy.Workflows, defaults.Workflows}, {&copy.LinkWorkflow, defaults.LinkWorkflow}, {&copy.NoWorkflows, defaults.NoWorkflows},
		{&copy.Open, defaults.Open}, {&copy.Unlink, defaults.Unlink}, {&copy.Restricted, defaults.Restricted}, {&copy.Unavailable, defaults.Unavailable},
		{&copy.Loading, defaults.Loading}, {&copy.PickerTitle, defaults.PickerTitle}, {&copy.PickerSearch, defaults.PickerSearch},
		{&copy.PickerOpenOnly, defaults.PickerOpenOnly}, {&copy.PickerAll, defaults.PickerAll}, {&copy.PickerEmpty, defaults.PickerEmpty},
		{&copy.PickerLoading, defaults.PickerLoading}, {&copy.Linked, defaults.Linked}, {&copy.FilterLinked, defaults.FilterLinked},
		{&copy.LinkFailed, defaults.LinkFailed}, {&copy.StatusOpen, defaults.StatusOpen}, {&copy.StatusActive, defaults.StatusActive},
		{&copy.StatusDone, defaults.StatusDone}, {&copy.StatusClosed, defaults.StatusClosed}, {&copy.UnlinkFrom, defaults.UnlinkFrom},
		{&copy.BoardTitle, defaults.BoardTitle}, {&copy.BoardHint, defaults.BoardHint},
	} {
		if *pair.value == "" {
			*pair.value = pair.fallback
		}
	}
	return copy
}

// workflowChip is the compact "flow · Promotion" mark on cards and rows.
func workflowChip(names []string) ui.Node {
	if len(names) == 0 {
		return nil
	}
	return html.Span(html.Props{Class: "projectui-flow-chip", Title: strings.Join(names, ", ")},
		html.Span(html.Props{Class: "projectui-flow-icon", Aria: map[string]string{"hidden": "true"}}),
		html.Span(html.Props{Text: names[0]}))
}

// workflowSection lists a ticket's linked workflow items; compact drops the
// empty-state sentence for the modal.
func workflowSection(model DetailModel, compact bool) ui.Node {
	wf := model.Copy.Workflow
	head := []ui.Node{html.H2(html.Props{ID: "projectui-workflows-title", Text: wf.Workflows})}
	if len(model.Workflows) > 0 {
		head = append(head, html.Span(html.Props{Class: "projectui-count", Text: model.Copy.FormatNumber(len(model.Workflows))}))
	}
	if model.CanEdit {
		head = append(head, html.Button(html.Props{Type: "button", Class: "projectui-link-button projectui-workflow-add", Data: map[string]string{"projectui-action": "open-workflow-picker"}, Aria: map[string]string{"haspopup": "dialog"}, Text: wf.LinkWorkflow}))
	}
	children := []ui.Node{html.Div(html.Props{Class: "projectui-section-head projectui-workflows-head"}, head...)}
	if len(model.Workflows) == 0 {
		if !compact {
			children = append(children, html.P(html.Props{Class: "projectui-muted projectui-workflows-empty", Text: wf.NoWorkflows}))
		}
	} else {
		rows := make([]ui.Node, 0, len(model.Workflows))
		for _, link := range model.Workflows {
			rows = append(rows, workflowRow(model, link))
		}
		children = append(children, html.Ul(html.Props{Class: "projectui-workflows"}, rows...))
	}
	children = append(children, fieldStatus(model, "workflows"))
	class := "projectui-page-section projectui-workflows-section"
	if compact {
		class = "projectui-modal-section projectui-workflows-section"
	}
	return html.Section(html.Props{Class: class, Aria: map[string]string{"labelledby": "projectui-workflows-title"}}, children...)
}

func workflowRow(model DetailModel, link WorkflowLink) ui.Node {
	wf := model.Copy.Workflow
	tone := link.StatusTone
	if tone == "" {
		tone = "todo"
	}
	var body []ui.Node
	switch link.State {
	case ReferenceReady, "":
		title := link.Workflow
		if link.JourneyID != "" && link.Person != "" {
			title = link.Workflow + ": " + link.Person
		} else if link.Step != "" {
			title += " · " + link.Step
		}
		main := []ui.Node{html.Span(html.Props{Class: "projectui-workflow-title", Dir: "auto", Text: title})}
		meta := []ui.Node{}
		if link.From != "" || link.To != "" {
			meta = append(meta, html.Span(html.Props{Class: "projectui-workflow-change", Dir: "ltr", Text: strings.TrimSpace(link.From + " → " + link.To)}))
		} else if link.Person != "" {
			meta = append(meta, html.Span(html.Props{Class: "projectui-person"}, avatar(link.Person), html.Span(html.Props{Dir: "auto", Text: link.Person})))
		}
		if link.Status != "" {
			meta = append(meta, html.Span(html.Props{Class: "projectui-status-pill", Data: map[string]string{"tone": tone}}, statusGlyph(tone), html.Span(html.Props{Text: link.Status})))
		}
		if link.Due != "" {
			meta = append(meta, dueChip(link.Due, model.Today, model.Copy, tone != "done"))
		}
		main = append(main, html.Span(html.Props{Class: "projectui-workflow-meta"}, meta...))
		body = append(body, html.Span(html.Props{Class: "projectui-flow-icon projectui-workflow-glyph", Aria: map[string]string{"hidden": "true"}}), html.Span(html.Props{Class: "projectui-workflow-main"}, main...))
		if link.Href != "" {
			body = append(body, html.A(html.Props{Href: link.Href, Class: "projectui-button projectui-workflow-open", Text: wf.Open}))
		}
	case ReferenceLoading:
		body = append(body, html.Span(html.Props{Class: "projectui-flow-icon projectui-workflow-glyph", Aria: map[string]string{"hidden": "true"}}), html.Span(html.Props{Class: "projectui-muted", Text: wf.Loading}))
	default:
		label := wf.Restricted
		if link.State == ReferenceUnavailable {
			label = wf.Unavailable
		}
		body = append(body, html.Span(html.Props{Class: "projectui-lock-icon projectui-workflow-glyph", Aria: map[string]string{"hidden": "true"}}), html.Span(html.Props{Class: "projectui-muted", Text: label}))
	}
	if model.CanEdit && link.LinkID != "" {
		data := fieldControlData(model, "unlink-workflow")
		data["link-id"] = link.LinkID
		body = append(body, html.Button(html.Props{Type: "button", Class: "projectui-workflow-unlink", Title: wf.UnlinkFrom, Data: data, Aria: map[string]string{"label": wf.Unlink}}, html.Span(html.Props{Aria: map[string]string{"hidden": "true"}, Text: "×"})))
	}
	return html.Li(html.Props{Class: "projectui-workflow", Data: map[string]string{"state": string(link.State), "work-item-id": link.WorkItemID, "journey-id": link.JourneyID}}, body...)
}

// WorkflowPicker is the "Link workflow" dialog: work items the viewer can
// see, grouped by workflow, open ones first. The host fills the search and
// status filters and performs the link.
func WorkflowPicker(model DetailModel) ui.Node {
	if !model.CanEdit {
		return nil
	}
	wf := model.Copy.Workflow
	groups := map[string][]WorkflowOption{}
	order := []string{}
	for _, option := range model.WorkflowOptions {
		group := option.Group
		if group == "" {
			group = option.Workflow
		}
		if _, seen := groups[group]; !seen {
			order = append(order, group)
		}
		groups[group] = append(groups[group], option)
	}
	list := []ui.Node{}
	for _, name := range order {
		rows := []ui.Node{}
		for _, option := range groups[name] {
			data := fieldControlData(model, "link-workflow")
			data["work-item-id"] = option.WorkItemID
			data["journey-id"] = option.JourneyID
			data["search"] = strings.ToLower(strings.Join([]string{option.Workflow, option.Step, option.Person, option.Status, option.From, option.To}, " "))
			data["open"] = boolString(option.Open)
			tone := option.StatusTone
			if tone == "" {
				tone = "todo"
			}
			title := option.Step
			if option.JourneyID != "" {
				title = option.Workflow + ": " + option.Person
			} else if option.Person != "" {
				title = option.Person + " · " + option.Step
			}
			meta := []ui.Node{html.Span(html.Props{Class: "projectui-status-pill", Data: map[string]string{"tone": tone}}, statusGlyph(tone), html.Span(html.Props{Text: option.Status}))}
			if option.From != "" || option.To != "" {
				meta = append([]ui.Node{html.Span(html.Props{Class: "projectui-workflow-change", Dir: "ltr", Text: strings.TrimSpace(option.From + " → " + option.To)})}, meta...)
			}
			if option.Due != "" {
				meta = append(meta, dueChip(option.Due, model.Today, model.Copy, option.Open))
			}
			if option.Linked {
				meta = append(meta, html.Span(html.Props{Class: "projectui-chip", Text: wf.Linked}))
			}
			rows = append(rows, html.Li(html.Props{Class: "projectui-picker-item", Data: map[string]string{"search": data["search"], "open": data["open"]}},
				html.Button(html.Props{Type: "button", Class: "projectui-picker-row", Disabled: option.Linked, Data: data},
					html.Span(html.Props{Class: "projectui-workflow-title", Dir: "auto", Text: title}),
					html.Span(html.Props{Class: "projectui-workflow-meta"}, meta...))))
		}
		list = append(list, html.Li(html.Props{Class: "projectui-picker-group"},
			html.H3(html.Props{Class: "projectui-picker-group-title"}, html.Span(html.Props{Class: "projectui-flow-icon", Aria: map[string]string{"hidden": "true"}}), html.Span(html.Props{Text: name})),
			html.Ul(html.Props{Class: "projectui-picker-list"}, rows...)))
	}
	body := []ui.Node{
		html.Div(html.Props{Class: "projectui-share-head"},
			html.H2(html.Props{ID: "projectui-workflow-picker-title", Text: wf.PickerTitle}),
			html.Button(html.Props{Type: "button", Class: "projectui-modal-close", Data: map[string]string{"projectui-action": "close-workflow-picker"}, Aria: map[string]string{"label": model.Copy.Close}}, html.Span(html.Props{Class: "projectui-modal-close-icon", Aria: map[string]string{"hidden": "true"}}))),
		html.Div(html.Props{Class: "projectui-picker-tools"},
			html.Div(html.Props{Class: "projectui-filter-search"},
				html.Label(html.Props{For: "projectui-workflow-search", Class: "projectui-sr", Text: wf.PickerSearch}),
				html.Span(html.Props{Class: "projectui-filter-search-icon", Aria: map[string]string{"hidden": "true"}}),
				html.Input(html.Props{ID: "projectui-workflow-search", Type: "search", Placeholder: wf.PickerSearch, AutoComplete: "off"})),
			html.Div(html.Props{Class: "projectui-thread-tabs projectui-picker-status", Role: "radiogroup", Aria: map[string]string{"label": model.Copy.StatusField}},
				html.Button(html.Props{Type: "button", Class: "projectui-thread-tab", Role: "radio", Data: map[string]string{"projectui-action": "workflow-status", "status": "open"}, Aria: map[string]string{"checked": "true"}, Text: wf.PickerOpenOnly}),
				html.Button(html.Props{Type: "button", Class: "projectui-thread-tab", Role: "radio", Data: map[string]string{"projectui-action": "workflow-status", "status": "all"}, Aria: map[string]string{"checked": "false"}, Text: wf.PickerAll}))),
	}
	switch {
	case model.WorkflowOptionsLoading && len(model.WorkflowOptions) == 0:
		body = append(body, html.P(html.Props{Class: "projectui-muted projectui-picker-empty", Role: "status", Text: wf.PickerLoading}))
	case len(model.WorkflowOptions) == 0:
		body = append(body, html.P(html.Props{Class: "projectui-muted projectui-picker-empty", Text: wf.PickerEmpty}))
	default:
		body = append(body, html.Ul(html.Props{Class: "projectui-picker-groups"}, list...),
			html.P(html.Props{Class: "projectui-muted projectui-picker-nomatch", Hidden: true, Text: wf.PickerEmpty}))
	}
	return html.Tag("dialog", html.Props{ID: "projectui-workflow-picker", Class: "projectui-share-dialog projectui-workflow-picker", Aria: map[string]string{"labelledby": "projectui-workflow-picker-title"}},
		html.Div(html.Props{Class: "projectui-share-form projectui-picker-body"}, body...))
}

// boardWorkflowsPanel is the board toolbar's Workflows button and popover:
// every journey linked from the project's tickets, with its stage and a way
// to open it.
func boardWorkflowsPanel(model Model) ui.Node {
	wf := model.Copy.Workflow
	detail := DetailModel{Copy: model.Copy, Today: model.Today}
	rows := make([]ui.Node, 0, len(model.Workflows))
	for _, link := range model.Workflows {
		link.LinkID = ""
		rows = append(rows, workflowRow(detail, link))
	}
	return html.Details(html.Props{Class: "projectui-share projectui-board-workflows"},
		html.Summary(html.Props{Class: "projectui-share-trigger", Aria: map[string]string{"haspopup": "dialog"}},
			html.Span(html.Props{Class: "projectui-flow-icon", Aria: map[string]string{"hidden": "true"}}),
			html.Span(html.Props{Text: wf.BoardTitle}),
			html.Span(html.Props{Class: "projectui-filter-count", Text: model.Copy.FormatNumber(len(model.Workflows))})),
		html.Div(html.Props{Class: "projectui-card-menu-panel projectui-board-workflows-panel", Role: "dialog", Aria: map[string]string{"label": wf.BoardTitle}},
			html.P(html.Props{Class: "projectui-menu-title", Text: wf.BoardHint}),
			html.Ul(html.Props{Class: "projectui-workflows"}, rows...)),
	)
}
