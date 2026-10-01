package clockservice

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/punchpolicy"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/timesession"
)

type attestObservationStore struct {
	rows []ObservationRecord
}

func (s *attestObservationStore) AppendObservation(_ context.Context, _ string, row ObservationRecord) (ObservationRecord, bool, error) {
	for _, prior := range s.rows {
		if prior.Source == row.Source && prior.IdempotencyKey == row.IdempotencyKey {
			if prior.Digest != row.Digest {
				return ObservationRecord{}, false, ErrPunchIdempotencyConflict
			}
			return prior, true, nil
		}
	}
	s.rows = append(s.rows, row)
	return row, false, nil
}

func (s *attestObservationStore) ListObservations(_ context.Context, _ string, _ string, _, _ time.Time, _ string, _ int) ([]ObservationRecord, string, error) {
	return append([]ObservationRecord(nil), s.rows...), "", nil
}

func attestationPolicy() regPolicy {
	return regPolicy{set: punchpolicy.QuestionSet{
		ID: "clockout-ca-v1", Version: 3, Jurisdiction: "US-CA",
		Questions: []punchpolicy.Question{
			{ID: "break", Kind: punchpolicy.QuestionBreakProvided, TextKey: punchpolicy.KeyBreakProvided, Required: true},
			{ID: "injury", Kind: punchpolicy.QuestionInjury, TextKey: punchpolicy.KeyInjury, Required: true},
			{ID: "reason", Kind: punchpolicy.QuestionMissedBreakReason, TextKey: punchpolicy.KeyMissedBreakReason},
			{ID: "custom", Kind: punchpolicy.QuestionCustom, TextKey: "tenant.custom", Options: []string{"A", "B"}},
		},
	}}
}

// TestTodo_TCLOCK_010 proves that a clock-out retains the versioned question
// set, every worker answer, and the worker's exact tip statement as immutable
// evidence linked to the OUT observation. It also proves the existing session
// transition closes and reopens a labor segment at a job transfer.
func TestTodo_TCLOCK_010(t *testing.T) {
	work := newRegWork()
	observations := &attestObservationStore{}
	svc := regService(&regWorkers{active: true, assign: true}, regAuth{}, work)
	svc.Observations = observations
	svc.Policies = attestationPolicy()
	svc.PremiumInputs = &regPremium{}
	svc.CaseTasks = &regCase{}
	p := regPrincipal(t, regWorker)

	if _, err := svc.ClockIn(context.Background(), p, regPunch("attest-in", regNow)); err != nil {
		t.Fatal(err)
	}
	transfer := regPunch("attest-transfer", regNow.Add(2*time.Hour))
	transfer.JobRef, transfer.CostCodeRef = "JOB-B", "CC-B"
	if _, err := svc.TransferJob(context.Background(), p, transfer); err != nil {
		t.Fatal(err)
	}
	outReq := ClockOutRequest{PunchRequest: regPunch("attest-out", regNow.Add(4*time.Hour)), SiteID: "site-clock-reg", TipAmount: "42.50"}.WithAttestationAnswers(
		NewClockOutAnswer("break", "false"),
		NewClockOutAnswer("injury", "true"),
		NewClockOutAnswer("reason", "no coverage"),
		NewClockOutAnswer("custom", "A"),
	)
	out, err := svc.ClockOut(context.Background(), p, outReq)
	if err != nil {
		t.Fatalf("ClockOut = %+v, %v", out, err)
	}
	if out.State != timesession.StateClosed || len(observations.rows) != 1 {
		t.Fatalf("clock-out evidence count/state = %d/%s, want one/CLOSED", len(observations.rows), out.State)
	}
	var evidence ClockOutEvidence
	if err := json.Unmarshal(observations.rows[0].Payload, &evidence); err != nil {
		t.Fatal(err)
	}
	if evidence.ReceiptObservationID != out.ObservationID || evidence.QuestionSetID != "clockout-ca-v1" || evidence.QuestionSetVersion != 3 || len(evidence.Answers) != 4 || evidence.TipDeclaration == nil || evidence.TipDeclaration.Amount != "42.50" {
		t.Fatalf("evidence = %+v", evidence)
	}
	var session timesession.Session
	if err := json.Unmarshal(work.records[out.SessionID].Payload, &session); err != nil {
		t.Fatal(err)
	}
	if len(session.Segments) != 2 || session.Segments[0].End != transfer.DeviceTime || session.Segments[1].Start != transfer.DeviceTime || session.Segments[1].JobRef != "JOB-B" || session.Segments[1].CostCodeRef != "CC-B" {
		t.Fatalf("transferred session segments = %+v", session.Segments)
	}
}

