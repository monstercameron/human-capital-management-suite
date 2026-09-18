package adversarial

import (
	"context"
	"strings"
	"sync"
	"testing"
)

// PRIMARY: the reusable adversarial suite covers cache, queue, object-store,
// log and backup paths with deny/isolated verdicts and zero foreign bytes.
func TestTodo_SECARCH_010(t *testing.T) {
	// Cache-key collision: same logical key under two tenants never collides
	// and each key parses back to its owning tenant.
	ka := CacheKey("tenant-a", "session:123")
	kb := CacheKey("tenant-b", "session:123")
	if ka == kb {
		t.Fatalf("cache keys collide across tenants: %q", ka)
	}
	if got, ok := CacheKeyTenant(ka); !ok || got != "tenant-a" {
		t.Fatalf("cache key tenant = %q,%v want tenant-a,true", got, ok)
	}
	if got, ok := CacheKeyTenant(kb); !ok || got != "tenant-b" {
		t.Fatalf("cache key tenant = %q,%v want tenant-b,true", got, ok)
	}

	// Queue/job cross-tenant execution: a job owned by tenant-a must not
	// execute under tenant-b's identity.
	env := NewJobEnvelope("tenant-a", "job-1", []byte("payload"))
	if err := ExecuteJob("tenant-a", env); err != nil {
		t.Fatalf("own-tenant job execution denied: %v", err)
	}
	if err := ExecuteJob("tenant-b", env); !IsNonDisclosingDeny(err) {
		t.Fatalf("cross-tenant job execution not denied without disclosure: %v", err)
	}

	// Object-store prefix: same prefix/name under two tenants never shares
	// a physical key and each key parses back to its owner.
	oa := ObjectKey("tenant-a", "exports", "pay.csv")
	ob := ObjectKey("tenant-b", "exports", "pay.csv")
	if oa == ob {
		t.Fatalf("object keys collide across tenants: %q", oa)
	}
	if got, ok := ObjectKeyTenant(oa); !ok || got != "tenant-a" {
		t.Fatalf("object key tenant = %q,%v want tenant-a,true", got, ok)
	}

	// Log disclosure: a line emitted for tenant-a carries zero bytes of
	// tenant-b's marker.
	line := LogLine("tenant-a", "payout released")
	if n := ForeignBytes(line, "tenant-a", []string{"tenant-a", "tenant-b"}); n != 0 {
		t.Fatalf("log line carries %d foreign bytes: %q", n, line)
	}
	leaked := "payout for tenant-b ssn=123 tenant-a done"
	if n := ForeignBytes(leaked, "tenant-a", []string{"tenant-a", "tenant-b"}); n == 0 {
		t.Fatalf("foreign-byte detector missed planted leak: %q", leaked)
	}

	// Backup restore placement: a snapshot placed for tenant-a restores only
	// into tenant-a.
	placement := BackupPlacement("tenant-a", "snap-001")
	if err := AllowRestore("tenant-a", placement); err != nil {
		t.Fatalf("own-tenant restore denied: %v", err)
	}
	if err := AllowRestore("tenant-b", placement); !IsNonDisclosingDeny(err) {
		t.Fatalf("cross-tenant restore not denied without disclosure: %v", err)
	}
}

func TestTodo_SECARCH_010_Golden(t *testing.T) {
	ka1 := CacheKey("tenant-a", "k")
	ka2 := CacheKey("tenant-a", "k")
	if ka1 != ka2 {
		t.Fatalf("cache key not deterministic: %q vs %q", ka1, ka2)
	}
	oa1 := ObjectKey("tenant-a", "p", "n")
	oa2 := ObjectKey("tenant-a", "p", "n")
	if oa1 != oa2 {
		t.Fatalf("object key not deterministic: %q vs %q", oa1, oa2)
	}
	once := BackupPlacement("tenant-a", "s")
	twice := BackupPlacement("tenant-a", "s")
	if once != twice {
		t.Fatal("backup placement not deterministic")
	}
	engine := NewEngine(DefaultDenyHandler)
	for _, j := range PresetIsolationJourneys() {
		r1 := engine.Run(context.Background(), j)
		r2 := engine.Run(context.Background(), j)
		if r1.Denied != r2.Denied || r1.EvidenceID != r2.EvidenceID {
			t.Fatalf("isolation journey %s not stable: %+v vs %+v", j.ID, r1, r2)
		}
	}
}

