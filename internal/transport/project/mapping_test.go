package project

import (
	"context"
	"errors"
	"testing"

	commonv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/common/v1"
	projectv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/project/v1"
	applicationactivity "github.com/monstercameron/human-capital-management-suite/internal/application/projectactivity"
	"github.com/monstercameron/human-capital-management-suite/internal/application/projectservice"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectconfigstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/projectstore"
	domainproject "github.com/monstercameron/human-capital-management-suite/internal/domains/project"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	domainactivity "github.com/monstercameron/human-capital-management-suite/internal/domains/projectactivity"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectboard"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectlink"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectworkflow"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestTransportMappingsRoundTrip(t *testing.T) {
	fields, err := typedFieldEdits([]*projectv1.TypedFieldValue{
		{FieldId: "text", Value: &projectv1.TypedFieldValue_TextValue{TextValue: "hello"}},
		{FieldId: "number", Value: &projectv1.TypedFieldValue_NumberValue{NumberValue: &commonv1.Decimal{Sign: commonv1.DecimalSign_DECIMAL_SIGN_NEGATIVE, UnscaledMagnitude: []byte{0x7b}, Scale: 2}}},
		{FieldId: "date", Value: &projectv1.TypedFieldValue_DateValue{DateValue: "2026-09-24"}},
		{FieldId: "enum", Value: &projectv1.TypedFieldValue_EnumValue{EnumValue: "ready"}},
		{FieldId: "person", Value: &projectv1.TypedFieldValue_PersonId{PersonId: "worker-1"}},
		{FieldId: "link", Value: &projectv1.TypedFieldValue_LinkValue{LinkValue: "https://example.test"}},
		{FieldId: "flag", Value: &projectv1.TypedFieldValue_BooleanValue{BooleanValue: true}},
	})
	if err != nil || len(fields) != 7 || fields[1].CanonicalValue != "-1.23" || fields[6].CanonicalValue != "true" {
		t.Fatalf("typed edits=%+v err=%v", fields, err)
	}
	for i, field := range fields {
		got := typedFieldMessage(field.FieldID, field)
		if got == nil || got.GetFieldId() != []string{"text", "number", "date", "enum", "person", "link", "flag"}[i] {
			t.Fatalf("typed field %d mapped to %+v", i, got)
		}
	}
	if _, err := typedFieldEdits([]*projectv1.TypedFieldValue{{FieldId: "missing"}}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("missing oneof=%v", err)
	}
	if _, err := typedFieldEdits([]*projectv1.TypedFieldValue{{FieldId: "bad", Value: &projectv1.TypedFieldValue_NumberValue{NumberValue: &commonv1.Decimal{Sign: commonv1.DecimalSign_DECIMAL_SIGN_UNSPECIFIED, UnscaledMagnitude: []byte{1}}}}}); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("bad number sign=%v", err)
	}
	if got := typedFieldMessage("bad", domainproject.TaskFieldEdit{Type: "BOOLEAN", CanonicalValue: "perhaps"}); got != nil {
		t.Fatalf("invalid stored boolean=%+v", got)
	}
	if got := typedFieldMessage("bad", domainproject.TaskFieldEdit{Type: "NUMBER", CanonicalValue: "not-a-number"}); got != nil {
		t.Fatalf("invalid stored number=%+v", got)
	}
}

