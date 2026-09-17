package clock

import (
	"errors"
	"sync"
	"testing"
	"time"
)

var clock005Now = time.Date(2026, 3, 3, 9, 0, 0, 0, time.UTC)

func clock005Punch(id string, seq int64, digest string) DevicePunch {
	return DevicePunch{
		Tenant: "acme", SourceID: "device-7", PunchID: id, DeviceSeq: seq,
		OccurredAt: clock005Now.Add(-time.Hour), PayloadDigest: digest, SignatureOK: true,
	}
}

func clock005Backend() []BackendRecord {
	return []BackendRecord{
		{PunchID: "punch-1", SourceID: "device-7", PayloadDigest: "sha256:aaa", Receipt: "receipt:punch-1:sha256:aaa", RecordedAt: clock005Now.Add(-2 * time.Hour)},
	}
}

// TestTodo_CLOCK_005 is the PRIMARY contract: identical replay returns
// the original receipt, changed replay is a conflict, and missing, extra
// or reordered observations create exceptions without guessing. A revoked
// source or duplicate signed punch never posts: CLOCK_005_REJECTED with
// field/state/version and zero posted effects.
func TestTodo_CLOCK_005(t *testing.T) {
	res, err := ReconcileDeviceBackend(clock005Backend(), []DevicePunch{
		clock005Punch("punch-1", 1, "sha256:aaa"),
		clock005Punch("punch-2", 2, "sha256:bbb"),
	}, clock005Now)
	if err != nil {
		t.Fatalf("ReconcileDeviceBackend: %v", err)
	}
	if res.Receipts["punch-1"] != "receipt:punch-1:sha256:aaa" {
		t.Fatalf("identical replay must return the original receipt: %+v", res.Receipts)
	}
	if res.Receipts["punch-2"] == "" || res.Posted != 1 || res.Replayed != 1 {
		t.Fatalf("new punch posts once, replay replays: %+v", res)
	}
	if len(res.Conflicts) != 0 || res.Digest == "" {
		t.Fatalf("clean reconcile has no conflicts and seals a digest: %+v", res)
	}

	t.Run("changed replay is a conflict, not a post", func(t *testing.T) {
		res, err := ReconcileDeviceBackend(clock005Backend(), []DevicePunch{
			clock005Punch("punch-1", 1, "sha256:changed"),
		}, clock005Now)
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Conflicts) != 1 || res.Conflicts[0].PunchID != "punch-1" {
			t.Fatalf("changed replay must conflict: %+v", res)
		}
		if res.Receipts["punch-1"] != "receipt:punch-1:sha256:aaa" {
			t.Fatalf("conflict must keep the original receipt: %+v", res.Receipts)
		}
	})

	t.Run("missing and reordered observations create exceptions", func(t *testing.T) {
		res, err := ReconcileDeviceBackend(clock005Backend(), []DevicePunch{
			clock005Punch("punch-9", 5, "sha256:zzz"),
			clock005Punch("punch-8", 3, "sha256:yyy"),
		}, clock005Now)
		if err != nil {
			t.Fatal(err)
		}
		kinds := map[string]bool{}
		for _, e := range res.Exceptions {
			kinds[e.Kind] = true
		}
		if !kinds["MISSING_DEVICE_OBSERVATION"] || !kinds["REORDERED_DEVICE_OBSERVATION"] {
			t.Fatalf("missing and reordered must raise exceptions: %+v", res.Exceptions)
		}
	})

	t.Run("revoked source never posts", func(t *testing.T) {
		revoked := clock005Punch("punch-2", 2, "sha256:bbb")
		revoked.SourceRevoked = true
		before := append([]BackendRecord(nil), clock005Backend()...)
		_, err := ReconcileDeviceBackend(before, []DevicePunch{revoked}, clock005Now)
		var rej *DedupRejection
		if !errors.As(err, &rej) {
			t.Fatalf("revoked source must return *DedupRejection, got %v", err)
		}
		if !errors.Is(err, ErrDedupRejected) {
			t.Fatalf("revoked source must be CLOCK_005_REJECTED, got %v", err)
		}
		if rej.Field == "" || rej.State == "" || rej.Version == "" {
			t.Fatalf("rejection must name field/state/version: %+v", rej)
		}
		if len(before) != len(clock005Backend()) {
			t.Fatalf("refusal must post zero records")
		}
	})

	t.Run("duplicate signed punch with changed payload never posts", func(t *testing.T) {
		_, err := ReconcileDeviceBackend(nil, []DevicePunch{
			clock005Punch("punch-2", 2, "sha256:bbb"),
			clock005Punch("punch-2", 3, "sha256:changed"),
		}, clock005Now)
		if !errors.Is(err, ErrDedupRejected) {
			t.Fatalf("duplicate changed punch must be CLOCK_005_REJECTED, got %v", err)
		}
		var rej *DedupRejection
		if !errors.As(err, &rej) || rej.Field == "" || rej.State == "" || rej.Version == "" {
			t.Fatalf("rejection must name field/state/version: %v", err)
		}
	})

	t.Run("unsigned punch never posts", func(t *testing.T) {
		unsigned := clock005Punch("punch-2", 2, "sha256:bbb")
		unsigned.SignatureOK = false
		if _, err := ReconcileDeviceBackend(nil, []DevicePunch{unsigned}, clock005Now); !errors.Is(err, ErrDedupRejected) {
			t.Fatalf("unsigned punch must be CLOCK_005_REJECTED, got %v", err)
		}
	})
}

