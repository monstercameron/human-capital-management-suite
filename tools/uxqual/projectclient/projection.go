package projectclient

import (
	"errors"
	"fmt"
	"math/big"
	"sort"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/projectui"
)

// ProjectionOptions contains route identity and caller-provided authorized
// presentation data. The generated responses themselves are treated only as
// service data; they never supply permission flags or callbacks.
type ProjectionOptions struct {
	ExpectedProjectID string
	ExpectedViewID    string
	ExpectedTaskID    string
	Mode              projectui.ViewMode
	Statuses          []*projectv1.ProjectStatus
	Fields            []*projectv1.ProjectFieldDefinition
	AssigneeNames     map[string]string
	LaneLabels        map[string]string
	UnassignedLabel   string
	PendingTaskIDs    map[string]bool
	Conflicts         map[string]string
	FailedActions     map[string]string
	CanMoveStatus     func(*projectv1.ProjectTask) bool
	CanMoveLane       func(*projectv1.ProjectTask) bool
	TaskHref          func(taskID string) string
	LinkHref          func(*projectv1.TaskLinkReference) string
}

// BoardModel projects a page returned by GetBoardResponse. Expected IDs are
// mandatory so a stale or misrouted response cannot be displayed in another
// project/view route.
func BoardModel(response *projectv1.GetBoardResponse, options ProjectionOptions) (projectui.Model, error) {
	if response == nil || response.GetView() == nil {
		return projectui.Model{}, errors.New("projectclient: board response has no view")
	}
	view := response.GetView()
	if options.ExpectedProjectID == "" || view.GetProjectId() != options.ExpectedProjectID || options.ExpectedViewID == "" || view.GetViewId() != options.ExpectedViewID {
		return projectui.Model{}, errors.New("projectclient: board response route identity mismatch")
	}
	model := projectui.Model{Title: view.GetName(), Mode: options.Mode, WorkflowRevision: response.GetWorkflowRevision()}
	if model.Mode == "" {
		model.Mode = projectui.ViewBoard
	}
	statusLabels := make(map[string]string, len(options.Statuses))
	statusOptions := make(map[string]projectui.Status, len(options.Statuses))
	for _, status := range options.Statuses {
		if status == nil || status.GetStatusId() == "" {
			continue
		}
		label := status.GetName()
		if label == "" {
			label = status.GetStatusId()
		}
		statusLabels[status.GetStatusId()] = label
		statusOptions[status.GetStatusId()] = projectui.Status{ID: status.GetStatusId(), Label: label}
	}
	columns := append([]*projectv1.BoardColumn(nil), view.GetColumns()...)
	sort.SliceStable(columns, func(i, j int) bool {
		if columns[i] == nil {
			return false
		}
		if columns[j] == nil {
			return true
		}
		return columns[i].GetPosition() < columns[j].GetPosition()
	})
	statusColumn := make(map[string]string)
	for _, column := range columns {
		if column == nil || column.GetColumnId() == "" {
			continue
		}
		projectColumn := projectui.Column{ID: column.GetColumnId(), Label: column.GetName()}
		if projectColumn.Label == "" {
			projectColumn.Label = column.GetColumnId()
		}
		for _, statusID := range column.GetStatusIds() {
			if statusID == "" || statusColumn[statusID] != "" {
				continue
			}
			statusColumn[statusID] = column.GetColumnId()
			status, ok := statusOptions[statusID]
			if !ok {
				status = projectui.Status{ID: statusID, Label: statusID}
			}
			projectColumn.Statuses = append(projectColumn.Statuses, status)
		}
		model.Columns = append(model.Columns, projectColumn)
	}
	fieldLabels := make(map[string]string, len(options.Fields))
	for _, field := range options.Fields {
		if field != nil && field.GetFieldId() != "" {
			fieldLabels[field.GetFieldId()] = field.GetName()
		}
	}
	for _, task := range response.GetTasks() {
		if task == nil {
			continue
		}
		if task.GetProjectId() != options.ExpectedProjectID {
			return projectui.Model{}, errors.New("projectclient: task project identity mismatch")
		}
		card := boardCard(task, view, statusColumn, statusLabels, fieldLabels, options)
		card.WorkflowRevision = model.WorkflowRevision
		model.Cards = append(model.Cards, card)
	}
	if len(model.Cards) > 100 {
		model.Cards = model.Cards[:100]
	}
	model.Lanes = buildLanes(view, response.GetTasks(), options)
	return model, nil
}

