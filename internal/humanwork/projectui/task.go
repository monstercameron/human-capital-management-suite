package projectui

import (
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/html"
	"github.com/monstercameron/GoWebComponents/v5/ui"
)

// The task modal and task page share one set of field controls. Every
// control is a native form element carrying the task identity and the
// revisions its write must expect; the host's document listeners perform the
// write and re-render, so this package stays presentation-only.

// TaskModalContent renders the body of the ticket modal: key, title and
// status, the description, quick edits for status, assignee and priority,
// the remaining facts, the latest comments and a link to the task page. The
// host owns the <dialog> element around it.
func TaskModalContent(model DetailModel, titleID string) ui.Node {
	model = normalizedDetail(model)
	copy := model.Copy
	head := []ui.Node{
		html.Div(html.Props{Class: "projectui-modal-kicker"},
			html.Span(html.Props{Class: "projectui-task-key", Text: TaskKey(model.TaskID)}),
		),
		html.H2(html.Props{ID: titleID, Class: "projectui-modal-title", Dir: "auto", Text: model.Title}),
	}
	if model.CanEdit {
		// The title edits in place, as on the task page: the heading stays
		// for the dialog's name and the outline, the field looks like it.
		head[1] = html.H2(html.Props{ID: titleID, Class: "projectui-sr", Text: model.Title})
		head = append(head, titleForm(model, "projectui-modal-title-editor"))
	}
	closeButton := html.Button(html.Props{Type: "button", Class: "projectui-modal-close", Data: map[string]string{"projectui-close": "true"}, Aria: map[string]string{"label": copy.Close}}, html.Span(html.Props{Class: "projectui-modal-close-icon", Aria: map[string]string{"hidden": "true"}}))
	description := html.P(html.Props{Class: "projectui-modal-description", Dir: "auto", Text: model.Description})
	if strings.TrimSpace(model.Description) == "" {
		description = html.P(html.Props{Class: "projectui-modal-description projectui-muted", Text: copy.NoDescription})
	}
	main := []ui.Node{
		html.Div(html.Props{Class: "projectui-modal-section"}, html.H3(html.Props{Text: copy.Description}), description),
	}
	latest := model.Comments
	if len(latest) > 3 {
		latest = latest[len(latest)-3:]
	}
	commentsHead := []ui.Node{html.H3(html.Props{Text: copy.LatestComments})}
	if model.PageHref != "" && model.CommentTotal > 0 {
		commentsHead = append(commentsHead, html.A(html.Props{Href: model.PageHref, Class: "projectui-inline-link", Text: copy.AllComments + " (" + copy.FormatNumber(model.CommentTotal) + ")"}))
	}
	comments := []ui.Node{html.Div(html.Props{Class: "projectui-section-head"}, commentsHead...)}
	if len(latest) == 0 {
		comments = append(comments, html.P(html.Props{Class: "projectui-muted projectui-empty-note", Text: copy.NoCommentsHint}))
	} else {
		items := make([]ui.Node, 0, len(latest))
		for _, comment := range latest {
			items = append(items, commentItem(model, comment, false))
		}
		comments = append(comments, html.Ul(html.Props{Class: "projectui-comments"}, items...))
	}
	if len(model.Workflows) > 0 || model.CanEdit {
		main = append(main, workflowSection(model, true))
	}
	main = append(main, html.Div(html.Props{Class: "projectui-modal-section"}, comments...))
	side := html.Div(html.Props{Class: "projectui-modal-side"},
		html.H3(html.Props{Class: "projectui-side-title", Text: copy.Details}),
		detailFieldList(model, "projectui-modal", true),
	)
	tools := []ui.Node{}
	if model.PageHref != "" {
		tools = append(tools, html.A(html.Props{Href: model.PageHref, Class: "projectui-button projectui-modal-open", Data: map[string]string{"projectui-open-page": "true"}}, html.Span(html.Props{Class: "projectui-menu-icon projectui-open-icon", Data: map[string]string{"icon": "open"}, Aria: map[string]string{"hidden": "true"}}), html.Span(html.Props{Text: copy.OpenTaskPage})))
	}
	if share := ShareMenu(copy, model.Share); share != nil {
		tools = append([]ui.Node{share}, tools...)
	}
	tools = append(tools, closeButton)
	return html.Div(html.Props{Class: "projectui-modal", Data: taskData(model)},
		html.Header(html.Props{Class: "projectui-modal-head"}, html.Div(html.Props{Class: "projectui-modal-heading"}, head...), html.Div(html.Props{Class: "projectui-modal-tools"}, tools...)),
		html.Div(html.Props{Class: "projectui-modal-body"},
			html.Div(html.Props{Class: "projectui-modal-main"}, main...),
			side,
		),
		shareToast("projectui-toast-modal", copy.Board),
		workflowPickerOrEmpty(model),
	)
}

