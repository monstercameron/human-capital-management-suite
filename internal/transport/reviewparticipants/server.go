package reviewparticipants

import (
	"context"

	reviewparticipantsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/reviewparticipants/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Dependencies contains the authenticated application read. Transport only
// validates admission and maps its typed projection to the generated API.
type Dependencies struct {
	Read     func(context.Context, *trust.Principal) (productui.ReviewParticipantsProjection, error)
	IsDenied func(error) bool
}

type server struct {
	reviewparticipantsv1.UnimplementedReviewParticipantsServiceServer
	deps Dependencies
}

func Register(srv *grpc.Server, deps Dependencies) {
	reviewparticipantsv1.RegisterReviewParticipantsServiceServer(srv, &server{deps: deps})
}

func (s *server) GetReviewParticipants(ctx context.Context, _ *reviewparticipantsv1.GetReviewParticipantsRequest) (*reviewparticipantsv1.GetReviewParticipantsResponse, error) {
	principal, err := admittedPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if s.deps.Read == nil {
		return nil, status.Error(codes.Unavailable, "review participant read is unavailable")
	}
	projection, err := s.deps.Read(ctx, principal)
	if err != nil {
		if s.deps.IsDenied != nil && s.deps.IsDenied(err) {
			return nil, status.Error(codes.PermissionDenied, "review participant read is not authorized")
		}
		return nil, status.Error(codes.Unavailable, "review participant read is unavailable")
	}
	response := &reviewparticipantsv1.GetReviewParticipantsResponse{Cycles: make([]*reviewparticipantsv1.ReviewCycle, 0, len(projection.Cycles))}
	for _, cycle := range projection.Cycles {
		item := &reviewparticipantsv1.ReviewCycle{
			CycleId: cycle.CycleID, CycleRevision: cycle.CycleRevision,
			GraphRevision: cycle.GraphRevision, GraphDigest: cycle.GraphDigest,
			Assignments: make([]*reviewparticipantsv1.ReviewerAssignment, 0, len(cycle.Assignments)),
		}
		for _, assignment := range cycle.Assignments {
			item.Assignments = append(item.Assignments, &reviewparticipantsv1.ReviewerAssignment{
				ParticipantId: assignment.ParticipantID, ReviewerId: assignment.ReviewerID, Relationship: assignment.Relationship,
			})
		}
		response.Cycles = append(response.Cycles, item)
	}
	return response, nil
}

func admittedPrincipal(ctx context.Context) (*trust.Principal, error) {
	if _, ok := transport.InvocationFromContext(ctx); !ok {
		return nil, status.Error(codes.Unauthenticated, "request has no trusted context")
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil || principal.Subject() == "" || principal.Tenant().String() == "" {
		return nil, status.Error(codes.Unauthenticated, "request has no authenticated principal")
	}
	return principal, nil
}

var _ reviewparticipantsv1.ReviewParticipantsServiceServer = (*server)(nil)