func TestWorkflowConfigurationMappings(t *testing.T) {
	spec := &projectv1.WorkflowConfigurationSpec{
		Statuses:    []*projectv1.ProjectStatus{{StatusId: "todo", Name: "Todo", Category: projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_NOT_STARTED, AllowedNextStatusIds: []string{"doing"}, RequiredFieldIds: []string{"reason"}}, {StatusId: "doing", Name: "Doing", Category: projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_ACTIVE}, {StatusId: "blocked", Name: "Blocked", Category: projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_BLOCKED}, {StatusId: "done", Name: "Done", Category: projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_DONE}, {StatusId: "cancelled", Name: "Cancelled", Category: projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_CANCELLED}},
		TaskTypes:   []*projectv1.ProjectTaskType{{TaskTypeId: "task", Name: "Task", FieldIds: []string{"text", "enum"}, InitialStatusId: "todo"}},
		Fields:      []*projectv1.ProjectFieldDefinition{{FieldId: "text", Name: "Text", Type: projectv1.ProjectFieldType_PROJECT_FIELD_TYPE_TEXT, Required: true}, {FieldId: "num", Name: "Num", Type: projectv1.ProjectFieldType_PROJECT_FIELD_TYPE_NUMBER}, {FieldId: "date", Name: "Date", Type: projectv1.ProjectFieldType_PROJECT_FIELD_TYPE_DATE}, {FieldId: "enum", Name: "Enum", Type: projectv1.ProjectFieldType_PROJECT_FIELD_TYPE_ENUM, EnumValues: []string{"a"}}, {FieldId: "person", Name: "Person", Type: projectv1.ProjectFieldType_PROJECT_FIELD_TYPE_PERSON}, {FieldId: "link", Name: "Link", Type: projectv1.ProjectFieldType_PROJECT_FIELD_TYPE_LINK}, {FieldId: "bool", Name: "Bool", Type: projectv1.ProjectFieldType_PROJECT_FIELD_TYPE_BOOLEAN}},
		Transitions: []*projectv1.ProjectWorkflowTransition{{FromStatusId: "todo", ToStatusId: "doing", TaskTypeId: "task", RequiredFieldIds: []string{"reason"}}},
		Columns:     []*projectv1.ProjectWorkflowColumn{{ColumnId: "todo", Name: "To do", StatusIds: []string{"todo"}}, {ColumnId: "doing", Name: "Doing", StatusIds: []string{"doing", "blocked"}}, {ColumnId: "done", Name: "Done", StatusIds: []string{"done", "cancelled"}}},
	}
	cfg := workflowFromSpec(spec)
	if len(cfg.Statuses) != 5 || len(cfg.Transitions) != 1 || cfg.Transitions[0].TaskTypeID != "task" || len(cfg.Transitions[0].RequiredFields) != 1 || len(cfg.Columns) != 3 || len(cfg.Columns[1].StatusIDs) != 2 || len(cfg.Fields) != 7 || len(cfg.TaskTypes) != 1 || !cfg.Fields[0].Required || len(cfg.TaskTypes[0].FieldIDs) != 2 || len(cfg.Statuses[0].RequiredFieldIDs) != 1 {
		t.Fatalf("workflow config=%+v", cfg)
	}
	got := workflowSpec(cfg)
	if len(got.GetStatuses()) != 5 || len(got.GetStatuses()[0].GetAllowedNextStatusIds()) != 1 || len(got.GetStatuses()[0].GetRequiredFieldIds()) != 1 || len(got.GetFields()) != 7 || !got.GetFields()[0].GetRequired() || len(got.GetTaskTypes()[0].GetFieldIds()) != 2 || got.GetTaskTypes()[0].GetInitialStatusId() != "todo" || len(got.GetTransitions()) != 1 || got.GetTransitions()[0].GetToStatusId() != "doing" || len(got.GetColumns()) != 3 || len(got.GetColumns()[1].GetStatusIds()) != 2 {
		t.Fatalf("workflow spec=%+v", got)
	}
	for _, tc := range []struct {
		wire projectv1.ProjectStatusCategory
		want projectworkflow.StatusCategory
	}{{projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_NOT_STARTED, projectworkflow.CategoryNotStarted}, {projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_ACTIVE, projectworkflow.CategoryActive}, {projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_BLOCKED, projectworkflow.CategoryBlocked}, {projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_DONE, projectworkflow.CategoryDone}, {projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_CANCELLED, projectworkflow.CategoryCancelled}, {projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_UNSPECIFIED, ""}} {
		if workflowCategory(tc.wire) != tc.want || workflowCategoryMessage(tc.want) != tc.wire {
			t.Errorf("status category %v", tc.wire)
		}
	}
	for _, tc := range []struct {
		wire projectv1.ProjectFieldType
		want projectworkflow.FieldType
	}{{projectv1.ProjectFieldType_PROJECT_FIELD_TYPE_TEXT, projectworkflow.FieldText}, {projectv1.ProjectFieldType_PROJECT_FIELD_TYPE_NUMBER, projectworkflow.FieldNumber}, {projectv1.ProjectFieldType_PROJECT_FIELD_TYPE_DATE, projectworkflow.FieldDate}, {projectv1.ProjectFieldType_PROJECT_FIELD_TYPE_ENUM, projectworkflow.FieldEnum}, {projectv1.ProjectFieldType_PROJECT_FIELD_TYPE_PERSON, projectworkflow.FieldPerson}, {projectv1.ProjectFieldType_PROJECT_FIELD_TYPE_LINK, projectworkflow.FieldLink}, {projectv1.ProjectFieldType_PROJECT_FIELD_TYPE_BOOLEAN, projectworkflow.FieldBoolean}, {projectv1.ProjectFieldType_PROJECT_FIELD_TYPE_UNSPECIFIED, ""}} {
		if workflowFieldType(tc.wire) != tc.want || workflowFieldTypeMessage(tc.want) != tc.wire {
			t.Errorf("field type %v", tc.wire)
		}
	}
	if workflowCategoryMessage("unknown") != projectv1.ProjectStatusCategory_PROJECT_STATUS_CATEGORY_UNSPECIFIED || workflowFieldTypeMessage("unknown") != projectv1.ProjectFieldType_PROJECT_FIELD_TYPE_UNSPECIFIED {
		t.Fatal("unknown workflow enum did not map to unspecified")
	}
}

