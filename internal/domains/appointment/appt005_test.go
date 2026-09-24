package appointment

import (
	"errors"
	"testing"
	"time"
)

// TestTodo_APPT_005 is the PRIMARY contract (APPT-005: attendance/no-show).
// Attended/no-show/cancelled/unknown evidence is distinct, trusted and
// correctable; downstream fee/case/workflow action follows policy, never
// assumption. Rejections carry APPT_005_REJECTED and persist zero records,
// history and dispatched actions.
func TestTodo_APPT_005(t *testing.T) {
	now := appt003Now() // 08:00; slot is 09:00-09:30.

	book := func(t *testing.T, policy CancellationRules) (*ReservationLedger, *AttendanceLedger, Reservation) {
		t.Helper()
		base := typedAppointmentRequirement(t)
		base.CancellationRules = policy
		req, err := NewAppointmentRequirement(base)
		if err != nil {
			t.Fatal(err)
		}
		ledger := newTestReservationLedger()
		_, r := appt003KnownRequest(t)
		res, err := ledger.Reserve(req, r, now)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ledger.Confirm(res.ID, res.RequirementDigest, "confirm-1", now.Add(5*time.Minute)); err != nil {
			t.Fatal(err)
		}
		stored, err := ledger.Get(res.ID)
		if err != nil {
			t.Fatal(err)
		}
		return ledger, NewAttendanceLedger(ledger), stored
	}
	reject := func(t *testing.T, err error, wantField, wantState string) *Rejection {
		t.Helper()
		if err == nil {
			t.Fatal("accepted defective attendance evidence")
		}
		if !errors.Is(err, ErrAttendanceRejected) {
			t.Fatalf("err = %v, want attendance rejection", err)
		}
		var rej *Rejection
		if !errors.As(err, &rej) {
			t.Fatalf("err = %v, want *Rejection", err)
		}
		if rej.Code != "APPT_005_REJECTED" {
			t.Fatalf("code = %q, want APPT_005_REJECTED", rej.Code)
		}
		if rej.Field != wantField {
			t.Fatalf("field = %q, want %q (%v)", rej.Field, wantField, err)
		}
		if rej.State != wantState {
			t.Fatalf("state = %q, want %q (%v)", rej.State, wantState, err)
		}
		if rej.Version != "v1" {
			t.Fatalf("version = %q, want v1", rej.Version)
		}
		return rej
	}

	t.Run("attended-dispatches-no-action", func(t *testing.T) {
		_, att, res := book(t, CancellationRules{Kind: CancellationAllowed, NoShow: NoShowFollowUp})
		rec, err := att.Record(res.ID, AttendanceAttended, "ev-1", now.Add(90*time.Minute))
		if err != nil {
			t.Fatalf("Record: %v", err)
		}
		if rec.Outcome != AttendanceAttended || rec.Action != AttendanceActionNone {
			t.Fatalf("record = %+v, want ATTENDED/NONE", rec)
		}
		if rec.Sequence != 1 || rec.Supersedes != "" || rec.Digest == "" {
			t.Fatalf("record lacks trusted identity: %+v", rec)
		}
		got, ok := att.Get(res.ID)
		if !ok || got.Digest != rec.Digest {
			t.Fatalf("Get = %+v, %v, want persisted %+v", got, ok, rec)
		}
		dispatched := att.Dispatched()
		if len(dispatched) != 1 || dispatched[0].Action != AttendanceActionNone {
			t.Fatalf("dispatched = %+v, want one NONE action", dispatched)
		}
	})

	t.Run("no-show-follows-policy", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			policy NoShowKind
			want   AttendanceAction
		}{
			{"follow-up", NoShowFollowUp, AttendanceActionFollowUp},
			{"review", NoShowReview, AttendanceActionReview},
			{"none", NoShowNone, AttendanceActionNone},
		} {
			t.Run(tc.name, func(t *testing.T) {
				_, att, res := book(t, CancellationRules{Kind: CancellationAllowed, NoShow: tc.policy})
				rec, err := att.Record(res.ID, AttendanceNoShow, "ev-1", now.Add(90*time.Minute))
				if err != nil {
					t.Fatalf("Record: %v", err)
				}
				if rec.Action != tc.want {
					t.Fatalf("action = %q, want %q", rec.Action, tc.want)
				}
			})
		}
	})

	t.Run("unknown-means-no-record-no-action", func(t *testing.T) {
		_, att, res := book(t, CancellationRules{Kind: CancellationAllowed, NoShow: NoShowFollowUp})
		if _, ok := att.Get(res.ID); ok {
			t.Fatal("unknown attendance reads as evidence")
		}
		if got := AttendanceActionFor("", CancellationRules{Kind: CancellationAllowed, NoShow: NoShowFollowUp}); got != AttendanceActionNone {
			t.Fatalf("unknown action = %q, want NONE (never assumption)", got)
		}
		if len(att.Dispatched()) != 0 {
			t.Fatalf("dispatched = %d, want 0 without evidence", len(att.Dispatched()))
		}
	})

	t.Run("cancelled-is-not-attendance", func(t *testing.T) {
		ledger, att, res := book(t, CancellationRules{Kind: CancellationAllowed, NoShow: NoShowReview})
		if _, err := ledger.Cancel(res.ID, res.RequirementDigest, "cancel-1", now.Add(10*time.Minute)); err != nil {
			t.Fatal(err)
		}
		reject(t, errorOfAttendance(att.Record(res.ID, AttendanceNoShow, "ev-1", now.Add(90*time.Minute))), "state", "CANCELLED")
		reject(t, errorOfAttendance(att.Record(res.ID, AttendanceAttended, "ev-2", now.Add(90*time.Minute))), "state", "CANCELLED")
		// Cancelled is a reservation state, never recordable attendance evidence.
		reject(t, errorOfAttendance(att.Record(res.ID, "CANCELLED", "ev-3", now.Add(90*time.Minute))), "state", "CANCELLED")
		if _, ok := att.Get(res.ID); ok {
			t.Fatal("cancelled reservation carries attendance evidence")
		}
		if len(att.Dispatched()) != 0 {
			t.Fatalf("dispatched = %d, want 0 after refused records", len(att.Dispatched()))
		}
	})

	t.Run("unconfirmed-and-early-are-assumption", func(t *testing.T) {
		base := typedAppointmentRequirement(t)
		req, err := NewAppointmentRequirement(base)
		if err != nil {
			t.Fatal(err)
		}
		ledger := newTestReservationLedger()
		_, r := appt003KnownRequest(t)
		res, err := ledger.Reserve(req, r, now)
		if err != nil {
			t.Fatal(err)
		}
		att := NewAttendanceLedger(ledger)
		reject(t, errorOfAttendance(att.Record(res.ID, AttendanceAttended, "ev-1", now.Add(90*time.Minute))), "state", "HELD")
		if _, err := ledger.Confirm(res.ID, res.RequirementDigest, "confirm-1", now.Add(5*time.Minute)); err != nil {
			t.Fatal(err)
		}
		reject(t, errorOfAttendance(att.Record(res.ID, AttendanceAttended, "ev-2", now.Add(30*time.Minute))), "at", "CONFIRMED")
		if _, ok := att.Get(res.ID); ok {
			t.Fatal("assumed evidence persisted")
		}
	})

	t.Run("correction-supersedes", func(t *testing.T) {
		_, att, res := book(t, CancellationRules{Kind: CancellationAllowed, NoShow: NoShowFollowUp})
		first, err := att.Record(res.ID, AttendanceNoShow, "ev-1", now.Add(90*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if first.Action != AttendanceActionFollowUp {
			t.Fatalf("action = %q, want FOLLOW_UP", first.Action)
		}
		fixed, err := att.Correct(res.ID, AttendanceAttended, "ev-2", now.Add(100*time.Minute))
		if err != nil {
			t.Fatalf("Correct: %v", err)
		}
		if fixed.Sequence != 2 || fixed.Supersedes != first.Digest || fixed.Digest == first.Digest {
			t.Fatalf("correction lacks lineage: %+v", fixed)
		}
		if fixed.Action != AttendanceActionNone {
			t.Fatalf("corrected action = %q, want NONE", fixed.Action)
		}
		hist := att.History(res.ID)
		if len(hist) != 2 || hist[0].Digest != first.Digest || hist[1].Digest != fixed.Digest {
			t.Fatalf("history = %+v, want [first fixed]", hist)
		}
		if got := len(att.Dispatched()); got != 2 {
			t.Fatalf("dispatched = %d, want 2", got)
		}
		// A second Record without correction is refused; state is unchanged.
		reject(t, errorOfAttendance(att.Record(res.ID, AttendanceNoShow, "ev-3", now.Add(110*time.Minute))), "sequence", "CONFIRMED")
		got, _ := att.Get(res.ID)
		if got.Digest != fixed.Digest {
			t.Fatalf("refused record moved state: %+v", got)
		}
	})
}

func errorOfAttendance(_ AttendanceRecord, err error) error { return err }

// TestTodo_APPT_005_Property: generated outcome/policy cases preserve the
// action algebra, correction lineage and digest evolution.
func TestTodo_APPT_005_Property(t *testing.T) {
	policies := []NoShowKind{NoShowFollowUp, NoShowReview, NoShowNone}
	outcomes := []AttendanceOutcome{AttendanceAttended, AttendanceNoShow}
	for _, policy := range policies {
		for _, outcome := range outcomes {
			rules := CancellationRules{Kind: CancellationAllowed, NoShow: policy}
			got := AttendanceActionFor(outcome, rules)
			var want AttendanceAction
			switch {
			case outcome == AttendanceAttended:
				want = AttendanceActionNone
			case policy == NoShowFollowUp:
				want = AttendanceActionFollowUp
			case policy == NoShowReview:
				want = AttendanceActionReview
			default:
				want = AttendanceActionNone
			}
			if got != want {
				t.Fatalf("action(%q,%q) = %q, want %q", outcome, policy, got, want)
			}
		}
		// Unknown/empty evidence never implies an action under any policy.
		if got := AttendanceActionFor("", CancellationRules{Kind: CancellationAllowed, NoShow: policy}); got != AttendanceActionNone {
			t.Fatalf("action(unknown,%q) = %q, want NONE", policy, got)
		}
	}

	// Correction chains preserve first-evidence lineage and evolve digests.
	now := appt003Now()
	base := typedAppointmentRequirement(t)
	base.CancellationRules = CancellationRules{Kind: CancellationAllowed, NoShow: NoShowReview}
	req, err := NewAppointmentRequirement(base)
	if err != nil {
		t.Fatal(err)
	}
	ledger := newTestReservationLedger()
	_, r := appt003KnownRequest(t)
	res, err := ledger.Reserve(req, r, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.Confirm(res.ID, res.RequirementDigest, "confirm-1", now.Add(5*time.Minute)); err != nil {
		t.Fatal(err)
	}
	att := NewAttendanceLedger(ledger)
	chain := []AttendanceOutcome{AttendanceNoShow, AttendanceAttended, AttendanceNoShow}
	var prev string
	for i, outcome := range chain {
		var rec AttendanceRecord
		key := "key-" + string(rune('a'+i))
		if i == 0 {
			rec, err = att.Record(res.ID, outcome, key, now.Add(90*time.Minute))
		} else {
			rec, err = att.Correct(res.ID, outcome, key, now.Add(time.Duration(90+i*10)*time.Minute))
		}
		_ = prev
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
		if rec.Sequence != uint64(i+1) {
			t.Fatalf("step %d sequence = %d", i, rec.Sequence)
		}
		prev = rec.Digest
	}
	hist := att.History(res.ID)
	if len(hist) != len(chain) {
		t.Fatalf("history = %d, want %d", len(hist), len(chain))
	}
	seen := map[string]bool{}
	for i, rec := range hist {
		if rec.Digest == "" || seen[rec.Digest] {
			t.Fatalf("history[%d] digest not unique: %+v", i, rec)
		}
		seen[rec.Digest] = true
		if i > 0 && hist[i].Supersedes != hist[i-1].Digest {
			t.Fatalf("history[%d] breaks lineage: %+v", i, rec)
		}
	}
	if hist[0].Supersedes != "" {
		t.Fatalf("first evidence supersedes %q, want empty", hist[0].Supersedes)
	}
	_ = prev
}

// TestTodo_APPT_005_Integration: reserve, confirm and record through the real
// ledgers; cancellation policy changes the downstream action while the
// reservation state stays CONFIRMED.
func TestTodo_APPT_005_Integration(t *testing.T) {
	now := appt003Now()
	for _, tc := range []struct {
		name   string
		policy NoShowKind
		want   AttendanceAction
	}{
		{"follow-up", NoShowFollowUp, AttendanceActionFollowUp},
		{"review", NoShowReview, AttendanceActionReview},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := typedAppointmentRequirement(t)
			base.CancellationRules = CancellationRules{Kind: CancellationAllowed, NoShow: tc.policy}
			req, err := NewAppointmentRequirement(base)
			if err != nil {
				t.Fatal(err)
			}
			ledger := newTestReservationLedger()
			_, r := appt003KnownRequest(t)
			res, err := ledger.Reserve(req, r, now)
			if err != nil {
				t.Fatal(err)
			}
			confirmed, err := ledger.Confirm(res.ID, res.RequirementDigest, "confirm-1", now.Add(5*time.Minute))
			if err != nil {
				t.Fatal(err)
			}
			att := NewAttendanceLedger(ledger)
			rec, err := att.Record(res.ID, AttendanceNoShow, "ev-1", now.Add(90*time.Minute))
			if err != nil {
				t.Fatal(err)
			}
			if rec.Action != tc.want {
				t.Fatalf("action = %q, want %q", rec.Action, tc.want)
			}
			stored, err := ledger.Get(res.ID)
			if err != nil {
				t.Fatal(err)
			}
			if stored.State != ReservationConfirmed || stored.Digest != confirmed.Digest {
				t.Fatalf("attendance moved the reservation: %+v", stored)
			}
			fixed, err := att.Correct(res.ID, AttendanceAttended, "ev-2", now.Add(100*time.Minute))
			if err != nil {
				t.Fatal(err)
			}
			if fixed.Action != AttendanceActionNone {
				t.Fatalf("corrected action = %q, want NONE", fixed.Action)
			}
			if got := len(att.Dispatched()); got != 2 {
				t.Fatalf("dispatched = %d, want 2", got)
			}
		})
	}
}