// workflowPickerOrEmpty keeps the modal's children fixed in number.
func workflowPickerOrEmpty(model DetailModel) ui.Node {
	if picker := WorkflowPicker(model); picker != nil {
		return picker
	}
	return html.Span(html.Props{Hidden: true})
}

// TaskPage renders one task as a full page: breadcrumb, an editable title and
// description, the comment thread and activity in the main column, and the
// Details fields a project manager fills out in the sidebar.
func TaskPage(model DetailModel) ui.Node {
	model = normalizedDetail(model)
	copy := model.Copy
	crumbs := []ui.Node{}
	if model.ProjectsHref != "" {
		crumbs = append(crumbs, html.Li(html.Props{}, html.A(html.Props{Href: model.ProjectsHref, Dir: "auto", Text: copy.Projects})))
	}
	if model.ProjectName != "" {
		if model.BoardHref != "" {
			crumbs = append(crumbs, html.Li(html.Props{Class: "projectui-crumb-back"}, html.A(html.Props{Href: model.BoardHref, Dir: "auto", Text: model.ProjectName})))
		} else {
			crumbs = append(crumbs, html.Li(html.Props{}, html.Span(html.Props{Dir: "auto", Text: model.ProjectName})))
		}
	}
	// The key, not the title, ends the trail: the title is the heading
	// right below, and the key matches what the modal shows.
	current := TaskKey(model.TaskID)
	if current == "" {
		current = model.Title
	}
	crumbs = append(crumbs, html.Li(html.Props{Class: "projectui-crumb-key", Aria: map[string]string{"current": "page"}}, html.Span(html.Props{Dir: "auto", Text: current})))

	board := copy.Board
	f := fieldRows{model: model, prefix: "projectui-page"}
	status, _ := f.statusControl()
	tools := []ui.Node{html.Div(html.Props{Class: "projectui-fields projectui-taskpage-status", Data: map[string]string{"field": "status", "state": model.FieldStates["status"]}}, status, fieldStatus(model, "status"))}
	if share := ShareMenu(copy, model.Share); share != nil {
		tools = append(tools, share)
	}
	head := html.Header(html.Props{Class: "projectui-taskpage-head"},
		html.Div(html.Props{Class: "projectui-taskpage-headline"},
			html.Span(html.Props{Class: "projectui-task-key", Text: TaskKey(model.TaskID)}),
			titleEditor(model)),
		html.Div(html.Props{Class: "projectui-taskpage-tools"}, tools...),
	)

	details := []ui.Node{f.labels(true), f.typeRow(), f.lane()}
	details = append(details, f.facts()...)
	main := []ui.Node{
		descriptionEditor(model),
		html.Section(html.Props{Class: "projectui-page-section projectui-details-block", Aria: map[string]string{"labelledby": "projectui-details-title"}},
			html.H2(html.Props{ID: "projectui-details-title", Text: copy.Details}), fieldList(details...)),
		workflowSection(model, false),
	}
	// Comments and Activity share one area; two radios switch the panel
	// without script, and each keeps its own heading for the outline.
	activity := []ui.Node{html.H2(html.Props{ID: "projectui-activity-title", Class: "projectui-sr", Text: copy.Activity}), activityList(model.Activity, copy)}
	if model.ActivityMoreHref != "" {
		activity = append(activity, html.A(html.Props{Href: model.ActivityMoreHref, Class: "projectui-more", Text: copy.MoreActivity}))
	}
	commentsLabel := []ui.Node{html.Span(html.Props{Text: copy.Comments})}
	if total := len(model.Comments); total > 0 {
		commentsLabel = append(commentsLabel, html.Span(html.Props{Class: "projectui-count", Text: copy.FormatNumber(total)}))
	}
	main = append(main, html.Section(html.Props{Class: "projectui-page-section projectui-thread"},
		html.Div(html.Props{Class: "projectui-thread-tabs", Role: "radiogroup", Aria: map[string]string{"label": copy.Comments + " / " + copy.Activity}},
			html.Input(html.Props{ID: "projectui-thread-comments", Type: "radio", Name: "projectui-thread", Class: "projectui-thread-radio", Checked: true, Raw: map[string]any{"value": "comments"}}),
			html.Label(html.Props{For: "projectui-thread-comments", Class: "projectui-thread-tab"}, commentsLabel...),
			html.Input(html.Props{ID: "projectui-thread-activity", Type: "radio", Name: "projectui-thread", Class: "projectui-thread-radio", Raw: map[string]any{"value": "activity"}}),
			html.Label(html.Props{For: "projectui-thread-activity", Class: "projectui-thread-tab", Text: copy.Activity}),
		),
		html.Div(html.Props{Class: "projectui-thread-panel", Data: map[string]string{"panel": "comments"}, Aria: map[string]string{"labelledby": "projectui-comments-title"}}, commentThread(model)...),
		html.Div(html.Props{Class: "projectui-thread-panel", Data: map[string]string{"panel": "activity"}, Aria: map[string]string{"labelledby": "projectui-activity-title"}}, activity...),
	))

	card := func(class, title string, children ...ui.Node) ui.Node {
		return html.Div(html.Props{Class: "projectui-side-card " + class},
			html.Div(html.Props{Class: "projectui-side-card-head"}, html.H2(html.Props{Class: "projectui-side-title", Text: title})),
			html.Div(html.Props{}, children...))
	}
	sideCards := []ui.Node{
		card("projectui-side-people", board.People, fieldList(f.assignee(), f.reporter())),
		card("projectui-side-planning", board.Planning, fieldList(f.priority(), f.start(), f.due(), f.points())),
	}
	if dates := f.dates(); len(dates) > 0 {
		sideCards = append(sideCards, card("projectui-side-dates", board.Dates, dates...))
	}
	if len(model.Links) > 0 {
		sideCards = append(sideCards, card("projectui-side-links", copy.LinkedItems, references(model.Links, copy)))
	}
	children := []ui.Node{
		html.Nav(html.Props{Class: "projectui-breadcrumb", Aria: map[string]string{"label": copy.Projects}}, html.Ol(html.Props{}, crumbs...)),
		head,
		html.Div(html.Props{Class: "projectui-taskpage-grid"},
			html.Div(html.Props{Class: "projectui-taskpage-main"}, main...),
			html.Aside(html.Props{Class: "projectui-taskpage-side", Aria: map[string]string{"label": copy.Details}}, sideCards...),
		),
	}
	if dialog := ShareDialog(copy, model.Share); dialog != nil {
		children = append(children, dialog)
	}
	if picker := WorkflowPicker(model); picker != nil {
		children = append(children, picker)
	}
	return html.Section(html.Props{Class: "projectui-taskpage", Role: "region", Aria: map[string]string{"label": copy.TaskDetails}, Data: taskData(model)}, children...)
}

