package appointment

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// appt006Setup reserves and confirms one appointment and returns the ledger
// plus the reconciler expectation built from the stored record.
func appt006Setup(t *testing.T, ledger *ReservationLedger, now time.Time) (Reservation, ExpectedAppointment) {
	t.Helper()
	_, r := appt003KnownRequest(t)
	base := typedAppointmentRequirement(t)
	req, err := NewAppointmentRequirement(base)
	if err != nil {
		t.Fatal(err)
	}
	res, err := ledger.Reserve(req, r, now)
	if err != nil {
		t.Fatal(err)
	}
	confirmed, err := ledger.Confirm(res.ID, res.RequirementDigest, "confirm-1", now.Add(5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	return confirmed, ExpectedAppointment{
		ReservationID: confirmed.ID,
		Slot:          confirmed.Slot,
		Status:        confirmed.State,
		Version:       "v1",
	}
}

func appt006Observation(res Reservation, seq uint64, at time.Time) ProviderObservation {
	return ProviderObservation{
		EventID:       "evt-1",
		ReservationID: res.ID,
		Slot:          res.Slot,
		HasSlot:       true,
		Status:        ProviderConfirmed,
		Seq:           seq,
		ObservedAt:    at,
	}
}

func appt006Rejection(t *testing.T, err error, wantField string) *Rejection {
	t.Helper()
	if err == nil {
		t.Fatal("accepted a defective external observation")
	}
	if !errors.Is(err, ErrReconcileRejected) {
		t.Fatalf("err = %v, want reconcile rejection", err)
	}
	var rej *Rejection
	if !errors.As(err, &rej) {
		t.Fatalf("err = %v, want *Rejection", err)
	}
	if rej.Code != "APPT_006_REJECTED" {
		t.Fatalf("code = %q, want APPT_006_REJECTED", rej.Code)
	}
	if rej.Field != wantField {
		t.Fatalf("field = %q, want %q (%v)", rej.Field, wantField, err)
	}
	if rej.Version != "v1" {
		t.Fatalf("version = %q, want v1", rej.Version)
	}
	return rej
}

// TestTodo_APPT_006 is the PRIMARY contract (APPT-006: reconcile external
// calendars). Expected slot/participants/status versus provider observation
// handles stale/partial/duplicate/deleted events and creates repair without
// exposing private calendar content. Missing time or resource contract, or a
// competing slot reservation, is rejected with APPT_006_REJECTED and persists
// zero repairs.
func TestTodo_APPT_006(t *testing.T) {
	now := appt003Now()

	t.Run("match", func(t *testing.T) {
		ledger := NewReservationLedger()
		res, expected := appt006Setup(t, ledger, now)
		rec := NewReconciler()
		dec, err := rec.Reconcile(expected, nil, appt006Observation(res, 1, now.Add(time.Hour)), now.Add(2*time.Hour))
		if err != nil {
			t.Fatalf("Reconcile: %v", err)
		}
		if dec.Verdict != ReconcileMatch || dec.Repair.Kind != RepairNone || dec.Digest == "" {
			t.Fatalf("decision = %+v, want MATCH/NONE with digest", dec)
		}
		if len(rec.Repairs()) != 0 {
			t.Fatalf("repairs = %d, want 0 on match", len(rec.Repairs()))
		}
	})

	t.Run("missing-time-and-contract-rejected", func(t *testing.T) {
		ledger := NewReservationLedger()
		res, expected := appt006Setup(t, ledger, now)
		rec := NewReconciler()
		// Seeded defect: an observation with no time and no status is
		// accepted as a match.
		empty := ProviderObservation{EventID: "evt-empty", ReservationID: res.ID, Seq: 1, ObservedAt: now.Add(time.Hour)}
		appt006Rejection(t, errorOfReconcile(rec.Reconcile(expected, nil, empty, now.Add(2*time.Hour))), "slot")
		// Seeded defect: an observation naming no reservation is accepted.
		orphan := appt006Observation(res, 1, now.Add(time.Hour))
		orphan.ReservationID = ""
		appt006Rejection(t, errorOfReconcile(rec.Reconcile(expected, nil, orphan, now.Add(2*time.Hour))), "reservation")
		// Seeded defect: an event with no identity is accepted.
		noid := appt006Observation(res, 1, now.Add(time.Hour))
		noid.EventID = ""
		appt006Rejection(t, errorOfReconcile(rec.Reconcile(expected, nil, noid, now.Add(2*time.Hour))), "event")
		if len(rec.Repairs()) != 0 {
			t.Fatalf("repairs = %d, want 0 after refused observations", len(rec.Repairs()))
		}
	})

	t.Run("competing-slot-rejected", func(t *testing.T) {
		ledger := NewReservationLedger()
		resA, expectedA := appt006Setup(t, ledger, now)
		// A second, non-overlapping reservation fences 09:30-10:00.
		reqB, err := NewAppointmentRequirement(typedAppointmentRequirement(t))
		if err != nil {
			t.Fatal(err)
		}
		slotB := appt003Slot(t, 9, 30, 10, 0)
		rB := ReservationRequest{
			Slot:         slotB,
			Participants: []values.EntityRef{typedAppointmentRef(values.Kind("interviewer"), "002")},
			Resources: []ResourceHold{
				{ResourceRef: typedAppointmentRef(values.Kind("resource"), "006"), ResourceTypeRef: typedAppointmentRef(values.Kind("resource_type"), "003"), Quantity: appt003Quantity(t, "1")},
			},
			HoldTTL: 15 * time.Minute,
		}
		resB, err := ledger.Reserve(reqB, rB, now)
		if err != nil {
			t.Fatal(err)
		}
		competing := []ExpectedAppointment{{ReservationID: resB.ID, Slot: resB.Slot, Status: resB.State, Version: "v1"}}
		// Seeded defect: the provider claims A moved into B's fenced slot
		// and the move is accepted.
		claim := appt006Observation(resA, 1, now.Add(time.Hour))
		claim.Slot = slotB
		rec := NewReconciler()
		err = errorOfReconcile(rec.Reconcile(expectedA, competing, claim, now.Add(2*time.Hour)))
		rej := appt006Rejection(t, err, "slot")
		if rej.State != "CONFLICT" {
			t.Fatalf("state = %q, want CONFLICT", rej.State)
		}
		if !errors.Is(err, ErrReservationConflict) {
			t.Fatalf("err = %v, want conflict semantics", err)
		}
		if len(rec.Repairs()) != 0 {
			t.Fatalf("repairs = %d, want 0 after conflict", len(rec.Repairs()))
		}
	})

	t.Run("stale-partial-duplicate-deleted", func(t *testing.T) {
		ledger := NewReservationLedger()
		res, expected := appt006Setup(t, ledger, now)
		rec := NewReconciler()
		at := now.Add(time.Hour)
		first, err := rec.Reconcile(expected, nil, appt006Observation(res, 2, at), now.Add(2*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if first.Verdict != ReconcileMatch {
			t.Fatalf("verdict = %q, want MATCH", first.Verdict)
		}
		// Stale sequence: observed but never repaired.
		stale := appt006Observation(res, 1, at)
		stale.EventID = "evt-stale"
		dec, err := rec.Reconcile(expected, nil, stale, now.Add(2*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if dec.Verdict != ReconcileStale || dec.Repair.Kind != RepairNone {
			t.Fatalf("stale = %+v, want STALE/NONE", dec)
		}
		// Duplicate delivery: idempotent replay, no second repair.
		dup, err := rec.Reconcile(expected, nil, appt006Observation(res, 2, at), now.Add(3*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if dup.Verdict != ReconcileDuplicate || dup.Digest != first.Digest {
			t.Fatalf("duplicate = %+v, want DUPLICATE replay of %q", dup, first.Digest)
		}
		// Partial: status without time asks for resync, never assumes.
		partial := ProviderObservation{EventID: "evt-partial", ReservationID: res.ID, Status: ProviderConfirmed, Seq: 3, ObservedAt: at}
		dec, err = rec.Reconcile(expected, nil, partial, now.Add(2*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if dec.Verdict != ReconcilePartial || dec.Repair.Kind != RepairRequestResync {
			t.Fatalf("partial = %+v, want PARTIAL/REQUEST_RESYNC", dec)
		}
		// Provider deleted a live appointment: flag for review.
		deleted := appt006Observation(res, 4, at)
		deleted.EventID = "evt-deleted"
		deleted.Status = ProviderDeleted
		dec, err = rec.Reconcile(expected, nil, deleted, now.Add(2*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if dec.Verdict != ReconcileDeletedExternal || dec.Repair.Kind != RepairFlagReview {
			t.Fatalf("deleted = %+v, want DELETED_EXTERNAL/FLAG_REVIEW", dec)
		}
		if got := len(rec.Repairs()); got != 2 {
			t.Fatalf("repairs = %d, want 2 (partial + deleted)", got)
		}
	})

	t.Run("drift-creates-repair", func(t *testing.T) {
		ledger := NewReservationLedger()
		res, expected := appt006Setup(t, ledger, now)
		rec := NewReconciler()
		moved := appt006Observation(res, 1, now.Add(time.Hour))
		moved.EventID = "evt-drift"
		moved.Slot = appt003Slot(t, 9, 30, 10, 0)
		dec, err := rec.Reconcile(expected, nil, moved, now.Add(2*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if dec.Verdict != ReconcileDriftSlot || dec.Repair.Kind != RepairProposeReschedule {
			t.Fatalf("drift = %+v, want DRIFT_SLOT/PROPOSE_RESCHEDULE", dec)
		}
		cancelled := appt006Observation(res, 2, now.Add(time.Hour))
		cancelled.EventID = "evt-cancelled"
		cancelled.Status = ProviderCancelled
		dec, err = rec.Reconcile(expected, nil, cancelled, now.Add(2*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if dec.Verdict != ReconcileDriftStatus || dec.Repair.Kind != RepairFlagReview {
			t.Fatalf("status drift = %+v, want DRIFT_STATUS/FLAG_REVIEW", dec)
		}
	})

	t.Run("repair-exposes-no-calendar-content", func(t *testing.T) {
		ledger := NewReservationLedger()
		res, expected := appt006Setup(t, ledger, now)
		rec := NewReconciler()
		obs := appt006Observation(res, 1, now.Add(time.Hour))
		obs.EventID = "evt-private"
		obs.Status = ProviderDeleted
		obs.Note = "alice.private@example.com 1:1 with bob.private@example.com"
		dec, err := rec.Reconcile(expected, nil, obs, now.Add(2*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		blob := dec.Verdict.String() + dec.Repair.String() + dec.Digest
		for _, secret := range []string{"alice.private", "bob.private", "1:1"} {
			if strings.Contains(blob, secret) {
				t.Fatalf("repair exposes private calendar content %q in %q", secret, blob)
			}
		}
	})
}

func errorOfReconcile(_ ReconcileDecision, err error) error { return err }

// TestTodo_APPT_006_Integration: reconcile the stored reservation behind the
// real ledger; MATCH leaves the reservation and repair log alone while drift
// appends exactly one repair.
func TestTodo_APPT_006_Integration(t *testing.T) {
	now := appt003Now()
	ledger := NewReservationLedger()
	res, expected := appt006Setup(t, ledger, now)
	rec := NewReconciler()

	before, err := ledger.Get(res.ID)
	if err != nil {
		t.Fatal(err)
	}
	dec, err := rec.Reconcile(expected, nil, appt006Observation(res, 1, now.Add(time.Hour)), now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if dec.Verdict != ReconcileMatch {
		t.Fatalf("verdict = %q, want MATCH", dec.Verdict)
	}
	after, err := ledger.Get(res.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Digest != before.Digest || after.State != ReservationConfirmed {
		t.Fatalf("reconcile moved the reservation: %+v", after)
	}
	if len(rec.Repairs()) != 0 {
		t.Fatalf("repairs = %d, want 0 after match", len(rec.Repairs()))
	}

	moved := appt006Observation(res, 2, now.Add(time.Hour))
	moved.EventID = "evt-int-drift"
	moved.Slot = appt003Slot(t, 9, 30, 10, 0)
	dec, err = rec.Reconcile(expected, nil, moved, now.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if dec.Verdict != ReconcileDriftSlot {
		t.Fatalf("verdict = %q, want DRIFT_SLOT", dec.Verdict)
	}
	repairs := rec.Repairs()
	if len(repairs) != 1 || repairs[0].ReservationID != res.ID || repairs[0].Kind != RepairProposeReschedule {
		t.Fatalf("repairs = %+v, want one PROPOSE_RESCHEDULE for %s", repairs, res.ID)
	}
	still, err := ledger.Get(res.ID)
	if err != nil {
		t.Fatal(err)
	}
	if still.Digest != before.Digest {
		t.Fatalf("drift repair moved the reservation: %+v", still)
	}
}

// TestTodo_APPT_006_Mutation: seeded semantic mutants are killed — empty
// events, future-dated observations and unsequenced events are rejected;
// stale and duplicate deliveries never create repairs.
func TestTodo_APPT_006_Mutation(t *testing.T) {
	now := appt003Now()
	ledger := NewReservationLedger()
	res, expected := appt006Setup(t, ledger, now)
	rec := NewReconciler()

	t.Run("future-observation-rejected", func(t *testing.T) {
		obs := appt006Observation(res, 1, now.Add(5*time.Hour))
		if err := errorOfReconcile(rec.Reconcile(expected, nil, obs, now.Add(2*time.Hour))); !errors.Is(err, ErrReconcileRejected) {
			t.Fatalf("mutant survived: future-dated observation accepted (%v)", err)
		}
	})

	t.Run("unsequenced-rejected", func(t *testing.T) {
		obs := appt006Observation(res, 0, now.Add(time.Hour))
		appt006Rejection(t, errorOfReconcile(rec.Reconcile(expected, nil, obs, now.Add(2*time.Hour))), "seq")
	})

	t.Run("bad-status-rejected", func(t *testing.T) {
		obs := appt006Observation(res, 1, now.Add(time.Hour))
		obs.Status = "SNOOZED"
		appt006Rejection(t, errorOfReconcile(rec.Reconcile(expected, nil, obs, now.Add(2*time.Hour))), "status")
	})

	t.Run("stale-and-duplicate-never-repair", func(t *testing.T) {
		at := now.Add(time.Hour)
		if _, err := rec.Reconcile(expected, nil, appt006Observation(res, 5, at), now.Add(2*time.Hour)); err != nil {
			t.Fatal(err)
		}
		stale := appt006Observation(res, 4, at)
		stale.EventID = "evt-mut-stale"
		dec, err := rec.Reconcile(expected, nil, stale, now.Add(2*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if dec.Verdict != ReconcileStale {
			t.Fatalf("mutant survived: stale treated as %q", dec.Verdict)
		}
		dup, err := rec.Reconcile(expected, nil, appt006Observation(res, 5, at), now.Add(3*time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if dup.Verdict != ReconcileDuplicate {
			t.Fatalf("mutant survived: duplicate treated as %q", dup.Verdict)
		}
		if len(rec.Repairs()) != 0 {
			t.Fatalf("mutant survived: stale/duplicate created %d repairs", len(rec.Repairs()))
		}
	})
}