// TestTodo_APPT_005_Fault: clock boundaries, duplicate evidence and replayed
// idempotency keys under injected time.
func TestTodo_APPT_005_Fault(t *testing.T) {
	now := appt003Now()
	setup := func(t *testing.T) (*AttendanceLedger, Reservation) {
		t.Helper()
		base := typedAppointmentRequirement(t)
		base.CancellationRules = CancellationRules{Kind: CancellationAllowed, NoShow: NoShowFollowUp}
		req, err := NewAppointmentRequirement(base)
		if err != nil {
			t.Fatal(err)
		}
		ledger := newTestReservationLedger()
		_, r := appt003KnownRequest(t)
		res, err := ledger.Reserve(req, r, now)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ledger.Confirm(res.ID, res.RequirementDigest, "confirm-1", now.Add(5*time.Minute)); err != nil {
			t.Fatal(err)
		}
		return NewAttendanceLedger(ledger), res
	}
	slotStart := time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)

	t.Run("slot-start-boundary", func(t *testing.T) {
		att, res := setup(t)
		if err := errorOfAttendance(att.Record(res.ID, AttendanceAttended, "early", slotStart.Add(-time.Nanosecond))); !errors.Is(err, ErrAttendanceRejected) {
			t.Fatalf("1ns-early record err = %v, want rejection", err)
		}
		if _, err := att.Record(res.ID, AttendanceAttended, "exact", slotStart); err != nil {
			t.Fatalf("exact-start record: %v", err)
		}
	})

	t.Run("idempotent-replay", func(t *testing.T) {
		att, res := setup(t)
		first, err := att.Record(res.ID, AttendanceNoShow, "same-key", now.Add(90*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		replay, err := att.Record(res.ID, AttendanceNoShow, "same-key", now.Add(95*time.Minute))
		if err != nil {
			t.Fatalf("replay: %v", err)
		}
		if replay.Digest != first.Digest {
			t.Fatalf("replay digest = %q, want %q", replay.Digest, first.Digest)
		}
		if got := len(att.Dispatched()); got != 1 {
			t.Fatalf("dispatched = %d, want 1 after replay", got)
		}
	})

	t.Run("correction-faults", func(t *testing.T) {
		att, res := setup(t)
		if err := errorOfAttendance(att.Correct(res.ID, AttendanceAttended, "k", now.Add(90*time.Minute))); !errors.Is(err, ErrAttendanceRejected) {
			t.Fatalf("correction without evidence err = %v, want rejection", err)
		}
		if _, err := att.Record(res.ID, AttendanceNoShow, "ev-1", now.Add(90*time.Minute)); err != nil {
			t.Fatal(err)
		}
		if err := errorOfAttendance(att.Correct(res.ID, AttendanceNoShow, "ev-2", now.Add(100*time.Minute))); !errors.Is(err, ErrAttendanceRejected) {
			t.Fatalf("no-change correction err = %v, want rejection", err)
		}
		if err := errorOfAttendance(att.Correct("rsv-missing", AttendanceAttended, "ev-3", now.Add(100*time.Minute))); !errors.Is(err, ErrAttendanceRejected) {
			t.Fatalf("unknown reservation correction err = %v, want rejection", err)
		}
		var rej *Rejection
		if err := errorOfAttendance(att.Record("rsv-missing", AttendanceAttended, "ev-4", now.Add(100*time.Minute))); !errors.As(err, &rej) || rej.Code != "APPT_005_REJECTED" {
			t.Fatalf("err = %v, want APPT_005_REJECTED", err)
		}
	})
}

// TestTodo_APPT_005_Mutation: seeded semantic mutants are killed — attendance
// never assumes, never upgrades NONE policy, never downgrades evidence.
func TestTodo_APPT_005_Mutation(t *testing.T) {
	now := appt003Now()
	book := func(t *testing.T, policy CancellationRules) (*AttendanceLedger, Reservation) {
		t.Helper()
		base := typedAppointmentRequirement(t)
		base.CancellationRules = policy
		req, err := NewAppointmentRequirement(base)
		if err != nil {
			t.Fatal(err)
		}
		ledger := newTestReservationLedger()
		_, r := appt003KnownRequest(t)
		res, err := ledger.Reserve(req, r, now)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ledger.Confirm(res.ID, res.RequirementDigest, "confirm-1", now.Add(5*time.Minute)); err != nil {
			t.Fatal(err)
		}
		return NewAttendanceLedger(ledger), res
	}

	t.Run("none-policy-never-escalates", func(t *testing.T) {
		att, res := book(t, CancellationRules{Kind: CancellationAllowed, NoShow: NoShowNone})
		rec, err := att.Record(res.ID, AttendanceNoShow, "ev-1", now.Add(90*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if rec.Action != AttendanceActionNone {
			t.Fatalf("mutant survived: NONE policy escalated to %q", rec.Action)
		}
	})

	t.Run("attended-never-charges", func(t *testing.T) {
		att, res := book(t, CancellationRules{Kind: CancellationAllowed, NoShow: NoShowFollowUp})
		rec, err := att.Record(res.ID, AttendanceAttended, "ev-1", now.Add(90*time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		if rec.Action != AttendanceActionNone {
			t.Fatalf("mutant survived: attended drew action %q", rec.Action)
		}
	})

	t.Run("empty-outcome-rejected", func(t *testing.T) {
		att, res := book(t, CancellationRules{Kind: CancellationAllowed, NoShow: NoShowReview})
		if err := errorOfAttendance(att.Record(res.ID, "", "ev-1", now.Add(90*time.Minute))); !errors.Is(err, ErrAttendanceRejected) {
			t.Fatalf("mutant survived: empty outcome accepted (%v)", err)
		}
		var rej *Rejection
		if err := errorOfAttendance(att.Record(res.ID, "UNKNOWN", "ev-2", now.Add(90*time.Minute))); !errors.As(err, &rej) || rej.Code != "APPT_005_REJECTED" {
			t.Fatalf("mutant survived: UNKNOWN outcome accepted (%v)", err)
		}
		if _, ok := att.Get(res.ID); ok {
			t.Fatal("mutant survived: invalid evidence persisted")
		}
	})

	t.Run("held-never-records", func(t *testing.T) {
		base := typedAppointmentRequirement(t)
		req, err := NewAppointmentRequirement(base)
		if err != nil {
			t.Fatal(err)
		}
		ledger := newTestReservationLedger()
		_, r := appt003KnownRequest(t)
		res, err := ledger.Reserve(req, r, now)
		if err != nil {
			t.Fatal(err)
		}
		att := NewAttendanceLedger(ledger)
		if err := errorOfAttendance(att.Record(res.ID, AttendanceNoShow, "ev-1", now.Add(90*time.Minute))); !errors.Is(err, ErrAttendanceRejected) {
			t.Fatalf("mutant survived: HELD attendance accepted (%v)", err)
		}
	})
}
