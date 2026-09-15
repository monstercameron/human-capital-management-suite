package journeyclient

import (
	"context"

	journeyv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/journey/v1"
	"google.golang.org/grpc"
)

// Service is the workflow page's view of the cell: the eight workflow operations
// hcmnext.journey.v1.JourneyService publishes, with the call options and the
// credential already dealt with.
//
// It exists as an interface for one reason -- so [App] can be driven by a
// fake in an ordinary `go test` with no server, no tunnel and no browser.
// The production implementation ([NewGRPCService]) is thin enough to read in
// one screen precisely because everything worth testing is on the other side
// of this seam.
type Service interface {
	ListJourneys(ctx context.Context, in *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error)
	ProposePromotion(ctx context.Context, in *journeyv1.ProposePromotionRequest) (*journeyv1.ProposePromotionResponse, error)
	InspectJourney(ctx context.Context, in *journeyv1.InspectJourneyRequest) (*journeyv1.InspectJourneyResponse, error)
	ExecuteJourney(ctx context.Context, in *journeyv1.ExecuteJourneyRequest) (*journeyv1.ExecuteJourneyResponse, error)
	DecideJourney(ctx context.Context, in *journeyv1.DecideJourneyRequest) (*journeyv1.DecideJourneyResponse, error)
	// EditProposal corrects an unstarted or mid-flight proposal (PROMOUX-013):
	// it cancels the original and mints an edited successor, and returns the
	// successor journey plus the original's own id.
	EditProposal(ctx context.Context, in *journeyv1.EditProposalRequest) (*journeyv1.EditProposalResponse, error)
	// PreviewJourneyIntervention answers, without mutating anything, whether
	// a typed WITHDRAW or CANCEL intervention is available right now and
	// what confirming it would do.
	PreviewJourneyIntervention(ctx context.Context, in *journeyv1.PreviewJourneyInterventionRequest) (*journeyv1.PreviewJourneyInterventionResponse, error)
	// RequestJourneyIntervention runs a typed WITHDRAW or CANCEL
	// intervention.
	RequestJourneyIntervention(ctx context.Context, in *journeyv1.RequestJourneyInterventionRequest) (*journeyv1.RequestJourneyInterventionResponse, error)
	// WatchJourney opens the server-streaming change feed. The stream is
	// returned rather than a channel so cancellation stays where it belongs:
	// the caller's context ends the stream, and Recv reports that as an
	// error the caller can distinguish from a refusal.
	WatchJourney(ctx context.Context, in *journeyv1.WatchJourneyRequest) (WatchStream, error)
	// ListWorkers reads the population a journey can be proposed for -- the
	// release's fixed corpus plus this tenant's created employees -- and, in
	// the same answer, the closed set of placements a new employee may be
	// given. The two travel together because a client holding one and not
	// the other can draw a picker but not a form.
	ListWorkers(ctx context.Context, in *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error)
	// CreateWorker records one new employee as a durable, append-only fact.
	// It is the one write on this page that is not about a journey, and it
	// runs behind the same P1B execution authority the execution and
	// decision writes do.
	CreateWorker(ctx context.Context, in *journeyv1.CreateWorkerRequest) (*journeyv1.CreateWorkerResponse, error)
}

