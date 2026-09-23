package transport

import (
	"context"

	dataopsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/dataops/v1"
	integrationv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/integration/v1"
	intentsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/intents/v1"
	notificationv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/notification/v1"
	registryv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/registry/v1"
)

// IntentHandler is the port the BusinessIntent lifecycle surface delegates to.
// The application service that implements it (internal/intent, wired later)
// owns every rule; the transport owns none.
//
// The contract for an implementation is narrow and deliberate:
//
//   - The authenticated principal is read from the context with
//     trust.FromContext, and the request's own scope, initiator and other
//     trusted fields have already been overwritten server-side. An
//     implementation must never read ambient headers or gRPC metadata; it has
//     no access to them.
//   - The context deadline is the effective, server-capped deadline.
//     Cancellation propagates; an implementation returns promptly on ctx.Done.
//   - A returned error should be an *envelope.Error so that its owned code,
//     field paths, retry classification and evidence reference project
//     identically on every channel. Any other error is coerced to a generic
//     UNAVAILABLE with the original nested as an unprojected diagnostic.
type IntentHandler interface {
	CreateIntent(ctx context.Context, req *intentsv1.CreateIntentRequest) (*intentsv1.CreateIntentResponse, error)
	GetIntent(ctx context.Context, req *intentsv1.GetIntentRequest) (*intentsv1.GetIntentResponse, error)
	ListIntents(ctx context.Context, req *intentsv1.ListIntentsRequest) (*intentsv1.ListIntentsResponse, error)
	SimulateIntent(ctx context.Context, req *intentsv1.SimulateIntentRequest) (*intentsv1.SimulateIntentResponse, error)
	ExecuteIntent(ctx context.Context, req *intentsv1.ExecuteIntentRequest) (*intentsv1.ExecuteIntentResponse, error)
	SubmitIntent(ctx context.Context, req *intentsv1.SubmitIntentRequest) (*intentsv1.SubmitIntentResponse, error)
	CancelIntent(ctx context.Context, req *intentsv1.CancelIntentRequest) (*intentsv1.CancelIntentResponse, error)
	SupersedeIntent(ctx context.Context, req *intentsv1.SupersedeIntentRequest) (*intentsv1.SupersedeIntentResponse, error)
	ExplainIntent(ctx context.Context, req *intentsv1.ExplainIntentRequest) (*intentsv1.ExplainIntentResponse, error)
	ListIntentTimeline(ctx context.Context, req *intentsv1.ListIntentTimelineRequest) (*intentsv1.ListIntentTimelineResponse, error)
}

// RegistryHandler is the port the authorized discovery surface delegates to.
// The same contract as [IntentHandler] applies.
type RegistryHandler interface {
	ListIntentDefinitions(ctx context.Context, req *registryv1.ListIntentDefinitionsRequest) (*registryv1.ListIntentDefinitionsResponse, error)
	GetIntentDefinition(ctx context.Context, req *registryv1.GetIntentDefinitionRequest) (*registryv1.GetIntentDefinitionResponse, error)
	ListCapabilities(ctx context.Context, req *registryv1.ListCapabilitiesRequest) (*registryv1.ListCapabilitiesResponse, error)
	GetCapability(ctx context.Context, req *registryv1.GetCapabilityRequest) (*registryv1.GetCapabilityResponse, error)
}

// DataOpsHandler is the port the DataOps service surface delegates to. The
// application service that implements it owns every rule; the transport owns
// none. The same contract as [IntentHandler] applies.
type DataOpsHandler interface {
	ExplainFieldHistory(ctx context.Context, req *dataopsv1.ExplainFieldHistoryRequest) (*dataopsv1.ExplainFieldHistoryResponse, error)
	DiffRecord(ctx context.Context, req *dataopsv1.DiffRecordRequest) (*dataopsv1.DiffRecordResponse, error)
	CreateRepairPlan(ctx context.Context, req *dataopsv1.CreateRepairPlanRequest) (*dataopsv1.CreateRepairPlanResponse, error)
	SimulateRepair(ctx context.Context, req *dataopsv1.SimulateRepairRequest) (*dataopsv1.SimulateRepairResponse, error)
}

// IntegrationHandler is the port the Integration service surface delegates
// to. The application service that implements it owns every rule; the
// transport owns none. The same contract as [IntentHandler] applies.
type IntegrationHandler interface {
	ListConnectorDefinitions(ctx context.Context, req *integrationv1.ListConnectorDefinitionsRequest) (*integrationv1.ListConnectorDefinitionsResponse, error)
	GetConnectorDefinition(ctx context.Context, req *integrationv1.GetConnectorDefinitionRequest) (*integrationv1.GetConnectorDefinitionResponse, error)
	ListConnectorConnections(ctx context.Context, req *integrationv1.ListConnectorConnectionsRequest) (*integrationv1.ListConnectorConnectionsResponse, error)
	GetConnectorConnection(ctx context.Context, req *integrationv1.GetConnectorConnectionRequest) (*integrationv1.GetConnectorConnectionResponse, error)
	TestConnectorConnection(ctx context.Context, req *integrationv1.TestConnectorConnectionRequest) (*integrationv1.TestConnectorConnectionResponse, error)
	ListExternalObservations(ctx context.Context, req *integrationv1.ListExternalObservationsRequest) (*integrationv1.ListExternalObservationsResponse, error)
	GetExternalObservation(ctx context.Context, req *integrationv1.GetExternalObservationRequest) (*integrationv1.GetExternalObservationResponse, error)
}

// NotificationHandler is the port the recipient-owned notification feed
// delegates to. The application service that implements it owns every rule;
// the transport owns none. The same contract as [IntentHandler] applies.
type NotificationHandler interface {
	ListNotifications(ctx context.Context, req *notificationv1.ListNotificationsRequest) (*notificationv1.ListNotificationsResponse, error)
	MarkNotificationRead(ctx context.Context, req *notificationv1.MarkNotificationReadRequest) (*notificationv1.MarkNotificationReadResponse, error)
	ArchiveNotification(ctx context.Context, req *notificationv1.ArchiveNotificationRequest) (*notificationv1.ArchiveNotificationResponse, error)
	PinNotification(ctx context.Context, req *notificationv1.PinNotificationRequest) (*notificationv1.PinNotificationResponse, error)
}
