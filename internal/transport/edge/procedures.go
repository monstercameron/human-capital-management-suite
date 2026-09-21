package edge

import (
	"sort"

	"google.golang.org/protobuf/proto"

	evidencev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/evidence/v1"
	humanworkv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/humanwork/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	registryv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/registry/v1"
	workflowv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/workflow/v1"
	transporthumanwork "github.com/monstercameron/human-capital-management-suite/internal/transport/humanwork"
	transportjourney "github.com/monstercameron/human-capital-management-suite/internal/transport/journey"
	transportoperations "github.com/monstercameron/human-capital-management-suite/internal/transport/operations"
	transportworkflow "github.com/monstercameron/human-capital-management-suite/internal/transport/workflow"
)

// Procedure paths. They are the gRPC method names verbatim, so one method
// vocabulary covers both transports: the same string appears in
// grpc.UnaryServerInfo.FullMethod, in connect.Spec.Procedure, in
// transport.Invocation.Method and in every log record and endpoint rule.
const (
	ProcedureCreateIntent       = "/hcmnext.intents.v1.IntentService/CreateIntent"
	ProcedureGetIntent          = "/hcmnext.intents.v1.IntentService/GetIntent"
	ProcedureListIntents        = "/hcmnext.intents.v1.IntentService/ListIntents"
	ProcedureSimulateIntent     = "/hcmnext.intents.v1.IntentService/SimulateIntent"
	ProcedureExecuteIntent      = "/hcmnext.intents.v1.IntentService/ExecuteIntent"
	ProcedureSubmitIntent       = "/hcmnext.intents.v1.IntentService/SubmitIntent"
	ProcedureCancelIntent       = "/hcmnext.intents.v1.IntentService/CancelIntent"
	ProcedureSupersedeIntent    = "/hcmnext.intents.v1.IntentService/SupersedeIntent"
	ProcedureExplainIntent      = "/hcmnext.intents.v1.IntentService/ExplainIntent"
	ProcedureListIntentTimeline = "/hcmnext.intents.v1.IntentService/ListIntentTimeline"

	ProcedureListIntentDefinitions = "/hcmnext.registry.v1.RegistryService/ListIntentDefinitions"
	ProcedureGetIntentDefinition   = "/hcmnext.registry.v1.RegistryService/GetIntentDefinition"
	ProcedureListCapabilities      = "/hcmnext.registry.v1.RegistryService/ListCapabilities"
	ProcedureGetCapability         = "/hcmnext.registry.v1.RegistryService/GetCapability"
)

// requestFactories maps a procedure to a constructor for its request message.
// The strict JSON screen needs the target type before connect decodes the
// body, because "unknown field" is a statement about a specific descriptor.
var requestFactories = map[string]func() proto.Message{
	ProcedureCreateIntent:       func() proto.Message { return &intentsv1.CreateIntentRequest{} },
	ProcedureGetIntent:          func() proto.Message { return &intentsv1.GetIntentRequest{} },
	ProcedureListIntents:        func() proto.Message { return &intentsv1.ListIntentsRequest{} },
	ProcedureSimulateIntent:     func() proto.Message { return &intentsv1.SimulateIntentRequest{} },
	ProcedureExecuteIntent:      func() proto.Message { return &intentsv1.ExecuteIntentRequest{} },
	ProcedureSubmitIntent:       func() proto.Message { return &intentsv1.SubmitIntentRequest{} },
	ProcedureCancelIntent:       func() proto.Message { return &intentsv1.CancelIntentRequest{} },
	ProcedureSupersedeIntent:    func() proto.Message { return &intentsv1.SupersedeIntentRequest{} },
	ProcedureExplainIntent:      func() proto.Message { return &intentsv1.ExplainIntentRequest{} },
	ProcedureListIntentTimeline: func() proto.Message { return &intentsv1.ListIntentTimelineRequest{} },

	ProcedureListIntentDefinitions:                  func() proto.Message { return &registryv1.ListIntentDefinitionsRequest{} },
	ProcedureGetIntentDefinition:                    func() proto.Message { return &registryv1.GetIntentDefinitionRequest{} },
	ProcedureListCapabilities:                       func() proto.Message { return &registryv1.ListCapabilitiesRequest{} },
	ProcedureGetCapability:                          func() proto.Message { return &registryv1.GetCapabilityRequest{} },
	transportjourney.ProposePromotionProcedure:      func() proto.Message { return &journeyv1.ProposePromotionRequest{} },
	transportjourney.ProposeIntoManagementProcedure: func() proto.Message { return &journeyv1.ProposePromotionRequest{} },
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
	transportworkflow.NavigateWorkflowDraftHistoryProcedure: func() proto.Message {
		return &workflowv1.NavigateWorkflowDraftHistoryRequest{}
	},
	transportworkflow.ApplyWorkflowTemplateOverlayProcedure: func() proto.Message {
		return &workflowv1.ApplyWorkflowTemplateOverlayRequest{}
	},
	transportworkflow.PauseWorkflowProcedure:     func() proto.Message { return &workflowv1.PauseWorkflowRequest{} },
	transportworkflow.ResumeWorkflowProcedure:    func() proto.Message { return &workflowv1.ResumeWorkflowRequest{} },
	transportworkflow.CancelWorkflowProcedure:    func() proto.Message { return &workflowv1.CancelWorkflowRequest{} },
	transportworkflow.RetryNodeProcedure:         func() proto.Message { return &workflowv1.RetryNodeRequest{} },
	transporthumanwork.ListWorkItemsProcedure:    func() proto.Message { return &humanworkv1.ListWorkItemsRequest{} },
	transporthumanwork.GetWorkItemProcedure:      func() proto.Message { return &humanworkv1.GetWorkItemRequest{} },
	transportoperations.GetOperationProcedure:    func() proto.Message { return &evidencev1.GetOperationRequest{} },
	transportoperations.CancelOperationProcedure: func() proto.Message { return &evidencev1.CancelOperationRequest{} },
}

// Procedures returns every procedure path this edge publishes.
func Procedures() []string {
	// Procedures is the established discovery inventory for the Intent and
	// Registry edge. Optional inspection/operations surfaces are mounted when
	// composed, but are intentionally not folded into this legacy inventory:
	// its callers use the fixed 14-method endpoint parity fixture.
	legacy := []string{
		ProcedureCreateIntent, ProcedureGetIntent, ProcedureListIntents,
		ProcedureSimulateIntent, ProcedureExecuteIntent, ProcedureSubmitIntent,
		ProcedureCancelIntent, ProcedureSupersedeIntent, ProcedureExplainIntent,
		ProcedureListIntentTimeline, ProcedureListIntentDefinitions,
		ProcedureGetIntentDefinition, ProcedureListCapabilities, ProcedureGetCapability,
	}
	out := make([]string, 0, len(legacy))
	out = append(out, legacy...)
	sort.Strings(out)
	return out
}
