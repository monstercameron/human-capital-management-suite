package clockservice

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/punchpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timesession"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const (
	regTenant = "tenant-clock-reg"
	regWorker = "worker-clock-reg"
	regAssign = "assignment-clock-reg"
	regActor  = "actor-clock-reg"
)

var regNow = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

type regWorkers struct {
	active bool
	assign bool
}

func (w regWorkers) ResolveWorker(context.Context, string, string) (string, bool, error) {
	if !w.active {
		return "", false, nil
	}
	return regWorker, true, nil
}

func (w regWorkers) ResolveAssignment(context.Context, string, string, string) (string, string, bool, error) {
	if !w.assign {
		return "", "", false, nil
	}
	return "project-clock-reg", "site-clock-reg", true, nil
}

type regAuth struct{ delegated bool }

func (a regAuth) AuthorizePunch(context.Context, *trust.Principal, string, string, string) (bool, error) {
	return a.delegated, nil
}
func (regAuth) AuthorizeDeviceAdmin(context.Context, *trust.Principal, string, string) error {
	return nil
}
func (regAuth) AuthorizeSupervisorOverride(context.Context, *trust.Principal, string, string) error {
	return nil
}

type regIDs struct{ n int }

func (i *regIDs) Deterministic(parts ...string) string {
	if len(parts) > 0 && parts[len(parts)-1] != "" {
		return "session-" + parts[len(parts)-1]
	}
	i.n++
	return "id-" + string(rune('a'+i.n))
}
func (*regIDs) Random() string { return "random-clock-reg" }

type regWork struct {
	records      map[string]SessionRecord
	observations map[string]ObservationRecord
	fail         error
	duplicates   map[string]bool
	workCalls    int
}

func newRegWork() *regWork {
	return &regWork{records: make(map[string]SessionRecord), observations: make(map[string]ObservationRecord), duplicates: make(map[string]bool)}
}

func (w *regWork) Punch(_ context.Context, _ string, work PunchWork) (PunchResult, error) {
	w.workCalls++
	if w.fail != nil {
		return PunchResult{}, w.fail
	}
	if w.duplicates[work.Observation.IdempotencyKey] {
		return PunchResult{Session: work.Session, Observation: w.observations[work.Observation.IdempotencyKey], Duplicate: true}, nil
	}
	w.records[work.Session.ID] = work.Session
	w.observations[work.Observation.IdempotencyKey] = work.Observation
	return PunchResult{Session: work.Session, Observation: work.Observation}, nil
}

func (*regWork) Batch(context.Context, string, []PunchWork) ([]PunchResult, error) {
	return nil, errors.New("batch not used by punch regression")
}

type regSessions struct{ work *regWork }

func (s regSessions) CurrentSession(_ context.Context, _ string, worker, assignment string) (SessionRecord, error) {
	for _, record := range s.work.records {
		if record.WorkerRef == worker && record.AssignmentRef == assignment && (record.Status == string(timesession.StateOpen) || record.Status == string(timesession.StateOnBreak)) {
			return record, nil
		}
	}
	return SessionRecord{}, errors.New("current session missing")
}
func (regSessions) OpenSession(context.Context, string, SessionRecord, SessionEvent) (SessionRecord, error) {
	return SessionRecord{}, errors.New("open session should use unit of work")
}
func (regSessions) ApplyTransition(context.Context, string, string, uint64, SessionRecord, []SessionEvent) (SessionRecord, error) {
	return SessionRecord{}, errors.New("transition should use unit of work")
}

type regObservation struct{}

func (regObservation) AppendObservation(context.Context, string, ObservationRecord) (ObservationRecord, bool, error) {
	return ObservationRecord{}, false, errors.New("observation should use unit of work")
}
func (regObservation) ListObservations(context.Context, string, string, time.Time, time.Time, string, int) ([]ObservationRecord, string, error) {
	return nil, "", nil
}

type regPolicy struct{ set punchpolicy.QuestionSet }

func (p regPolicy) PunchPolicy(context.Context, string, string) (punchpolicy.Policy, error) {
	return punchpolicy.Policy{}, nil
}
func (p regPolicy) AttestationQuestions(context.Context, string, string) (punchpolicy.QuestionSet, error) {
	return p.set, nil
}

type regPremium struct{ calls int }

func (p *regPremium) RequestPremiumInput(context.Context, string, string, string, string, string, time.Time) error {
	p.calls++
	return nil
}

type regCase struct{ calls int }

func (c *regCase) OpenCaseTask(context.Context, string, string, string, string, string) error {
	c.calls++
	return nil
}

func regPrincipal(t *testing.T, subject string) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId(regTenant), Subject: subject, SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "session-clock-reg", IssuedAt: regNow.Add(-time.Minute), ExpiresAt: regNow.Add(time.Hour),
		CredentialDigest: "cred:clock-reg",
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func regService(w *regWorkers, auth regAuth, work *regWork) Service {
	ids := &regIDs{}
	return Service{Workers: *w, Auth: auth, Work: work, PunchWorkflow: regWorkflowExecutor{work: work}, Sessions: regSessions{work: work}, Observations: regObservation{}, IDs: ids, Clock: func() time.Time { return regNow }}
}

