package appointment

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func appt004Requirement(t *testing.T, notice time.Duration, kind CancellationKind) Requirement {
	t.Helper()
	base := typedAppointmentRequirement(t)
	base.CancellationRules = CancellationRules{Notice: notice, Kind: kind, NoShow: NoShowReview}
	req, err := NewAppointmentRequirement(base)
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func appt004Reserve(t *testing.T, ledger *ReservationLedger, req Requirement, now time.Time) Reservation {
	t.Helper()
	_, r := appt003KnownRequest(t)
	res, err := ledger.Reserve(req, r, now)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func appt004Rejection(t *testing.T, err error, wantField string) *Rejection {
	t.Helper()
	if err == nil {
		t.Fatal("accepted a defective transition")
	}
	if !errors.Is(err, ErrTransitionRejected) && !errors.Is(err, ErrReservationConflict) {
		t.Fatalf("err = %v, want transition rejection", err)
	}
	var rej *Rejection
	if !errors.As(err, &rej) {
		t.Fatalf("err = %v, want *Rejection", err)
	}
	if rej.Code != "APPT_004_REJECTED" {
		t.Fatalf("code = %q, want APPT_004_REJECTED", rej.Code)
	}
	if rej.Field != wantField {
		t.Fatalf("field = %q, want %q (%v)", rej.Field, wantField, err)
	}
	if rej.Version != "v1" {
		t.Fatalf("version = %q, want v1", rej.Version)
	}
	return rej
}

// TestTodo_APPT_004 is the PRIMARY contract: confirm, reschedule and cancel
// move a reservation through fenced states, bind the contracted requirement
// digest, honor the cancellation policy, and emit exactly one governed
// communication per transition.
func TestTodo_APPT_004(t *testing.T) {
	now := appt003Now()

	t.Run("confirm", func(t *testing.T) {
		req := appt004Requirement(t, 0, CancellationAllowed)
		ledger := NewReservationLedger()
		res := appt004Reserve(t, ledger, req, now)

		confirmed, err := ledger.Confirm(res.ID, res.RequirementDigest, "confirm-1", now.Add(5*time.Minute))
		if err != nil {
			t.Fatalf("Confirm: %v", err)
		}
		if confirmed.State != ReservationConfirmed {
			t.Fatalf("state = %q, want CONFIRMED", confirmed.State)
		}
		outbox := ledger.Outbox()
		if len(outbox) != 1 || outbox[0].Kind != NotificationConfirmed || outbox[0].ReservationID != res.ID {
			t.Fatalf("outbox = %+v, want one CONFIRMED notification", outbox)
		}
		if outbox[0].Digest == "" || !outbox[0].At.Time().Equal(now.Add(5*time.Minute)) {
			t.Fatalf("notification lacks identity: %+v", outbox[0])
		}

		replay, err := ledger.Confirm(res.ID, res.RequirementDigest, "confirm-1", now.Add(6*time.Minute))
		if err != nil {
			t.Fatalf("idempotent confirm: %v", err)
		}
		if replay.State != ReservationConfirmed {
			t.Fatalf("replay state = %q, want CONFIRMED", replay.State)
		}
		if got := len(ledger.Outbox()); got != 1 {
			t.Fatalf("outbox after replay = %d, want 1", got)
		}

		if _, err := ledger.Confirm(res.ID, res.RequirementDigest, "confirm-2", now.Add(7*time.Minute)); !errors.Is(err, ErrTransitionRejected) {
			t.Fatalf("second confirm err = %v, want rejection", err)
		}
	})

	t.Run("confirm-stale-digest", func(t *testing.T) {
		req := appt004Requirement(t, 0, CancellationAllowed)
		ledger := NewReservationLedger()
		res := appt004Reserve(t, ledger, req, now)
		appt004Rejection(t, errorOfTransition(ledger.Confirm(res.ID, "bogus-digest", "k", now.Add(time.Minute))), "requirement_digest")
		stored, err := ledger.Get(res.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.State != ReservationHeld {
			t.Fatalf("state = %q, want HELD after refused confirm", stored.State)
		}
		if got := len(ledger.Outbox()); got != 0 {
			t.Fatalf("outbox = %d, want 0 after refused confirm", got)
		}
	})

	t.Run("confirm-after-expiry", func(t *testing.T) {
		req := appt004Requirement(t, 0, CancellationAllowed)
		ledger := NewReservationLedger()
		res := appt004Reserve(t, ledger, req, now)
		appt004Rejection(t, errorOfTransition(ledger.Confirm(res.ID, res.RequirementDigest, "k", now.Add(time.Hour))), "state")
		stored, err := ledger.Get(res.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.State != ReservationExpired {
			t.Fatalf("state = %q, want EXPIRED after late confirm", stored.State)
		}
	})

	t.Run("reschedule", func(t *testing.T) {
		req := appt004Requirement(t, 0, CancellationAllowed)
		ledger := NewReservationLedger()
		res := appt004Reserve(t, ledger, req, now)

		moved, err := ledger.Reschedule(res.ID, res.RequirementDigest, appt003Slot(t, 9, 30, 10, 0), "move-1", now.Add(5*time.Minute))
		if err != nil {
			t.Fatalf("Reschedule: %v", err)
		}
		if moved.State != ReservationHeld {
			t.Fatalf("state = %q, want HELD after reschedule", moved.State)
		}
		start, _ := moved.Slot.StartInstant()
		if start.Time().Hour() != 9 || start.Time().Minute() != 30 {
			t.Fatalf("slot = %v, want 09:30 start", start.Time())
		}
		outbox := ledger.Outbox()
		if len(outbox) != 1 || outbox[0].Kind != NotificationRescheduled {
			t.Fatalf("outbox = %+v, want one RESCHEDULED notification", outbox)
		}

		if _, err := ledger.Reschedule(res.ID, res.RequirementDigest, appt003Slot(t, 10, 0, 10, 30), "move-2", now.Add(6*time.Minute)); err == nil {
			t.Fatal("reschedule outside the contracted window was accepted")
		} else {
			appt004Rejection(t, err, "slot")
		}
		stored, err := ledger.Get(res.ID)
		if err != nil {
			t.Fatal(err)
		}
		start, _ = stored.Slot.StartInstant()
		if start.Time().Minute() != 30 {
			t.Fatalf("failed reschedule moved the slot to %v", start.Time())
		}
		if got := len(ledger.Outbox()); got != 1 {
			t.Fatalf("outbox = %d, want 1 after refused reschedule", got)
		}
	})

	t.Run("reschedule-conflict", func(t *testing.T) {
		req := appt004Requirement(t, 0, CancellationAllowed)
		ledger := NewReservationLedger()
		first := appt004Reserve(t, ledger, req, now)
		_, secondReq := appt003KnownRequest(t)
		secondReq.Slot = appt003Slot(t, 9, 30, 10, 0)
		if _, err := ledger.Reserve(req, secondReq, now); err != nil {
			t.Fatal(err)
		}
		err := errorOfTransition(ledger.Reschedule(first.ID, first.RequirementDigest, appt003Slot(t, 9, 30, 10, 0), "move-x", now.Add(5*time.Minute)))
		if !errors.Is(err, ErrReservationConflict) {
			t.Fatalf("conflicting reschedule err = %v, want conflict", err)
		}
		appt004Rejection(t, err, "slot")
	})

	t.Run("cancel", func(t *testing.T) {
		req := appt004Requirement(t, 0, CancellationAllowed)
		ledger := NewReservationLedger()
		res := appt004Reserve(t, ledger, req, now)

		cancelled, err := ledger.Cancel(res.ID, res.RequirementDigest, "cancel-1", now.Add(5*time.Minute))
		if err != nil {
			t.Fatalf("Cancel: %v", err)
		}
		if cancelled.State != ReservationCancelled {
			t.Fatalf("state = %q, want CANCELLED", cancelled.State)
		}
		outbox := ledger.Outbox()
		if len(outbox) != 1 || outbox[0].Kind != NotificationCancelled {
			t.Fatalf("outbox = %+v, want one CANCELLED notification", outbox)
		}
		if _, err := ledger.Cancel(res.ID, res.RequirementDigest, "cancel-2", now.Add(6*time.Minute)); !errors.Is(err, ErrTransitionRejected) {
			t.Fatalf("double cancel err = %v, want rejection", err)
		}
	})

	t.Run("cancel-late-violates-policy", func(t *testing.T) {
		req := appt004Requirement(t, 2*time.Hour, CancellationAllowed)
		ledger := NewReservationLedger()
		res := appt004Reserve(t, ledger, req, now)
		// 08:05 is inside the 2h notice window before the 09:00 start but
		// still inside the 15m hold, so the refusal must be the policy, not
		// expiry.
		appt004Rejection(t, errorOfTransition(ledger.Cancel(res.ID, res.RequirementDigest, "k", now.Add(5*time.Minute))), "cancellation_policy")
		stored, err := ledger.Get(res.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.State != ReservationHeld {
			t.Fatalf("state = %q, want HELD after refused cancel", stored.State)
		}
		if got := len(ledger.Outbox()); got != 0 {
			t.Fatalf("outbox = %d, want 0 after refused cancel", got)
		}
	})

	t.Run("cancel-disabled", func(t *testing.T) {
		req := appt004Requirement(t, 0, CancellationDisabled)
		ledger := NewReservationLedger()
		res := appt004Reserve(t, ledger, req, now)
		appt004Rejection(t, errorOfTransition(ledger.Cancel(res.ID, res.RequirementDigest, "k", now.Add(5*time.Minute))), "cancellation_policy")
	})
}

func errorOfTransition(_ Reservation, err error) error { return err }

// TestTodo_APPT_004_Race proves idempotent single-emission under
// concurrency: shared-key confirms converge on one notification, and a
// confirm/cancel race commits exactly one transition with a matching
// notification.
func TestTodo_APPT_004_Race(t *testing.T) {
	now := appt003Now()

	t.Run("shared-key-confirm-emits-once", func(t *testing.T) {
		req := appt004Requirement(t, 0, CancellationAllowed)
		ledger := NewReservationLedger()
		res := appt004Reserve(t, ledger, req, now)
		digest := res.RequirementDigest
		id := res.ID

		var wg sync.WaitGroup
		errs := make([]error, 16)
		for i := 0; i < 16; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, errs[i] = ledger.Confirm(id, digest, "shared-key", now.Add(5*time.Minute))
			}(i)
		}
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				t.Fatalf("shared-key confirm err = %v, want success", err)
			}
		}
		stored, err := ledger.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if stored.State != ReservationConfirmed {
			t.Fatalf("state = %q, want CONFIRMED", stored.State)
		}
		if got := len(ledger.Outbox()); got != 1 {
			t.Fatalf("outbox = %d, want exactly 1", got)
		}
	})

	t.Run("confirm-cancel-legal-path", func(t *testing.T) {
		req := appt004Requirement(t, 0, CancellationAllowed)
		ledger := NewReservationLedger()
		res := appt004Reserve(t, ledger, req, now)
		digest := res.RequirementDigest
		id := res.ID

		var wg sync.WaitGroup
		errs := make([]error, 16)
		for i := 0; i < 16; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				at := now.Add(5 * time.Minute)
				if i%2 == 0 {
					_, errs[i] = ledger.Confirm(id, digest, "race-confirm", at)
				} else {
					_, errs[i] = ledger.Cancel(id, digest, "race-cancel", at)
				}
			}(i)
		}
		wg.Wait()
		// Confirm-then-cancel is a legal lifecycle path, so a mixed race
		// can emit one notification (one kind won outright) or two (a
		// confirm won, then a cancel moved it on). Either way the
		// notification sequence must be a legal path whose last entry
		// matches the terminal state, with no duplicate kinds.
		outbox := ledger.Outbox()
		stored, err := ledger.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		seen := map[NotificationKind]bool{}
		for _, n := range outbox {
			if n.ReservationID != id {
				t.Fatalf("outbox entry for wrong reservation: %+v", n)
			}
			if seen[n.Kind] {
				t.Fatalf("duplicate %s notification in %+v", n.Kind, outbox)
			}
			seen[n.Kind] = true
		}
		switch {
		case len(outbox) == 1 && outbox[0].Kind == NotificationConfirmed && stored.State == ReservationConfirmed:
		case len(outbox) == 1 && outbox[0].Kind == NotificationCancelled && stored.State == ReservationCancelled:
		case len(outbox) == 2 && outbox[0].Kind == NotificationConfirmed && outbox[1].Kind == NotificationCancelled && stored.State == ReservationCancelled:
		default:
			t.Fatalf("state = %q with illegal notification path %+v", stored.State, outbox)
		}
		for _, err := range errs {
			if err != nil && !errors.Is(err, ErrTransitionRejected) {
				t.Fatalf("race err = %v, want rejection or success", err)
			}
		}
	})
}