// TestTodo_TCLOCK_010_Integration proves an adjustment is authorized by the
// current worker/assignment authority and appended as a new observation that
// points at the declaration; the declaration row is never rewritten.
func TestTodo_TCLOCK_010_Integration(t *testing.T) {
	work := newRegWork()
	observations := &attestObservationStore{}
	svc := regService(&regWorkers{active: true, assign: true}, regAuth{delegated: true}, work)
	svc.Observations = observations
	declaration := ObservationRecord{ID: "declaration-observation", TenantID: regTenant, WorkerRef: regWorker, AssignmentRef: regAssign, Source: clockOutAttestationSource, IdempotencyKey: "clock-out-attestation:out", Digest: "sha256:declaration"}
	observations.rows = append(observations.rows, declaration)
	row, err := svc.AppendTipAdjustment(context.Background(), regPrincipal(t, regActor), TipAdjustmentRequest{
		WorkerRef: regWorker, AssignmentRef: regAssign, DeclarationObservationID: declaration.ID,
		Amount: "-2.00", Reason: "duplicate tip line removed", IdempotencyKey: "adjustment-1",
	})
	if err != nil {
		t.Fatalf("AppendTipAdjustment: %v", err)
	}
	if row.CorrectsID != declaration.ID || row.EventType != tipAdjustmentEvent || len(observations.rows) != 2 {
		t.Fatalf("adjustment row = %+v; rows=%d", row, len(observations.rows))
	}
	var adjustment tipAdjustmentEvidence
	if err := json.Unmarshal(row.Payload, &adjustment); err != nil {
		t.Fatal(err)
	}
	if adjustment.Amount != "-2.00" || adjustment.AdjustedBy != regActor || adjustment.Reason == "" {
		t.Fatalf("adjustment evidence = %+v", adjustment)
	}
}

// TestTodo_TCLOCK_010_Golden pins the evidence envelope and its field names
// so downstream device and audit readers cannot silently change the record.
func TestTodo_TCLOCK_010_Golden(t *testing.T) {
	evidence := ClockOutEvidence{
		SchemaVersion: 1, ReceiptObservationID: "obs-out", TenantID: "tenant", WorkerRef: "worker",
		AssignmentRef: "assignment", SessionID: "session", SiteID: "site", QuestionSetID: "qset",
		QuestionSetVersion: 2, Jurisdiction: "US-CA", Answers: []ClockOutAnswer{{QuestionID: "break", Value: "false"}},
		TipDeclaration: &TipDeclarationEvidence{WorkerID: "worker", ShiftID: "session", DeclaredBy: "worker", DeclaredAt: time.Date(2026, 9, 28, 16, 0, 0, 0, time.UTC), Amount: "12.50"},
		RecordedAt:     time.Date(2026, 9, 28, 16, 0, 1, 0, time.UTC),
	}
	got, err := json.Marshal(evidence)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"schema_version":1,"receipt_observation_id":"obs-out","tenant_id":"tenant","worker_ref":"worker","assignment_ref":"assignment","session_id":"session","site_id":"site","question_set_id":"qset","question_set_version":2,"jurisdiction":"US-CA","answers":[{"question_id":"break","value":"false"}],"tip_declaration":{"worker_id":"worker","shift_id":"session","declared_by":"worker","declared_at":"2026-09-28T16:00:00Z","amount":"12.50"},"recorded_at":"2026-09-28T16:00:01Z"}`
	if string(got) != want {
		t.Fatalf("golden evidence = %s\nwant %s", got, want)
	}
}

// TestTodo_TCLOCK_010_I18n proves the policy's built-in question keys resolve
// in all supported device locales; tenant custom text remains tenant data.
func TestTodo_TCLOCK_010_I18n(t *testing.T) {
	for _, key := range []string{punchpolicy.KeyBreakProvided, punchpolicy.KeyMissedBreakReason, punchpolicy.KeyInjury} {
		for _, locale := range punchpolicy.SupportedLocales() {
			text, err := punchpolicy.Localize(key, locale)
			if err != nil || text == "" {
				t.Fatalf("Localize(%q, %q) = %q, %v", key, locale, text, err)
			}
		}
	}
	if _, err := punchpolicy.Localize("tenant.custom", punchpolicy.LocaleEnUS); err == nil {
		t.Fatal("tenant custom question text must not be treated as a built-in localization key")
	}
}
