package clockservice

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/clock"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type missingSessionFake struct{ row MissingPunchSession }

func (f missingSessionFake) GetMissingPunchSession(context.Context, string, string) (MissingPunchSession, error) {
	return f.row, nil
}

type missingObsFake struct{ row MissingPunchObservation }

func (f missingObsFake) GetMissingPunchObservation(context.Context, string, string) (MissingPunchObservation, error) {
	return f.row, nil
}

type missingReviewFake struct{ row MissingPunchRecord }

func (f missingReviewFake) GetMissingPunchRequest(context.Context, string, string) (MissingPunchRecord, error) {
	return f.row, nil
}

type missingAuthFake struct{ request, decision error }

func (f missingAuthFake) AuthorizeRequest(context.Context, *trust.Principal, string, string) error {
	return f.request
}
func (f missingAuthFake) AuthorizeDecision(context.Context, *trust.Principal, string, string) error {
	return f.decision
}

type missingWorkflowFake struct {
	result MissingPunchWorkflowResult
	input  MissingPunchWorkflowRequest
	calls  int
}

func (f *missingWorkflowFake) ExecuteMissingPunch(_ context.Context, in MissingPunchWorkflowRequest) (MissingPunchWorkflowResult, error) {
	f.calls++
	f.input = in
	if in.Action == "DECIDE" {
		f.result.NodeID = "append_correction"
		f.result.Record.WorkflowNodeID = f.result.NodeID
	}
	return f.result, nil
}

type missingCorrectionFake struct {
	calls int
	row   ObservationRecord
}

func (f *missingCorrectionFake) AppendMissingPunchCorrection(_ context.Context, _ string, _ string, _ uint64, row ObservationRecord) (ObservationRecord, bool, error) {
	f.calls++
	f.row = row
	return row, false, nil
}

func missingPrincipal(t *testing.T, subject string) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant-a"), Subject: subject, SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "sess", IssuedAt: time.Unix(1, 0), ExpiresAt: time.Unix(1000, 0), CredentialDigest: "cred"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func missingFixture(t *testing.T) (MissingPunchService, *missingWorkflowFake, *missingCorrectionFake) {
	now := time.Unix(200, 0).UTC()
	original := clock.TimeObservation{Accepted: true, Tenant: values.TenantId("tenant-a"), EventType: clock.EventClockIn, WorkerRef: "worker-1", OccurredAt: now.Add(-time.Hour), Digest: "sha256:in"}
	instance := uuid.New()
	originalInstance := uuid.New().String()
	wf := &missingWorkflowFake{result: MissingPunchWorkflowResult{Record: MissingPunchRecord{ID: "req-1", TenantID: "tenant-a", WorkerRef: "worker-1", SessionID: "session-1", CreatedAt: now, Decision: "PENDING", OriginalWorkflowInstanceRef: originalInstance, OriginalObservationID: "obs-in", ClaimedOutAt: time.Unix(150, 0).UTC(), Reason: "forgot", RequestedBy: "worker-1", Revision: 1, ExpectedSessionRevision: 1, WorkflowInstanceID: instance.String(), WorkflowTraceID: "trace", WorkflowNodeID: "commit_missing_punch_request", WorkflowPlanDigest: "plan", WorkflowAttempt: 1, WorkflowInstanceVersion: 1}, InstanceID: instance, WorkflowID: MissingPunchWorkflowID, PlanDigest: "plan", TraceID: "trace", NodeID: "commit_missing_punch_request", Attempt: 1, InstanceVersion: 1, Committed: true}}
	correction := &missingCorrectionFake{}
	s := MissingPunchService{Sessions: missingSessionFake{row: MissingPunchSession{TenantID: "tenant-a", ID: "session-1", WorkerRef: "worker-1", Status: "OPEN", Revision: 1, MissingOut: true, OriginalWorkflowInstanceRef: originalInstance}}, Observations: missingObsFake{row: MissingPunchObservation{ID: "obs-in", TenantID: "tenant-a", Observation: original}}, Authorization: missingAuthFake{}, Workflow: wf, Clock: func() time.Time { return now }}
	review := wf.result.Record
	review.Revision = 1
	s.Reviews = missingReviewFake{row: review}
	return s, wf, correction
}

