package dataops

import (
	"context"
	"errors"
	"testing"

	dataopsv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/dataops/v1"
)

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
