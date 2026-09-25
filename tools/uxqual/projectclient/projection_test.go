package projectclient

import (
	"strings"
	"testing"

	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/projectui"
)

func TestBoardModelProjectsColumnsFieldsLanesAndOptimisticState(t *testing.T) {
	response := &projectv1.GetBoardResponse{
		View: &projectv1.BoardView{
			ViewId: "view-1", ProjectId: "p-1", Name: "Operations",
			CardFieldIds:     []string{"description", "assignee", "due_date", "priority"},
			SwimlaneGrouping: projectv1.BoardSwimlaneGrouping_BOARD_SWIMLANE_GROUPING_ASSIGNEE,
			Columns: []*projectv1.BoardColumn{
				{ColumnId: "closed", Name: "Done", StatusIds: []string{"done"}, Position: 2},
				{ColumnId: "active", Name: "In progress", StatusIds: []string{"open", "doing"}, Position: 1},
			},
		},
		Tasks: []*projectv1.ProjectTask{
			{TaskId: "t-1", ProjectId: "p-1", Title: "Prepare launch", Description: "Checklist", StatusId: "doing", AssigneeId: "worker-1", DueDate: "2026-10-02", Priority: projectv1.TaskPriority_TASK_PRIORITY_HIGH},
			{TaskId: "t-2", ProjectId: "p-1", Title: "Unassigned", StatusId: "open"},
		},
	}
	options := ProjectionOptions{
		ExpectedProjectID: "p-1", ExpectedViewID: "view-1", Mode: projectui.ViewBoard,
		Statuses: []*projectv1.ProjectStatus{
			{StatusId: "open", Name: "Open", AllowedNextStatusIds: []string{"doing"}},
			{StatusId: "doing", Name: "Doing", AllowedNextStatusIds: []string{"done"}},
			{StatusId: "done", Name: "Done"},
		},
		AssigneeNames: map[string]string{"worker-1": "Casey"}, PendingTaskIDs: map[string]bool{"t-1": true},
		Conflicts:     map[string]string{"t-2": "Changed elsewhere"},
		CanMoveStatus: func(*projectv1.ProjectTask) bool { return true },
		CanMoveLane:   func(task *projectv1.ProjectTask) bool { return task.GetTaskId() == "t-1" },
	}
	model, err := BoardModel(response, options)
	if err != nil {
		t.Fatal(err)
	}
	if model.Title != "Operations" || len(model.Columns) != 2 || model.Columns[0].ID != "active" || len(model.Columns[0].Statuses) != 2 {
		t.Fatalf("project columns = %+v", model.Columns)
	}
	if len(model.Cards) != 2 {
		t.Fatalf("cards = %+v", model.Cards)
	}
	card := model.Cards[0]
	if card.ColumnID != "active" || card.StatusLabel != "Doing" || card.Summary != "Checklist" || card.Assignee != "Casey" || card.DueDate != "2026-10-02" || card.Priority != "High" || !card.Pending || !card.CanMoveStatus || !card.CanMoveLane {
		t.Fatalf("projected card = %+v", card)
	}
	if len(card.StatusOptions) != 2 || card.StatusOptions[1].ID != "done" {
		t.Fatalf("status options = %+v", card.StatusOptions)
	}
	if len(model.Lanes) != 2 || model.Lanes[1].ID != "worker-1" || model.Lanes[1].Label != "Casey" || model.Lanes[1].Count != 1 || !model.Lanes[1].MayEdit {
		t.Fatalf("lanes = %+v", model.Lanes)
	}
	if model.Cards[1].LaneID != "unassigned" || !model.Cards[1].ConflictState || model.Cards[1].Conflict != "" || model.Cards[1].CanMoveLane {
		t.Fatalf("unassigned/conflict card = %+v", model.Cards[1])
	}
}

func TestBoardModelLimitsOptionalFieldsToConfiguredCardAllowlist(t *testing.T) {
	response := boardFixture()
	response.View.CardFieldIds = []string{"priority"}
	model, err := BoardModel(response, ProjectionOptions{ExpectedProjectID: "p-1", ExpectedViewID: "view-1"})
	if err != nil {
		t.Fatal(err)
	}
	card := model.Cards[0]
	if card.Summary != "" || card.Assignee != "" || card.DueDate != "" || card.Type != "" || card.Priority != "High" {
		t.Fatalf("card disclosed fields outside view allowlist: %+v", card)
	}
}