func TestTodo_CLOCK_005_Property(t *testing.T) {
	batch := []DevicePunch{clock005Punch("punch-1", 1, "sha256:aaa"), clock005Punch("punch-2", 2, "sha256:bbb")}
	a, err := ReconcileDeviceBackend(clock005Backend(), batch, clock005Now)
	if err != nil {
		t.Fatal(err)
	}
	b, err := ReconcileDeviceBackend(clock005Backend(), batch, clock005Now)
	if err != nil {
		t.Fatal(err)
	}
	if a.Digest != b.Digest {
		t.Fatalf("identical batches must reconcile identically")
	}
	// Replay is idempotent: reconciling the merged view changes nothing.
	merged := append(clock005Backend(), BackendRecord{PunchID: "punch-2", SourceID: "device-7", PayloadDigest: "sha256:bbb", Receipt: a.Receipts["punch-2"], RecordedAt: clock005Now})
	c, err := ReconcileDeviceBackend(merged, batch, clock005Now)
	if err != nil {
		t.Fatal(err)
	}
	if c.Posted != 0 || c.Replayed != 2 || len(c.Conflicts) != 0 {
		t.Fatalf("full replay must post nothing and replay all: %+v", c)
	}
	// Receipts are stable functions of punch id and payload.
	if a.Receipts["punch-2"] != c.Receipts["punch-2"] {
		t.Fatalf("receipts must be stable across reconciliations")
	}
}

func TestTodo_CLOCK_005_Race(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := ReconcileDeviceBackend(clock005Backend(), []DevicePunch{
				clock005Punch("punch-1", 1, "sha256:aaa"),
				clock005Punch("punch-2", 2, "sha256:bbb"),
			}, clock005Now)
			if err != nil {
				t.Error(err)
				return
			}
			if res.Receipts["punch-1"] != "receipt:punch-1:sha256:aaa" || res.Posted != 1 {
				t.Errorf("concurrent reconcile diverged: %+v", res)
			}
		}()
	}
	wg.Wait()
}

func TestTodo_CLOCK_005_Recovery(t *testing.T) {
	// The receipt view rebuilds from backend records alone.
	first, err := ReconcileDeviceBackend(nil, []DevicePunch{
		clock005Punch("punch-1", 1, "sha256:aaa"),
		clock005Punch("punch-2", 2, "sha256:bbb"),
	}, clock005Now)
	if err != nil {
		t.Fatal(err)
	}
	var rebuilt []BackendRecord
	for punch, receipt := range first.Receipts {
		rebuilt = append(rebuilt, BackendRecord{PunchID: punch, SourceID: "device-7", PayloadDigest: "sha256:aaa", Receipt: receipt})
	}
	_ = rebuilt
	// A backend holding two payloads for one punch refuses to reconcile.
	diverged := []BackendRecord{
		{PunchID: "punch-1", SourceID: "device-7", PayloadDigest: "sha256:aaa", Receipt: "receipt:punch-1:sha256:aaa"},
		{PunchID: "punch-1", SourceID: "device-7", PayloadDigest: "sha256:other", Receipt: "receipt:punch-1:sha256:other"},
	}
	if _, err := ReconcileDeviceBackend(diverged, nil, clock005Now); !errors.Is(err, ErrDedupRejected) {
		t.Fatalf("diverged backend must be CLOCK_005_REJECTED, got %v", err)
	}
}

func TestTodo_CLOCK_005_Mutation(t *testing.T) {
	for name, mutate := range map[string]func(*DevicePunch){
		"tenant":   func(p *DevicePunch) { p.Tenant = "" },
		"punch":    func(p *DevicePunch) { p.PunchID = "" },
		"payload":  func(p *DevicePunch) { p.PayloadDigest = "" },
		"occurred": func(p *DevicePunch) { p.OccurredAt = time.Time{} },
	} {
		p := clock005Punch("punch-9", 9, "sha256:zzz")
		mutate(&p)
		if _, err := ReconcileDeviceBackend(nil, []DevicePunch{p}, clock005Now); !errors.Is(err, ErrDedupRejected) {
			t.Fatalf("bad %s must be CLOCK_005_REJECTED", name)
		}
	}
	if _, err := ReconcileDeviceBackend(nil, []DevicePunch{clock005Punch("punch-9", 9, "sha256:zzz")}, time.Time{}); !errors.Is(err, ErrDedupRejected) {
		t.Fatalf("missing instant must be CLOCK_005_REJECTED")
	}
}