func regPunch(key string, at time.Time) PunchRequest {
	return PunchRequest{ClaimedWorkerRef: regWorker, AssignmentRef: regAssign, DeviceKind: "KIOSK", DeviceRef: "device-clock-reg", Method: timesession.IdentPIN, DeviceTime: at, IdempotencyKey: key, Scheduled: true}
}

func TestRegClockService_ClockInRejectsInvalidAndUnauthorizedClaims(t *testing.T) {
	cases := []struct {
		name string
		p    *trust.Principal
		w    regWorkers
		a    regAuth
		want error
	}{
		{"nil principal", nil, regWorkers{active: true, assign: true}, regAuth{}, ErrInvalidPrincipal},
		{"inactive worker", regPrincipal(t, regWorker), regWorkers{assign: true}, regAuth{}, ErrWorkerNotEligible},
		{"missing assignment", regPrincipal(t, regWorker), regWorkers{active: true}, regAuth{}, ErrAssignmentNotFound},
		{"delegation required", regPrincipal(t, regActor), regWorkers{active: true, assign: true}, regAuth{}, ErrDelegationRequired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			work := newRegWork()
			got := regService(&tc.w, tc.a, work)
			_, err := got.ClockIn(context.Background(), tc.p, regPunch("invalid-"+tc.name, regNow))
			if !errors.Is(err, tc.want) {
				t.Fatalf("ClockIn error = %v, want %v", err, tc.want)
			}
			if work.workCalls != 0 {
				t.Fatalf("rejected punch reached the commit port: %d calls", work.workCalls)
			}
		})
	}
}

func TestRegClockService_TransitionsAndDelegatedActor(t *testing.T) {
	work := newRegWork()
	svc := regService(&regWorkers{active: true, assign: true}, regAuth{delegated: true}, work)
	actor := regPrincipal(t, regActor)
	in, err := svc.ClockIn(context.Background(), actor, regPunch("in-reg", regNow))
	if err != nil || in.Status != ReceiptAccepted || in.State != timesession.StateOpen {
		t.Fatalf("ClockIn = %+v, %v", in, err)
	}
	brk, err := svc.StartBreak(context.Background(), actor, regPunch("break-reg", regNow.Add(time.Hour)))
	if err != nil || brk.State != timesession.StateOnBreak {
		t.Fatalf("StartBreak = %+v, %v", brk, err)
	}
	resume, err := svc.EndBreak(context.Background(), actor, regPunch("resume-reg", regNow.Add(2*time.Hour)))
	if err != nil || resume.State != timesession.StateOpen {
		t.Fatalf("EndBreak = %+v, %v", resume, err)
	}
	transfer := regPunch("transfer-reg", regNow.Add(3*time.Hour))
	transfer.JobRef = "job-b"
	transfer.CostCodeRef = "cost-b"
	moved, err := svc.TransferJob(context.Background(), actor, transfer)
	if err != nil || moved.State != timesession.StateOpen {
		t.Fatalf("TransferJob = %+v, %v", moved, err)
	}
	out, err := svc.ClockOut(context.Background(), actor, ClockOutRequest{PunchRequest: regPunch("out-reg", regNow.Add(4*time.Hour))})
	if err != nil || out.Status != ReceiptAccepted || out.State != timesession.StateClosed || out.Revision != 5 {
		t.Fatalf("ClockOut = %+v, %v", out, err)
	}
	var session timesession.Session
	if err := json.Unmarshal(work.records[in.SessionID].Payload, &session); err != nil {
		t.Fatal(err)
	}
	if len(session.Segments) != 4 || session.Segments[1].Kind != timesession.SegmentBreak || session.Segments[2].Kind != timesession.SegmentWork || session.Segments[3].Kind != timesession.SegmentJobTransfer {
		t.Fatalf("segments = %+v", session.Segments)
	}
}

func TestRegClockService_CommitFaultIsRetryableAndDoesNotPersist(t *testing.T) {
	work := newRegWork()
	work.fail = ErrRetryLater
	svc := regService(&regWorkers{active: true, assign: true}, regAuth{}, work)
	receipt, err := svc.ClockIn(context.Background(), regPrincipal(t, regWorker), regPunch("retry-reg", regNow))
	if !errors.Is(err, ErrRetryLater) || receipt.Status != ReceiptRetryLater {
		t.Fatalf("ClockIn fault = %+v, %v; want retry-later receipt", receipt, err)
	}
	if len(work.records) != 0 || len(work.observations) != 0 {
		t.Fatalf("failed unit of work persisted state: records=%d observations=%d", len(work.records), len(work.observations))
	}
}

