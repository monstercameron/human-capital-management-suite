package manifest

// projectRules is the reviewed HTTP, authorization and retry contract for
// ProjectService. These methods are backed by the project application and
// activity services; their project-owned capabilities are intentionally
// separate from the generic BusinessIntent registry.
func projectRules() map[string]rule {
	type binding struct {
		name, verb, path, body, action, capability string
		behavior                                   IntentBehavior
		idempotent                                 bool
		revision, pagination, ordering             string
	}
	rows := []binding{
		{"GetProjectMembership", "GET", "/v1/projects/{project}/members/{member}", "", "members.get", "projectaccess.manage_members", IntentBehaviorObserves, false, "RETURNS_ETAG", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"ListProjectMembers", "GET", "/v1/projects/{project}/members", "", "members.list", "projectaccess.manage_members", IntentBehaviorObserves, false, "NOT_APPLICABLE", "OPAQUE_CURSOR_BOUNDED", "STABLE_SNAPSHOT_ORDER"},
		{"InviteProjectMember", "POST", "/v1/projects/{project}/members:invite", "*", "members.invite", "projectaccess.manage_members", IntentBehaviorConsumes, true, "REQUIRED_EXACT_MATCH", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"AcceptProjectInvitation", "POST", "/v1/projects/{project}/members:accept", "*", "members.accept", "projectaccess.manage_members", IntentBehaviorConsumes, true, "REQUIRED_EXACT_MATCH", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"ChangeProjectMemberRole", "POST", "/v1/projects/{project}/members/{member}:role", "*", "members.role", "projectaccess.manage_members", IntentBehaviorConsumes, true, "REQUIRED_EXACT_MATCH", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"RevokeProjectMember", "POST", "/v1/projects/{project}/members/{member}:revoke", "*", "members.revoke", "projectaccess.manage_members", IntentBehaviorConsumes, true, "REQUIRED_EXACT_MATCH", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"TransferProjectOwnership", "POST", "/v1/projects/{project}/members/{member}:transfer-ownership", "*", "members.transfer_ownership", "projectaccess.manage_members", IntentBehaviorConsumes, true, "REQUIRED_EXACT_MATCH", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"GetProject", "GET", "/v1/projects/{project}", "", "get", "projectaccess.read_project", IntentBehaviorObserves, false, "RETURNS_ETAG", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"ListProjects", "GET", "/v1/projects", "", "list", "projectaccess.read_project", IntentBehaviorObserves, false, "NOT_APPLICABLE", "OPAQUE_CURSOR_BOUNDED", "STABLE_SNAPSHOT_ORDER"},
		{"CreateProject", "POST", "/v1/projects", "*", "create", "projectaccess.project_create_owner", IntentBehaviorCreates, true, "RETURNS_ETAG", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"UpdateProjectSettings", "PATCH", "/v1/projects/{project}", "*", "update_settings", "projectaccess.manage_project", IntentBehaviorConsumes, true, "REQUIRED_EXACT_MATCH", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"ArchiveProject", "POST", "/v1/projects/{project}:archive", "*", "archive", "projectaccess.archive", IntentBehaviorConsumes, true, "REQUIRED_EXACT_MATCH", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"RestoreProject", "POST", "/v1/projects/{project}:restore", "*", "restore", "projectaccess.archive", IntentBehaviorConsumes, true, "REQUIRED_EXACT_MATCH", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"CreateTask", "POST", "/v1/projects/{project}/tasks", "*", "tasks.create", "projectaccess.edit_task", IntentBehaviorCreates, true, "RETURNS_ETAG", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"GetTask", "GET", "/v1/projects/{project}/tasks/{task}", "", "tasks.get", "projectaccess.read_task", IntentBehaviorObserves, false, "RETURNS_ETAG", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"ListTasks", "GET", "/v1/projects/{project}/tasks", "", "tasks.list", "projectaccess.read_task", IntentBehaviorObserves, false, "NOT_APPLICABLE", "OPAQUE_CURSOR_BOUNDED", "STABLE_SNAPSHOT_ORDER"},
		{"MoveTask", "POST", "/v1/projects/{project}/tasks/{task}:move", "*", "tasks.move", "projectaccess.edit_task", IntentBehaviorConsumes, true, "REQUIRED_EXACT_MATCH", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"PatchTask", "PATCH", "/v1/projects/{project}/tasks/{task}", "*", "tasks.update", "projectaccess.edit_task", IntentBehaviorConsumes, true, "REQUIRED_EXACT_MATCH", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"ArchiveTask", "POST", "/v1/projects/{project}/tasks/{task}:archive", "*", "tasks.archive", "projectaccess.edit_task", IntentBehaviorConsumes, true, "REQUIRED_EXACT_MATCH", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"RestoreTask", "POST", "/v1/projects/{project}/tasks/{task}:restore", "*", "tasks.restore", "projectaccess.edit_task", IntentBehaviorConsumes, true, "REQUIRED_EXACT_MATCH", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"SaveWorkflowDraft", "PUT", "/v1/projects/{project}/workflow/drafts/{draft}", "*", "workflow.drafts.save", "projectaccess.configure_workflow", IntentBehaviorConsumes, true, "REQUIRED_EXACT_MATCH", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"GetWorkflowDraft", "GET", "/v1/projects/{project}/workflow/drafts/{draft}", "", "workflow.drafts.get", "projectaccess.configure_workflow", IntentBehaviorObserves, false, "RETURNS_ETAG", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"PreviewWorkflowDraft", "POST", "/v1/projects/{project}/workflow/drafts/{draft}:preview", "*", "workflow.drafts.preview", "projectaccess.configure_workflow", IntentBehaviorNonMaterial, false, "REQUIRED_EXACT_MATCH", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"PublishWorkflowDraft", "POST", "/v1/projects/{project}/workflow/drafts/{draft}:publish", "*", "workflow.publish", "projectaccess.configure_workflow", IntentBehaviorConsumes, true, "REQUIRED_EXACT_MATCH", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"GetWorkflowConfiguration", "GET", "/v1/projects/{project}/workflow", "", "workflow.get", "projectaccess.configure_workflow", IntentBehaviorObserves, false, "RETURNS_ETAG", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"SaveBoardView", "PUT", "/v1/projects/{project}/views/{view}", "*", "views.save", "projectaccess.manage_views", IntentBehaviorConsumes, true, "REQUIRED_EXACT_MATCH", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"ListBoardViews", "GET", "/v1/projects/{project}/views", "", "views.list", "projectaccess.manage_views", IntentBehaviorObserves, false, "NOT_APPLICABLE", "OPAQUE_CURSOR_BOUNDED", "STABLE_SNAPSHOT_ORDER"},
		{"GetBoard", "GET", "/v1/projects/{project}/views/{view}/board", "", "board.get", "projectaccess.read_task", IntentBehaviorObserves, false, "RETURNS_ETAG", "OPAQUE_CURSOR_BOUNDED", "STABLE_SNAPSHOT_ORDER"},
		{"SearchTasks", "POST", "/v1/projects/{project}/tasks:search", "*", "tasks.search", "projectaccess.read_task", IntentBehaviorObserves, false, "NOT_APPLICABLE", "OPAQUE_CURSOR_BOUNDED", "STABLE_SNAPSHOT_ORDER"},
		{"AddTaskLink", "POST", "/v1/projects/{project}/tasks/{task}/links", "*", "task_links.add", "projectaccess.edit_task", IntentBehaviorConsumes, true, "REQUIRED_EXACT_MATCH", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"ListTaskLinks", "GET", "/v1/projects/{project}/tasks/{task}/links", "", "task_links.list", "projectaccess.read_task", IntentBehaviorObserves, false, "NOT_APPLICABLE", "OPAQUE_CURSOR_BOUNDED", "STABLE_SNAPSHOT_ORDER"},
		{"ListTaskLinksByTarget", "POST", "/v1/task-links:byTarget", "*", "task_links.by_target", "projectaccess.read_task", IntentBehaviorObserves, false, "NOT_APPLICABLE", "OPAQUE_CURSOR_BOUNDED", "STABLE_SNAPSHOT_ORDER"},
		{"RemoveTaskLink", "DELETE", "/v1/projects/{project}/tasks/{task}/links/{link}", "*", "task_links.remove", "projectaccess.edit_task", IntentBehaviorConsumes, true, "REQUIRED_EXACT_MATCH", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"AddTaskComment", "POST", "/v1/projects/{project}/tasks/{task}/comments", "*", "comments.add", "projectaccess.comment", IntentBehaviorConsumes, true, "NOT_APPLICABLE", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"EditTaskComment", "PATCH", "/v1/projects/{project}/tasks/{task}/comments/{comment}", "*", "comments.edit", "projectaccess.comment", IntentBehaviorConsumes, true, "REQUIRED_EXACT_MATCH", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"DeleteTaskComment", "DELETE", "/v1/projects/{project}/tasks/{task}/comments/{comment}", "*", "comments.delete", "projectaccess.comment", IntentBehaviorConsumes, true, "REQUIRED_EXACT_MATCH", "NOT_APPLICABLE", "NOT_APPLICABLE"},
		{"ListTaskComments", "GET", "/v1/projects/{project}/tasks/{task}/comments", "", "comments.list", "projectaccess.read_task", IntentBehaviorObserves, false, "NOT_APPLICABLE", "OPAQUE_CURSOR_BOUNDED", "STABLE_SNAPSHOT_ORDER"},
		{"ListTaskActivity", "GET", "/v1/projects/{project}/tasks/{task}/activity", "", "activity.list", "projectaccess.read_task", IntentBehaviorObserves, false, "NOT_APPLICABLE", "OPAQUE_CURSOR_BOUNDED", "STABLE_SNAPSHOT_ORDER"},
	}
	out := make(map[string]rule, len(rows))
	for _, row := range rows {
		path := "/hcmnext.project.v1.ProjectService/" + row.name
		key := IdempotencyReadSafe
		keySource := ""
		if row.idempotent {
			key = IdempotencyKey
			keySource = "request.idempotency_key"
		}
		out[path] = rule{
			owner: "PROJECT", behavior: row.behavior,
			disposition:       DispositionServed,
			dispositionReason: "ProjectService operation implemented by the Phase 3 project application surface (planning/specs/customer-project-management-and-adaptive-boards.md)",
			httpMethod:        "POST", httpPath: path, httpBody: "*",
			authzAction:       "hcmnext.project." + row.action,
			classificationRef: "CONFIDENTIAL_HR", idempotencyClass: key,
			idempotencyKeySrc: keySource, revisionPolicy: row.revision,
			retryPolicy:      map[bool]string{true: "IDEMPOTENCY_KEY_DEDUPED", false: "CLIENT_MAY_RETRY"}[row.idempotent],
			paginationPolicy: row.pagination, orderingPolicy: row.ordering,
			compatibilityStatus: "ACTIVE", phase: "PHASE_3",
			capabilityRefs: []string{"hcmnext." + row.capability},
		}
	}
	return out
}