// TaskDetailModel projects GetTaskResponse and an already authorized link
// result page to the presentation-only task detail model.
func TaskDetailModel(response *projectv1.GetTaskResponse, links *projectv1.ListTaskLinksResponse, options ProjectionOptions) (projectui.DetailModel, error) {
	if response == nil || response.GetTask() == nil {
		return projectui.DetailModel{}, errors.New("projectclient: task response is empty")
	}
	task := response.GetTask()
	if options.ExpectedProjectID == "" || task.GetProjectId() != options.ExpectedProjectID || options.ExpectedTaskID == "" || task.GetTaskId() != options.ExpectedTaskID {
		return projectui.DetailModel{}, errors.New("projectclient: task response route identity mismatch")
	}
	fieldLabels := make(map[string]string, len(options.Fields))
	for _, field := range options.Fields {
		if field != nil && field.GetFieldId() != "" {
			fieldLabels[field.GetFieldId()] = field.GetName()
		}
	}
	model := projectui.DetailModel{Title: task.GetTitle(), Description: task.GetDescription(), DueDate: task.GetDueDate()}
	model.Assignee = options.AssigneeNames[task.GetAssigneeId()]
	model.Status = task.GetStatusId()
	for _, status := range options.Statuses {
		if status != nil && status.GetStatusId() == task.GetStatusId() && status.GetName() != "" {
			model.Status = status.GetName()
			break
		}
	}
	for _, field := range task.GetCustomFields() {
		if field == nil || field.GetFieldId() == "" {
			continue
		}
		label := fieldLabels[field.GetFieldId()]
		value := typedFieldText(field)
		if label != "" && value != "" {
			model.Fields = append(model.Fields, projectui.Fact{Label: label, Value: value})
		}
	}
	if links != nil {
		for _, link := range links.GetLinks() {
			// Workflow links have their own section, built from the Work
			// projection; they are not generic references.
			if link.GetReference().GetWorkItem() != nil || link.GetReference().GetJourney() != nil {
				continue
			}
			model.Links = append(model.Links, projectReference(link, options.LinkHref))
		}
	}
	return model, nil
}

func boardCard(task *projectv1.ProjectTask, view *projectv1.BoardView, statusColumn, statusLabels, fieldLabels map[string]string, options ProjectionOptions) projectui.Card {
	statusID := task.GetStatusId()
	label := statusLabels[statusID]
	if label == "" {
		label = statusID
	}
	card := projectui.Card{
		ID: task.GetTaskId(), Title: task.GetTitle(), ColumnID: statusColumn[statusID], StatusID: statusID,
		TaskRevision: task.GetRevision(), WorkflowRevision: task.GetWorkflowRevision(), FailedAction: options.FailedActions[task.GetTaskId()],
		StatusLabel: label, DueDate: task.GetDueDate(), Type: task.GetTaskTypeId(),
		Priority: priorityLabel(task.GetPriority()), PriorityID: task.GetPriority().String(), Pending: options.PendingTaskIDs[task.GetTaskId()],
		ConflictState: options.Conflicts[task.GetTaskId()] != "",
		Labels:        append([]string(nil), task.GetLabels()...), StoryPoints: int(task.GetStoryPoints()),
	}
	card.Assignee = options.AssigneeNames[task.GetAssigneeId()]
	if href := options.TaskHref; href != nil {
		card.DetailHref = href(task.GetTaskId())
	}
	allowedTargets := map[string]bool{statusID: true}
	for _, status := range options.Statuses {
		if status != nil && status.GetStatusId() == statusID {
			for _, targetID := range status.GetAllowedNextStatusIds() {
				allowedTargets[targetID] = true
			}
			break
		}
	}
	for _, status := range options.Statuses {
		if status == nil || status.GetStatusId() == "" {
			continue
		}
		if allowedTargets[status.GetStatusId()] {
			statusLabel := status.GetName()
			if statusLabel == "" {
				statusLabel = status.GetStatusId()
			}
			card.StatusOptions = append(card.StatusOptions, projectui.Status{ID: status.GetStatusId(), Label: statusLabel})
		}
	}
	card.LaneID, card.LaneLabel = taskLane(task, view, fieldLabels, options)
	if options.CanMoveStatus != nil {
		card.CanMoveStatus = options.CanMoveStatus(task)
	}
	if options.CanMoveLane != nil {
		card.CanMoveLane = options.CanMoveLane(task)
	}
	// The configured card-field allowlist controls optional card content.
	allowed := make(map[string]bool, len(view.GetCardFieldIds()))
	for _, id := range view.GetCardFieldIds() {
		allowed[id] = true
	}
	if allowed["description"] {
		card.Summary = task.GetDescription()
	}
	if !allowed["assignee"] {
		card.Assignee = ""
	}
	if !allowed["due_date"] {
		card.DueDate = ""
	}
	if !allowed["type"] {
		card.Type = ""
	}
	if !allowed["priority"] {
		card.Priority, card.PriorityID = "", ""
	}
	return card
}

