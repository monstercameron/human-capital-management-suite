package manifest

// requiredFieldPaths is the generated equivalent of the hand-written
// requiredFields table in internal/transport/validate.go. That file
// documents itself as a placeholder: "This table is the placeholder for the
// generated endpoint manifest. When ENDPOINT-001 lands, [DefaultValidator]
// reads the manifest instead and this map goes away." validate.go is frozen
// for this change (owned by a different lane), so this table is produced
// independently here rather than by editing it; the two are compared row
// for row in TestTodo_ENDPOINT_001_Golden and in this package's own doc
// comment below.
//
// Every dotted path names a request field a method cannot proceed without.
// Proto3 declares no field required at the wire level, so — exactly as
// validate.go's own comment states — structural presence has to be stated
// somewhere; this is that same statement, generated from the same reviewed
// domain knowledge, kept in one place per method instead of two.
//
// Diff against internal/transport/validate.go's requiredFields (read
// 2026-09-03, HEAD 94610e8): every one of the thirteen pre-REV-007-04 keys
// below has an identical field-path list to the corresponding entry there.
// The four REV-007-04 rows have no validate.go counterpart: validate.go is
// frozen for another lane, and the manifest is the enforced table (Build
// fails closed on a missing entry). This package does not import
// validate.go's unexported map (it is unexported, and the file is frozen),
// so the comparison is manual and recorded here rather than asserted by an
// import; TestTodo_ENDPOINT_001_Golden pins this package's own table against
// a golden fixture transcribed from the same source read, so a future
// accidental drift in either table's semantics still fails a test even
// though the two packages cannot import each other's private state.
var requiredFieldPaths = map[string][]string{
	"/hcmnext.project.v1.ProjectService/GetProjectMembership":     {"project_id", "user_id", "scope"},
	"/hcmnext.project.v1.ProjectService/ListProjectMembers":       {"project_id", "scope"},
	"/hcmnext.project.v1.ProjectService/InviteProjectMember":      {"project_id", "user_id", "role", "expected_membership_revision", "idempotency_key", "scope"},
	"/hcmnext.project.v1.ProjectService/AcceptProjectInvitation":  {"project_id", "expected_membership_revision", "idempotency_key", "scope"},
	"/hcmnext.project.v1.ProjectService/ChangeProjectMemberRole":  {"project_id", "user_id", "role", "expected_membership_revision", "idempotency_key", "scope"},
	"/hcmnext.project.v1.ProjectService/RevokeProjectMember":      {"project_id", "user_id", "expected_membership_revision", "idempotency_key", "scope"},
	"/hcmnext.project.v1.ProjectService/TransferProjectOwnership": {"project_id", "user_id", "expected_membership_revision", "idempotency_key", "scope"},
	"/hcmnext.intents.v1.IntentService/CreateIntent":              {"idempotency_key", "definition.intent_type_id", "request.schema.schema_id"},
	"/hcmnext.intents.v1.IntentService/GetIntent":                 {"intent_id"},
	"/hcmnext.intents.v1.IntentService/ListIntents":               nil,
	"/hcmnext.intents.v1.IntentService/SimulateIntent":            {"intent_id"},
	"/hcmnext.intents.v1.IntentService/ExecuteIntent":             {"idempotency_key", "intent_id", "approval.proposal_revision_id", "approval.approval_ref"},
	"/hcmnext.intents.v1.IntentService/SubmitIntent":              {"idempotency_key", "intent_id", "proposal_revision_id"},
	"/hcmnext.intents.v1.IntentService/CancelIntent":              {"idempotency_key", "intent_id", "reason_ref"},
	"/hcmnext.intents.v1.IntentService/SupersedeIntent":           {"idempotency_key", "superseded_intent_id", "definition.intent_type_id", "reason_ref"},
	"/hcmnext.intents.v1.IntentService/ExplainIntent":             {"intent_id"},
	"/hcmnext.intents.v1.IntentService/ListIntentTimeline":        {"intent_id"},
	"/hcmnext.intents.v1.IntentService/RecommendIntentAction": {
		"tenant_id", "organization_id", "purpose", "analysis",
		"action.capability_ref", "population", "governance", "simulation",
	},
	"/hcmnext.intents.v1.IntentService/GetIntentDeepLink":   {"intent_id"},
	"/hcmnext.intents.v1.IntentService/InspectIntentFields": {"intent_id"},
	"/hcmnext.intents.v1.IntentService/ExportIntentFields":  {"intent_id", "purpose"},

	"/hcmnext.registry.v1.RegistryService/ListIntentDefinitions": nil,
	"/hcmnext.registry.v1.RegistryService/GetIntentDefinition":   {"definition.intent_type_id"},
	"/hcmnext.registry.v1.RegistryService/ListCapabilities":      nil,
	"/hcmnext.registry.v1.RegistryService/GetCapability":         {"capability_id"},

	"/hcmnext.project.v1.ProjectService/GetProject":               {"project_id", "scope"},
	"/hcmnext.project.v1.ProjectService/ListProjects":             {"scope"},
	"/hcmnext.project.v1.ProjectService/CreateProject":            {"idempotency_key", "name", "project_timezone", "scope"},
	"/hcmnext.project.v1.ProjectService/UpdateProjectSettings":    {"project_id", "name", "project_timezone", "expected_project_revision", "idempotency_key", "scope"},
	"/hcmnext.project.v1.ProjectService/ArchiveProject":           {"project_id", "expected_project_revision", "idempotency_key", "scope"},
	"/hcmnext.project.v1.ProjectService/RestoreProject":           {"project_id", "expected_project_revision", "idempotency_key", "scope"},
	"/hcmnext.project.v1.ProjectService/CreateTask":               {"idempotency_key", "project_id", "title", "expected_workflow_revision", "scope"},
	"/hcmnext.project.v1.ProjectService/GetTask":                  {"project_id", "task_id", "scope"},
	"/hcmnext.project.v1.ProjectService/ListTasks":                {"project_id", "scope"},
	"/hcmnext.project.v1.ProjectService/SearchTasks":              {"project_id", "scope"},
	"/hcmnext.project.v1.ProjectService/MoveTask":                 {"idempotency_key", "project_id", "task_id", "target_status_id", "expected_task_revision", "expected_workflow_revision", "scope"},
	"/hcmnext.project.v1.ProjectService/PatchTask":                {"idempotency_key", "project_id", "task_id", "expected_task_revision", "expected_workflow_revision", "scope"},
	"/hcmnext.project.v1.ProjectService/ArchiveTask":              {"idempotency_key", "project_id", "task_id", "expected_task_revision", "expected_workflow_revision", "scope"},
	"/hcmnext.project.v1.ProjectService/RestoreTask":              {"idempotency_key", "project_id", "task_id", "expected_task_revision", "expected_workflow_revision", "scope"},
	"/hcmnext.project.v1.ProjectService/SaveWorkflowDraft":        {"idempotency_key", "project_id", "configuration", "scope"},
	"/hcmnext.project.v1.ProjectService/GetWorkflowDraft":         {"draft_id", "project_id", "scope"},
	"/hcmnext.project.v1.ProjectService/PreviewWorkflowDraft":     {"draft_id", "project_id", "expected_draft_revision", "scope"},
	"/hcmnext.project.v1.ProjectService/PublishWorkflowDraft":     {"idempotency_key", "project_id", "draft_id", "expected_project_workflow_revision", "expected_draft_revision", "reviewed_digest", "scope"},
	"/hcmnext.project.v1.ProjectService/GetWorkflowConfiguration": {"project_id", "scope"},
	"/hcmnext.project.v1.ProjectService/SaveBoardView":            {"idempotency_key", "project_id", "view", "scope"},
	"/hcmnext.project.v1.ProjectService/ListBoardViews":           {"project_id", "scope"},
	"/hcmnext.project.v1.ProjectService/GetBoard":                 {"project_id", "view_id", "scope"},
	"/hcmnext.project.v1.ProjectService/AddTaskLink":              {"idempotency_key", "project_id", "task_id", "reference", "expected_task_revision", "scope"},
	"/hcmnext.project.v1.ProjectService/ListTaskLinks":            {"project_id", "task_id", "scope"},
	"/hcmnext.project.v1.ProjectService/ListTaskLinksByTarget":    {"scope", "target"},
	"/hcmnext.project.v1.ProjectService/RemoveTaskLink":           {"idempotency_key", "project_id", "task_id", "link_id", "expected_task_revision", "scope"},
	"/hcmnext.project.v1.ProjectService/AddTaskComment":           {"idempotency_key", "project_id", "task_id", "body_text", "scope"},
	"/hcmnext.project.v1.ProjectService/EditTaskComment":          {"idempotency_key", "project_id", "task_id", "comment_id", "expected_revision", "body_text", "scope"},
	"/hcmnext.project.v1.ProjectService/DeleteTaskComment":        {"idempotency_key", "project_id", "task_id", "comment_id", "expected_revision", "scope"},
	"/hcmnext.project.v1.ProjectService/ListTaskComments":         {"project_id", "task_id", "page_size", "scope"},
	"/hcmnext.project.v1.ProjectService/ListTaskActivity":         {"project_id", "task_id", "page_size", "scope"},

	"/hcmnext.workorder.v1.WorkOrderService/CreateWorkOrder":        {"idempotency_key", "project_id", "title", "scope", "template_id", "template_version"},
	"/hcmnext.workorder.v1.WorkOrderService/GetWorkOrder":           {"work_order_id", "scope_context"},
	"/hcmnext.workorder.v1.WorkOrderService/ListWorkOrders":         {"project_id", "page", "scope_context"},
	"/hcmnext.workorder.v1.WorkOrderService/SubmitInitiatorRequest": {"idempotency_key", "work_order_id", "expected_revision", "kind", "details", "scope_context"},
	"/hcmnext.workorder.v1.WorkOrderService/DecideInitiatorRequest": {"idempotency_key", "work_order_id", "request_id", "expected_revision", "decision", "scope_context"},
	"/hcmnext.workorder.v1.WorkOrderService/AddWorkOrderNote":       {"idempotency_key", "work_order_id", "expected_revision", "body", "scope_context"},
	"/hcmnext.workorder.v1.WorkOrderService/RequestPhaseTransition": {"idempotency_key", "work_order_id", "expected_revision", "target_phase_id", "scope_context"},
	"/hcmnext.workorder.v1.WorkOrderService/RecordWorkEntry":        {"idempotency_key", "work_order_id", "expected_revision", "worker_id", "kind", "work_date", "scope_context"},
	"/hcmnext.workorder.v1.WorkOrderService/RecordProgressEntry":    {"idempotency_key", "work_order_id", "expected_revision", "line_id", "completed_quantity", "scope_context"},
	"/hcmnext.workorder.v1.WorkOrderService/RecordSpendEntry":       {"idempotency_key", "work_order_id", "expected_revision", "category", "disposition", "amount", "scope_context"},
	"/hcmnext.workorder.v1.WorkOrderService/RequestWorkOrderReport": {"idempotency_key", "work_order_id", "expected_revision", "kind", "definition_id", "definition_version", "scope_context"},
	"/hcmnext.workorder.v1.WorkOrderService/RequestBillingDraft":    {"idempotency_key", "work_order_id", "expected_revision", "contract_version", "period_start", "period_end", "scope_context"},
}

// RequiredFieldPaths returns the required request field paths for one gRPC
// procedure path, sorted for determinism. A method with no required fields
// (a bare list/discovery read) returns an empty, non-nil slice so a caller
// can distinguish "no requirement" from "unknown method".
func RequiredFieldPaths(procedure string) ([]string, bool) {
	paths, ok := requiredFieldPaths[procedure]
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(paths))
	out = append(out, paths...)
	sortStrings(out)
	return out, true
}
