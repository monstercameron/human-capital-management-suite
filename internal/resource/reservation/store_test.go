package reservation

import (
	"errors"
	"testing"
	"time"
)

func request(digest string) Request {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return Request{Resource: "position/42", Version: 1, Quantity: Quantity{Value: 500, Scale: 3}, Interval: Interval{From: now, To: now.Add(time.Hour)}, Owner: "tenant-a", Priority: 10, ExpiresAt: now.Add(time.Hour), ProposalDigest: digest, AuthorityDigest: Digest([]byte("authority")), IdempotencyKey: "req-1"}
}
func TestAcquireBindsProposalAndIsIdempotent(t *testing.T) {
	s := NewStore()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r := request(Digest([]byte("proposal")))
	a, err := s.Acquire(r, Quantity{Value: 1000, Scale: 3}, now)
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Acquire(r, Quantity{Value: 1000, Scale: 3}, now)
	if err != nil {
		t.Fatal(err)
	}
	if a.ID != b.ID || a.Fence != b.Fence {
		t.Fatal("replay did not return the original hold")
	}
	r.ProposalDigest = Digest([]byte("changed"))
	if _, err = s.Acquire(r, Quantity{Value: 1000, Scale: 3}, now); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed proposal error = %v", err)
	}
}
func TestAcquireCapacityAndFence(t *testing.T) {
	s := NewStore()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r := request(Digest([]byte("one")))
	a, _ := s.Acquire(r, Quantity{Value: 600, Scale: 3}, now)
	r.IdempotencyKey = "req-2"
	r.ProposalDigest = Digest([]byte("two"))
	if _, err := s.Acquire(r, Quantity{Value: 600, Scale: 3}, now); !errors.Is(err, ErrCapacity) {
		t.Fatalf("capacity error = %v", err)
	}
	if _, err := s.Consume(a.ID, a.Fence+1, now); !errors.Is(err, ErrFence) {
		t.Fatalf("fence error = %v", err)
	}
	if _, err := s.Consume(a.ID, a.Fence, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Consume(a.ID, a.Fence, now); err != nil {
		t.Fatal("terminal replay must be idempotent")
	}
}
func TestExpireIsAppendOnlyAndTerminal(t *testing.T) {
	s := NewStore()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	r := request(Digest([]byte("one")))
	r.ExpiresAt = now.Add(time.Minute)
	a, _ := s.Acquire(r, Quantity{Value: 1000, Scale: 3}, now)
	if got := s.Expire(now.Add(time.Minute)); len(got) != 1 || got[0].Status != Expired {
		t.Fatalf("expired = %#v", got)
	}
	if len(s.Events(a.ID)) != 2 {
		t.Fatal("expiry must append one event")
	}
}
func TestValidationRejectsMalformedRequest(t *testing.T) {
	r := request("bad")
	if err := r.Validate(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); !errors.Is(err, ErrInvalidDigest) {
		t.Fatal(err)
	}
}

func TestTodo_CONF_021_ReservationServedPath(t *testing.T) {
	if ServingContractID == "" {
		t.Fatal("served contract id is empty")
	}
	if err := ValidateServingContract(); err != nil {
		t.Fatalf("serving contract: %v", err)
	}
}