func TestWorkflowFieldMetadataRoundTrip(t *testing.T) {
	minLength, maxLength := int64(2), int64(40)
	minNumber, maxNumber := 1.5, 100.0
	spec := &projectv1.WorkflowConfigurationSpec{
		Statuses:  []*projectv1.ProjectStatus{{StatusId: "retired", Name: "Retired", Retired: true}},
		TaskTypes: []*projectv1.ProjectTaskType{{TaskTypeId: "task", Name: "Task", RequiredFieldIds: []string{"title"}}},
		Fields: []*projectv1.ProjectFieldDefinition{
			{FieldId: "title", Name: "Title", Type: projectv1.ProjectFieldType_PROJECT_FIELD_TYPE_TEXT, Classification: "PROJECT_WIDE", DefaultJson: `"hello"`, MinLength: &minLength, MaxLength: &maxLength, Indexed: true, Searchable: true, Retired: true},
			{FieldId: "budget", Name: "Budget", Type: projectv1.ProjectFieldType_PROJECT_FIELD_TYPE_NUMBER, Classification: "PROJECT_WIDE", MinNumber: &minNumber, MaxNumber: &maxNumber},
		},
	}
	got := workflowSpec(workflowFromSpec(spec))
	if !got.GetStatuses()[0].GetRetired() || len(got.GetTaskTypes()[0].GetRequiredFieldIds()) != 1 {
		t.Fatalf("status/type metadata dropped: %+v", got)
	}
	text := got.GetFields()[0]
	if text.GetClassification() != "PROJECT_WIDE" || text.GetDefaultJson() != `"hello"` || text.MinLength == nil || text.GetMinLength() != 2 || text.MaxLength == nil || text.GetMaxLength() != 40 || !text.GetIndexed() || !text.GetSearchable() || !text.GetRetired() {
		t.Fatalf("text field metadata dropped: %+v", text)
	}
	number := got.GetFields()[1]
	if number.MinNumber == nil || number.GetMinNumber() != 1.5 || number.MaxNumber == nil || number.GetMaxNumber() != 100 {
		t.Fatalf("number field metadata dropped: %+v", number)
	}
}