func TestTodo_SECARCH_010_Security(t *testing.T) {
	// Cross-tenant vectors through the adversarial engine deny without
	// existence/metadata/telemetry leakage and carry redacted evidence.
	engine := NewEngine(DefaultDenyHandler)
	results := engine.RunAll(context.Background(), PresetIsolationJourneys())
	if err := VerifyNoLeakage(results); err != nil {
		t.Fatalf("isolation journeys leaked: %v", err)
	}
	// Direct unit-level denies are non-disclosing: no tenant marker escapes.
	env := NewJobEnvelope("tenant-a", "job-x", []byte("x"))
	for _, err := range []error{ExecuteJob("tenant-b", env), AllowRestore("tenant-b", BackupPlacement("tenant-a", "s"))} {
		if !IsNonDisclosingDeny(err) {
			t.Fatalf("disclosing deny: %v", err)
		}
		if strings.Contains(err.Error(), "tenant-") {
			t.Fatalf("deny discloses tenant marker: %v", err)
		}
	}
}

func TestTodo_SECARCH_010_Integration(t *testing.T) {
	// The five vectors are retained as release-gating conformance: engine
	// verdicts plus zero-foreign-byte checks over the concrete artifacts.
	engine := NewEngine(DefaultDenyHandler)
	journeys := PresetIsolationJourneys()
	if len(journeys) != 5 {
		t.Fatalf("want 5 retained isolation vectors, got %d", len(journeys))
	}
	names := map[string]bool{}
	for _, j := range journeys {
		names[j.ID] = true
	}
	for _, want := range []string{"ISO-CACHE-01", "ISO-QUEUE-01", "ISO-OBJECT-01", "ISO-LOG-01", "ISO-BACKUP-01"} {
		if !names[want] {
			t.Fatalf("retained vector %s missing from %v", want, journeys)
		}
	}
	if err := VerifyNoLeakage(engine.RunAll(context.Background(), journeys)); err != nil {
		t.Fatalf("integration leakage: %v", err)
	}
	if got := ForeignBytes(LogLine("tenant-a", "hello"), "tenant-a", []string{"tenant-a", "tenant-b"}); got != 0 {
		t.Fatalf("integration log carries %d foreign bytes", got)
	}
}

func TestTodo_SECARCH_010_Race(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ka := CacheKey("tenant-a", "k")
			if got, ok := CacheKeyTenant(ka); !ok || got != "tenant-a" {
				t.Errorf("race cache tenant = %q,%v", got, ok)
			}
			env := NewJobEnvelope("tenant-a", "j", []byte("p"))
			if err := ExecuteJob("tenant-a", env); err != nil {
				t.Errorf("race own-tenant job denied: %v", err)
			}
			if err := ExecuteJob("tenant-b", env); !IsNonDisclosingDeny(err) {
				t.Errorf("race cross-tenant job allowed: %v", err)
			}
		}()
	}
	wg.Wait()
}

func TestTodo_SECARCH_010_Mutation(t *testing.T) {
	// Naive unscoped implementations must fail these guards: raw keys with
	// no tenant namespace collide, and raw restores without a placement
	// check allow cross-tenant access.
	rawA, rawB := "session:123", "session:123"
	if rawA != rawB {
		t.Fatal("test setup broken: raw keys should collide without namespacing")
	}
	if CacheKey("tenant-a", "session:123") == CacheKey("tenant-b", "session:123") {
		t.Fatal("mutation survived: unscoped cache keys collide")
	}
	env := NewJobEnvelope("tenant-a", "job-m", []byte("p"))
	if ExecuteJob("tenant-b", env) == nil {
		t.Fatal("mutation survived: cross-tenant job execution allowed")
	}
	if AllowRestore("tenant-b", BackupPlacement("tenant-a", "snap-m")) == nil {
		t.Fatal("mutation survived: cross-tenant restore allowed")
	}
	if ForeignBytes("marker tenant-b inside", "tenant-a", []string{"tenant-a", "tenant-b"}) == 0 {
		t.Fatal("mutation survived: foreign-byte detector blind")
	}
}
