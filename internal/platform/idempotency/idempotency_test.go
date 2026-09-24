package idempotency

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func idRequest(now time.Time) Request {
	return Request{Identity: Identity{Tenant: "tenant-1", Capability: "promotion.execute", EffectScope: "promotion:worker-1", Key: "key-1"}, Layer: "command", Canonical: []byte(`{"worker":"worker-1","effective":"2026-10-01"}`), Retention: RetentionPolicy{ExpiresAt: now.Add(24 * time.Hour), Mode: RejectReuse, Tombstone: true}, Now: now}
}

func TestCrossLayerIdempotencyLifecyclePreservesOneLogicalActionAcrossRetentionAndReplayBoundaries(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	r := NewRegistry()
	req := idRequest(now)
	first, err := r.Reserve(req)
	if err != nil || first.Decision != Reserved {
		t.Fatalf("reserve=%#v err=%v", first, err)
	}
	if _, err := r.Complete(req.Identity, first.Record.RequestDigest, "result-1", "effect-1", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	replay, err := r.Reserve(req)
	if err != nil || replay.Decision != Replay || replay.Record.ResultRef != "result-1" || replay.Record.EffectRef != "effect-1" {
		t.Fatalf("replay=%#v err=%v", replay, err)
	}
	if r.Expire(now.Add(48*time.Hour)) != 1 {
		t.Fatal("record did not expire")
	}
	retention := req.Retention
	retention.ExpiresAt = now.Add(72 * time.Hour)
	if _, err := r.Reserve(Request{Identity: req.Identity, Canonical: req.Canonical, Retention: retention, Now: now.Add(48 * time.Hour)}); !errors.Is(err, ErrExpired) {
		t.Fatalf("tombstone was reusable: %v", err)
	}
}

func TestTodo_IDEMP_001_Property(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	registry := NewRegistry()
	request := idRequest(now)
	first, err := registry.Reserve(request)
	if err != nil || first.Decision != Reserved {
		t.Fatalf("first reserve = %+v, %v", first, err)
	}
	changedLayer := request
	changedLayer.Layer = "provider-redelivery"
	replay, err := registry.Reserve(changedLayer)
	if err != nil || replay.Decision != InFlight || replay.Record.ExecutionRef != first.Record.ExecutionRef {
		t.Fatalf("layer metadata changed logical identity: first=%+v replay=%+v err=%v", first, replay, err)
	}
	changedEffect := request
	changedEffect.Identity.EffectScope = "promotion:worker-2"
	separate, err := registry.Reserve(changedEffect)
	if err != nil || separate.Decision != Reserved || separate.Record.ExecutionRef == first.Record.ExecutionRef {
		t.Fatalf("distinct effect scope was deduplicated: first=%+v separate=%+v err=%v", first, separate, err)
	}
}

func TestTodo_IDEMP_001_Golden(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	r := NewRegistry()
	one, err := r.Reserve(idRequest(now))
	if err != nil {
		t.Fatal(err)
	}
	two, err := r.Reserve(idRequest(now))
	if err != nil {
		t.Fatal(err)
	}
	if one.Record.ExecutionRef != two.Record.ExecutionRef || Explain(two.Record) == "" {
		t.Fatal("identity is not stable")
	}
}

func TestTodo_IDEMP_001_Integration(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	r := NewRegistry()
	req := idRequest(now)
	req.Layer = "webhook"
	a, err := r.Reserve(req)
	if err != nil {
		t.Fatal(err)
	}
	req.Layer = "provider"
	b, err := r.Reserve(req)
	if err != nil || a.Record.ExecutionRef != b.Record.ExecutionRef {
		t.Fatalf("layer changed identity: %#v %#v err=%v", a, b, err)
	}
}

func TestTodo_IDEMP_001_Fault(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	r := NewRegistry()
	req := idRequest(now)
	req.Retention.ExpiresAt = now.Add(-time.Second)
	if _, err := r.Reserve(req); !errors.Is(err, ErrRetentionInvalid) {
		t.Fatalf("short retention accepted: %v", err)
	}
}

func TestTodo_IDEMP_001_Security(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	r := NewRegistry()
	a := idRequest(now)
	if _, err := r.Reserve(a); err != nil {
		t.Fatal(err)
	}
	b := a
	b.Identity.Tenant = "tenant-2"
	got, err := r.Reserve(b)
	if err != nil || got.Decision != Reserved {
		t.Fatalf("tenant collision: %#v err=%v", got, err)
	}
}

func TestTodo_IDEMP_001_Conformance(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	r := NewRegistry()
	req := idRequest(now)
	first, err := r.Reserve(req)
	if err != nil {
		t.Fatal(err)
	}
	req.Canonical = []byte("different")
	if _, err := r.Reserve(req); !errors.Is(err, ErrConflict) || first.Decision != Reserved {
		t.Fatalf("canonical conflict not enforced: %v", err)
	}
}

func TestTodo_IDEMP_001_Recovery(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	r := NewRegistry()
	req := idRequest(now)
	first, err := r.Reserve(req)
	if err != nil {
		t.Fatal(err)
	}
	afterCrash, err := r.Reserve(req)
	if err != nil || afterCrash.Decision != InFlight || afterCrash.Record.ExecutionRef != first.Record.ExecutionRef {
		t.Fatalf("crash recovery launched another effect: %#v err=%v", afterCrash, err)
	}
}

func TestTodo_IDEMP_001_ModelBased(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	r := NewRegistry()
	req := idRequest(now)
	if _, err := r.Reserve(req); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Complete(req.Identity, "bad-digest", "r", "e", now); !errors.Is(err, ErrConflict) {
		t.Fatalf("bad completion changed state: %v", err)
	}
}

func TestTodo_IDEMP_001_Mutation(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	registry := NewRegistry()
	request := idRequest(now)
	reserved, err := registry.Reserve(request)
	if err != nil {
		t.Fatal(err)
	}
	request.Canonical[2] ^= 1
	if _, err := registry.Reserve(request); !errors.Is(err, ErrConflict) {
		t.Fatalf("mutated canonical request error = %v, want conflict", err)
	}
	stored, err := registry.Lookup(request.Identity)
	if err != nil {
		t.Fatal(err)
	}
	if stored.RequestDigest != reserved.Record.RequestDigest || stored.State != InProgress || stored.ReplayCount != 0 {
		t.Fatalf("conflicting mutation changed original reservation: %+v", stored)
	}
}

func TestTodo_IDEMP_001_Race(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	r := NewRegistry()
	req := idRequest(now)
	var wg sync.WaitGroup
	results := make(chan Resolution, 16)
	errs := make(chan error, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); x, err := r.Reserve(req); results <- x; errs <- err }()
	}
	wg.Wait()
	close(results)
	close(errs)
	var execution string
	for x := range results {
		if execution == "" {
			execution = x.Record.ExecutionRef
		}
		if x.Record.ExecutionRef != execution {
			t.Fatal("execution identity changed")
		}
	}
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func FuzzTodo_IDEMP_001(f *testing.F) {
	f.Add([]byte("canonical"), "key")
	f.Fuzz(func(t *testing.T, canonical []byte, key string) {
		if key == "" || len(canonical) == 0 {
			return
		}
		now := time.Unix(100, 0).UTC()
		req := idRequest(now)
		req.Identity.Key = key
		req.Canonical = canonical
		r := NewRegistry()
		if _, err := r.Reserve(req); err != nil {
			t.Fatal(err)
		}
	})
}
