package dataops

import (
	"context"
	"errors"
	"io"
	"testing"

	dataopsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/dataops/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

type fakeStageHandler struct {
	source, key string
	payload     []byte
	response    *dataopsv1.StageCSVResponse
	err         error
}

func (f *fakeStageHandler) StageCSV(_ context.Context, source, key string, payload []byte) (*dataopsv1.StageCSVResponse, error) {
	f.source, f.key, f.payload = source, key, append([]byte(nil), payload...)
	return f.response, f.err
}

type fakeStageStream struct {
	ctx      context.Context
	frames   []*dataopsv1.StageCSVRequest
	closeErr error
	response *dataopsv1.StageCSVResponse
}

func (s *fakeStageStream) Recv() (*dataopsv1.StageCSVRequest, error) {
	if len(s.frames) == 0 {
		return nil, io.EOF
	}
	frame := s.frames[0]
	s.frames = s.frames[1:]
	return frame, nil
}
func (s *fakeStageStream) SendAndClose(response *dataopsv1.StageCSVResponse) error {
	s.response = response
	return s.closeErr
}
func (s *fakeStageStream) SetHeader(metadata.MD) error  { return nil }
func (s *fakeStageStream) SendHeader(metadata.MD) error { return nil }
func (s *fakeStageStream) SetTrailer(metadata.MD)       {}
func (s *fakeStageStream) Context() context.Context     { return s.ctx }
func (s *fakeStageStream) SendMsg(any) error            { return nil }
func (s *fakeStageStream) RecvMsg(any) error            { return nil }

type fakeHandler struct {
	err error

	explainReq *dataopsv1.ExplainFieldHistoryRequest
	diffReq    *dataopsv1.DiffRecordRequest
	planReq    *dataopsv1.CreateRepairPlanRequest
	simReq     *dataopsv1.SimulateRepairRequest
}

func (f *fakeHandler) ExplainFieldHistory(_ context.Context, req *dataopsv1.ExplainFieldHistoryRequest) (*dataopsv1.ExplainFieldHistoryResponse, error) {
	f.explainReq = req
	return &dataopsv1.ExplainFieldHistoryResponse{}, f.err
}

func (f *fakeHandler) DiffRecord(_ context.Context, req *dataopsv1.DiffRecordRequest) (*dataopsv1.DiffRecordResponse, error) {
	f.diffReq = req
	return &dataopsv1.DiffRecordResponse{}, f.err
}

func (f *fakeHandler) CreateRepairPlan(_ context.Context, req *dataopsv1.CreateRepairPlanRequest) (*dataopsv1.CreateRepairPlanResponse, error) {
	f.planReq = req
	return &dataopsv1.CreateRepairPlanResponse{}, f.err
}

func (f *fakeHandler) SimulateRepair(_ context.Context, req *dataopsv1.SimulateRepairRequest) (*dataopsv1.SimulateRepairResponse, error) {
	f.simReq = req
	return &dataopsv1.SimulateRepairResponse{}, f.err
}

// TestTodo_REV_030_01 proves Service is a pure forward to the DataOps
// handler port: every method delivers the caller's exact request to the
// handler, returns the handler's response untouched, and surfaces the
// handler's error instead of a fabricated success. It also pins the
// compile-time conformance to the generated server interface, so a proto
// change that adds an RPC fails here rather than silently unserved.
func TestTodo_REV_030_01(t *testing.T) {
	var _ dataopsv1.DataOpsServiceServer = (*Service)(nil)

	fake := &fakeHandler{}
	svc := &Service{Handler: fake}
	ctx := context.Background()

	explainReq := &dataopsv1.ExplainFieldHistoryRequest{}
	if _, err := svc.ExplainFieldHistory(ctx, explainReq); err != nil {
		t.Fatalf("ExplainFieldHistory: %v", err)
	}
	if fake.explainReq != explainReq {
		t.Fatal("ExplainFieldHistory did not forward the caller's request")
	}

	diffReq := &dataopsv1.DiffRecordRequest{}
	if _, err := svc.DiffRecord(ctx, diffReq); err != nil {
		t.Fatalf("DiffRecord: %v", err)
	}
	if fake.diffReq != diffReq {
		t.Fatal("DiffRecord did not forward the caller's request")
	}

	planReq := &dataopsv1.CreateRepairPlanRequest{}
	if _, err := svc.CreateRepairPlan(ctx, planReq); err != nil {
		t.Fatalf("CreateRepairPlan: %v", err)
	}
	if fake.planReq != planReq {
		t.Fatal("CreateRepairPlan did not forward the caller's request")
	}

	simReq := &dataopsv1.SimulateRepairRequest{}
	if _, err := svc.SimulateRepair(ctx, simReq); err != nil {
		t.Fatalf("SimulateRepair: %v", err)
	}
	if fake.simReq != simReq {
		t.Fatal("SimulateRepair did not forward the caller's request")
	}

	sentinel := errors.New("handler refused")
	fake.err = sentinel
	if _, err := svc.ExplainFieldHistory(ctx, explainReq); !errors.Is(err, sentinel) {
		t.Fatalf("ExplainFieldHistory error = %v, want the handler's error", err)
	}
	if _, err := svc.DiffRecord(ctx, diffReq); !errors.Is(err, sentinel) {
		t.Fatalf("DiffRecord error = %v, want the handler's error", err)
	}
	if _, err := svc.CreateRepairPlan(ctx, planReq); !errors.Is(err, sentinel) {
		t.Fatalf("CreateRepairPlan error = %v, want the handler's error", err)
	}
	if _, err := svc.SimulateRepair(ctx, simReq); !errors.Is(err, sentinel) {
		t.Fatalf("SimulateRepair error = %v, want the handler's error", err)
	}
}

func TestTodo_REV_030_01_StageCSV(t *testing.T) {
	want := &dataopsv1.StageCSVResponse{StagedImport: &dataopsv1.StagedImport{BatchId: "batch-1"}}
	handler := &fakeStageHandler{response: want}
	stream := &fakeStageStream{ctx: context.Background(), frames: []*dataopsv1.StageCSVRequest{
		{SourceUri: "upload://csv", IdempotencyKey: "stage-1", CsvChunk: []byte("a,b\n")},
		{CsvChunk: []byte("1,2\n")},
	}}
	if err := (&Service{Stage: handler}).StageCSV(stream); err != nil {
		t.Fatalf("StageCSV: %v", err)
	}
	if handler.source != "upload://csv" || handler.key != "stage-1" || string(handler.payload) != "a,b\n1,2\n" || stream.response != want {
		t.Fatalf("forwarded (%q,%q,%q,%p), want source/key/payload/response", handler.source, handler.key, handler.payload, stream.response)
	}

	for name, frames := range map[string][]*dataopsv1.StageCSVRequest{
		"missing metadata":         {{CsvChunk: []byte("a,b\n")}},
		"metadata on second frame": {{SourceUri: "u", IdempotencyKey: "k"}, {SourceUri: "spoof"}},
	} {
		t.Run(name, func(t *testing.T) {
			if err := (&Service{Stage: handler}).StageCSV(&fakeStageStream{ctx: context.Background(), frames: frames}); status.Code(err) != codes.InvalidArgument {
				t.Fatalf("StageCSV error = %v, want InvalidArgument", err)
			}
		})
	}
	if err := (&Service{}).StageCSV(&fakeStageStream{ctx: context.Background()}); status.Code(err) != codes.Unimplemented {
		t.Fatalf("unconfigured StageCSV = %v, want Unimplemented", err)
	}
}
