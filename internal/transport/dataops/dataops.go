// Package dataops adapts the generated DataOpsService server interface to
// the transport.DataOpsHandler port. Every method is a forward:
// authentication, trusted-context construction, validation and error
// projection already happened in the interceptor chain, and nothing else
// belongs here.
package dataops

import (
	"bytes"
	"context"
	"fmt"
	"io"

	dataopsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/dataops/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// MaxStageCSVFrameBytes is the largest client-streaming frame accepted by the
// adapter. Keeping frames below gRPC's default 4 MiB receive limit lets the
// application accept the importer's full 64 MiB aggregate budget safely.
const MaxStageCSVFrameBytes = 1 << 20

const maxStageCSVFrames = 65 // one metadata frame plus at most 64 MiB of 1 MiB chunks

// StageCSVHandler is the application port for the client-streaming importer.
// The transport has already authenticated the caller; the implementation must
// derive tenant identity from trust context rather than request fields.
type StageCSVHandler interface {
	StageCSV(ctx context.Context, sourceURI, idempotencyKey string, payload []byte) (*dataopsv1.StageCSVResponse, error)
}

// StageCSVHandlerFunc adapts a composition-root closure to StageCSVHandler.
type StageCSVHandlerFunc func(context.Context, string, string, []byte) (*dataopsv1.StageCSVResponse, error)

func (f StageCSVHandlerFunc) StageCSV(ctx context.Context, sourceURI, idempotencyKey string, payload []byte) (*dataopsv1.StageCSVResponse, error) {
	return f(ctx, sourceURI, idempotencyKey, payload)
}

// Service adapts the generated DataOpsService server interface to the
// transport.DataOpsHandler port. The handler owns every rule; this type owns
// none.
type Service struct {
	dataopsv1.UnimplementedDataOpsServiceServer

	Handler transport.DataOpsHandler
	Stage   StageCSVHandler
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

// StageCSV collects bounded frames and forwards one bounded payload to the
// application port. It does not parse CSV or decide tenant scope.
func (s *Service) StageCSV(stream dataopsv1.DataOpsService_StageCSVServer) error {
	if s == nil || s.Stage == nil {
		return status.Error(codes.Unimplemented, "DataOpsService.StageCSV is not configured")
	}
	first, err := stream.Recv()
	if err != nil {
		return err
	}
	if first == nil || first.GetSourceUri() == "" || first.GetIdempotencyKey() == "" {
		return status.Error(codes.InvalidArgument, "the first StageCSV frame requires source_uri and idempotency_key")
	}
	if len(first.GetCsvChunk()) > MaxStageCSVFrameBytes {
		return status.Error(codes.InvalidArgument, "StageCSV frame exceeds the maximum size")
	}
	var payload bytes.Buffer
	if _, err := payload.Write(first.GetCsvChunk()); err != nil {
		return fmt.Errorf("stage csv first frame: %w", err)
	}
	frames := 1
	for payload.Len() <= MaxStageCSVFrameBytes*maxStageCSVFrames {
		frame, recvErr := stream.Recv()
		if recvErr != nil {
			if recvErr == io.EOF {
				break
			}
			return recvErr
		}
		frames++
		if frames > maxStageCSVFrames {
			return status.Error(codes.ResourceExhausted, "StageCSV stream exceeds the 64 MiB source budget")
		}
		if frame == nil || frame.GetSourceUri() != "" || frame.GetIdempotencyKey() != "" {
			return status.Error(codes.InvalidArgument, "only the first StageCSV frame may carry source metadata")
		}
		if len(frame.GetCsvChunk()) > MaxStageCSVFrameBytes {
			return status.Error(codes.InvalidArgument, "StageCSV frame exceeds the maximum size")
		}
		if payload.Len()+len(frame.GetCsvChunk()) > MaxStageCSVFrameBytes*64 {
			return status.Error(codes.ResourceExhausted, "StageCSV stream exceeds the 64 MiB source budget")
		}
		if _, err := payload.Write(frame.GetCsvChunk()); err != nil {
			return fmt.Errorf("stage csv frame: %w", err)
		}
	}
	response, err := s.Stage.StageCSV(stream.Context(), first.GetSourceUri(), first.GetIdempotencyKey(), payload.Bytes())
	if err != nil {
		return err
	}
	return stream.SendAndClose(response)
}
