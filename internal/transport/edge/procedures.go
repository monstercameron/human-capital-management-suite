package edge

import (
	"sort"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	dataopsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/dataops/v1"
	evidencev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/evidence/v1"
	humanworkv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/humanwork/v1"
	integrationv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/integration/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	registryv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/registry/v1"
	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
	workorderv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workorder/v1"
	transporthumanwork "github.com/monstercameron/human-capital-management-suite/internal/transport/humanwork"
	transportjourney "github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
	transportoperations "github.com/monstercameron/human-capital-management-suite/internal/transport/operations"
	transportworkflow "github.com/monstercameron/human-capital-management-suite/internal/transport/workflow"
	transportworkorder "github.com/monstercameron/human-capital-management-suite/internal/transport/workorder"
)

// Procedure paths. They are the gRPC method names verbatim, so one method
// vocabulary covers both transports: the same string appears in
// grpc.UnaryServerInfo.FullMethod, in connect.Spec.Procedure, in
// transport.Invocation.Method and in every log record and endpoint rule.
const (
	ProcedureCreateIntent          = "/hcmnext.intents.v1.IntentService/CreateIntent"
	ProcedureGetIntent             = "/hcmnext.intents.v1.IntentService/GetIntent"
	ProcedureListIntents           = "/hcmnext.intents.v1.IntentService/ListIntents"
	ProcedureSimulateIntent        = "/hcmnext.intents.v1.IntentService/SimulateIntent"
	ProcedureExecuteIntent         = "/hcmnext.intents.v1.IntentService/ExecuteIntent"
	ProcedureSubmitIntent          = "/hcmnext.intents.v1.IntentService/SubmitIntent"
	ProcedureCancelIntent          = "/hcmnext.intents.v1.IntentService/CancelIntent"
	ProcedureSupersedeIntent       = "/hcmnext.intents.v1.IntentService/SupersedeIntent"
	ProcedureExplainIntent         = "/hcmnext.intents.v1.IntentService/ExplainIntent"
	ProcedureListIntentTimeline    = "/hcmnext.intents.v1.IntentService/ListIntentTimeline"
	ProcedureRecommendIntentAction = "/hcmnext.intents.v1.IntentService/RecommendIntentAction"
	ProcedureGetIntentDeepLink     = "/hcmnext.intents.v1.IntentService/GetIntentDeepLink"
	ProcedureInspectIntentFields   = "/hcmnext.intents.v1.IntentService/InspectIntentFields"
	ProcedureExportIntentFields    = "/hcmnext.intents.v1.IntentService/ExportIntentFields"

	ProcedureListIntentDefinitions = "/hcmnext.registry.v1.RegistryService/ListIntentDefinitions"
	ProcedureGetIntentDefinition   = "/hcmnext.registry.v1.RegistryService/GetIntentDefinition"
	ProcedureListCapabilities      = "/hcmnext.registry.v1.RegistryService/ListCapabilities"
	ProcedureGetCapability         = "/hcmnext.registry.v1.RegistryService/GetCapability"

	ProcedureExplainFieldHistory      = "/hcmnext.dataops.v1.DataOpsService/ExplainFieldHistory"
	ProcedureDiffRecord               = "/hcmnext.dataops.v1.DataOpsService/DiffRecord"
	ProcedureCreateRepairPlan         = "/hcmnext.dataops.v1.DataOpsService/CreateRepairPlan"
	ProcedureSimulateRepair           = "/hcmnext.dataops.v1.DataOpsService/SimulateRepair"
	ProcedureListConnectorDefinitions = "/hcmnext.integration.v1.IntegrationService/ListConnectorDefinitions"
	ProcedureGetConnectorDefinition   = "/hcmnext.integration.v1.IntegrationService/GetConnectorDefinition"
	ProcedureListConnectorConnections = "/hcmnext.integration.v1.IntegrationService/ListConnectorConnections"
	ProcedureGetConnectorConnection   = "/hcmnext.integration.v1.IntegrationService/GetConnectorConnection"
	ProcedureTestConnectorConnection  = "/hcmnext.integration.v1.IntegrationService/TestConnectorConnection"
	ProcedureListExternalObservations = "/hcmnext.integration.v1.IntegrationService/ListExternalObservations"
	ProcedureGetExternalObservation   = "/hcmnext.integration.v1.IntegrationService/GetExternalObservation"
)

