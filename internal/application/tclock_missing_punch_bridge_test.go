package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	clockservice "github.com/monstercameron/human-capital-management-suite/internal/application/clockservice"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	transporttimeclock "github.com/monstercameron/human-capital-management-suite/internal/transport/timeclock"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type bridgeResolver struct {
	session MissingPunchSessionFacts
	request MissingPunchRequestFacts
	err     error
	calls   int
}

func (r *bridgeResolver) ResolveMissingPunchSession(context.Context, *trust.Principal, string) (MissingPunchSessionFacts, error) {
	r.calls++
	return r.session, r.err
}
func (r *bridgeResolver) ResolveMissingPunchRequest(context.Context, *trust.Principal, string) (MissingPunchRequestFacts, error) {
	r.calls++
	return r.request, r.err
}

type bridgeSessionReader struct {
	row clockservice.MissingPunchSession
}

func (f bridgeSessionReader) GetMissingPunchSession(context.Context, string, string) (clockservice.MissingPunchSession, error) {
	return f.row, nil
}

type bridgeObservationReader struct {
	row clockservice.MissingPunchObservation
}

func (f bridgeObservationReader) GetMissingPunchObservation(context.Context, string, string) (clockservice.MissingPunchObservation, error) {
	return f.row, nil
}

type bridgeReviewReader struct {
	row clockservice.MissingPunchRecord
}

func (f bridgeReviewReader) GetMissingPunchRequest(context.Context, string, string) (clockservice.MissingPunchRecord, error) {
	return f.row, nil
}

type bridgeAuthorization struct{}

func (bridgeAuthorization) AuthorizeRequest(context.Context, *trust.Principal, string, string) error {
	return nil
}
func (bridgeAuthorization) AuthorizeDecision(context.Context, *trust.Principal, string, string) error {
	return nil
}

type bridgeWorkflow struct {
	result clockservice.MissingPunchWorkflowResult
}

type bridgeReadStore struct {
	context                 transporttimeclock.MissingPunchCorrectionContext
	rows                    []clockservice.MissingPunchRecord
	contextCalls, listCalls int
}

func (s *bridgeReadStore) GetMissingPunchCorrectionContext(context.Context, string, string) (transporttimeclock.MissingPunchCorrectionContext, []clockservice.MissingPunchRecord, error) {
	s.contextCalls++
	return s.context, s.rows, nil
}
func (s *bridgeReadStore) ListPendingMissingPunch(context.Context, string, []string, uint32) ([]clockservice.MissingPunchRecord, error) {
	s.listCalls++
	return s.rows, nil
}

type bridgeReadAuth struct {
	contextCalls, pendingCalls int
	err                        error
}

func (a *bridgeReadAuth) AuthorizeMissingPunchContext(context.Context, *trust.Principal, string, string) error {
	a.contextCalls++
	return a.err
}
func (a *bridgeReadAuth) AuthorizeMissingPunchPending(context.Context, *trust.Principal, string) ([]string, error) {
	a.pendingCalls++
	if a.err != nil {
		return nil, a.err
	}
	return []string{"worker"}, nil
}

func (f bridgeWorkflow) ExecuteMissingPunch(context.Context, clockservice.MissingPunchWorkflowRequest) (clockservice.MissingPunchWorkflowResult, error) {
	return f.result, nil
}

func bridgePrincipal(t *testing.T) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant", Subject: "worker", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session", IssuedAt: time.Unix(1, 0), ExpiresAt: time.Unix(1000, 0), CredentialDigest: "digest"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func bridgeService() clockservice.MissingPunchService {
	now := time.Unix(100, 0).UTC()
	original := clock.TimeObservation{Accepted: true, Tenant: "tenant", EventType: clock.EventClockIn, WorkerRef: "worker", OccurredAt: time.Unix(10, 0)}
	record := clockservice.MissingPunchRecord{ID: "request", TenantID: "tenant", WorkerRef: "worker", SessionID: "session", OriginalObservationID: "observation", ClaimedOutAt: time.Unix(90, 0), Reason: "forgot", RequestedBy: "worker", Decision: "PENDING", Revision: 1, ExpectedSessionRevision: 1, OriginalWorkflowInstanceRef: "00000000-0000-0000-0000-000000000001", WorkflowInstanceID: "00000000-0000-0000-0000-000000000002", WorkflowTraceID: "trace", WorkflowNodeID: "commit_missing_punch_request", WorkflowPlanDigest: "digest", WorkflowAttempt: 1, WorkflowInstanceVersion: 1}
	return clockservice.MissingPunchService{
		Sessions:     bridgeSessionReader{row: clockservice.MissingPunchSession{TenantID: "tenant", ID: "session", WorkerRef: "worker", Status: "OPEN", Revision: 1, MissingOut: true, OriginalWorkflowInstanceRef: "00000000-0000-0000-0000-000000000001"}},
		Observations: bridgeObservationReader{row: clockservice.MissingPunchObservation{ID: "observation", TenantID: "tenant", Observation: original}},
		Reviews:      bridgeReviewReader{row: record}, Authorization: bridgeAuthorization{},
		Workflow: bridgeWorkflow{result: clockservice.MissingPunchWorkflowResult{Record: record, Committed: true, InstanceID: mustUUID(), WorkflowID: clockservice.MissingPunchWorkflowID, PlanDigest: "digest", TraceID: "trace", NodeID: "commit_missing_punch_request", Attempt: 1, InstanceVersion: 1}}, Clock: func() time.Time { return now },
	}
}

func mustUUID() uuid.UUID { return uuid.MustParse("00000000-0000-0000-0000-000000000001") }