// TaskKey is the short, stable display key for a task: the first eight
// characters of its opaque ID. It is a label, never a selector.
func TaskKey(taskID string) string {
	key := strings.ToUpper(strings.ReplaceAll(taskID, "-", ""))
	if len(key) > 8 {
		key = key[:8]
	}
	if key == "" {
		return ""
	}
	return "T-" + key
}

func normalizedDetail(model DetailModel) DetailModel {
	model.Copy = localizedCopy(model.Copy)
	model.Copy.Board = localizedBoardCopy(model.Copy.Board, model.Copy.FormatNumber)
	model.Copy.Workflow = localizedWorkflowCopy(model.Copy.Workflow)
	if len(model.Comments) > detailEntryLimit {
		model.Comments = model.Comments[len(model.Comments)-detailEntryLimit:]
	}
	if len(model.Activity) > detailEntryLimit {
		model.Activity = model.Activity[:detailEntryLimit]
	}
	if len(model.Links) > detailEntryLimit {
		model.Links = model.Links[:detailEntryLimit]
	}
	if model.Today.IsZero() {
		model.Today = time.Now()
	}
	return model
}

func taskData(model DetailModel) map[string]string {
	return map[string]string{
		"task-id": model.TaskID, "project-id": model.ProjectID,
		"task-revision":     strconv.FormatUint(model.TaskRevision, 10),
		"workflow-revision": strconv.FormatUint(model.WorkflowRevision, 10),
	}
}

// fieldControlData is the identity every write control carries.
func fieldControlData(model DetailModel, action string) map[string]string {
	data := taskData(model)
	data["projectui-action"] = action
	return data
}

func fieldStatus(model DetailModel, field string) ui.Node {
	state := model.FieldStates[field]
	text := ""
	switch state {
	case "saving":
		text = model.Copy.Saving
	case "saved":
		text = model.Copy.Saved
	case "error":
		text = model.FieldErrors[field]
		if text == "" {
			text = model.Copy.SaveFailed
		}
	}
	role := "status"
	if state == "error" {
		role = "alert"
	}
	return html.Span(html.Props{Class: "projectui-field-status", Role: role, Data: map[string]string{"state": state}, Text: text})
}

