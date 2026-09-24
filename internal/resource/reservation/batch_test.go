package reservation

import (
	"errors"
	"testing"
	"time"
)

func TestAcquireBatchIsAtomicAndCommittedHoldsRemainActive(t *testing.T) {
	store := NewStore()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	first := request(Digest([]byte("first")))
	second := request(Digest([]byte("second")))
	second.Resource = "position/43"
	second.IdempotencyKey = "req-2"
	second.AuthorityDigest = first.AuthorityDigest
	second.Interval = first.Interval
	second.ExpiresAt = first.ExpiresAt
	second.Owner = first.Owner
	second.Version = first.Version
	second.Quantity = first.Quantity
	_, err := store.AcquireBatch([]AcquireRequest{
		{Request: first, Capacity: Quantity{Value: 1000, Scale: 3}},
		{Request: second, Capacity: Quantity{Value: 1000, Scale: 3}},
	}, now)
	if err != nil {
		t.Fatal(err)
	}

	holds, err := store.AcquireBatch([]AcquireRequest{
		{Request: first, Capacity: Quantity{Value: 1000, Scale: 3}},
		{Request: second, Capacity: Quantity{Value: 1000, Scale: 3}},
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	fences := []Fence{{ID: holds[0].ID, Token: holds[0].Fence}, {ID: holds[1].ID, Token: holds[1].Fence + 1}}
	if _, err := store.CommitBatch(fences, now.Add(time.Minute)); !errors.Is(err, ErrFence) {
		t.Fatalf("stale later fence err = %v, want ErrFence", err)
	}
	unchanged, _ := store.Get(holds[0].ID)
	if unchanged.Status != Held {
		t.Fatalf("first status = %s, want HELD after atomic refusal", unchanged.Status)
	}

	fences[1].Token = holds[1].Fence
	committed, err := store.CommitBatch(fences, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if committed[0].Status != Committed || committed[1].Status != Committed {
		t.Fatalf("committed batch = %+v", committed)
	}
	competitor := request(Digest([]byte("competitor")))
	competitor.IdempotencyKey = "req-3"
	competitor.Quantity = Quantity{Value: 600, Scale: 3}
	if _, err := store.Acquire(competitor, Quantity{Value: 1000, Scale: 3}, now.Add(2*time.Minute)); !errors.Is(err, ErrCapacity) {
		t.Fatalf("acquire against committed hold err = %v, want capacity conflict", err)
	}
	if _, err := store.ReleaseBatch(fences, now.Add(3*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Acquire(competitor, Quantity{Value: 1000, Scale: 3}, now.Add(4*time.Minute)); err != nil {
		t.Fatalf("acquire after committed release: %v", err)
	}
}

func TestUpdateIntervalBatchFencesNewIntervalAtomically(t *testing.T) {
	store := NewStore()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	first := request(Digest([]byte("moving")))
	hold, err := store.Acquire(first, Quantity{Value: 1000, Scale: 3}, now)
	if err != nil {
		t.Fatal(err)
	}
	movedInterval := Interval{From: first.Interval.From.Add(2 * time.Hour), To: first.Interval.To.Add(2 * time.Hour)}
	moved, err := store.UpdateIntervalBatch([]Fence{{ID: hold.ID, Token: hold.Fence}}, movedInterval, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if moved[0].Request.Interval != movedInterval || moved[0].Fence == hold.Fence {
		t.Fatalf("updated reservation = %+v", moved[0])
	}
	if _, err := store.Release(hold.ID, hold.Fence, now.Add(2*time.Minute)); !errors.Is(err, ErrFence) {
		t.Fatalf("old fence release err = %v, want stale fence", err)
	}
	oldSlot := request(Digest([]byte("old-slot")))
	oldSlot.IdempotencyKey = "old-slot"
	if _, err := store.Acquire(oldSlot, Quantity{Value: 1000, Scale: 3}, now.Add(3*time.Minute)); err != nil {
		t.Fatalf("acquire old interval after move: %v", err)
	}
	newSlot := request(Digest([]byte("new-slot")))
	newSlot.IdempotencyKey = "new-slot"
	newSlot.Interval = movedInterval
	newSlot.Quantity = Quantity{Value: 600, Scale: 3}
	if _, err := store.Acquire(newSlot, Quantity{Value: 1000, Scale: 3}, now.Add(3*time.Minute)); !errors.Is(err, ErrCapacity) {
		t.Fatalf("acquire moved interval err = %v, want capacity conflict", err)
	}
}
