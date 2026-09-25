package contracts

import (
	"testing"

	"google.golang.org/protobuf/types/descriptorpb"
)

// TestTodo_PM_026_Conformance pins the minimum typed project RPC surface
// and checks that writes carry replay/concurrency fences, reads are paged,
// and links cannot be represented as untyped URLs.
func TestTodo_PM_026_Conformance(t *testing.T) {
	repoRoot := findRepoRoot(t)
	fds := buildDescriptorSet(t, repoRoot, t.TempDir(), "project.binpb")
	const fileName = "hcmnext/project/v1/project_service.proto"
	var file *descriptorpb.FileDescriptorProto
	for _, candidate := range fds.GetFile() {
		if candidate.GetName() == fileName {
			file = candidate
			break
		}
	}
	if file == nil {
		t.Fatalf("descriptor set omits %s", fileName)
	}

	var service *descriptorpb.ServiceDescriptorProto
	for _, candidate := range file.GetService() {
		if candidate.GetName() == "ProjectService" {
			service = candidate
			break
		}
	}
	if service == nil {
		t.Fatal("ProjectService is missing")
	}
	wantMethods := []string{
		"GetProjectMembership", "ListProjectMembers", "InviteProjectMember", "AcceptProjectInvitation",
		"ChangeProjectMemberRole", "RevokeProjectMember", "TransferProjectOwnership",
		"GetProject", "ListProjects", "CreateProject", "UpdateProjectSettings", "ArchiveProject", "RestoreProject",
		"CreateTask", "GetTask", "ListTasks", "SearchTasks", "MoveTask", "PatchTask", "ArchiveTask", "RestoreTask", "SaveWorkflowDraft", "GetWorkflowDraft",
		"PreviewWorkflowDraft", "PublishWorkflowDraft", "GetWorkflowConfiguration",
		"SaveBoardView", "ListBoardViews", "GetBoard", "AddTaskLink",
		"ListTaskLinks", "ListTaskLinksByTarget", "RemoveTaskLink", "AddTaskComment", "EditTaskComment", "DeleteTaskComment",
		"ListTaskComments", "ListTaskActivity",
	}
	if len(service.GetMethod()) != len(wantMethods) {
		t.Fatalf("ProjectService has %d methods, want %d", len(service.GetMethod()), len(wantMethods))
	}
	for i, method := range service.GetMethod() {
		if method.GetName() != wantMethods[i] {
			t.Errorf("method %d = %q, want %q", i, method.GetName(), wantMethods[i])
		}
	}
	assertServiceHasTotalDispositionComments(t, fds, fileName, "ProjectService", wantMethods)

	messages := make(map[string]*descriptorpb.DescriptorProto, len(file.GetMessageType()))
	for _, message := range file.GetMessageType() {
		messages[message.GetName()] = message
	}
	assertFields := func(messageName string, names ...string) {
		t.Helper()
		message := messages[messageName]
		if message == nil {
			t.Errorf("message %s is missing", messageName)
			return
		}
		fields := make(map[string]bool, len(message.GetField()))
		for _, field := range message.GetField() {
			fields[field.GetName()] = true
		}
		for _, name := range names {
			if !fields[name] {
				t.Errorf("%s is missing field %q", messageName, name)
			}
		}
	}
	assertFields("CreateProjectRequest", "scope", "idempotency_key", "name", "project_timezone")
	assertFields("UpdateProjectSettingsRequest", "scope", "project_id", "name", "project_timezone", "expected_project_revision", "idempotency_key")
	assertFields("SetProjectLifecycleRequest", "scope", "project_id", "expected_project_revision", "idempotency_key")
	assertFields("ListProjectsRequest", "scope", "page")
	assertFields("CreateTaskRequest", "scope", "idempotency_key", "project_id", "expected_workflow_revision", "title", "initial_status_id", "priority", "source_reference")
	assertFields("GetTaskRequest", "scope", "project_id", "task_id")
	assertFields("ListTasksRequest", "scope", "project_id", "filter", "page")
	assertFields("MoveTaskRequest", "scope", "idempotency_key", "project_id", "task_id", "target_status_id", "expected_task_revision", "expected_workflow_revision", "lane_field_edit")
	assertFields("PatchTaskRequest", "scope", "idempotency_key", "project_id", "task_id", "expected_task_revision", "expected_workflow_revision")
	assertFields("ArchiveTaskRequest", "scope", "idempotency_key", "project_id", "task_id", "expected_task_revision", "expected_workflow_revision")
	assertFields("RestoreTaskRequest", "scope", "idempotency_key", "project_id", "task_id", "expected_task_revision", "expected_workflow_revision")
	assertFields("PreviewWorkflowDraftRequest", "expected_draft_revision", "status_mappings", "field_mappings")
	assertFields("PreviewWorkflowDraftResponse", "reviewed_digest", "migration_plan_digest", "affected_task_ids")
	assertFields("PublishWorkflowDraftRequest", "idempotency_key", "expected_project_workflow_revision", "expected_draft_revision", "reviewed_digest", "reviewed_migration_plan_digest", "status_mappings", "field_mappings")
	assertFields("WorkflowStatusMigrationMapping", "source_status_id", "target_status_id")
	assertFields("WorkflowFieldMigrationMapping", "source_field_id", "target_field_id")
	assertFields("GetWorkflowDraftResponse", "draft_revision", "configuration")
	assertFields("GetWorkflowConfigurationResponse", "configuration")
	assertFields("ListBoardViewsRequest", "project_id", "page")
	assertFields("GetBoardRequest", "project_id", "view_id", "page")
	assertFields("GetBoardResponse", "tasks", "page", "project_revision", "workflow_revision")
	assertFields("BoardView", "swimlane_grouping", "swimlane_field_id", "swimlane_value_order", "order")
	assertFields("BoardFilter", "priorities", "task_type_ids", "enum_filters")
	assertFields("ProjectTask", "priority", "archived")
	assertFields("ProjectTaskType", "initial_status_id")
	assertFields("TaskLinkReference", "chat_conversation", "chat_post", "deployed_document", "work_item")
	assertFields("AddTaskLinkRequest", "scope", "idempotency_key", "project_id", "task_id", "reference", "expected_task_revision")
	assertFields("AddTaskLinkResponse", "link_id", "task_revision")
	assertFields("RemoveTaskLinkRequest", "scope", "project_id", "task_id", "link_id", "expected_task_revision", "idempotency_key")
	assertFields("RemoveTaskLinkResponse", "task_revision")
	assertFields("ProjectSourceReference", "chat_conversation", "chat_post", "deployed_document")
	assertFields("DeployedDocumentLink", "document_id", "deployed_version_id", "scope_id")
	assertFields("TaskLinkPreview", "observed_work_item_version", "safe_work_item_status", "freshness")
	assertFields("AddTaskCommentRequest", "scope", "project_id", "task_id", "idempotency_key", "body_text")
	assertFields("EditTaskCommentRequest", "scope", "project_id", "task_id", "comment_id", "expected_revision", "idempotency_key", "body_text")
	assertFields("DeleteTaskCommentRequest", "scope", "project_id", "task_id", "comment_id", "expected_revision", "idempotency_key")
	assertFields("ListTaskCommentsRequest", "scope", "project_id", "task_id", "page_size", "page_cursor")
	assertFields("ListTaskCommentsResponse", "comments", "next_page_cursor")
	assertFields("ListTaskActivityRequest", "scope", "project_id", "task_id", "page_size", "page_cursor")
	assertFields("ListTaskActivityResponse", "entries", "next_page_cursor")
	assertFields("ProjectTaskComment", "comment_id", "current_revision", "safe_html", "source_text", "tombstone", "actor_id", "created_at", "updated_at")
	assertFields("ProjectTaskActivity", "sequence", "comment_id", "kind", "revision", "actor_id", "occurred_at")

	link := messages["TaskLinkReference"]
	if link != nil {
		if len(link.GetOneofDecl()) != 1 || link.GetOneofDecl()[0].GetName() != "target" {
			t.Error("TaskLinkReference must use one typed target oneof")
		}
		for _, field := range link.GetField() {
			if field.OneofIndex == nil || field.GetOneofIndex() != 0 {
				t.Errorf("TaskLinkReference.%s is not in the target oneof", field.GetName())
			}
		}
	}
	source := messages["ProjectSourceReference"]
	if source != nil && (len(source.GetOneofDecl()) != 1 || source.GetOneofDecl()[0].GetName() != "source") {
		t.Error("ProjectSourceReference must use one typed source oneof")
	}
}