// PreferenceService is the product-workspace extension implemented by the
// production gRPC client. Keeping it separate leaves the workflow App's test
// seam narrowly focused on journey execution.
type PreferenceService interface {
	GetProductPreferences(context.Context, *journeyv1.GetProductPreferencesRequest) (*journeyv1.GetProductPreferencesResponse, error)
	SaveUserPreferences(context.Context, *journeyv1.SaveUserPreferencesRequest) (*journeyv1.SaveUserPreferencesResponse, error)
	SaveTenantAppearance(context.Context, *journeyv1.SaveTenantAppearanceRequest) (*journeyv1.SaveTenantAppearanceResponse, error)
	SaveOrganizationVisibility(context.Context, *journeyv1.SaveOrganizationVisibilityRequest) (*journeyv1.SaveOrganizationVisibilityResponse, error)
	GetRoleAccess(context.Context, *journeyv1.GetRoleAccessRequest) (*journeyv1.GetRoleAccessResponse, error)
	SaveAccessRole(context.Context, *journeyv1.SaveAccessRoleRequest) (*journeyv1.SaveAccessRoleResponse, error)
	SaveWorkerRoleAssignment(context.Context, *journeyv1.SaveWorkerRoleAssignmentRequest) (*journeyv1.SaveWorkerRoleAssignmentResponse, error)
	SaveRoleOrganizationVisibility(context.Context, *journeyv1.SaveRoleOrganizationVisibilityRequest) (*journeyv1.SaveRoleOrganizationVisibilityResponse, error)
	SaveRolePagePermission(context.Context, *journeyv1.SaveRolePagePermissionRequest) (*journeyv1.SaveRolePagePermissionResponse, error)
	SaveRoleFeaturePermission(context.Context, *journeyv1.SaveRoleFeaturePermissionRequest) (*journeyv1.SaveRoleFeaturePermissionResponse, error)
	RecordWorkflowUse(context.Context, *journeyv1.RecordWorkflowUseRequest) (*journeyv1.RecordWorkflowUseResponse, error)
	GetWorkerIDPolicy(context.Context, *journeyv1.GetWorkerIDPolicyRequest) (*journeyv1.GetWorkerIDPolicyResponse, error)
	SaveWorkerIDPolicy(context.Context, *journeyv1.SaveWorkerIDPolicyRequest) (*journeyv1.SaveWorkerIDPolicyResponse, error)
}

// WatchStream is the receiving half of one WatchJourney call: the generated
// grpc.ServerStreamingClient narrowed to the one method this client uses, so
// a fake stream in a test is four lines rather than an embedded generated
// type.
type WatchStream interface {
	Recv() (*journeyv1.WatchJourneyResponse, error)
}

// AuthorizationHeader is the metadata key every RPC carries the page's
// credential in.
//
// This is not belt-and-braces over an already-authenticated socket: the
// tunnel forwards only diagnostic headers (x-request-id, x-correlation-id,
// traceparent) from the WebSocket upgrade into gRPC metadata, never
// Authorization. A call that omits this is admitted as an anonymous call and
// refused UNAUTHENTICATED, no matter how the socket was opened.
const AuthorizationHeader = "authorization"

// BearerScheme is the credential's scheme, as
// internal/humanwork/workspace normalizes it away before writing the island.
const BearerScheme = "Bearer "

// grpcService is [Service] over one client connection.
type grpcService struct {
	client journeyv1.JourneyServiceClient
}

// NewGRPCService returns the production [Service]: the generated client over
// the qualified [RPCAdapter]. The adapter owns the per-RPC credential,
// canonical-method, message-bound, metadata and deadline policy.
func NewGRPCService(conn grpc.ClientConnInterface, bearer string) Service {
	adapter := NewRPCAdapter(conn, RPCAdapterConfig{Bearer: bearer})
	return &grpcService{client: journeyv1.NewJourneyServiceClient(adapter)}
}

func (s *grpcService) ListJourneys(ctx context.Context, in *journeyv1.ListJourneysRequest) (*journeyv1.ListJourneysResponse, error) {
	return s.client.ListJourneys(ctx, in)
}

func (s *grpcService) ProposePromotion(ctx context.Context, in *journeyv1.ProposePromotionRequest) (*journeyv1.ProposePromotionResponse, error) {
	return s.client.ProposePromotion(ctx, in)
}

func (s *grpcService) InspectJourney(ctx context.Context, in *journeyv1.InspectJourneyRequest) (*journeyv1.InspectJourneyResponse, error) {
	return s.client.InspectJourney(ctx, in)
}

func (s *grpcService) ExecuteJourney(ctx context.Context, in *journeyv1.ExecuteJourneyRequest) (*journeyv1.ExecuteJourneyResponse, error) {
	return s.client.ExecuteJourney(ctx, in)
}

func (s *grpcService) DecideJourney(ctx context.Context, in *journeyv1.DecideJourneyRequest) (*journeyv1.DecideJourneyResponse, error) {
	return s.client.DecideJourney(ctx, in)
}

func (s *grpcService) EditProposal(ctx context.Context, in *journeyv1.EditProposalRequest) (*journeyv1.EditProposalResponse, error) {
	return s.client.EditProposal(ctx, in)
}

func (s *grpcService) PreviewJourneyIntervention(ctx context.Context, in *journeyv1.PreviewJourneyInterventionRequest) (*journeyv1.PreviewJourneyInterventionResponse, error) {
	return s.client.PreviewJourneyIntervention(ctx, in)
}