func buildLanes(view *projectv1.BoardView, tasks []*projectv1.ProjectTask, options ProjectionOptions) []projectui.Lane {
	if view.GetSwimlaneGrouping() == projectv1.BoardSwimlaneGrouping_BOARD_SWIMLANE_GROUPING_NONE || view.GetSwimlaneGrouping() == projectv1.BoardSwimlaneGrouping_BOARD_SWIMLANE_GROUPING_UNSPECIFIED {
		return nil
	}
	counts := make(map[string]int)
	labels := make(map[string]string)
	for _, task := range tasks {
		if task == nil {
			continue
		}
		id, label := taskLane(task, view, nil, options)
		if id == "" {
			continue
		}
		counts[id]++
		labels[id] = label
	}
	ordered := append([]string(nil), view.GetSwimlaneValueOrder()...)
	seen := make(map[string]bool, len(ordered))
	lanes := make([]projectui.Lane, 0, len(counts))
	appendLane := func(id string) {
		if seen[id] || id == "" {
			return
		}
		seen[id] = true
		label := labels[id]
		if options.LaneLabels[id] != "" {
			label = options.LaneLabels[id]
		}
		if label == "" {
			label = id
		}
		lane := projectui.Lane{ID: id, Label: label, Count: counts[id]}
		for _, task := range tasks {
			if task == nil || options.CanMoveLane == nil || !options.CanMoveLane(task) {
				continue
			}
			if laneID, _ := taskLane(task, view, nil, options); laneID == id {
				lane.MayEdit = true
				break
			}
		}
		lanes = append(lanes, lane)
	}
	for _, id := range ordered {
		appendLane(id)
	}
	var remaining []string
	for id := range counts {
		if !seen[id] {
			remaining = append(remaining, id)
		}
	}
	sort.Strings(remaining)
	for _, id := range remaining {
		appendLane(id)
	}
	return lanes
}

func taskLane(task *projectv1.ProjectTask, view *projectv1.BoardView, fieldLabels map[string]string, options ProjectionOptions) (string, string) {
	switch view.GetSwimlaneGrouping() {
	case projectv1.BoardSwimlaneGrouping_BOARD_SWIMLANE_GROUPING_ASSIGNEE:
		id := task.GetAssigneeId()
		if id == "" {
			id = "unassigned"
			label := options.UnassignedLabel
			if label == "" {
				label = "Unassigned"
			}
			return id, label
		}
		label := options.AssigneeNames[id]
		if label == "" {
			label = id
		}
		return id, label
	case projectv1.BoardSwimlaneGrouping_BOARD_SWIMLANE_GROUPING_PRIORITY:
		id := task.GetPriority().String()
		return id, priorityLabel(task.GetPriority())
	case projectv1.BoardSwimlaneGrouping_BOARD_SWIMLANE_GROUPING_ENUM_FIELD:
		for _, field := range task.GetCustomFields() {
			if field != nil && field.GetFieldId() == view.GetSwimlaneFieldId() {
				id := field.GetEnumValue()
				if id != "" {
					label := id
					if options.LaneLabels[id] != "" {
						label = options.LaneLabels[id]
					}
					return id, label
				}
			}
		}
		label := options.UnassignedLabel
		if label == "" {
			label = "Unassigned"
		}
		return "unset", label
	default:
		return "", ""
	}
}