// fieldRows builds the Details rows shared by the modal and the task page.
// Every write control carries the task identity and revisions; the host's
// document listeners perform the write.
type fieldRows struct {
	model  DetailModel
	prefix string
}

func (f fieldRows) row(field, label, controlID string, control ui.Node) ui.Node {
	labelNode := html.Span(html.Props{Class: "projectui-field-label", Text: label})
	if controlID != "" {
		labelNode = html.Label(html.Props{For: controlID, Class: "projectui-field-label", Text: label})
	}
	return html.Div(html.Props{Class: "projectui-field", Data: map[string]string{"field": field, "state": f.model.FieldStates[field]}},
		labelNode,
		html.Div(html.Props{Class: "projectui-field-value"}, control, fieldStatus(f.model, field)),
	)
}

func (f fieldRows) text(value string) ui.Node {
	return html.Span(html.Props{Class: "projectui-field-text", Text: value})
}

// statusControl is the status select, or the locked status with its reason.
func (f fieldRows) statusControl() (ui.Node, string) {
	model, copy := f.model, f.model.Copy
	if model.CanMoveStatus && len(model.StatusOptions) > 1 {
		id := f.prefix + "-status"
		options := make([]ui.Node, 0, len(model.StatusOptions))
		for _, status := range model.StatusOptions {
			options = append(options, html.Option(html.Props{Value: status.ID, Selected: status.ID == model.StatusID, Text: status.Label}))
		}
		props := html.Props{ID: id, Name: "status", Class: "projectui-field-select projectui-status-menu", Data: fieldControlData(model, "move-status"), Disabled: model.FieldStates["status"] == "saving", Aria: map[string]string{"label": copy.StatusFor + model.Title}}
		return html.Div(html.Props{Class: "projectui-select-wrap projectui-status-wrap", Data: map[string]string{"tone": toneOf(model)}}, statusGlyph(toneOf(model)), html.Select(props, options...)), id
	}
	return html.Span(html.Props{Class: "projectui-status-locked-wrap"}, lockedStatus(f.prefix+"-status-lock", toneOf(model), model.Status, copy)...), ""
}

func (f fieldRows) status() ui.Node {
	control, id := f.statusControl()
	return f.row("status", f.model.Copy.StatusField, id, control)
}

func (f fieldRows) assignee() ui.Node {
	model, copy := f.model, f.model.Copy
	assigneeName := model.Assignee
	if assigneeName == "" && model.AssigneeID != "" {
		assigneeName = copy.Unassigned
	}
	if model.CanEdit {
		id := f.prefix + "-assignee"
		options := []ui.Node{html.Option(html.Props{Raw: map[string]any{"value": ""}, Selected: model.AssigneeID == "", Text: copy.Unassigned})}
		found := model.AssigneeID == ""
		for _, person := range model.Members {
			if person.ID == "" || person.Name == "" {
				continue
			}
			found = found || person.ID == model.AssigneeID
			options = append(options, html.Option(html.Props{Value: person.ID, Selected: person.ID == model.AssigneeID, Text: person.Name}))
		}
		if !found && model.Assignee != "" {
			options = append(options, html.Option(html.Props{Value: model.AssigneeID, Selected: true, Text: model.Assignee}))
		}
		lead := avatarWithPhoto(assigneeName, model.AssigneePhoto)
		if model.AssigneeID == "" {
			lead = html.Span(html.Props{Class: "projectui-avatar projectui-avatar-empty", Aria: map[string]string{"hidden": "true"}})
		}
		return f.row("assignee", copy.AssigneeField, id, html.Div(html.Props{Class: "projectui-select-wrap projectui-select-person"}, lead,
			html.Select(html.Props{ID: id, Name: "assignee", Class: "projectui-field-select", Data: fieldControlData(model, "set-assignee"), Disabled: model.FieldStates["assignee"] == "saving"}, options...)))
	}
	if model.Assignee != "" {
		return f.row("assignee", copy.AssigneeField, "", html.Span(html.Props{Class: "projectui-person"}, avatarWithPhoto(model.Assignee, model.AssigneePhoto), html.Span(html.Props{Text: model.Assignee})))
	}
	return f.row("assignee", copy.AssigneeField, "", f.text(copy.Unassigned))
}

