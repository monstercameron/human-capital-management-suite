package position

import (
	"context"
	"errors"
	"strings"

	positionv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/position/v1"
	app "github.com/monstercameron/human-capital-management-suite/internal/intent/app"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Dependencies are the read-only application service already composed over
// positionfacts.Reader. The transport owns no position business rules.
type Dependencies struct {
	ListObjectOptions    func(context.Context, *trust.Principal) ([]OptionRead, error)
	ListOccupancyOptions func(context.Context, *trust.Principal) ([]OptionRead, error)
	GetObject            func(context.Context, *trust.Principal, string) (ObjectRead, error)
	GetOccupancy         func(context.Context, *trust.Principal, string) (OccupancyRead, error)
}

type OptionRead struct {
	Reference, PositionID, Title, Organization, JobCode, OrgUnit string
}

type ObjectRead struct {
	PositionID, Revision, JobCode, OrgUnit, Lifecycle string
	Compatible                                        bool
}

type OccupancyRead struct {
	PositionID, CapacityFTE, ConsumedFTE, AvailableFTE string
	CapacityHeads, ConsumedHeads, AvailableHeads       int64
	Occupants                                          []OccupantRead
}

type OccupantRead struct{ WorkerID, FTE string }

type server struct {
	positionv1.UnimplementedPositionServiceServer
	deps Dependencies
}

// Register adds PositionService to an already admitted gRPC server. The
// caller must use the same trusted unary interceptor chain as JourneyService.
func Register(srv *grpc.Server, deps Dependencies) {
	positionv1.RegisterPositionServiceServer(srv, &server{deps: deps})
}

func (s *server) ListPositionObjectOptions(ctx context.Context, _ *positionv1.ListPositionObjectOptionsRequest) (*positionv1.ListPositionObjectOptionsResponse, error) {
	principal, err := admittedPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if s.deps.ListObjectOptions == nil {
		return nil, status.Error(codes.Unavailable, "position options are unavailable")
	}
	options, err := s.deps.ListObjectOptions(ctx, principal)
	if err != nil {
		return nil, positionReadError(err)
	}
	return &positionv1.ListPositionObjectOptionsResponse{Options: positionOptions(options)}, nil
}

func (s *server) ListPositionOccupancyOptions(ctx context.Context, _ *positionv1.ListPositionOccupancyOptionsRequest) (*positionv1.ListPositionOccupancyOptionsResponse, error) {
	principal, err := admittedPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if s.deps.ListOccupancyOptions == nil {
		return nil, status.Error(codes.Unavailable, "position options are unavailable")
	}
	options, err := s.deps.ListOccupancyOptions(ctx, principal)
	if err != nil {
		return nil, positionReadError(err)
	}
	return &positionv1.ListPositionOccupancyOptionsResponse{Options: positionOptions(options)}, nil
}

func positionOptions(options []OptionRead) []*positionv1.PositionOption {
	response := make([]*positionv1.PositionOption, 0, len(options))
	for _, option := range options {
		response = append(response, &positionv1.PositionOption{
			PositionRevisionRef: option.Reference, PositionId: option.PositionID,
			Title: option.Title, Organization: option.Organization, JobCode: option.JobCode, OrgUnit: option.OrgUnit,
		})
	}
	return response
}

func (s *server) GetPositionObject(ctx context.Context, req *positionv1.GetPositionObjectRequest) (*positionv1.GetPositionObjectResponse, error) {
	principal, err := admittedPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || strings.TrimSpace(req.GetPositionRevisionRef()) == "" {
		return nil, status.Error(codes.InvalidArgument, "position revision reference is required")
	}
	if s.deps.GetObject == nil {
		return nil, status.Error(codes.Unavailable, "position read is unavailable")
	}
	result, err := s.deps.GetObject(ctx, principal, req.GetPositionRevisionRef())
	if err != nil {
		return nil, positionReadError(err)
	}
	return &positionv1.GetPositionObjectResponse{
		PositionId: result.PositionID, Revision: result.Revision, JobCode: result.JobCode,
		OrgUnit: result.OrgUnit, Lifecycle: result.Lifecycle, Compatible: result.Compatible,
		Exists: result.PositionID != "",
	}, nil
}

func (s *server) GetPositionOccupancy(ctx context.Context, req *positionv1.GetPositionOccupancyRequest) (*positionv1.GetPositionOccupancyResponse, error) {
	principal, err := admittedPrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if req == nil || strings.TrimSpace(req.GetPositionRevisionRef()) == "" {
		return nil, status.Error(codes.InvalidArgument, "position revision reference is required")
	}
	if s.deps.GetOccupancy == nil {
		return nil, status.Error(codes.Unavailable, "position read is unavailable")
	}
	result, err := s.deps.GetOccupancy(ctx, principal, req.GetPositionRevisionRef())
	if err != nil {
		return nil, positionReadError(err)
	}
	response := &positionv1.GetPositionOccupancyResponse{
		PositionId: result.PositionID, CapacityFte: result.CapacityFTE, CapacityHeads: result.CapacityHeads,
		ConsumedFte: result.ConsumedFTE, ConsumedHeads: result.ConsumedHeads,
		AvailableFte: result.AvailableFTE, AvailableHeads: result.AvailableHeads,
		Exists: result.PositionID != "", Occupants: make([]*positionv1.PositionOccupant, 0, len(result.Occupants)),
	}
	for _, occupant := range result.Occupants {
		response.Occupants = append(response.Occupants, &positionv1.PositionOccupant{WorkerId: occupant.WorkerID, Fte: occupant.FTE})
	}
	return response, nil
}

func admittedPrincipal(ctx context.Context) (*trust.Principal, error) {
	if _, ok := transport.InvocationFromContext(ctx); !ok {
		return nil, status.Error(codes.Unauthenticated, "request has no trusted context")
	}
	principal, ok := trust.FromContext(ctx)
	if !ok || principal == nil {
		return nil, status.Error(codes.Unauthenticated, "request has no authenticated principal")
	}
	return principal, nil
}

func positionReadError(err error) error {
	switch {
	case errors.Is(err, app.ErrPositionUnauthorized):
		return status.Error(codes.PermissionDenied, "position is outside the viewer's authorized scope")
	case errors.Is(err, app.ErrPositionInvalidRevisionRef):
		return status.Error(codes.InvalidArgument, "position revision reference is invalid")
	default:
		return status.Error(codes.Unavailable, "position read is unavailable")
	}
}