func TestTodo_TCLOCK_011_RequestRequiresWorkflowCommitAndPreservesNoCorrection(t *testing.T) {
	s, wf, correction := missingFixture(t)
	got, err := s.RequestMissingPunch(context.Background(), missingPrincipal(t, "worker-1"), MissingPunchRequest{SessionID: "session-1", WorkerRef: "worker-1", OriginalObservationID: "obs-in", ProposedOutAt: time.Unix(150, 0), Reason: "forgot", IdempotencyKey: "idem-1", ExpectedRevision: 1})
	if err != nil || got.ID != "req-1" || wf.calls != 1 || correction.calls != 0 {
		t.Fatalf("request = %#v, err=%v, workflow=%d corrections=%d", got, err, wf.calls, correction.calls)
	}
}

func TestTodo_TCLOCK_011_SecurityRejectsUnauthorizedAndInvalidTemporalClaim(t *testing.T) {
	s, _, _ := missingFixture(t)
	s.Authorization = missingAuthFake{request: errors.New("outside scope")}
	_, err := s.RequestMissingPunch(context.Background(), missingPrincipal(t, "worker-1"), MissingPunchRequest{SessionID: "session-1", WorkerRef: "worker-1", OriginalObservationID: "obs-in", ProposedOutAt: time.Unix(150, 0), Reason: "forgot", IdempotencyKey: "idem-1", ExpectedRevision: 1})
	if !errors.Is(err, ErrMissingPunchForbidden) {
		t.Fatalf("auth error = %v", err)
	}
	s.Authorization = missingAuthFake{}
	_, err = s.RequestMissingPunch(context.Background(), missingPrincipal(t, "worker-1"), MissingPunchRequest{SessionID: "session-1", WorkerRef: "worker-1", OriginalObservationID: "obs-in", ProposedOutAt: time.Unix(-4000, 0), Reason: "forgot", IdempotencyKey: "idem-1", ExpectedRevision: 1})
	if !errors.Is(err, ErrMissingPunchInvalid) {
		t.Fatalf("temporal error = %v", err)
	}
}

func TestTodo_TCLOCK_011_ApprovalAppendsOnlyAfterCommittedWorkflow(t *testing.T) {
	s, wf, correction := missingFixture(t)
	wf.result.Record.DecidedBy = "supervisor-1"
	wf.result.Record.Decision = "APPROVED"
	wf.result.Record.Revision = 2
	wf.result.NodeID = "append_correction"
	got, err := s.DecideMissingPunch(context.Background(), missingPrincipal(t, "supervisor-1"), MissingPunchDecision{RequestID: "req-1", WorkerRef: "worker-1", Approve: true, IdempotencyKey: "decision-1", ExpectedRevision: 1})
	if err != nil || got.ID != "req-1" || wf.calls != 1 || correction.calls != 0 || wf.input.Action != "DECIDE" {
		t.Fatalf("approval = %#v, err=%v, workflow=%d correction=%d", got, err, wf.calls, correction.calls)
	}
}

func TestTodo_TCLOCK_011_ClosedPeriodNeedsTypedReopen(t *testing.T) {
	s, wf, correction := missingFixture(t)
	s.Reviews = missingReviewFake{row: MissingPunchRecord{ID: "req-1", TenantID: "tenant-a", WorkerRef: "worker-1", Revision: 1, PeriodClosed: true}}
	wf.result.Record.Revision = 2
	wf.result.NodeID = "append_correction"
	_, err := s.DecideMissingPunch(context.Background(), missingPrincipal(t, "supervisor-1"), MissingPunchDecision{RequestID: "req-1", WorkerRef: "worker-1", Approve: true, PeriodClosed: true, IdempotencyKey: "decision-1", ExpectedRevision: 1})
	if !errors.Is(err, ErrMissingPunchClosed) || correction.calls != 0 {
		t.Fatalf("closed period = %v corrections=%d", err, correction.calls)
	}
}