func (f fieldRows) reporter() ui.Node {
	model := f.model
	if model.Reporter == "" {
		// Tasks created before reporters were recorded keep the row, so
		// People has one shape.
		return f.row("reporter", model.Copy.Board.Reporter, "", html.Span(html.Props{Class: "projectui-field-text projectui-muted", Text: model.Copy.Board.ReporterUnknown}))
	}
	return f.row("reporter", model.Copy.Board.Reporter, "", html.Span(html.Props{Class: "projectui-person"}, avatarWithPhoto(model.Reporter, model.ReporterPhoto), html.Span(html.Props{Text: model.Reporter})))
}

func (f fieldRows) priority() ui.Node {
	model, copy := f.model, f.model.Copy
	if model.CanEdit && len(model.PriorityOptions) > 0 {
		id := f.prefix + "-priority"
		options := make([]ui.Node, 0, len(model.PriorityOptions))
		for _, option := range model.PriorityOptions {
			options = append(options, html.Option(html.Props{Value: option.ID, Selected: option.ID == model.PriorityID, Text: option.Label}))
		}
		return f.row("priority", copy.PriorityField, id, html.Div(html.Props{Class: "projectui-select-wrap"}, priorityIndicatorBars(model.PriorityID),
			html.Select(html.Props{ID: id, Name: "priority", Class: "projectui-field-select", Data: fieldControlData(model, "set-priority"), Disabled: model.FieldStates["priority"] == "saving"}, options...)))
	}
	if model.Priority != "" {
		return f.row("priority", copy.PriorityField, "", priorityIndicator(model.Priority, copy))
	}
	return nil
}

// dateRow is a date field shown as text over a native date input.
func (f fieldRows) dateRow(field, label, action, value, empty string, chip bool) ui.Node {
	model := f.model
	if model.CanEdit {
		id := f.prefix + "-" + field
		props := html.Props{ID: id, Name: field, Type: "date", Class: "projectui-field-date", Data: fieldControlData(model, action), Disabled: model.FieldStates[field] == "saving", Raw: map[string]any{"value": value}}
		shown := html.Span(html.Props{Class: "projectui-date-empty", Text: empty})
		if value != "" {
			if chip {
				shown = dueChip(value, model.Today, model.Copy, toneOf(model) != "done")
			} else {
				shown = html.Span(html.Props{Class: "projectui-date-text", Text: formatCivilDate(value, model.Copy)})
			}
		}
		return f.row(field, label, id, html.Span(html.Props{Class: "projectui-date-field"}, shown, html.Span(html.Props{Class: "projectui-date-chevron", Aria: map[string]string{"hidden": "true"}}), html.Input(props)))
	}
	if value == "" {
		return f.row(field, label, "", f.text("—"))
	}
	if chip {
		return f.row(field, label, "", dueChip(value, model.Today, model.Copy, toneOf(model) != "done"))
	}
	return f.row(field, label, "", f.text(formatCivilDate(value, model.Copy)))
}

func (f fieldRows) due() ui.Node {
	return f.dateRow("due", f.model.Copy.DueField, "set-due", f.model.DueDate, f.model.Copy.AddDueDate, true)
}

func (f fieldRows) start() ui.Node {
	board := f.model.Copy.Board
	return f.dateRow("start", board.StartDate, "set-start", f.model.StartDate, board.AddStartDate, false)
}

func (f fieldRows) points() ui.Node {
	model, board := f.model, f.model.Copy.Board
	if model.CanEdit {
		id := f.prefix + "-points"
		value := ""
		if model.StoryPoints > 0 {
			value = strconv.Itoa(model.StoryPoints)
		}
		return f.row("points", board.StoryPoints, id, html.Input(html.Props{ID: id, Name: "points", Type: "number", Class: "projectui-field-number", Placeholder: board.AddEstimate, Data: fieldControlData(model, "set-points"), Disabled: model.FieldStates["points"] == "saving", Raw: map[string]any{"value": value, "min": "0", "max": "1000", "step": "1", "inputmode": "numeric"}}))
	}
	if model.StoryPoints > 0 {
		return f.row("points", board.StoryPoints, "", f.text(model.Copy.FormatNumber(model.StoryPoints)))
	}
	return nil
}

