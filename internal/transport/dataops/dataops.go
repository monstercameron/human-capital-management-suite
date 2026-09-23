// Package dataops adapts the generated DataOpsService server interface to
// the transport.DataOpsHandler port. Every method is a forward:
// authentication, trusted-context construction, validation and error
// projection already happened in the interceptor chain, and nothing else
// belongs here.
package dataops

import (
	"context"

	dataopsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/dataops/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
)

// Service adapts the generated DataOpsService server interface to the
// transport.DataOpsHandler port. The handler owns every rule; this type owns
// none.
type Service struct {
	dataopsv1.UnimplementedDataOpsServiceServer

	Handler transport.DataOpsHandler
}

func (s *Service) ExplainFieldHistory(ctx context.Context, req *dataopsv1.ExplainFieldHistoryRequest) (*dataopsv1.ExplainFieldHistoryResponse, error) {
	return s.Handler.ExplainFieldHistory(ctx, req)
}

func (s *Service) DiffRecord(ctx context.Context, req *dataopsv1.DiffRecordRequest) (*dataopsv1.DiffRecordResponse, error) {
	return s.Handler.DiffRecord(ctx, req)
}

func (s *Service) CreateRepairPlan(ctx context.Context, req *dataopsv1.CreateRepairPlanRequest) (*dataopsv1.CreateRepairPlanResponse, error) {
	return s.Handler.CreateRepairPlan(ctx, req)
}

func (s *Service) SimulateRepair(ctx context.Context, req *dataopsv1.SimulateRepairRequest) (*dataopsv1.SimulateRepairResponse, error) {
	return s.Handler.SimulateRepair(ctx, req)
}