func TestRegClockService_RejectsMissingShapeAndWiringBeforeCommit(t *testing.T) {
	work := newRegWork()
	svc := regService(&regWorkers{active: true, assign: true}, regAuth{}, work)
	p := regPrincipal(t, regWorker)
	cases := []struct {
		name string
		mut  func(*Service, *PunchRequest)
		want error
	}{
		{"missing device time", func(_ *Service, r *PunchRequest) { r.DeviceTime = time.Time{} }, ErrInvalidRequest},
		{"missing idempotency key", func(_ *Service, r *PunchRequest) { r.IdempotencyKey = "" }, ErrInvalidRequest},
		{"missing IDs port", func(s *Service, _ *PunchRequest) { s.IDs = nil }, ErrUnavailable},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			local := svc
			req := regPunch("shape-"+tc.name, regNow)
			tc.mut(&local, &req)
			if _, err := local.ClockIn(context.Background(), p, req); !errors.Is(err, tc.want) {
				t.Fatalf("ClockIn error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestRegClockService_IdempotentReplayReturnsDuplicateReceipt(t *testing.T) {
	work := newRegWork()
	svc := regService(&regWorkers{active: true, assign: true}, regAuth{}, work)
	p := regPrincipal(t, regWorker)
	req := regPunch("same-reg", regNow)
	first, err := svc.ClockIn(context.Background(), p, req)
	if err != nil || first.Status != ReceiptAccepted {
		t.Fatalf("first ClockIn = %+v, %v", first, err)
	}
	work.duplicates[req.IdempotencyKey] = true
	second, err := svc.ClockIn(context.Background(), p, req)
	if err != nil || second.Status != ReceiptDuplicate {
		t.Fatalf("replayed ClockIn = %+v, %v", second, err)
	}
}

func TestRegClockService_ClockOutAttestationDispatchesConsequences(t *testing.T) {
	work := newRegWork()
	premium := &regPremium{}
	caseTask := &regCase{}
	policy := regPolicy{set: punchpolicy.QuestionSet{ID: "qset-reg", Version: 1, Jurisdiction: "US-CA", Questions: []punchpolicy.Question{
		{ID: "break-reg", Kind: punchpolicy.QuestionBreakProvided, TextKey: punchpolicy.KeyBreakProvided, Required: true},
		{ID: "injury-reg", Kind: punchpolicy.QuestionInjury, TextKey: punchpolicy.KeyInjury, Required: true},
	}}}
	svc := regService(&regWorkers{active: true, assign: true}, regAuth{}, work)
	svc.Policies, svc.PremiumInputs, svc.CaseTasks = policy, premium, caseTask
	p := regPrincipal(t, regWorker)
	if _, err := svc.ClockIn(context.Background(), p, regPunch("att-in-reg", regNow)); err != nil {
		t.Fatal(err)
	}
	out, err := svc.ClockOut(context.Background(), p, ClockOutRequest{PunchRequest: regPunch("att-out-reg", regNow.Add(time.Hour)), SiteID: "site-clock-reg", Answers: []punchAnswer{{QuestionID: "break-reg", Value: "false"}, {QuestionID: "injury-reg", Value: "true"}}})
	if err != nil || out.State != timesession.StateClosed || premium.calls != 1 || caseTask.calls != 1 {
		t.Fatalf("ClockOut = %+v, %v; premium=%d case=%d", out, err, premium.calls, caseTask.calls)
	}
}

func TestRegClockService_MissingRequiredAttestationLeavesCommittedPunch(t *testing.T) {
	work := newRegWork()
	svc := regService(&regWorkers{active: true, assign: true}, regAuth{}, work)
	svc.Policies = regPolicy{set: punchpolicy.QuestionSet{ID: "qset-required-reg", Version: 1, Jurisdiction: "US-CA", Questions: []punchpolicy.Question{
		{ID: "break-required-reg", Kind: punchpolicy.QuestionBreakProvided, TextKey: punchpolicy.KeyBreakProvided, Required: true},
		{ID: "injury-required-reg", Kind: punchpolicy.QuestionInjury, TextKey: punchpolicy.KeyInjury, Required: true},
	}}}
	p := regPrincipal(t, regWorker)
	if _, err := svc.ClockIn(context.Background(), p, regPunch("required-in-reg", regNow)); err != nil {
		t.Fatal(err)
	}
	closed, err := svc.ClockOut(context.Background(), p, ClockOutRequest{PunchRequest: regPunch("required-out-reg", regNow.Add(time.Hour)), SiteID: "site-clock-reg", Answers: []punchAnswer{{QuestionID: "break-required-reg", Value: "true"}}})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("ClockOut error = %v, want ErrInvalidRequest", err)
	}
	if closed.State != timesession.StateClosed || len(work.records) != 1 {
		t.Fatalf("required-answer failure must retain authoritative OUT commit: receipt=%+v records=%d", closed, len(work.records))
	}
}
