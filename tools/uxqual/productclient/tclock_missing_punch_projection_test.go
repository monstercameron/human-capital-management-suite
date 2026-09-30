package productclient

import (
	"context"
	"testing"
	"time"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"google.golang.org/grpc"
)

type missingPunchClientFake struct {
	submit *timev1.SubmitCorrectionRequest
	review *timev1.ReviewCorrectionRequest
	answer *timev1.MissingPunchCorrection
}

func (f *missingPunchClientFake) SubmitCorrection(_ context.Context, req *timev1.SubmitCorrectionRequest, _ ...grpc.CallOption) (*timev1.SubmitCorrectionResponse, error) {
	f.submit = req
	return &timev1.SubmitCorrectionResponse{Correction: f.answer}, nil
}

func (f *missingPunchClientFake) ReviewCorrection(_ context.Context, req *timev1.ReviewCorrectionRequest, _ ...grpc.CallOption) (*timev1.ReviewCorrectionResponse, error) {
	f.review = req
	return &timev1.ReviewCorrectionResponse{Correction: f.answer}, nil
}

func validMissingPunchCorrection() *timev1.MissingPunchCorrection {
	return &timev1.MissingPunchCorrection{
		RequestId: "11111111-1111-4111-8111-111111111111", Status: "PENDING", Revision: 7,
		WorkflowReceipt: &timev1.WorkflowTrackedReceipt{
			ReceiptId: "22222222-2222-4222-8222-222222222222", WorkflowInstanceRef: "33333333-3333-4333-8333-333333333333",
			WorkflowTraceId: "44444444-4444-4444-8444-444444444444", WorkflowNodeId: "commit_missing_punch_request",
			WorkflowAttempt: 2, WorkflowInstanceVersion: 5, WorkflowId: "hcmnext.workflows.time.fix_missing_punch", WorkflowPlanDigest: "sha256:abc",
		},
	}
}

func TestTodo_TCLOCK011_MissingPunchTransportMapsSubmitAndReceipt(t *testing.T) {
	fake := &missingPunchClientFake{answer: validMissingPunchCorrection()}
	ctx := context.WithValue(context.Background(), struct{}{}, "principal")
	transport := NewMissingPunchTransport(ctx, fake)
	got, err := transport.SubmitMissingPunch(productui.MissingPunchSubmission{
		SessionID: "session-1", Reason: "forgot to clock out", ProposedOutAt: time.Date(2026, 9, 28, 17, 0, 0, 0, time.UTC),
		ExpectedRevision: 6, IdempotencyKey: "session-1:6",
	})
	if err != nil {
		t.Fatalf("SubmitMissingPunch: %v", err)
	}
	if fake.submit.GetSessionId() != "session-1" || fake.submit.GetClaimedEventType() != "CLOCK_OUT" || fake.submit.GetExpectedRevision() != 6 || fake.submit.GetIdempotencyKey() != "session-1:6" {
		t.Fatalf("request mapping = %+v", fake.submit)
	}
	if got.RequestID != validMissingPunchCorrection().GetRequestId() || got.WorkflowID != "hcmnext.workflows.time.fix_missing_punch" || got.Attempt != 2 || got.InstanceVersion != 5 || got.WorkflowTraceHref != "" {
		t.Fatalf("receipt mapping = %+v", got)
	}
	if !fake.submit.GetClaimedOccurredAt().AsTime().Equal(time.Date(2026, 9, 28, 17, 0, 0, 0, time.UTC)) {
		t.Fatalf("claimed time = %v", fake.submit.GetClaimedOccurredAt().AsTime())
	}
}

func TestTodo_TCLOCK011_MissingPunchTransportMapsReviewDecision(t *testing.T) {
	fake := &missingPunchClientFake{answer: validMissingPunchCorrection()}
	transport := NewMissingPunchTransport(context.Background(), fake)
	if _, err := transport.DecideMissingPunch(productui.MissingPunchDecision{RequestID: validMissingPunchCorrection().GetRequestId(), DecisionNote: "approved", Approve: true, ExpectedRevision: 7, IdempotencyKey: "review-7"}); err != nil {
		t.Fatalf("DecideMissingPunch: %v", err)
	}
	if fake.review.GetRequestId() != validMissingPunchCorrection().GetRequestId() || fake.review.GetDecision() != timev1.MissingPunchDecision_MISSING_PUNCH_DECISION_APPROVED || fake.review.GetExpectedRevision() != 7 {
		t.Fatalf("review mapping = %+v", fake.review)
	}
}

func TestTodo_TCLOCK011_MissingPunchTransportRejectsUntrustedReceipt(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*timev1.MissingPunchCorrection)
	}{
		{"bad request uuid", func(c *timev1.MissingPunchCorrection) { c.RequestId = "request-1" }},
		{"bad trace uuid", func(c *timev1.MissingPunchCorrection) { c.WorkflowReceipt.WorkflowTraceId = "trace-1" }},
		{"wrong workflow", func(c *timev1.MissingPunchCorrection) { c.WorkflowReceipt.WorkflowId = "other.workflow" }},
		{"missing node", func(c *timev1.MissingPunchCorrection) { c.WorkflowReceipt.WorkflowNodeId = "" }},
		{"zero attempt", func(c *timev1.MissingPunchCorrection) { c.WorkflowReceipt.WorkflowAttempt = 0 }},
		{"zero version", func(c *timev1.MissingPunchCorrection) { c.WorkflowReceipt.WorkflowInstanceVersion = 0 }},
		{"missing plan", func(c *timev1.MissingPunchCorrection) { c.WorkflowReceipt.WorkflowPlanDigest = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			answer := validMissingPunchCorrection()
			tc.mutate(answer)
			transport := NewMissingPunchTransport(context.Background(), &missingPunchClientFake{answer: answer})
			_, err := transport.SubmitMissingPunch(productui.MissingPunchSubmission{SessionID: "s", Reason: "r", ProposedOutAt: time.Date(2026, 9, 28, 17, 0, 0, 0, time.UTC), IdempotencyKey: "s:1"})
			if err == nil {
				t.Fatal("accepted untrusted workflow receipt")
			}
		})
	}
}

func TestTodo_TCLOCK011_MissingPunchTransportFailsClosedWithoutClient(t *testing.T) {
	if _, err := (*MissingPunchTransport)(nil).SubmitMissingPunch(productui.MissingPunchSubmission{}); err == nil {
		t.Fatal("nil transport succeeded")
	}
	if _, err := NewMissingPunchTransport(context.Background(), nil).DecideMissingPunch(productui.MissingPunchDecision{}); err == nil {
		t.Fatal("unconfigured transport succeeded")
	}
	if _, err := missingPunchReceipt(nil); err == nil {
		t.Fatal("nil correction succeeded")
	}
}