// labels is the tokenized label editor: chips with a remove button, and a
// field that adds on Enter or comma, suggesting labels the project uses.
func (f fieldRows) labels(editable bool) ui.Node {
	model, board := f.model, f.model.Copy.Board
	chips := []ui.Node{}
	for _, label := range model.Labels {
		chip := []ui.Node{html.Span(html.Props{Dir: "auto", Text: label})}
		if editable && model.CanEdit {
			data := fieldControlData(model, "remove-label")
			data["label"] = label
			chip = append(chip, html.Button(html.Props{Type: "button", Class: "projectui-label-remove", Data: data, Aria: map[string]string{"label": board.RemoveLabel + ": " + label}}, html.Span(html.Props{Aria: map[string]string{"hidden": "true"}, Text: "×"})))
		}
		chips = append(chips, html.Span(html.Props{Class: "projectui-label", Data: map[string]string{"hue": strconv.Itoa(hueBucket(label))}}, chip...))
	}
	if !editable || !model.CanEdit {
		if len(chips) == 0 {
			return f.row("labels", board.Labels, "", f.text(board.NoLabels))
		}
		return f.row("labels", board.Labels, "", html.Span(html.Props{Class: "projectui-labels"}, chips...))
	}
	id := f.prefix + "-label-input"
	listID := f.prefix + "-label-suggestions"
	suggestions := []ui.Node{}
	for _, label := range model.LabelSuggestions {
		suggestions = append(suggestions, html.Option(html.Props{Value: label}))
	}
	data := fieldControlData(model, "add-label")
	data["labels"] = strings.Join(model.Labels, "\n")
	editor := html.Div(html.Props{Class: "projectui-label-editor", Data: data},
		html.Span(html.Props{Class: "projectui-labels"}, chips...),
		html.Input(html.Props{ID: id, Name: "label", Type: "text", Class: "projectui-label-input", Placeholder: board.AddLabel, AutoComplete: "off", MaxLength: 40, Disabled: model.FieldStates["labels"] == "saving", Raw: map[string]any{"list": listID, "enterkeyhint": "done"}}),
		html.Tag("datalist", html.Props{ID: listID}, suggestions...),
	)
	return f.row("labels", board.Labels, id, editor)
}

func (f fieldRows) typeRow() ui.Node {
	if f.model.Type == "" {
		return nil
	}
	return f.row("type", f.model.Copy.TypeField, "", html.Span(html.Props{Class: "projectui-chip projectui-type", Text: f.model.Type}))
}

func (f fieldRows) lane() ui.Node {
	model := f.model
	if model.LaneFieldName == "" || len(model.LaneOptions) == 0 || !model.CanEdit {
		return nil
	}
	id := f.prefix + "-lane"
	options := make([]ui.Node, 0, len(model.LaneOptions))
	for _, lane := range model.LaneOptions {
		options = append(options, html.Option(html.Props{Value: lane.ID, Selected: lane.ID == model.LaneID, Text: lane.Label}))
	}
	return f.row("lane", model.LaneFieldName, id, html.Select(html.Props{ID: id, Name: "lane", Class: "projectui-field-select projectui-lane-menu", Data: fieldControlData(model, "move-lane"), Disabled: model.FieldStates["lane"] == "saving"}, options...))
}

func (f fieldRows) facts() []ui.Node {
	out := []ui.Node{}
	for _, fact := range f.model.Fields {
		if fact.Label != "" && fact.Value != "" {
			out = append(out, f.row("", fact.Label, "", f.text(fact.Value)))
		}
	}
	return out
}

func (f fieldRows) dates() []ui.Node {
	model, board := f.model, f.model.Copy.Board
	out := []ui.Node{}
	line := func(field, text, stamp string) {
		if text == "" {
			return
		}
		out = append(out, html.P(html.Props{Class: "projectui-date-line", Data: map[string]string{"field": field}}, html.Time(html.Props{Title: stamp, Raw: map[string]any{"dateTime": stamp}, Text: text})))
	}
	if model.CreatedText != "" {
		line("created", board.Created(model.CreatedText), model.CreatedAt)
	}
	if model.UpdatedText != "" {
		line("updated", board.Updated(model.UpdatedText), model.UpdatedAt)
	}
	return out
}

func fieldList(rows ...ui.Node) ui.Node {
	kept := make([]ui.Node, 0, len(rows))
	for _, row := range rows {
		if row != nil {
			kept = append(kept, row)
		}
	}
	return html.Div(html.Props{Class: "projectui-fields"}, kept...)
}

// detailFieldList renders the modal's compact Details: the quick edits,
// the planning fields and the read-only facts.
func detailFieldList(model DetailModel, prefix string, quick bool) ui.Node {
	f := fieldRows{model: model, prefix: prefix}
	rows := []ui.Node{f.status(), f.assignee(), f.priority(), f.due(), f.start(), f.points(), f.labels(false), f.typeRow(), f.lane()}
	return fieldList(append(rows, f.facts()...)...)
}

