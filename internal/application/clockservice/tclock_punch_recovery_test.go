package clockservice

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/timesession"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type recoveryObservationStore struct {
	rows        []ObservationRecord
	lookupCalls *int
}

func recoveryPrincipal(t *testing.T, subject string, expiresAt time.Time) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId(regTenant), Subject: subject, SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh,
		SessionRef: "session-clock-recovery", IssuedAt: regNow.Add(-time.Minute), ExpiresAt: expiresAt,
		CredentialDigest: "cred:clock-recovery",
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func (s recoveryObservationStore) AppendObservation(context.Context, string, ObservationRecord) (ObservationRecord, bool, error) {
	return ObservationRecord{}, false, errors.New("append not used")
}

func (s recoveryObservationStore) ListObservations(_ context.Context, tenant, worker string, _, _ time.Time, cursor string, limit int) ([]ObservationRecord, string, error) {
	if s.lookupCalls != nil {
		*s.lookupCalls++
	}
	if cursor != "" {
		return nil, "", nil
	}
	var out []ObservationRecord
	for _, row := range s.rows {
		if row.TenantID == tenant && row.WorkerRef == worker {
			out = append(out, row)
		}
	}
	if len(out) > limit {
		return out[:limit], out[limit-1].ID, nil
	}
	return out, "", nil
}

func TestTodo_TCLOCK003_PunchReplayUsesImmutableObservationIdentity(t *testing.T) {
	work := newRegWork()
	workers := &regWorkers{active: true, assign: true}
	svc := regService(workers, regAuth{}, work)
	principal := recoveryPrincipal(t, regWorker, regNow.Add(3*time.Hour))
	first, err := svc.ClockIn(context.Background(), principal, regPunch("replay-in", regNow))
	if err != nil {
		t.Fatal(err)
	}
	prior := work.observations["replay-in"]
	svc.Observations = recoveryObservationStore{rows: []ObservationRecord{prior}}
	svc.Clock = func() time.Time { return regNow.Add(17 * time.Minute) }
	replayed, err := svc.ClockIn(context.Background(), principal, regPunch("replay-in", regNow))
	if err != nil || replayed.Status != ReceiptDuplicate || replayed.ObservationID != first.ObservationID || replayed.Revision != first.Revision {
		t.Fatalf("replay=%+v err=%v first=%+v", replayed, err, first)
	}
	changed := regPunch("replay-in", regNow.Add(time.Second))
	if _, err := svc.ClockIn(context.Background(), principal, changed); !errors.Is(err, ErrPunchIdempotencyConflict) {
		t.Fatalf("changed retry error=%v, want idempotency conflict", err)
	}
}

func TestTodo_TCLOCK003_ClosedOutReplayDoesNotRequireOpenSession(t *testing.T) {
	work := newRegWork()
	workers := &regWorkers{active: true, assign: true}
	svc := regService(workers, regAuth{}, work)
	principal := recoveryPrincipal(t, regWorker, regNow.Add(3*time.Hour))
	if _, err := svc.ClockIn(context.Background(), principal, regPunch("replay-out-in", regNow)); err != nil {
		t.Fatal(err)
	}
	outReq := regPunch("replay-out", regNow.Add(time.Hour))
	first, err := svc.ClockOut(context.Background(), principal, ClockOutRequest{PunchRequest: outReq})
	if err != nil {
		t.Fatal(err)
	}
	rows := make([]ObservationRecord, 0, len(work.observations))
	for _, row := range work.observations {
		rows = append(rows, row)
	}
	svc.Observations = recoveryObservationStore{rows: rows}
	svc.Clock = func() time.Time { return regNow.Add(2 * time.Hour) }
	replayed, err := svc.ClockOut(context.Background(), principal, ClockOutRequest{PunchRequest: outReq})
	if err != nil || replayed.Status != ReceiptDuplicate || replayed.State != first.State || replayed.ObservationID != first.ObservationID {
		t.Fatalf("closed replay=%+v err=%v first=%+v", replayed, err, first)
	}
}

func TestTodo_TCLOCK003_ExpiredPrincipalRejectedBeforeReplayLookup(t *testing.T) {
	calls := 0
	store := recoveryObservationStore{lookupCalls: &calls}
	svc := regService(&regWorkers{active: true, assign: true}, regAuth{}, newRegWork())
	svc.Observations = store
	svc.Clock = func() time.Time { return regNow.Add(2 * time.Hour) }
	_, err := svc.ClockIn(context.Background(), recoveryPrincipal(t, regWorker, regNow.Add(time.Hour)), regPunch("expired-replay", regNow))
	if !errors.Is(err, ErrInvalidPrincipal) || calls != 0 {
		t.Fatalf("expired replay err=%v lookup calls=%d", err, calls)
	}
}

func TestTodo_TCLOCK003_WebReplayPinsRevisionWithoutRecapturingTime(t *testing.T) {
	work := newRegWork()
	svc := regService(&regWorkers{active: true, assign: true}, regAuth{}, work)
	principal := recoveryPrincipal(t, regWorker, regNow.Add(3*time.Hour))
	req := regPunch("web-replay", regNow)
	req.DeviceKind, req.ExpectedProjectionRevision = "WEB", 1
	first, err := svc.ClockIn(context.Background(), principal, req)
	if err != nil {
		t.Fatal(err)
	}
	svc.Observations = recoveryObservationStore{rows: []ObservationRecord{work.observations[req.IdempotencyKey]}}
	svc.Clock = func() time.Time { return regNow.Add(time.Minute) }
	req.DeviceTime = regNow.Add(time.Minute)
	replayed, err := svc.ClockIn(context.Background(), principal, req)
	if err != nil || replayed.Status != ReceiptDuplicate || replayed.ObservationID != first.ObservationID {
		t.Fatalf("replay=%+v first=%+v err=%v", replayed, first, err)
	}
	req.ExpectedProjectionRevision++
	if _, err := svc.ClockIn(context.Background(), principal, req); !errors.Is(err, ErrPunchIdempotencyConflict) {
		t.Fatalf("changed revision error=%v", err)
	}
}

func TestTodo_TCLOCK003_UnversionedDigestPreservesV2Identity(t *testing.T) {
	req := regPunch("legacy-digest", regNow)
	want := punchDigest("hcmnext.clock.punch/v2", regTenant, regWorker, regWorker, "false", string(timesession.PunchIn), req.AssignmentRef, req.JobRef, req.CostCodeRef, "false", req.DeviceKind, req.DeviceRef, string(req.Method), req.DeviceTime.UTC().Format(time.RFC3339Nano), "true", req.IdempotencyKey)
	if got := punchInputDigest(regTenant, regWorker, regWorker, false, timesession.PunchIn, req); got != want {
		t.Fatalf("digest=%s want=%s", got, want)
	}
}
