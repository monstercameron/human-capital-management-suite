package openapi

// The served surface is composed in internal/transport (cell.go for the gRPC
// server, edge/handler.go for the Connect edge), and neither exports a list of
// what it mounts. These checked-in inventories are therefore the generator's
// input, and exposure_test.go proves each one against the real composition:
// it builds the cell's gRPC server and reads GetServiceInfo, and it builds the
// cell's HTTP edge and probes every declared procedure path. A drift in either
// direction fails that test, not silently the document.

// registeredServices returns the full names of the hcmnext services the
// cell's gRPC server registers (internal/transport/cell.
// NewGRPCServerWithWorkflowInspectorAndOperations and grpcserver.NewServer).
func registeredServices() []string {
	return []string{
		"hcmnext.admin.v1.AdminService",
		"hcmnext.evidence.v1.OperationsService",
		"hcmnext.humanwork.v1.WorkService",
		"hcmnext.intents.v1.IntentService",
		"hcmnext.journey.v1.JourneyService",
		"hcmnext.registry.v1.RegistryService",
		"hcmnext.workflow.v1.WorkflowService",
	}
}

// httpExposedProcedures returns the Protobuf procedure paths the cell's
// Connect edge mounts (internal/transport/edge.NewHandler as composed by
// internal/transport/cell).
func httpExposedProcedures() []string {
	return []string{
		"/hcmnext.evidence.v1.OperationsService/CancelOperation",
		"/hcmnext.evidence.v1.OperationsService/GetOperation",
		"/hcmnext.humanwork.v1.WorkService/GetWorkItem",
		"/hcmnext.humanwork.v1.WorkService/ListWorkItems",
		"/hcmnext.intents.v1.IntentService/CancelIntent",
		"/hcmnext.intents.v1.IntentService/CreateIntent",
		"/hcmnext.intents.v1.IntentService/ExecuteIntent",
		"/hcmnext.intents.v1.IntentService/ExplainIntent",
		"/hcmnext.intents.v1.IntentService/GetIntent",
		"/hcmnext.intents.v1.IntentService/ListIntentTimeline",
		"/hcmnext.intents.v1.IntentService/ListIntents",
		"/hcmnext.intents.v1.IntentService/SimulateIntent",
		"/hcmnext.intents.v1.IntentService/SubmitIntent",
		"/hcmnext.intents.v1.IntentService/SupersedeIntent",
		"/hcmnext.journey.v1.JourneyService/ProposePromotion",
		"/hcmnext.registry.v1.RegistryService/GetCapability",
		"/hcmnext.registry.v1.RegistryService/GetIntentDefinition",
		"/hcmnext.registry.v1.RegistryService/ListCapabilities",
		"/hcmnext.registry.v1.RegistryService/ListIntentDefinitions",
		"/hcmnext.workflow.v1.WorkflowService/ApplyWorkflowTemplateOverlay",
		"/hcmnext.workflow.v1.WorkflowService/BindWorkflowDraftInput",
		"/hcmnext.workflow.v1.WorkflowService/CancelWorkflow",
		"/hcmnext.workflow.v1.WorkflowService/CompileWorkflowDraft",
		"/hcmnext.workflow.v1.WorkflowService/CreateWorkflowDraft",
		"/hcmnext.workflow.v1.WorkflowService/GetWorkflow",
		"/hcmnext.workflow.v1.WorkflowService/GetWorkflowDefinitionView",
		"/hcmnext.workflow.v1.WorkflowService/GetWorkflowDraft",
		"/hcmnext.workflow.v1.WorkflowService/InsertWorkflowPaletteEntry",
		"/hcmnext.workflow.v1.WorkflowService/ListNodeExecutions",
		"/hcmnext.workflow.v1.WorkflowService/ListWorkflowBlocks",
		"/hcmnext.workflow.v1.WorkflowService/ListWorkflowPublications",
		"/hcmnext.workflow.v1.WorkflowService/MoveWorkflowDraftNode",
		"/hcmnext.workflow.v1.WorkflowService/NavigateWorkflowDraftHistory",
		"/hcmnext.workflow.v1.WorkflowService/PauseWorkflow",
		"/hcmnext.workflow.v1.WorkflowService/ResumeWorkflow",
		"/hcmnext.workflow.v1.WorkflowService/RetryNode",
		"/hcmnext.workflow.v1.WorkflowService/SetWorkflowDraftOutcome",
		"/hcmnext.workflow.v1.WorkflowService/UpdateWorkflowDraftNode",
	}
}

// httpAliases returns the non-Protobuf HTTP routes the edge mounts for a
// procedure, keyed by the procedure path. The alias takes the same request
// message (edge's requestFactories binds it to ProposePromotionRequest).
func httpAliases() map[string][]string {
	return map[string][]string{
		"/hcmnext.journey.v1.JourneyService/ProposePromotion": {"/v1/promotions:proposeIntoManagement"},
	}
}

// setOf indexes a string list.
func setOf(list []string) map[string]bool {
	out := make(map[string]bool, len(list))
	for _, s := range list {
		out[s] = true
	}
	return out
}