// formatCivilDate renders YYYY-MM-DD with the locale's date format.
func formatCivilDate(raw string, copy Copy) string {
	value := raw
	if len(value) > 10 {
		value = value[:10]
	}
	date, err := time.Parse("2006-01-02", value)
	if err != nil {
		return raw
	}
	if copy.FormatDate != nil {
		return copy.FormatDate(date)
	}
	return date.Format("Jan 2, 2006")
}

func toneOf(model DetailModel) string {
	tone := classifyTone(model.StatusID + " " + model.Status)
	if tone == "" {
		return "todo"
	}
	return tone
}

func priorityIndicatorBars(priorityID string) ui.Node {
	return html.Span(html.Props{Class: "projectui-priority", Data: map[string]string{"level": priorityLevel(priorityID)}},
		html.Span(html.Props{Class: "projectui-priority-bars", Aria: map[string]string{"hidden": "true"}}, html.Span(html.Props{}), html.Span(html.Props{}), html.Span(html.Props{})))
}

func titleEditor(model DetailModel) ui.Node {
	if !model.CanEdit {
		return html.H1(html.Props{Class: "projectui-page-title", Text: model.Title})
	}
	// The heading keeps the page outline; the input beside it is the editor,
	// styled as the title until it is focused.
	return titleForm(model, "", html.H1(html.Props{Class: "projectui-sr", Text: model.Title}))
}

// titleForm is the in-place title editor: Enter saves, Escape restores.
func titleForm(model DetailModel, class string, lead ...ui.Node) ui.Node {
	copy := model.Copy
	children := append([]ui.Node{}, lead...)
	children = append(children,
		html.Textarea(html.Props{Name: "title", Class: "projectui-title-input", Data: map[string]string{"saved": model.Title}, Rows: 1, Required: true, MaxLength: 200, Aria: map[string]string{"label": copy.TaskField}, Raw: map[string]any{"value": model.Title}, Disabled: model.FieldStates["title"] == "saving"}),
		html.Div(html.Props{Class: "projectui-editor-actions"},
			html.Button(html.Props{Type: "submit", Class: "projectui-button projectui-button-primary", Text: copy.Save}),
			html.Button(html.Props{Type: "reset", Class: "projectui-button", Text: copy.Cancel}),
			fieldStatus(model, "title"),
		),
	)
	return html.Form(html.Props{Class: strings.TrimSpace("projectui-inline-editor projectui-title-editor " + class), Data: fieldControlData(model, "edit-title")}, children...)
}

func descriptionEditor(model DetailModel) ui.Node {
	copy := model.Copy
	body := []ui.Node{html.H2(html.Props{ID: "projectui-description-title", Text: copy.Description})}
	if model.CanEdit {
		body = []ui.Node{html.Div(html.Props{Class: "projectui-section-head"}, body[0],
			html.Label(html.Props{For: "projectui-description-input", Class: "projectui-link-button projectui-edit-label", Text: copy.Edit}))}
	}
	if !model.CanEdit {
		text := model.Description
		class := "projectui-description-text"
		if strings.TrimSpace(text) == "" {
			text, class = copy.NoDescription, class+" projectui-muted"
		}
		body = append(body, html.P(html.Props{Class: class, Dir: "auto", Text: text}))
		return html.Section(html.Props{Class: "projectui-page-section projectui-description-section"}, body...)
	}
	body = append(body, html.Form(html.Props{Class: "projectui-inline-editor projectui-description-editor", Data: fieldControlData(model, "edit-description")},
		html.Textarea(html.Props{ID: "projectui-description-input", Name: "description", Class: "projectui-description-input", Dir: "auto", Rows: 3, MaxLength: 8000, Placeholder: copy.AddDescription, Aria: map[string]string{"labelledby": "projectui-description-title"}, Raw: map[string]any{"value": model.Description}, Disabled: model.FieldStates["description"] == "saving"}),
		html.Div(html.Props{Class: "projectui-editor-actions"},
			html.Button(html.Props{Type: "submit", Class: "projectui-button projectui-button-primary", Text: copy.Save}),
			html.Button(html.Props{Type: "reset", Class: "projectui-button", Text: copy.Cancel}),
			fieldStatus(model, "description"),
		),
	))
	return html.Section(html.Props{Class: "projectui-page-section projectui-description-section"}, body...)
}

