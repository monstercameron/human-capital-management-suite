package appointment

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	sharedreservation "github.com/monstercameron/human-capital-management-suite/internal/resource/reservation"
)

func TestTodo_REV_046_02(t *testing.T) {
	now := appt003Now()
	store := sharedreservation.NewStore()
	first := NewReservationLedgerWithStore(store)
	second := NewReservationLedgerWithStore(store)
	req, request := appt003KnownRequest(t)

	reserved, err := first.Reserve(req, request, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.fences[reserved.ID]) != 2 {
		t.Fatalf("shared fences = %d, want participant and resource", len(first.fences[reserved.ID]))
	}
	for _, fence := range first.fences[reserved.ID] {
		stored, ok := store.Get(fence.ID)
		if !ok || stored.Fence != fence.Fence || stored.Status != sharedreservation.Held {
			t.Fatalf("shared fence = %+v, stored = %+v, found = %v", fence, stored, ok)
		}
	}
	if _, err := first.Confirm(reserved.ID, reserved.RequirementDigest, "confirm", now.Add(time.Minute)); err != nil {
		t.Fatalf("confirm: %v", err)
	}

	// An independent appointment adapter sees the shared resource fence.
	_, competing := appt003KnownRequest(t)
	competing.Participants = []values.EntityRef{typedAppointmentRef(values.Kind("interviewer"), "002")}
	if _, err := second.Reserve(req, competing, now.Add(2*time.Minute)); !errors.Is(err, ErrReservationConflict) {
		t.Fatalf("competing reservation err = %v, want shared conflict", err)
	}
	movedSlot := appt003Slot(t, 9, 30, 10, 0)
	if _, err := first.Reschedule(reserved.ID, reserved.RequirementDigest, movedSlot, "move", now.Add(3*time.Minute)); err != nil {
		t.Fatalf("reschedule through shared fences: %v", err)
	}
	competing.Slot = movedSlot
	if _, err := second.Reserve(req, competing, now.Add(4*time.Minute)); !errors.Is(err, ErrReservationConflict) {
		t.Fatalf("competing moved-slot reservation err = %v, want shared conflict", err)
	}
	competing.Slot = request.Slot
	if _, err := second.Reserve(req, competing, now.Add(5*time.Minute)); err != nil {
		t.Fatalf("reserve released old interval after reschedule: %v", err)
	}
}

func TestTodo_REV_046_02_Race(t *testing.T) {
	now := appt003Now()
	store := sharedreservation.NewStore()
	ledgers := []*ReservationLedger{NewReservationLedgerWithStore(store), NewReservationLedgerWithStore(store)}
	base := typedAppointmentRequirement(t)
	participants := []values.EntityRef{
		typedAppointmentRef(values.Kind("candidate"), "001"),
		typedAppointmentRef(values.Kind("interviewer"), "002"),
	}
	base.ParticipantRefs = participants
	base.Participants = []ParticipantRole{{Role: "candidate", Min: 1, Max: 1}, {Role: "interviewer", Min: 1, Max: 1}}
	req, err := NewAppointmentRequirement(base)
	if err != nil {
		t.Fatal(err)
	}
	_, typed := appt003KnownRequest(t)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range ledgers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			r := typed
			r.Participants = []values.EntityRef{participants[i]}
			_, errs[i] = ledgers[i].Reserve(req, r, now)
		}(i)
	}
	wg.Wait()
	wins := 0
	for _, err := range errs {
		if err == nil {
			wins++
		} else if !errors.Is(err, ErrReservationConflict) {
			t.Fatalf("race err = %v, want success or shared conflict", err)
		}
	}
	if wins != 1 {
		t.Fatalf("winners = %d, want one shared fence winner", wins)
	}

	t.Run("confirm-versus-shared-expiry", func(t *testing.T) {
		now := appt003Now()
		store := sharedreservation.NewStore()
		ledger := NewReservationLedgerWithStore(store)
		req, request := appt003KnownRequest(t)
		res, err := ledger.Reserve(req, request, now)
		if err != nil {
			t.Fatal(err)
		}
		fences := sharedTokens(ledger.fences[res.ID])
		start := make(chan struct{})
		var wg sync.WaitGroup
		var confirmErr, expireErr error
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			_, confirmErr = ledger.Confirm(res.ID, res.RequirementDigest, "race-confirm-expiry", now.Add(time.Minute))
		}()
		go func() {
			defer wg.Done()
			<-start
			_, expireErr = store.ExpireBatch(fences, res.ExpiresAt.Time())
		}()
		close(start)
		wg.Wait()

		stored, err := ledger.Get(res.ID)
		if err != nil {
			t.Fatal(err)
		}
		if confirmErr == nil {
			if stored.State != ReservationConfirmed || expireErr == nil {
				t.Fatalf("confirm won with state %q and expiry err %v", stored.State, expireErr)
			}
		} else {
			if !errors.Is(confirmErr, ErrTransitionRejected) || expireErr != nil || stored.State != ReservationHeld {
				t.Fatalf("expiry won with state %q, confirm err %v, expiry err %v", stored.State, confirmErr, expireErr)
			}
		}
		wantStatus := sharedreservation.Committed
		if confirmErr != nil {
			wantStatus = sharedreservation.Expired
		}
		for _, fence := range fences {
			shared, ok := store.Get(fence.ID)
			if !ok || shared.Status != wantStatus {
				t.Fatalf("shared fence = %+v, found=%v; want every fence %s", shared, ok, wantStatus)
			}
			if shared.Status == sharedreservation.Consumed {
				t.Fatalf("appointment confirmation incorrectly terminal-consumed a live fence: %+v", shared)
			}
		}
	})
}

func TestTodo_REV_046_02_Mutation(t *testing.T) {
	now := appt003Now()
	store := sharedreservation.NewStore()
	ledger := NewReservationLedgerWithStore(store)
	req, request := appt003KnownRequest(t)
	res, err := ledger.Reserve(req, request, now)
	if err != nil {
		t.Fatal(err)
	}
	backend := ledger.fences[res.ID][len(ledger.fences[res.ID])-1]
	if _, err := store.Consume(backend.ID, backend.Fence, now.Add(time.Minute)); err != nil {
		t.Fatalf("consume shared fence: %v", err)
	}
	if _, err := ledger.Confirm(res.ID, res.RequirementDigest, "confirm-after-external-consume", now.Add(2*time.Minute)); !errors.Is(err, ErrTransitionRejected) {
		t.Fatalf("confirm after shared fence consumption err = %v, want fenced rejection", err)
	}
	stored, err := ledger.Get(res.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stored.State != ReservationHeld {
		t.Fatalf("appointment state = %q, want HELD after stale shared fence refusal", stored.State)
	}
	firstFence, ok := store.Get(ledger.fences[res.ID][0].ID)
	if !ok || firstFence.Status != sharedreservation.Held {
		t.Fatalf("earlier shared fence = %+v, want unchanged HELD after later fence failure", firstFence)
	}
}