func TestTodo_TCLOCK_011(t *testing.T) {
	s, wf, _ := missingFixture(t)
	got, err := s.RequestMissingPunch(context.Background(), missingPrincipal(t, "worker-1"), MissingPunchRequest{SessionID: "session-1", WorkerRef: "worker-1", OriginalObservationID: "obs-in", ProposedOutAt: time.Unix(150, 0), Reason: "forgot", IdempotencyKey: "idem-primary", ExpectedRevision: 1})
	if err != nil || got.Decision != "PENDING" || wf.input.Action != "REQUEST" {
		t.Fatalf("request=%+v err=%v input=%+v", got, err, wf.input)
	}
	wf.result.Record.DecidedBy = "supervisor-1"
	wf.result.Record.Decision = "APPROVED"
	wf.result.Record.Revision = 2
	wf.result.NodeID = "append_correction"
	got, err = s.DecideMissingPunch(context.Background(), missingPrincipal(t, "supervisor-1"), MissingPunchDecision{RequestID: "req-1", WorkerRef: "worker-1", Approve: true, DecisionNote: "verified", IdempotencyKey: "decision-primary", ExpectedRevision: 1})
	if err != nil || got.Decision != "APPROVED" || wf.input.Action != "DECIDE" || wf.input.Actor != "supervisor-1" {
		t.Fatalf("decision=%+v err=%v input=%+v", got, err, wf.input)
	}
}

func TestTodo_TCLOCK_011_Security(t *testing.T) {
	s, wf, _ := missingFixture(t)
	s.Reviews = missingReviewFake{row: MissingPunchRecord{ID: "req-1", TenantID: "tenant-a", WorkerRef: "worker-1", SessionID: "session-1", OriginalObservationID: "obs-in", OriginalWorkflowInstanceRef: uuid.NewString(), Decision: "APPROVED", Revision: 1}}
	_, err := s.DecideMissingPunch(context.Background(), missingPrincipal(t, "supervisor-1"), MissingPunchDecision{RequestID: "req-1", WorkerRef: "worker-1", Approve: true, IdempotencyKey: "security", ExpectedRevision: 1})
	if !errors.Is(err, ErrMissingPunchConflict) || wf.calls != 0 {
		t.Fatalf("terminal review accepted: err=%v workflow=%d", err, wf.calls)
	}
	s, wf, _ = missingFixture(t)
	s.Reviews = missingReviewFake{row: wf.result.Record}
	_, err = s.DecideMissingPunch(context.Background(), missingPrincipal(t, "supervisor-1"), MissingPunchDecision{RequestID: "req-1", WorkerRef: "worker-1", Approve: false, IdempotencyKey: "security-reject", ExpectedRevision: 1})
	if !errors.Is(err, ErrMissingPunchInvalid) || wf.calls != 0 {
		t.Fatalf("reasonless rejection accepted: err=%v workflow=%d", err, wf.calls)
	}
}

func TestTodo_TCLOCK_011_Integration(t *testing.T) {
	s, wf, _ := missingFixture(t)
	_, err := s.RequestMissingPunch(context.Background(), missingPrincipal(t, "worker-1"), MissingPunchRequest{SessionID: "session-1", WorkerRef: "worker-1", OriginalObservationID: "obs-in", ProposedOutAt: time.Unix(150, 0), Reason: "forgot", IdempotencyKey: "idem-integration", ExpectedRevision: 1})
	if err != nil || wf.input.OriginalWorkflowInstanceRef == "" || wf.input.ExpectedRevision != 1 || wf.input.PeriodClosed {
		t.Fatalf("workflow boundary lost authoritative facts: err=%v input=%+v", err, wf.input)
	}
}

func TestTodo_TCLOCK_011_Race(t *testing.T) {
	const workers = 8
	results := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func() {
			s, _, _ := missingFixture(t)
			_, err := s.RequestMissingPunch(context.Background(), missingPrincipal(t, "worker-1"), MissingPunchRequest{SessionID: "session-1", WorkerRef: "worker-1", OriginalObservationID: "obs-in", ProposedOutAt: time.Unix(150, 0), Reason: "forgot", IdempotencyKey: "idem-race", ExpectedRevision: 1})
			results <- err
		}()
	}
	for i := 0; i < workers; i++ {
		if err := <-results; err != nil {
			t.Fatalf("concurrent request failed: %v", err)
		}
	}
}