func TestBoardViewAndLinkMappings(t *testing.T) {
	wire := &projectv1.BoardView{ViewId: "view", Name: "Sprint board", Audience: projectv1.BoardViewAudience_BOARD_VIEW_AUDIENCE_PROJECT, Columns: []*projectv1.BoardColumn{{ColumnId: "col", Name: "Doing", StatusIds: []string{"doing"}}}, Filter: &projectv1.BoardFilter{StatusIds: []string{"doing"}, AssigneeIds: []string{"worker"}, TaskTypeIds: []string{"task"}, Priorities: []projectv1.TaskPriority{projectv1.TaskPriority_TASK_PRIORITY_HIGH}, EnumFilters: []*projectv1.EnumBoardFilter{{FieldId: "team", Values: []string{"a"}}}}, SwimlaneGrouping: projectv1.BoardSwimlaneGrouping_BOARD_SWIMLANE_GROUPING_ENUM_FIELD, SwimlaneFieldId: "team", SwimlaneValueOrder: []string{"a", "b"}, Order: projectv1.BoardOrder_BOARD_ORDER_PRIORITY}
	if err := validateViewWire(wire); err != nil {
		t.Fatal(err)
	}
	view := viewFromMessage(wire)
	if view.Audience != projectboard.AudienceProject || view.Name != "Sprint board" || view.Filter.AssigneeID != "worker" || view.Filter.Priorities[0] != "HIGH" || view.Grouping.FieldID != "team" || view.OrderBy != projectboard.OrderPriority || len(view.SwimlaneValueOrder) != 2 {
		t.Fatalf("view mapping=%+v", view)
	}
	back := viewMessage("p1", "", view)
	if back.GetAudience() != wire.GetAudience() || back.GetName() != wire.GetName() || back.GetFilter().GetEnumFilters()[0].GetValues()[0] != "a" || back.GetSwimlaneFieldId() != "team" || back.GetOrder() != wire.GetOrder() || back.GetSwimlaneValueOrder()[1] != "b" {
		t.Fatalf("view roundtrip=%+v", back)
	}
	for _, bad := range []*projectv1.BoardView{{}, {Audience: projectv1.BoardViewAudience_BOARD_VIEW_AUDIENCE_PERSONAL, Filter: &projectv1.BoardFilter{AssigneeIds: []string{"a", "b"}}}, {Audience: projectv1.BoardViewAudience_BOARD_VIEW_AUDIENCE_PERSONAL, Order: projectv1.BoardOrder_BOARD_ORDER_UPDATED}} {
		if validateViewWire(bad) == nil {
			t.Errorf("unsupported view accepted: %+v", bad)
		}
	}
	for _, wireRef := range []*projectv1.TaskLinkReference{
		{Target: &projectv1.TaskLinkReference_ChatConversation{ChatConversation: &projectv1.ChatConversationLink{ConversationId: "conv"}}},
		{Target: &projectv1.TaskLinkReference_ChatPost{ChatPost: &projectv1.ChatPostLink{ConversationId: "conv", PostId: "post"}}},
		{Target: &projectv1.TaskLinkReference_DeployedDocument{DeployedDocument: &projectv1.DeployedDocumentLink{DocumentId: "doc", DeployedVersionId: "v1", ScopeId: "org"}}},
		{Target: &projectv1.TaskLinkReference_WorkItem{WorkItem: &projectv1.WorkItemLink{WorkItemId: "work"}}},
	} {
		ref, err := referenceFromMessage(wireRef)
		if err != nil || ref.Validate() != nil {
			t.Fatalf("reference %+v -> %+v,%v", wireRef, ref, err)
		}
		if referenceMessage(ref) == nil {
			t.Fatalf("reference lost: %+v", ref)
		}
	}
	for _, wireRef := range []*projectv1.ProjectSourceReference{{Source: &projectv1.ProjectSourceReference_ChatConversation{ChatConversation: &projectv1.ChatConversationLink{ConversationId: "conv"}}}, {Source: &projectv1.ProjectSourceReference_ChatPost{ChatPost: &projectv1.ChatPostLink{ConversationId: "conv", PostId: "post"}}}, {Source: &projectv1.ProjectSourceReference_DeployedDocument{DeployedDocument: &projectv1.DeployedDocumentLink{DocumentId: "doc", DeployedVersionId: "v1", ScopeId: "org"}}}} {
		if ref, err := sourceReferenceFromMessage(wireRef); err != nil || ref.Validate() != nil {
			t.Fatalf("source ref=%+v err=%v", ref, err)
		}
	}
	if _, err := referenceFromMessage(nil); !errors.Is(err, projectlink.ErrInvalidReference) {
		t.Fatalf("nil reference=%v", err)
	}
	if _, err := sourceReferenceFromMessage(&projectv1.ProjectSourceReference{}); !errors.Is(err, projectlink.ErrInvalidReference) {
		t.Fatalf("empty source=%v", err)
	}
	if state := linkMessage(projectservice.TaskLink{ID: "l1", Reference: projectlink.Reference{Kind: projectlink.ChatConversation, ID: "conv"}, Resolution: projectlink.Result{State: projectlink.Available, Preview: &projectlink.Preview{Title: "Conversation", Snippet: "Preview"}}}); state.GetPreview().GetTitle() != "Conversation" {
		t.Fatalf("available link=%+v", state)
	}
}