func projectReference(link *projectv1.ResolvedTaskLink, hrefFor func(*projectv1.TaskLinkReference) string) projectui.Reference {
	if link == nil {
		return projectui.Reference{State: projectui.ReferenceUnavailable}
	}
	kind := taskLinkKind(link.GetReference())
	if kind == "" {
		kind = "Linked item"
	}
	if link.GetState() != projectv1.TaskLinkResolutionState_TASK_LINK_RESOLUTION_STATE_AVAILABLE || link.GetPreview() == nil {
		state := projectui.ReferenceRestricted
		if link.GetState() == projectv1.TaskLinkResolutionState_TASK_LINK_RESOLUTION_STATE_UNSPECIFIED {
			state = projectui.ReferenceUnavailable
		}
		return projectui.Reference{Kind: kind, State: state}
	}
	ref := projectui.Reference{Kind: kind, State: projectui.ReferenceReady, Title: link.GetPreview().GetTitle()}
	if hrefFor != nil {
		ref.Href = hrefFor(link.GetReference())
	}
	if ref.Title == "" || ref.Href == "" {
		return projectui.Reference{Kind: kind, State: projectui.ReferenceUnavailable}
	}
	return ref
}

func taskLinkKind(reference *projectv1.TaskLinkReference) string {
	if reference == nil {
		return ""
	}
	switch {
	case reference.GetChatConversation() != nil, reference.GetChatPost() != nil:
		return "Chat"
	case reference.GetDeployedDocument() != nil:
		return "Docs"
	case reference.GetWorkItem() != nil:
		return "Work"
	default:
		return ""
	}
}

func typedFieldText(field *projectv1.TypedFieldValue) string {
	switch value := field.GetValue().(type) {
	case *projectv1.TypedFieldValue_TextValue:
		return value.TextValue
	case *projectv1.TypedFieldValue_NumberValue:
		if value.NumberValue != nil {
			return formatProjectDecimal(value.NumberValue)
		}
	case *projectv1.TypedFieldValue_DateValue:
		return value.DateValue
	case *projectv1.TypedFieldValue_EnumValue:
		return value.EnumValue
	case *projectv1.TypedFieldValue_BooleanValue:
		return fmt.Sprint(value.BooleanValue)
	}
	// Person and link values are opaque references, not safe display names.
	return ""
}

func formatProjectDecimal(value *commonv1.Decimal) string {
	// This helper's narrow contract intentionally omits malformed or
	// unexpectedly high-scale values instead of exposing their protobuf dump.
	scale := value.GetScale()
	if scale < 0 || scale > 18 {
		return ""
	}
	magnitude := new(big.Int).SetBytes(value.GetUnscaledMagnitude()).String()
	if magnitude == "0" {
		return "0"
	}
	if scale > 0 {
		for len(magnitude) <= int(scale) {
			magnitude = "0" + magnitude
		}
		at := len(magnitude) - int(scale)
		magnitude = magnitude[:at] + "." + magnitude[at:]
	}
	if value.GetSign() == commonv1.DecimalSign_DECIMAL_SIGN_NEGATIVE {
		return "-" + magnitude
	}
	if value.GetSign() != commonv1.DecimalSign_DECIMAL_SIGN_POSITIVE {
		return ""
	}
	return magnitude
}

func priorityLabel(value projectv1.TaskPriority) string {
	switch value {
	case projectv1.TaskPriority_TASK_PRIORITY_LOW:
		return "Low"
	case projectv1.TaskPriority_TASK_PRIORITY_NORMAL:
		return "Normal"
	case projectv1.TaskPriority_TASK_PRIORITY_HIGH:
		return "High"
	case projectv1.TaskPriority_TASK_PRIORITY_URGENT:
		return "Urgent"
	default:
		return ""
	}
}
