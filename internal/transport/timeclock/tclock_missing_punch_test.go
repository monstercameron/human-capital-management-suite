package timeclock

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	timev1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/time/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type missingPunchFake struct {
	submits, reviews int
	lastSubmit       MissingPunchSubmit
	lastReview       MissingPunchReview
	result           MissingPunchCorrection
	err              error
}

func (f *missingPunchFake) SubmitCorrection(_ context.Context, _ *trust.Principal, in MissingPunchSubmit) (MissingPunchCorrection, error) {
	f.submits++
	f.lastSubmit = in
	return f.result, f.err
}

func (f *missingPunchFake) ReviewCorrection(_ context.Context, _ *trust.Principal, in MissingPunchReview) (MissingPunchCorrection, error) {
	f.reviews++
	f.lastReview = in
	return f.result, f.err
}

func missingPunchPrincipal(t *testing.T, kind trust.SubjectKind) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "worker-a", SubjectKind: kind, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session-a", IssuedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Minute), CredentialDigest: "digest-a"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func validSubmitRequest() *timev1.SubmitCorrectionRequest {
	return &timev1.SubmitCorrectionRequest{SessionId: "session-1", ClaimedEventType: "CLOCK_OUT", ClaimedOccurredAt: timestamppb.New(time.Unix(10, 0)), Reason: "forgot to clock out", IdempotencyKey: "idem-1", ExpectedRevision: 1}
}

func TestTodo_TCLOCK011_SubmitBindsHumanAndReturnsTrackedReceipt(t *testing.T) {
	fake := &missingPunchFake{result: MissingPunchCorrection{RequestID: "req-1", WorkerRef: "worker-a", Status: "PENDING", Revision: 1, WorkflowReceipt: MissingPunchReceipt{ReceiptID: "receipt-1", WorkflowInstanceRef: "00000000-0000-0000-0000-000000000001", WorkflowTraceID: "trace-1", WorkflowNodeID: "commit_missing_punch_request", WorkflowID: "hcmnext.workflows.time.fix_missing_punch", WorkflowPlanDigest: "plan", WorkflowAttempt: 1, WorkflowInstanceVersion: 1}}}
	p := missingPunchPrincipal(t, trust.SubjectKindHuman)
	got, err := NewMissingPunchServer(fake).SubmitCorrection(trust.WithPrincipal(context.Background(), p), validSubmitRequest())
	if err != nil {
		t.Fatal(err)
	}
	if got.GetCorrection().GetRequestId() != "req-1" || got.GetCorrection().GetWorkflowReceipt().GetReceiptId() != "receipt-1" || fake.submits != 1 {
		t.Fatalf("response=%v calls=%d", got, fake.submits)
	}
	if fake.lastSubmit.SessionID != "session-1" || fake.lastSubmit.IdempotencyKey != "idem-1" || fake.lastSubmit.ExpectedRevision != 1 {
		t.Fatalf("submit=%+v", fake.lastSubmit)
	}
}

func TestTodo_TCLOCK011_RejectsUnauthorizedAndMalformedWithoutAck(t *testing.T) {
	fake := &missingPunchFake{}
	s := NewMissingPunchServer(fake)
	for _, tc := range []struct {
		name string
		ctx  context.Context
		in   *timev1.SubmitCorrectionRequest
		code codes.Code
	}{
		{name: "missing principal", ctx: context.Background(), in: validSubmitRequest(), code: codes.Unauthenticated},
		{name: "service principal", ctx: trust.WithPrincipal(context.Background(), missingPunchPrincipal(t, trust.SubjectKindService)), in: validSubmitRequest(), code: codes.Unauthenticated},
		{name: "missing reason", ctx: trust.WithPrincipal(context.Background(), missingPunchPrincipal(t, trust.SubjectKindHuman)), in: &timev1.SubmitCorrectionRequest{ClaimedOccurredAt: timestamppb.New(time.Unix(1, 0)), IdempotencyKey: "idem"}, code: codes.InvalidArgument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.SubmitCorrection(tc.ctx, tc.in)
			if status.Code(err) != tc.code || fake.submits != 0 {
				t.Fatalf("err=%v code=%s calls=%d", err, status.Code(err), fake.submits)
			}
		})
	}
}

func TestTodo_TCLOCK011_ReviewMapsDecisionAndPreservesStaleFailure(t *testing.T) {
	fake := &missingPunchFake{result: MissingPunchCorrection{RequestID: "req-1", Status: "APPROVED", Revision: 2, WorkflowReceipt: MissingPunchReceipt{ReceiptID: "receipt-1", WorkflowInstanceRef: "00000000-0000-0000-0000-000000000002", WorkflowTraceID: "trace-1", WorkflowNodeID: "supervisor_approval", WorkflowID: "hcmnext.workflows.time.fix_missing_punch", WorkflowPlanDigest: "plan", WorkflowAttempt: 1, WorkflowInstanceVersion: 2}}}
	p := missingPunchPrincipal(t, trust.SubjectKindHuman)
	s := NewMissingPunchServer(fake)
	_, err := s.ReviewCorrection(trust.WithPrincipal(context.Background(), p), &timev1.ReviewCorrectionRequest{RequestId: "req-1", ExpectedRevision: 1, Decision: timev1.MissingPunchDecision_MISSING_PUNCH_DECISION_APPROVED, Reason: "verified", IdempotencyKey: "review-1"})
	if err != nil || fake.lastReview.Decision != "APPROVED" || fake.lastReview.ExpectedRevision != 1 {
		t.Fatalf("err=%v review=%+v", err, fake.lastReview)
	}
	fake.err = status.Error(codes.FailedPrecondition, "stale revision")
	_, err = s.ReviewCorrection(trust.WithPrincipal(context.Background(), p), &timev1.ReviewCorrectionRequest{RequestId: "req-1", ExpectedRevision: 1, Decision: timev1.MissingPunchDecision_MISSING_PUNCH_DECISION_REJECTED, Reason: "changed", IdempotencyKey: "review-2"})
	if status.Code(err) != codes.FailedPrecondition || fake.reviews != 2 {
		t.Fatalf("stale err=%v reviews=%d", err, fake.reviews)
	}
}

func TestTodo_TCLOCK011_WorkflowFailureDoesNotAckHTTP(t *testing.T) {
	fake := &missingPunchFake{err: errors.New("workflow unavailable")}
	p := missingPunchPrincipal(t, trust.SubjectKindHuman)
	body, err := protojson.Marshal(validSubmitRequest())
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/v1/time/missing-punch/SubmitCorrection", strings.NewReader(string(body))).WithContext(trust.WithPrincipal(context.Background(), p))
	w := httptest.NewRecorder()
	NewMissingPunchServer(fake).HTTPHandler().ServeHTTP(w, r)
	if w.Code != 500 || strings.Contains(w.Body.String(), "request_id") || fake.submits != 1 {
		t.Fatalf("status=%d body=%s calls=%d", w.Code, w.Body.String(), fake.submits)
	}
}