func TestBoardModelGroupsEnumSwimlanesAndSortsThem(t *testing.T) {
	response := boardFixture()
	response.View.SwimlaneGrouping = projectv1.BoardSwimlaneGrouping_BOARD_SWIMLANE_GROUPING_ENUM_FIELD
	response.View.SwimlaneFieldId = "region"
	response.View.SwimlaneValueOrder = []string{"west", "east"}
	response.Tasks[0].CustomFields = []*projectv1.TypedFieldValue{{FieldId: "region", Value: &projectv1.TypedFieldValue_EnumValue{EnumValue: "east"}}}
	response.Tasks = append(response.Tasks, &projectv1.ProjectTask{TaskId: "t-2", ProjectId: "p-1", Title: "North", StatusId: "open", CustomFields: []*projectv1.TypedFieldValue{{FieldId: "region", Value: &projectv1.TypedFieldValue_EnumValue{EnumValue: "north"}}}})
	model, err := BoardModel(response, ProjectionOptions{ExpectedProjectID: "p-1", ExpectedViewID: "view-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Lanes) != 3 || model.Lanes[0].ID != "west" || model.Lanes[1].ID != "east" || model.Lanes[2].ID != "north" {
		t.Fatalf("lanes = %+v", model.Lanes)
	}
}

func TestBoardModelRejectsMismatchedProjectViewAndTask(t *testing.T) {
	for name, options := range map[string]ProjectionOptions{
		"project": {ExpectedProjectID: "other", ExpectedViewID: "view-1"},
		"view":    {ExpectedProjectID: "p-1", ExpectedViewID: "other"},
	} {
		if _, err := BoardModel(boardFixture(), options); err == nil {
			t.Errorf("%s mismatch was accepted", name)
		}
	}
	response := boardFixture()
	response.Tasks[0].ProjectId = "other"
	if _, err := BoardModel(response, ProjectionOptions{ExpectedProjectID: "p-1", ExpectedViewID: "view-1"}); err == nil {
		t.Fatal("mismatched task project was accepted")
	}
}

func TestTaskDetailModelMapsAuthorizedFieldsAndNeutralLinks(t *testing.T) {
	response := &projectv1.GetTaskResponse{Task: &projectv1.ProjectTask{
		TaskId: "t-1", ProjectId: "p-1", Title: "Review plan", Description: "Authorized description", StatusId: "doing", AssigneeId: "worker-1", DueDate: "2026-10-02",
		CustomFields: []*projectv1.TypedFieldValue{
			{FieldId: "region", Value: &projectv1.TypedFieldValue_EnumValue{EnumValue: "West"}},
			{FieldId: "private-person", Value: &projectv1.TypedFieldValue_PersonId{PersonId: "worker-secret"}},
			{FieldId: "private-link", Value: &projectv1.TypedFieldValue_LinkValue{LinkValue: "secret-target"}},
			{FieldId: "enabled", Value: &projectv1.TypedFieldValue_BooleanValue{BooleanValue: true}},
		},
	}}
	links := &projectv1.ListTaskLinksResponse{Links: []*projectv1.ResolvedTaskLink{
		{State: projectv1.TaskLinkResolutionState_TASK_LINK_RESOLUTION_STATE_RESTRICTED, Preview: &projectv1.TaskLinkPreview{Title: "Restricted secret"}},
		{State: projectv1.TaskLinkResolutionState_TASK_LINK_RESOLUTION_STATE_AVAILABLE, Reference: &projectv1.TaskLinkReference{Target: &projectv1.TaskLinkReference_ChatConversation{ChatConversation: &projectv1.ChatConversationLink{ConversationId: "c-1"}}}, Preview: &projectv1.TaskLinkPreview{Title: "Planning channel"}},
	}}
	model, err := TaskDetailModel(response, links, ProjectionOptions{
		ExpectedProjectID: "p-1", ExpectedTaskID: "t-1", AssigneeNames: map[string]string{"worker-1": "Casey"},
		Statuses: []*projectv1.ProjectStatus{{StatusId: "doing", Name: "Doing"}},
		Fields:   []*projectv1.ProjectFieldDefinition{{FieldId: "region", Name: "Region"}, {FieldId: "enabled", Name: "Enabled"}},
		LinkHref: func(ref *projectv1.TaskLinkReference) string {
			if ref.GetChatConversation() != nil {
				return "/workspace/app/chat#channel=c-1"
			}
			return ""
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if model.Title != "Review plan" || model.Description != "Authorized description" || model.Status != "Doing" || model.Assignee != "Casey" || len(model.Fields) != 2 {
		t.Fatalf("detail model = %+v", model)
	}
	if model.Fields[0].Label != "Region" || model.Fields[0].Value != "West" || model.Fields[1].Value != "true" {
		t.Fatalf("detail fields = %+v", model.Fields)
	}
	if len(model.Links) != 2 || model.Links[0].State != projectui.ReferenceRestricted || model.Links[0].Title != "" || model.Links[0].Href != "" || model.Links[1].State != projectui.ReferenceReady || model.Links[1].Href != "/workspace/app/chat#channel=c-1" {
		t.Fatalf("detail links = %+v", model.Links)
	}
	if strings.Contains(model.Links[0].Title, "secret") {
		t.Fatal("restricted title leaked")
	}
}

func TestTaskDetailModelRejectsMismatchedIdentityAndDoesNotShowOpaqueReferences(t *testing.T) {
	response := &projectv1.GetTaskResponse{Task: &projectv1.ProjectTask{TaskId: "t-1", ProjectId: "p-1", CustomFields: []*projectv1.TypedFieldValue{
		{FieldId: "person", Value: &projectv1.TypedFieldValue_PersonId{PersonId: "private-id"}},
		{FieldId: "link", Value: &projectv1.TypedFieldValue_LinkValue{LinkValue: "private-url"}},
	}}}
	if _, err := TaskDetailModel(response, nil, ProjectionOptions{ExpectedProjectID: "p-1", ExpectedTaskID: "other"}); err == nil {
		t.Fatal("mismatched task was accepted")
	}
	model, err := TaskDetailModel(response, nil, ProjectionOptions{ExpectedProjectID: "p-1", ExpectedTaskID: "t-1", Fields: []*projectv1.ProjectFieldDefinition{{FieldId: "person", Name: "Person"}, {FieldId: "link", Name: "Link"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(model.Fields) != 0 {
		t.Fatalf("opaque identifiers exposed as custom field values: %+v", model.Fields)
	}
}

func boardFixture() *projectv1.GetBoardResponse {
	return &projectv1.GetBoardResponse{View: &projectv1.BoardView{ViewId: "view-1", ProjectId: "p-1", Name: "Board", Columns: []*projectv1.BoardColumn{{ColumnId: "todo", Name: "To do", StatusIds: []string{"open"}}}}, Tasks: []*projectv1.ProjectTask{{TaskId: "t-1", ProjectId: "p-1", Title: "Task", StatusId: "open", Description: "Secret summary", AssigneeId: "worker-1", DueDate: "2026-10-02", Priority: projectv1.TaskPriority_TASK_PRIORITY_HIGH}}}
}