func (s *grpcService) RequestJourneyIntervention(ctx context.Context, in *journeyv1.RequestJourneyInterventionRequest) (*journeyv1.RequestJourneyInterventionResponse, error) {
	return s.client.RequestJourneyIntervention(ctx, in)
}

func (s *grpcService) ListWorkers(ctx context.Context, in *journeyv1.ListWorkersRequest) (*journeyv1.ListWorkersResponse, error) {
	return s.client.ListWorkers(ctx, in)
}

func (s *grpcService) CreateWorker(ctx context.Context, in *journeyv1.CreateWorkerRequest) (*journeyv1.CreateWorkerResponse, error) {
	return s.client.CreateWorker(ctx, in)
}

func (s *grpcService) WatchJourney(ctx context.Context, in *journeyv1.WatchJourneyRequest) (WatchStream, error) {
	stream, err := s.client.WatchJourney(ctx, in)
	if err != nil {
		return nil, err
	}
	return stream, nil
}

func (s *grpcService) GetProductPreferences(ctx context.Context, in *journeyv1.GetProductPreferencesRequest) (*journeyv1.GetProductPreferencesResponse, error) {
	return s.client.GetProductPreferences(ctx, in)
}

func (s *grpcService) SaveUserPreferences(ctx context.Context, in *journeyv1.SaveUserPreferencesRequest) (*journeyv1.SaveUserPreferencesResponse, error) {
	return s.client.SaveUserPreferences(ctx, in)
}

func (s *grpcService) SaveTenantAppearance(ctx context.Context, in *journeyv1.SaveTenantAppearanceRequest) (*journeyv1.SaveTenantAppearanceResponse, error) {
	return s.client.SaveTenantAppearance(ctx, in)
}

func (s *grpcService) SaveOrganizationVisibility(ctx context.Context, in *journeyv1.SaveOrganizationVisibilityRequest) (*journeyv1.SaveOrganizationVisibilityResponse, error) {
	return s.client.SaveOrganizationVisibility(ctx, in)
}

func (s *grpcService) GetRoleAccess(ctx context.Context, in *journeyv1.GetRoleAccessRequest) (*journeyv1.GetRoleAccessResponse, error) {
	return s.client.GetRoleAccess(ctx, in)
}

func (s *grpcService) SaveAccessRole(ctx context.Context, in *journeyv1.SaveAccessRoleRequest) (*journeyv1.SaveAccessRoleResponse, error) {
	return s.client.SaveAccessRole(ctx, in)
}

func (s *grpcService) SaveWorkerRoleAssignment(ctx context.Context, in *journeyv1.SaveWorkerRoleAssignmentRequest) (*journeyv1.SaveWorkerRoleAssignmentResponse, error) {
	return s.client.SaveWorkerRoleAssignment(ctx, in)
}

func (s *grpcService) SaveRoleOrganizationVisibility(ctx context.Context, in *journeyv1.SaveRoleOrganizationVisibilityRequest) (*journeyv1.SaveRoleOrganizationVisibilityResponse, error) {
	return s.client.SaveRoleOrganizationVisibility(ctx, in)
}

func (s *grpcService) SaveRolePagePermission(ctx context.Context, in *journeyv1.SaveRolePagePermissionRequest) (*journeyv1.SaveRolePagePermissionResponse, error) {
	return s.client.SaveRolePagePermission(ctx, in)
}

func (s *grpcService) SaveRoleFeaturePermission(ctx context.Context, in *journeyv1.SaveRoleFeaturePermissionRequest) (*journeyv1.SaveRoleFeaturePermissionResponse, error) {
	return s.client.SaveRoleFeaturePermission(ctx, in)
}

func (s *grpcService) RecordWorkflowUse(ctx context.Context, in *journeyv1.RecordWorkflowUseRequest) (*journeyv1.RecordWorkflowUseResponse, error) {
	return s.client.RecordWorkflowUse(ctx, in)
}

func (s *grpcService) GetWorkerIDPolicy(ctx context.Context, in *journeyv1.GetWorkerIDPolicyRequest) (*journeyv1.GetWorkerIDPolicyResponse, error) {
	return s.client.GetWorkerIDPolicy(ctx, in)
}

func (s *grpcService) SaveWorkerIDPolicy(ctx context.Context, in *journeyv1.SaveWorkerIDPolicyRequest) (*journeyv1.SaveWorkerIDPolicyResponse, error) {
	return s.client.SaveWorkerIDPolicy(ctx, in)
}
