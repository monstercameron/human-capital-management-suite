package appointment

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func appt003Now() time.Time { return time.Date(2026, 9, 10, 8, 0, 0, 0, time.UTC) }

func appt003Requirement(t *testing.T) Requirement {
	t.Helper()
	req, err := NewAppointmentRequirement(typedAppointmentRequirement(t))
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func appt003Slot(t *testing.T, sh, sm, eh, em int) values.EffectiveInterval {
	t.Helper()
	start := values.NewInstant(time.Date(2026, 9, 10, sh, sm, 0, 0, time.UTC))
	end := values.NewInstant(time.Date(2026, 9, 10, eh, em, 0, 0, time.UTC))
	slot, err := values.NewInstantInterval(start, end)
	if err != nil {
		t.Fatal(err)
	}
	return slot
}

func appt003Quantity(t *testing.T, n string) values.Quantity {
	t.Helper()
	q, err := values.NewQuantity(n, "EACH", 0, values.RoundingHalfEven)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func appt003KnownRequest(t *testing.T) (Requirement, ReservationRequest) {
	t.Helper()
	req := appt003Requirement(t)
	r := ReservationRequest{
		Slot:         appt003Slot(t, 9, 0, 9, 30),
		Participants: []values.EntityRef{typedAppointmentRef(values.Kind("candidate"), "001")},
		Resources: []ResourceHold{
			{
				ResourceRef:     typedAppointmentRef(values.Kind("resource"), "005"),
				ResourceTypeRef: typedAppointmentRef(values.Kind("resource_type"), "003"),
				Quantity:        appt003Quantity(t, "1"),
			},
		},
		HoldTTL: 15 * time.Minute,
	}
	return req, r
}

func appt003Rejection(t *testing.T, err error, wantField string) *Rejection {
	t.Helper()
	if err == nil {
		t.Fatal("accepted a defective reservation")
	}
	if !errors.Is(err, ErrReservationRejected) && !errors.Is(err, ErrReservationConflict) {
		t.Fatalf("err = %v, want reservation rejection", err)
	}
	var rej *Rejection
	if !errors.As(err, &rej) {
		t.Fatalf("err = %v, want *Rejection", err)
	}
	if rej.Code != "APPT_003_REJECTED" {
		t.Fatalf("code = %q, want APPT_003_REJECTED", rej.Code)
	}
	if rej.Field != wantField {
		t.Fatalf("field = %q, want %q (%v)", rej.Field, wantField, err)
	}
	if rej.Version != "v1" {
		t.Fatalf("version = %q, want v1", rej.Version)
	}
	return rej
}

// TestTodo_APPT_003 is the PRIMARY contract: a valid slot plus resource
// contract commits every participant/resource hold together; expiry and
// release are fenced; a byte-identical replay returns the same record.
func TestTodo_APPT_003(t *testing.T) {
	req, r := appt003KnownRequest(t)
	ledger := NewReservationLedger()
	now := appt003Now()

	res, err := ledger.Reserve(req, r, now)
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if res.State != ReservationHeld {
		t.Fatalf("state = %q, want HELD", res.State)
	}
	if len(res.Participants) != 1 || len(res.Resources) != 1 {
		t.Fatalf("holds = %+v, want one participant and one resource hold", res)
	}
	if res.Digest == "" || res.ID == "" {
		t.Fatalf("reservation lacks identity: %+v", res)
	}
	wantDigest, err := req.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if res.RequirementDigest != wantDigest {
		t.Fatalf("requirement digest = %q, want %q", res.RequirementDigest, wantDigest)
	}
	if !res.ExpiresAt.Time().Equal(now.Add(15 * time.Minute)) {
		t.Fatalf("expiry = %v, want now+15m", res.ExpiresAt.Time())
	}
	if got := ledger.Active(); got != 1 {
		t.Fatalf("active = %d, want 1", got)
	}

	replay, err := ledger.Reserve(req, r, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("idempotent replay: %v", err)
	}
	if replay.ID != res.ID || replay.Digest != res.Digest {
		t.Fatalf("replay diverged: %+v vs %+v", replay, res)
	}
	if got := ledger.Active(); got != 1 {
		t.Fatalf("active after replay = %d, want 1", got)
	}

	if _, err := ledger.Expire(res.ID, now); !errors.Is(err, ErrReservationStateConflict) {
		t.Fatalf("early expire err = %v, want state conflict", err)
	}
	held, err := ledger.Get(res.ID)
	if err != nil {
		t.Fatal(err)
	}
	if held.State != ReservationHeld {
		t.Fatalf("state after early expire = %q, want HELD", held.State)
	}

	expired, err := ledger.Expire(res.ID, now.Add(15*time.Minute))
	if err != nil {
		t.Fatalf("Expire at exact expiry: %v", err)
	}
	if expired.State != ReservationExpired {
		t.Fatalf("state = %q, want EXPIRED", expired.State)
	}
	if got := ledger.Active(); got != 0 {
		t.Fatalf("active after expiry = %d, want 0", got)
	}

	// An expired hold no longer fences the slot: a competing known
	// participant may reserve the same resource.
	other := ReservationRequest{
		Slot:         appt003Slot(t, 9, 0, 9, 30),
		Participants: []values.EntityRef{typedAppointmentRef(values.Kind("interviewer"), "002")},
		Resources:    r.Resources,
		HoldTTL:      15 * time.Minute,
	}
	if _, err := ledger.Reserve(req, other, now.Add(16*time.Minute)); err != nil {
		t.Fatalf("reserve after expiry: %v", err)
	}

	fresh := NewReservationLedger()
	held2, err := fresh.Reserve(req, r, now)
	if err != nil {
		t.Fatal(err)
	}
	released, err := fresh.Release(held2.ID)
	if err != nil {
		t.Fatalf("Release: %v", err)
	}
	if released.State != ReservationReleased {
		t.Fatalf("state = %q, want RELEASED", released.State)
	}
	if _, err := fresh.Release(held2.ID); !errors.Is(err, ErrReservationStateConflict) {
		t.Fatalf("double release err = %v, want state conflict", err)
	}
}

// TestTodo_APPT_003_Mutation kills the seeded defects named in the RED
// contract: a missing time, a missing resource contract, or a competing slot
// reservation must never be accepted, and failures must persist zero holds.
func TestTodo_APPT_003_Mutation(t *testing.T) {
	req := appt003Requirement(t)
	now := appt003Now()

	t.Run("missing-time", func(t *testing.T) {
		ledger := NewReservationLedger()
		_, r := appt003KnownRequest(t)
		r.Slot = values.EffectiveInterval{}
		appt003Rejection(t, errorOf(ledger.Reserve(req, r, now)), "slot")
		if got := ledger.Active(); got != 0 {
			t.Fatalf("active = %d, want 0 after rejected reserve", got)
		}
	})

	t.Run("missing-resource-contract", func(t *testing.T) {
		ledger := NewReservationLedger()
		_, r := appt003KnownRequest(t)
		r.Resources = nil
		appt003Rejection(t, errorOf(ledger.Reserve(req, r, now)), "resources")
		if got := ledger.Active(); got != 0 {
			t.Fatalf("active = %d, want 0 after rejected reserve", got)
		}
	})

	t.Run("zero-quantity-hold-commits-nothing", func(t *testing.T) {
		ledger := NewReservationLedger()
		_, r := appt003KnownRequest(t)
		r.Resources = append([]ResourceHold(nil), r.Resources...)
		r.Resources = append(r.Resources, ResourceHold{
			ResourceRef:     typedAppointmentRef(values.Kind("resource"), "006"),
			ResourceTypeRef: typedAppointmentRef(values.Kind("resource_type"), "003"),
		})
		appt003Rejection(t, errorOf(ledger.Reserve(req, r, now)), "resources[1]")
		if got := ledger.Active(); got != 0 {
			t.Fatalf("active = %d, want 0: partial holds must not commit", got)
		}
	})

	t.Run("unknown-participant", func(t *testing.T) {
		ledger := NewReservationLedger()
		_, r := appt003KnownRequest(t)
		r.Participants = []values.EntityRef{typedAppointmentRef(values.Kind("worker"), "099")}
		appt003Rejection(t, errorOf(ledger.Reserve(req, r, now)), "participants[0]")
	})

	t.Run("competing-resource-hold", func(t *testing.T) {
		ledger := NewReservationLedger()
		req, first := appt003KnownRequest(t)
		if _, err := ledger.Reserve(req, first, now); err != nil {
			t.Fatal(err)
		}
		second := ReservationRequest{
			Slot:         appt003Slot(t, 9, 15, 9, 45),
			Participants: []values.EntityRef{typedAppointmentRef(values.Kind("interviewer"), "002")},
			Resources:    first.Resources,
			HoldTTL:      15 * time.Minute,
		}
		err := errorOf(ledger.Reserve(req, second, now))
		if !errors.Is(err, ErrReservationConflict) {
			t.Fatalf("competing reserve err = %v, want conflict", err)
		}
		appt003Rejection(t, err, "resources")
		if got := ledger.Active(); got != 1 {
			t.Fatalf("active = %d, want exactly the first winner", got)
		}
	})

	t.Run("competing-participant-hold", func(t *testing.T) {
		ledger := NewReservationLedger()
		req, first := appt003KnownRequest(t)
		if _, err := ledger.Reserve(req, first, now); err != nil {
			t.Fatal(err)
		}
		second := ReservationRequest{
			Slot:         appt003Slot(t, 9, 15, 9, 45),
			Participants: first.Participants,
			Resources: []ResourceHold{
				{
					ResourceRef:     typedAppointmentRef(values.Kind("resource"), "006"),
					ResourceTypeRef: typedAppointmentRef(values.Kind("resource_type"), "003"),
					Quantity:        appt003Quantity(t, "1"),
				},
			},
			HoldTTL: 15 * time.Minute,
		}
		err := errorOf(ledger.Reserve(req, second, now))
		if !errors.Is(err, ErrReservationConflict) {
			t.Fatalf("double-booked participant err = %v, want conflict", err)
		}
		appt003Rejection(t, err, "participants")
	})

	t.Run("invalid-requirement", func(t *testing.T) {
		ledger := NewReservationLedger()
		bad := appt003Requirement(t)
		bad.Duration = 0
		_, r := appt003KnownRequest(t)
		appt003Rejection(t, errorOf(ledger.Reserve(bad, r, now)), "requirement")
	})

	t.Run("slot-outside-window", func(t *testing.T) {
		ledger := NewReservationLedger()
		_, r := appt003KnownRequest(t)
		r.Slot = appt003Slot(t, 10, 0, 10, 30)
		appt003Rejection(t, errorOf(ledger.Reserve(req, r, now)), "slot")
	})

	t.Run("zero-ttl", func(t *testing.T) {
		ledger := NewReservationLedger()
		_, r := appt003KnownRequest(t)
		r.HoldTTL = 0
		appt003Rejection(t, errorOf(ledger.Reserve(req, r, now)), "hold_ttl")
	})
}

func errorOf(_ Reservation, err error) error { return err }

// TestTodo_APPT_003_Race proves one-winner semantics under concurrency: 16
// distinct known participants racing for one exclusive resource commit
// exactly one reservation, while 16 identical replays converge on one record.
func TestTodo_APPT_003_Race(t *testing.T) {
	now := appt003Now()

	t.Run("single-winner", func(t *testing.T) {
		base := typedAppointmentRequirement(t)
		refs := make([]values.EntityRef, 0, 16)
		roles := make([]ParticipantRole, 0, 16)
		for i := 0; i < 16; i++ {
			ref := typedAppointmentRef(values.Kind("candidate"), "011")
			ref.Id = "00000000-0000-4000-8000-0000000001" + string(rune('0'+i/10)) + string(rune('0'+i%10))
			refs = append(refs, ref)
			roles = append(roles, ParticipantRole{Role: "candidate-" + string(rune('a'+i)), Min: 1, Max: 1})
		}
		base.ParticipantRefs = refs
		base.Participants = roles
		req, err := NewAppointmentRequirement(base)
		if err != nil {
			t.Fatal(err)
		}
		slot := appt003Slot(t, 9, 0, 9, 30)
		qty := appt003Quantity(t, "1")
		hold := ResourceHold{
			ResourceRef:     typedAppointmentRef(values.Kind("resource"), "005"),
			ResourceTypeRef: typedAppointmentRef(values.Kind("resource_type"), "003"),
			Quantity:        qty,
		}
		type outcome struct {
			res Reservation
			err error
		}
		requests := make([]ReservationRequest, 16)
		for i := range requests {
			requests[i] = ReservationRequest{
				Slot:         slot,
				Participants: []values.EntityRef{refs[i]},
				Resources:    []ResourceHold{hold},
				HoldTTL:      15 * time.Minute,
			}
		}
		ledger := NewReservationLedger()
		var wg sync.WaitGroup
		outcomes := make([]outcome, 16)
		for i := range requests {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				res, err := ledger.Reserve(req, requests[i], now)
				outcomes[i] = outcome{res: res, err: err}
			}(i)
		}
		wg.Wait()
		wins := 0
		var winner Reservation
		for _, o := range outcomes {
			if o.err == nil {
				wins++
				winner = o.res
			} else if !errors.Is(o.err, ErrReservationConflict) {
				t.Fatalf("race err = %v, want conflict or success", o.err)
			}
		}
		if wins != 1 {
			t.Fatalf("winners = %d, want exactly 1", wins)
		}
		if got := ledger.Active(); got != 1 {
			t.Fatalf("active = %d, want 1", got)
		}
		stored, err := ledger.Get(winner.ID)
		if err != nil {
			t.Fatal(err)
		}
		if stored.Digest != winner.Digest {
			t.Fatal("winner digest does not match the committed record")
		}
	})

	t.Run("identical-replay-converges", func(t *testing.T) {
		req, r := appt003KnownRequest(t)
		ledger := NewReservationLedger()
		var wg sync.WaitGroup
		ids := make([]string, 16)
		errs := make([]error, 16)
		for i := 0; i < 16; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				res, err := ledger.Reserve(req, r, now)
				if err == nil {
					ids[i] = res.ID
				}
				errs[i] = err
			}(i)
		}
		wg.Wait()
		for _, err := range errs {
			if err != nil {
				t.Fatalf("replay err = %v, want success", err)
			}
		}
		for _, id := range ids {
			if id != ids[0] || id == "" {
				t.Fatalf("replay ids = %v, want one shared id", ids)
			}
		}
		if got := ledger.Active(); got != 1 {
			t.Fatalf("active = %d, want 1", got)
		}
	})
}