func commentThread(model DetailModel) []ui.Node {
	copy := model.Copy
	count := len(model.Comments)
	head := html.Div(html.Props{Class: "projectui-section-head"},
		html.H2(html.Props{ID: "projectui-comments-title", Text: copy.Comments}),
		html.Span(html.Props{Class: "projectui-count", Hidden: count == 0, Text: copy.FormatNumber(count)}),
	)
	out := []ui.Node{head}
	if count == 0 {
		out = append(out, html.Div(html.Props{Class: "projectui-empty-comments"},
			html.Span(html.Props{Class: "projectui-empty-comments-icon", Aria: map[string]string{"hidden": "true"}}),
			html.P(html.Props{Class: "projectui-empty-comments-title", Text: copy.NoComments}),
			html.P(html.Props{Text: copy.NoCommentsHint}),
		))
	} else {
		items := make([]ui.Node, 0, count)
		// Oldest first, as in the modal: a thread reads top to bottom and
		// the composer sits under the latest reply.
		for _, comment := range model.Comments {
			items = append(items, commentItem(model, comment, true))
		}
		out = append(out, html.Ul(html.Props{Class: "projectui-comments projectui-comment-thread"}, items...))
	}
	if model.CanComment {
		out = append(out, html.Form(html.Props{Class: "projectui-comment-composer", Data: fieldControlData(model, "add-comment")},
			html.Label(html.Props{For: "projectui-comment-new", Class: "projectui-sr", Text: copy.AddComment}),
			html.Textarea(html.Props{ID: "projectui-comment-new", Name: "body", Rows: 3, MaxLength: 8000, Required: true, Placeholder: copy.CommentPlaceholder, Disabled: model.FieldStates["comment"] == "saving"}),
			html.Div(html.Props{Class: "projectui-editor-actions"},
				html.Button(html.Props{Type: "submit", Class: "projectui-button projectui-button-primary", Text: copy.AddComment}),
				fieldStatus(model, "comment"),
			),
		))
	}
	return out
}

func commentItem(model DetailModel, comment Comment, editable bool) ui.Node {
	copy := model.Copy
	byline := []ui.Node{html.Strong(html.Props{Text: comment.Author})}
	if comment.Time != "" {
		timeProps := html.Props{Text: comment.Time}
		if comment.DateTime != "" {
			timeProps.Raw = map[string]any{"dateTime": comment.DateTime}
			timeProps.Title = comment.DateTime
		}
		byline = append(byline, html.Time(timeProps))
	}
	if comment.Edited {
		byline = append(byline, html.Span(html.Props{Class: "projectui-comment-edited", Text: copy.Edited}))
	}
	body := []ui.Node{html.Div(html.Props{Class: "projectui-comment-byline"}, byline...), html.P(html.Props{Dir: "auto", Text: comment.Body})}
	if editable && comment.Own && comment.ID != "" && model.CanComment {
		data := fieldControlData(model, "edit-comment")
		data["comment-id"], data["comment-revision"] = comment.ID, strconv.FormatUint(comment.Revision, 10)
		deleteData := fieldControlData(model, "delete-comment")
		deleteData["comment-id"], deleteData["comment-revision"] = comment.ID, strconv.FormatUint(comment.Revision, 10)
		body = append(body, html.Div(html.Props{Class: "projectui-comment-tools"},
			html.Details(html.Props{Class: "projectui-comment-edit"},
				html.Summary(html.Props{Class: "projectui-link-button", Text: copy.Edit}),
				html.Form(html.Props{Class: "projectui-comment-edit-form", Data: data},
					html.Label(html.Props{For: "projectui-comment-edit-" + comment.ID, Class: "projectui-sr", Text: copy.Edit}),
					html.Textarea(html.Props{ID: "projectui-comment-edit-" + comment.ID, Name: "body", Rows: 3, MaxLength: 8000, Required: true, Raw: map[string]any{"value": comment.Body}}),
					html.Div(html.Props{Class: "projectui-editor-actions"}, html.Button(html.Props{Type: "submit", Class: "projectui-button projectui-button-primary", Text: copy.Save})),
				),
			),
			html.Form(html.Props{Class: "projectui-comment-delete", Data: deleteData},
				html.Button(html.Props{Type: "submit", Class: "projectui-link-button projectui-danger-link", Text: copy.Delete}),
			),
		))
	}
	return html.Li(html.Props{Class: "projectui-comment", Data: map[string]string{"comment-id": comment.ID, "own": boolString(comment.Own)}},
		avatarWithPhoto(comment.Author, comment.AuthorPhoto),
		html.Div(html.Props{Class: "projectui-comment-body"}, body...),
	)
}