// requestFactories maps a procedure to a constructor for its request message.
// The strict JSON screen needs the target type before connect decodes the
// body, because "unknown field" is a statement about a specific descriptor.
var requestFactories = map[string]func() proto.Message{
	ProcedureCreateIntent:          func() proto.Message { return &intentsv1.CreateIntentRequest{} },
	ProcedureGetIntent:             func() proto.Message { return &intentsv1.GetIntentRequest{} },
	ProcedureListIntents:           func() proto.Message { return &intentsv1.ListIntentsRequest{} },
	ProcedureSimulateIntent:        func() proto.Message { return &intentsv1.SimulateIntentRequest{} },
	ProcedureExecuteIntent:         func() proto.Message { return &intentsv1.ExecuteIntentRequest{} },
	ProcedureSubmitIntent:          func() proto.Message { return &intentsv1.SubmitIntentRequest{} },
	ProcedureCancelIntent:          func() proto.Message { return &intentsv1.CancelIntentRequest{} },
	ProcedureSupersedeIntent:       func() proto.Message { return &intentsv1.SupersedeIntentRequest{} },
	ProcedureExplainIntent:         func() proto.Message { return &intentsv1.ExplainIntentRequest{} },
	ProcedureListIntentTimeline:    func() proto.Message { return &intentsv1.ListIntentTimelineRequest{} },
	ProcedureRecommendIntentAction: func() proto.Message { return &intentsv1.RecommendIntentActionRequest{} },
	ProcedureGetIntentDeepLink:     func() proto.Message { return &intentsv1.GetIntentDeepLinkRequest{} },
	ProcedureInspectIntentFields:   func() proto.Message { return &intentsv1.InspectIntentFieldsRequest{} },
	ProcedureExportIntentFields:    func() proto.Message { return &intentsv1.ExportIntentFieldsRequest{} },

	ProcedureListIntentDefinitions:                  func() proto.Message { return &registryv1.ListIntentDefinitionsRequest{} },
	ProcedureGetIntentDefinition:                    func() proto.Message { return &registryv1.GetIntentDefinitionRequest{} },
	ProcedureListCapabilities:                       func() proto.Message { return &registryv1.ListCapabilitiesRequest{} },
	ProcedureGetCapability:                          func() proto.Message { return &registryv1.GetCapabilityRequest{} },
	ProcedureExplainFieldHistory:                    func() proto.Message { return &dataopsv1.ExplainFieldHistoryRequest{} },
	ProcedureDiffRecord:                             func() proto.Message { return &dataopsv1.DiffRecordRequest{} },
	ProcedureCreateRepairPlan:                       func() proto.Message { return &dataopsv1.CreateRepairPlanRequest{} },
	ProcedureSimulateRepair:                         func() proto.Message { return &dataopsv1.SimulateRepairRequest{} },
	ProcedureListConnectorDefinitions:               func() proto.Message { return &integrationv1.ListConnectorDefinitionsRequest{} },
	ProcedureGetConnectorDefinition:                 func() proto.Message { return &integrationv1.GetConnectorDefinitionRequest{} },
	ProcedureListConnectorConnections:               func() proto.Message { return &integrationv1.ListConnectorConnectionsRequest{} },
	ProcedureGetConnectorConnection:                 func() proto.Message { return &integrationv1.GetConnectorConnectionRequest{} },
	ProcedureTestConnectorConnection:                func() proto.Message { return &integrationv1.TestConnectorConnectionRequest{} },
	ProcedureListExternalObservations:               func() proto.Message { return &integrationv1.ListExternalObservationsRequest{} },
	ProcedureGetExternalObservation:                 func() proto.Message { return &integrationv1.GetExternalObservationRequest{} },
	transportjourney.ProposePromotionProcedure:      func() proto.Message { return &journeyv1.ProposePromotionRequest{} },
	transportjourney.ProposeIntoManagementProcedure: func() proto.Message { return &journeyv1.ProposePromotionRequest{} },
	transportjourney.CorrectWorkLoopProcedure:       func() proto.Message { return &structpb.Struct{} },
	transportworkflow.GetWorkflowProcedure:          func() proto.Message { return &workflowv1.GetWorkflowRequest{} },
	transportworkflow.ListNodeExecutionsProcedure:   func() proto.Message { return &workflowv1.ListNodeExecutionsRequest{} },
	transportworkflow.ListWorkflowPublicationsProcedure: func() proto.Message {
		return &workflowv1.ListWorkflowPublicationsRequest{}
	},
	transportworkflow.GetWorkflowDefinitionViewProcedure: func() proto.Message {
		return &workflowv1.GetWorkflowDefinitionViewRequest{}
	},
	transportworkflow.CompileWorkflowDraftProcedure: func() proto.Message {
		return &workflowv1.CompileWorkflowDraftRequest{}
	},
	transportworkflow.ListWorkflowBlocksProcedure: func() proto.Message {
		return &workflowv1.ListWorkflowBlocksRequest{}
	},
	transportworkflow.CreateWorkflowDraftProcedure: func() proto.Message {
		return &workflowv1.CreateWorkflowDraftRequest{}
	},
	transportworkflow.GetWorkflowDraftProcedure: func() proto.Message {
		return &workflowv1.GetWorkflowDraftRequest{}
	},
	transportworkflow.InsertWorkflowPaletteEntryProcedure: func() proto.Message {
		return &workflowv1.InsertWorkflowPaletteEntryRequest{}
	},
	transportworkflow.UpdateWorkflowDraftNodeProcedure: func() proto.Message {
		return &workflowv1.UpdateWorkflowDraftNodeRequest{}
	},
	transportworkflow.SetWorkflowDraftOutcomeProcedure: func() proto.Message {
		return &workflowv1.SetWorkflowDraftOutcomeRequest{}
	},
	transportworkflow.BindWorkflowDraftInputProcedure: func() proto.Message {
		return &workflowv1.BindWorkflowDraftInputRequest{}
	},
	transportworkflow.MoveWorkflowDraftNodeProcedure: func() proto.Message {
		return &workflowv1.MoveWorkflowDraftNodeRequest{}
	},
	transportworkflow.RemoveWorkflowDraftNodeProcedure: func() proto.Message {
		return &workflowv1.RemoveWorkflowDraftNodeRequest{}
	},
	transportworkflow.ClearWorkflowDraftOutcomeProcedure: func() proto.Message {
		return &workflowv1.ClearWorkflowDraftOutcomeRequest{}
	},
	transportworkflow.RenameWorkflowDraftProcedure: func() proto.Message {
		return &workflowv1.RenameWorkflowDraftRequest{}
	},
	transportworkflow.NavigateWorkflowDraftHistoryProcedure: func() proto.Message {
		return &workflowv1.NavigateWorkflowDraftHistoryRequest{}
	},
	transportworkflow.ApplyWorkflowTemplateOverlayProcedure: func() proto.Message {
		return &workflowv1.ApplyWorkflowTemplateOverlayRequest{}
	},
	transportworkflow.PauseWorkflowProcedure:  func() proto.Message { return &workflowv1.PauseWorkflowRequest{} },
	transportworkflow.ResumeWorkflowProcedure: func() proto.Message { return &workflowv1.ResumeWorkflowRequest{} },
	transportworkflow.CancelWorkflowProcedure: func() proto.Message { return &workflowv1.CancelWorkflowRequest{} },
	transportworkflow.RetryNodeProcedure:      func() proto.Message { return &workflowv1.RetryNodeRequest{} },
	transporthumanwork.ListWorkItemsProcedure: func() proto.Message { return &humanworkv1.ListWorkItemsRequest{} },
	transporthumanwork.GetWorkItemProcedure:   func() proto.Message { return &humanworkv1.GetWorkItemRequest{} },
	transporthumanwork.GetThresholdTableProcedure: func() proto.Message {
		return &humanworkv1.GetThresholdTableRequest{}
	},
	transportoperations.GetOperationProcedure:          func() proto.Message { return &evidencev1.GetOperationRequest{} },
	transportoperations.CancelOperationProcedure:       func() proto.Message { return &evidencev1.CancelOperationRequest{} },
	transportworkorder.CreateWorkOrderProcedure:        func() proto.Message { return &workorderv1.CreateWorkOrderRequest{} },
	transportworkorder.GetWorkOrderProcedure:           func() proto.Message { return &workorderv1.GetWorkOrderRequest{} },
	transportworkorder.ListWorkOrdersProcedure:         func() proto.Message { return &workorderv1.ListWorkOrdersRequest{} },
	transportworkorder.SubmitInitiatorRequestProcedure: func() proto.Message { return &workorderv1.SubmitInitiatorRequestRequest{} },
	transportworkorder.DecideInitiatorRequestProcedure: func() proto.Message { return &workorderv1.DecideInitiatorRequestRequest{} },
	transportworkorder.AddWorkOrderNoteProcedure:       func() proto.Message { return &workorderv1.AddWorkOrderNoteRequest{} },
	transportworkorder.RequestPhaseTransitionProcedure: func() proto.Message { return &workorderv1.RequestPhaseTransitionRequest{} },
	transportworkorder.RecordWorkEntryProcedure:        func() proto.Message { return &workorderv1.RecordWorkEntryRequest{} },
	transportworkorder.RecordProgressEntryProcedure:    func() proto.Message { return &workorderv1.RecordProgressEntryRequest{} },
	transportworkorder.RecordSpendEntryProcedure:       func() proto.Message { return &workorderv1.RecordSpendEntryRequest{} },
	transportworkorder.RequestWorkOrderReportProcedure: func() proto.Message { return &workorderv1.RequestWorkOrderReportRequest{} },
	transportworkorder.RequestBillingDraftProcedure:    func() proto.Message { return &workorderv1.RequestBillingDraftRequest{} },
}

// Procedures returns every procedure path this edge publishes.
func Procedures() []string {
	// Intent and Registry methods are the stable discovery inventory used by
	// endpoint parity and route conformance checks.
	procedures := []string{
		ProcedureCreateIntent, ProcedureGetIntent, ProcedureListIntents,
		ProcedureSimulateIntent, ProcedureExecuteIntent, ProcedureSubmitIntent,
		ProcedureCancelIntent, ProcedureSupersedeIntent, ProcedureExplainIntent,
		ProcedureListIntentTimeline, ProcedureRecommendIntentAction,
		ProcedureGetIntentDeepLink, ProcedureInspectIntentFields,
		ProcedureExportIntentFields, ProcedureListIntentDefinitions,
		ProcedureGetIntentDefinition, ProcedureListCapabilities, ProcedureGetCapability,
	}
	out := append([]string(nil), procedures...)
	sort.Strings(out)
	return out
}