func TestTodo_TCLOCK011_BridgeDerivesServerFactsAndReturnsReceipt(t *testing.T) {
	r := &bridgeResolver{session: MissingPunchSessionFacts{WorkerRef: "worker", OriginalObservationID: "observation", ExpectedRevision: 1}}
	b := NewMissingPunchBridge(bridgeService(), r)
	got, err := b.SubmitCorrection(context.Background(), bridgePrincipal(t), transporttimeclock.MissingPunchSubmit{SessionID: "session", ClaimedEventType: "CLOCK_OUT", ClaimedOccurredAt: time.Unix(90, 0), ExpectedRevision: 1, Reason: "forgot", IdempotencyKey: "idem"})
	if err != nil || r.calls != 1 || got.RequestID != "request" || got.WorkerRef != "worker" || got.WorkflowReceipt.WorkflowTraceID != "trace" {
		t.Fatalf("got=%+v err=%v calls=%d", got, err, r.calls)
	}
}

func TestTodo_TCLOCK011_BridgeRejectsInvalidBeforeResolver(t *testing.T) {
	r := &bridgeResolver{}
	b := NewMissingPunchBridge(bridgeService(), r)
	_, err := b.SubmitCorrection(context.Background(), bridgePrincipal(t), transporttimeclock.MissingPunchSubmit{SessionID: "session", ClaimedOccurredAt: time.Unix(90, 0), ExpectedRevision: 1, Reason: "forgot", IdempotencyKey: "idem"})
	if !errors.Is(err, clockservice.ErrMissingPunchInvalid) || r.calls != 0 {
		t.Fatalf("err=%v calls=%d", err, r.calls)
	}
}

func TestTodo_TCLOCK011_BridgeRejectsMissingConfiguration(t *testing.T) {
	b := NewMissingPunchBridge(clockservice.MissingPunchService{}, nil)
	_, err := b.ReviewCorrection(context.Background(), bridgePrincipal(t), transporttimeclock.MissingPunchReview{RequestID: "request", ExpectedRevision: 1, Decision: "APPROVED", Reason: "verified", IdempotencyKey: "idem"})
	if !errors.Is(err, clockservice.ErrMissingPunchUnavailable) {
		t.Fatalf("err=%v", err)
	}
}

func TestTodo_TCLOCK011_BridgeRejectsIncompleteWorkflowMetadata(t *testing.T) {
	record := clockservice.MissingPunchRecord{ID: "request", TenantID: "tenant", WorkerRef: "worker", SessionID: "session", ClaimedOutAt: time.Unix(90, 0), Revision: 2, WorkflowInstanceID: "00000000-0000-0000-0000-000000000001", WorkflowTraceID: "trace", WorkflowNodeID: "commit_missing_punch_request", WorkflowPlanDigest: "digest"}
	if _, err := missingPunchCorrection(record, "PENDING", "forgot"); !errors.Is(err, clockservice.ErrMissingPunchUnavailable) {
		t.Fatalf("err=%v", err)
	}
}

func TestTodo_TCLOCK011_ReadBridgeAuthorizesBeforeLookupAndMapsRows(t *testing.T) {
	row := bridgeService().Workflow.(bridgeWorkflow).result.Record
	store := &bridgeReadStore{context: transporttimeclock.MissingPunchCorrectionContext{SessionID: "session", WorkerRef: "worker", Revision: 1, OriginalWorkflowID: "clock_workflow", OriginalWorkflowInstanceRef: "00000000-0000-0000-0000-000000000001", PeriodClosed: false}, rows: []clockservice.MissingPunchRecord{row}}
	auth := &bridgeReadAuth{}
	b := NewMissingPunchBridge(clockservice.MissingPunchService{}, nil)
	b.ReadStore, b.ReadAuthorization = store, auth
	got, pending, err := b.GetCorrectionContext(context.Background(), bridgePrincipal(t), "session")
	if err != nil || got.SessionID != "session" || len(pending) != 1 || auth.contextCalls != 1 || store.contextCalls != 1 {
		t.Fatalf("context=%+v pending=%d err=%v auth=%d store=%d", got, len(pending), err, auth.contextCalls, store.contextCalls)
	}
	rows, err := b.ListPendingCorrections(context.Background(), bridgePrincipal(t), 10)
	if err != nil || len(rows) != 1 || auth.pendingCalls != 1 || store.listCalls != 1 {
		t.Fatalf("rows=%d err=%v auth=%d store=%d", len(rows), err, auth.pendingCalls, store.listCalls)
	}
}

func TestTodo_TCLOCK011_ReadBridgeFailsClosedBeforeUnauthorizedLookup(t *testing.T) {
	store := &bridgeReadStore{}
	auth := &bridgeReadAuth{err: errors.New("outside supervisor scope")}
	b := NewMissingPunchBridge(clockservice.MissingPunchService{}, nil)
	b.ReadStore, b.ReadAuthorization = store, auth
	_, _, err := b.GetCorrectionContext(context.Background(), bridgePrincipal(t), "session")
	if err == nil || auth.contextCalls != 1 || store.contextCalls != 0 {
		t.Fatalf("err=%v auth=%d store=%d", err, auth.contextCalls, store.contextCalls)
	}
}

func TestTodo_TCLOCK011_TimestoreReadStoreRequiresStore(t *testing.T) {
	s := TimestoreMissingPunchReadStore{}
	if _, _, err := s.GetMissingPunchCorrectionContext(context.Background(), "tenant", "session"); !errors.Is(err, clockservice.ErrMissingPunchUnavailable) {
		t.Fatalf("context err=%v", err)
	}
	if _, err := s.ListPendingMissingPunch(context.Background(), "tenant", []string{"worker"}, 10); !errors.Is(err, clockservice.ErrMissingPunchUnavailable) {
		t.Fatalf("list err=%v", err)
	}
}