func TestTodo_TCLOCK_011_RejectsMismatchedDecisionEvidence(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*MissingPunchRecord)
	}{
		{"session", func(r *MissingPunchRecord) { r.SessionID = "foreign" }},
		{"decider", func(r *MissingPunchRecord) { r.DecidedBy = "foreign" }},
		{"decision", func(r *MissingPunchRecord) { r.Decision = "REJECTED" }},
		{"period", func(r *MissingPunchRecord) { r.PeriodClosed = true }},
		{"reopen", func(r *MissingPunchRecord) { r.ReopenRef = "unrelated" }},
		{"original workflow", func(r *MissingPunchRecord) { r.OriginalWorkflowInstanceRef = "foreign" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, wf, _ := missingFixture(t)
			wf.result.Record.DecidedBy = "supervisor-1"
			wf.result.Record.Decision = "APPROVED"
			tc.mutate(&wf.result.Record)
			_, err := s.DecideMissingPunch(context.Background(), missingPrincipal(t, "supervisor-1"), MissingPunchDecision{RequestID: "req-1", WorkerRef: "worker-1", Approve: true, IdempotencyKey: "decision-1", ExpectedRevision: 1})
			if !errors.Is(err, ErrMissingPunchConflict) {
				t.Fatalf("mismatch accepted: %v", err)
			}
		})
	}
}

type missingReviewCounter struct{ calls int }

func (f *missingReviewCounter) GetMissingPunchRequest(context.Context, string, string) (MissingPunchRecord, error) {
	f.calls++
	return MissingPunchRecord{}, nil
}
func TestTodo_TCLOCK_011_AuthorizesBeforeReadingDecisionRequest(t *testing.T) {
	s, wf, _ := missingFixture(t)
	reader := &missingReviewCounter{}
	s.Reviews = reader
	s.Authorization = missingAuthFake{decision: errors.New("outside scope")}
	_, err := s.DecideMissingPunch(context.Background(), missingPrincipal(t, "supervisor-1"), MissingPunchDecision{RequestID: "foreign", WorkerRef: "worker-1", ExpectedRevision: 1, IdempotencyKey: "decision-1"})
	if !errors.Is(err, ErrMissingPunchForbidden) || reader.calls != 0 || wf.calls != 0 {
		t.Fatalf("err=%v reads=%d workflow=%d", err, reader.calls, wf.calls)
	}
}

func TestTodo_TCLOCK_011_RequestRejectsUnboundProjection(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*MissingPunchRecord)
	}{
		{"missing id", func(r *MissingPunchRecord) { r.ID = "" }},
		{"missing creation time", func(r *MissingPunchRecord) { r.CreatedAt = time.Time{} }},
		{"future creation time", func(r *MissingPunchRecord) { r.CreatedAt = time.Unix(201, 0) }},
		{"premature approval", func(r *MissingPunchRecord) { r.Decision = "APPROVED" }},
		{"foreign workflow lineage", func(r *MissingPunchRecord) { r.OriginalWorkflowInstanceRef = uuid.NewString() }},
		{"forged period", func(r *MissingPunchRecord) { r.PeriodClosed = true }},
		{"foreign requester", func(r *MissingPunchRecord) { r.RequestedBy = "foreign" }},
		{"wrong base revision", func(r *MissingPunchRecord) { r.ExpectedSessionRevision = 2 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			service, workflow, _ := missingFixture(t)
			tc.mutate(&workflow.result.Record)
			_, err := service.RequestMissingPunch(context.Background(), missingPrincipal(t, "worker-1"), MissingPunchRequest{SessionID: "session-1", WorkerRef: "worker-1", OriginalObservationID: "obs-in", ProposedOutAt: time.Unix(150, 0), Reason: "forgot", IdempotencyKey: "idem-1", ExpectedRevision: 1})
			if !errors.Is(err, ErrMissingPunchConflict) {
				t.Fatalf("unbound projection accepted: %v", err)
			}
		})
	}
}