func TestTransportErrorMapping(t *testing.T) {
	cases := []struct {
		err  error
		code codes.Code
	}{{projectservice.ErrInvalidRequest, codes.InvalidArgument}, {projectservice.ErrUnavailable, codes.Unavailable}, {applicationactivity.ErrInvalidPrincipal, codes.Unauthenticated}, {applicationactivity.ErrInvalidRequest, codes.InvalidArgument}, {applicationactivity.ErrUnavailable, codes.Unavailable}, {domainactivity.ErrInvalidRequest, codes.InvalidArgument}, {domainactivity.ErrCursor, codes.InvalidArgument}, {domainactivity.ErrDenied, codes.PermissionDenied}, {domainactivity.ErrNotFound, codes.NotFound}, {domainactivity.ErrConflict, codes.Aborted}, {domainactivity.ErrPortMissing, codes.Unavailable}, {projectaccess.ErrUnauthorized, codes.PermissionDenied}, {projectaccess.ErrTenantMismatch, codes.NotFound}, {projectaccess.ErrOwnerRequired, codes.FailedPrecondition}, {domainproject.ErrRevisionConflict, codes.Aborted}, {projectstore.ErrRevisionConflict, codes.Aborted}, {projectconfigstore.ErrRevisionConflict, codes.Aborted}, {projectstore.ErrIdempotencyConflict, codes.AlreadyExists}, {projectconfigstore.ErrIdempotencyReuse, codes.AlreadyExists}, {projectservice.ErrStaleView, codes.Aborted}, {projectworkflow.ErrInvalidConfig, codes.InvalidArgument}, {projectworkflow.ErrInvalidVersion, codes.Aborted}, {projectworkflow.ErrUnsafeMigration, codes.FailedPrecondition}, {projectservice.ErrUnsafeWorkflowPublish, codes.FailedPrecondition}, {projectconfigstore.ErrMigrationRequired, codes.FailedPrecondition}, {projectconfigstore.ErrDigestConflict, codes.FailedPrecondition}, {projectconfigstore.ErrReviewRequired, codes.FailedPrecondition}, {projectworkflow.ErrMigrationLimitExceeded, codes.ResourceExhausted}, {projectworkflow.ErrSnapshotLimitExceeded, codes.ResourceExhausted}, {projectworkflow.ErrTransitionRejected, codes.FailedPrecondition}, {projectlink.ErrResolverUnavailable, codes.Unavailable}, {projectservice.ErrUnsupportedLink, codes.InvalidArgument}, {context.Canceled, codes.Canceled}, {context.DeadlineExceeded, codes.DeadlineExceeded}, {errors.New("private error"), codes.Internal}}
	for _, tc := range cases {
		if got := status.Code(mapError(tc.err)); got != tc.code {
			t.Errorf("mapError(%v)=%v, want %v", tc.err, got, tc.code)
		}
	}
	owned := envelope.New(envelope.CodeAborted, "project.conflict", "project changed")
	if mapError(owned) != owned {
		t.Fatal("owned error was projected twice")
	}
}
