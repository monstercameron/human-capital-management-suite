package runtime

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func wfrun020Detector(t *testing.T) StuckDetector {
	t.Helper()
	d, err := NewStuckDetector([]StuckExpectation{
		{State: NodeWaiting, MaxIdle: 24 * time.Hour, Kinds: StuckKindsAll()},
		{State: NodeRunning, MaxIdle: time.Hour, Kinds: StuckKindsAll()},
		{State: NodeRetrying, MaxIdle: 30 * time.Minute, Kinds: StuckKindsAll(), PoisonAfter: 5},
		{State: NodeFailed, MaxIdle: 30 * time.Minute, Kinds: StuckKindsAll(), PoisonAfter: 5},
	}, 2*time.Hour)
	if err != nil {
		t.Fatalf("NewStuckDetector: %v", err)
	}
	return d
}

func wfrun020IDs() (tenant, inst uuid.UUID) {
	return uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		uuid.MustParse("22222222-2222-2222-2222-222222222222")
}

// TestTodo_WF_RUN_020_Race: the detector holds no mutable state, so
// concurrent scans over shared inputs stay race-free.
func TestTodo_WF_RUN_020_Race(t *testing.T) {
	d := wfrun020Detector(t)
	tenant, inst := wfrun020IDs()
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	obs := []NodeObservation{
		{Tenant: tenant, InstanceID: inst, NodeID: "n-wait", State: NodeWaiting,
			LastProgressAt: at.Add(-time.Hour), NextTimerAt: ptrTime(at.Add(time.Hour))},
		{Tenant: tenant, InstanceID: inst, NodeID: "n-run", State: NodeRunning,
			LastProgressAt: at.Add(-3 * time.Hour)},
	}
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				findings, err := d.Detect(at, tenant, obs)
				if err != nil {
					t.Errorf("Detect: %v", err)
					return
				}
				if len(findings) != 1 || findings[0].NodeID != "n-run" {
					t.Errorf("got %+v, want exactly the stuck n-run node", findings)
					return
				}
			}
		}()
	}
	wg.Wait()
}

// TestTodo_WF_RUN_020_Fault: corrupt scans refuse loudly; terminal and
// empty scans succeed quietly.
func TestTodo_WF_RUN_020_Fault(t *testing.T) {
	d := wfrun020Detector(t)
	tenant, inst := wfrun020IDs()
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	valid := NodeObservation{Tenant: tenant, InstanceID: inst, NodeID: "n",
		State: NodeRunning, LastProgressAt: at.Add(-time.Minute)}

	if _, err := d.Detect(time.Time{}, tenant, []NodeObservation{valid}); err == nil {
		t.Fatal("zero clock accepted")
	}
	if _, err := d.Detect(at, uuid.Nil, []NodeObservation{valid}); err == nil {
		t.Fatal("nil tenant accepted")
	}
	unknown := valid
	unknown.State = "LEVITATING"
	if _, err := d.Detect(at, tenant, []NodeObservation{unknown}); err == nil {
		t.Fatal("unknown state accepted")
	} else if CodeOf(err) != CodeStuckScanRefused {
		t.Fatalf("code=%q, want %q", CodeOf(err), CodeStuckScanRefused)
	}
	noprogress := valid
	noprogress.LastProgressAt = time.Time{}
	if _, err := d.Detect(at, tenant, []NodeObservation{noprogress}); err == nil {
		t.Fatal("missing progress timestamp accepted")
	}
	future := valid
	future.LastProgressAt = at.Add(time.Hour)
	if _, err := d.Detect(at, tenant, []NodeObservation{future}); err == nil {
		t.Fatal("future progress timestamp accepted")
	}
	noid := valid
	noid.NodeID = ""
	if _, err := d.Detect(at, tenant, []NodeObservation{noid}); err == nil {
		t.Fatal("missing node id accepted")
	}
	// Terminal nodes never strand; an all-terminal scan finds nothing.
	// (SUCCEEDED keeps one outgoing edge to COMPENSATED, so it is not
	// terminal in this state machine; SKIPPED is.)
	terminal := valid
	terminal.State = NodeSkipped
	findings, err := d.Detect(at, tenant, []NodeObservation{terminal})
	if err != nil || len(findings) != 0 {
		t.Fatalf("terminal scan: findings=%v err=%v", findings, err)
	}
	findings, err = d.Detect(at, tenant, nil)
	if err != nil || len(findings) != 0 {
		t.Fatalf("empty scan: findings=%v err=%v", findings, err)
	}
	// Construction faults refuse too.
	for _, exp := range [][]StuckExpectation{
		{{State: "LEVITATING", MaxIdle: time.Hour, Kinds: StuckKindsAll()}},
		{{State: NodeSkipped, MaxIdle: time.Hour, Kinds: StuckKindsAll()}},
		{{State: NodeRunning, MaxIdle: 0, Kinds: StuckKindsAll()}},
		{{State: NodeRunning, MaxIdle: time.Hour}},
		{{State: NodeRunning, MaxIdle: time.Hour, Kinds: []StuckKind{"VIBES"}}},
		{{State: NodeRunning, MaxIdle: time.Hour, Kinds: []StuckKind{StuckKindIdle, StuckKindIdle}}},
		{{State: NodeRunning, MaxIdle: time.Hour, Kinds: StuckKindsAll()},
			{State: NodeRunning, MaxIdle: 2 * time.Hour, Kinds: StuckKindsAll()}},
	} {
		if _, err := NewStuckDetector(exp, 2*time.Hour); err == nil {
			t.Fatalf("invalid expectations accepted: %+v", exp)
		}
	}
	if _, err := NewStuckDetector(nil, 0); err == nil {
		t.Fatal("non-positive default budget accepted")
	}
}

// TestTodo_WF_RUN_020_Security: tenant isolation is total — a cross-tenant
// observation is indistinguishable from a missing instance, and findings
// never carry another tenant's identity.
func TestTodo_WF_RUN_020_Security(t *testing.T) {
	d := wfrun020Detector(t)
	tenant, inst := wfrun020IDs()
	other := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	foreign := NodeObservation{Tenant: other, InstanceID: inst, NodeID: "n",
		State: NodeRunning, LastProgressAt: at.Add(-3 * time.Hour)}
	_, err := d.Detect(at, tenant, []NodeObservation{foreign})
	if err == nil {
		t.Fatal("cross-tenant observation scanned")
	}
	if CodeOf(err) != CodeInstanceNotFound {
		t.Fatalf("code=%q, want cross-tenant cover %q", CodeOf(err), CodeInstanceNotFound)
	}
	var target *Error
	if !errors.As(err, &target) {
		t.Fatalf("err=%T, want *runtime.Error", err)
	}
	own := NodeObservation{Tenant: tenant, InstanceID: inst, NodeID: "n",
		State: NodeRunning, LastProgressAt: at.Add(-3 * time.Hour)}
	findings, err := d.Detect(at, tenant, []NodeObservation{own})
	if err != nil || len(findings) != 1 {
		t.Fatalf("own scan: %+v %v", findings, err)
	}
	if findings[0].Tenant != tenant {
		t.Fatal("finding carries the wrong tenant")
	}
	for _, line := range findings[0].Evidence {
		if containsOtherTenant(line, other.String()) {
			t.Fatalf("evidence leaks foreign tenant: %q", line)
		}
	}
}

func containsOtherTenant(line, other string) bool {
	for i := 0; i+len(other) <= len(line); i++ {
		if line[i:i+len(other)] == other {
			return true
		}
	}
	return false
}

// TestTodo_WF_RUN_020_Mutation: boundary mutants die — idle, timer, lease
// and poison edges all resolve on the documented side.
func TestTodo_WF_RUN_020_Mutation(t *testing.T) {
	d := wfrun020Detector(t)
	tenant, inst := wfrun020IDs()
	at := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	base := NodeObservation{Tenant: tenant, InstanceID: inst, NodeID: "n", State: NodeRunning}

	// Idle exactly at budget: patient. One nanosecond past: stuck.
	exact := base
	exact.LastProgressAt = at.Add(-time.Hour)
	if f, _ := d.Detect(at, tenant, []NodeObservation{exact}); len(f) != 0 {
		t.Fatal("idle exactly at budget flagged")
	}
	over := base
	over.LastProgressAt = at.Add(-time.Hour - time.Nanosecond)
	if f, _ := d.Detect(at, tenant, []NodeObservation{over}); len(f) != 1 {
		t.Fatal("idle past budget invisible")
	}
	// Timer at the instant: fires now, not overdue. Past it: missed.
	firing := base
	firing.LastProgressAt = at.Add(-time.Minute)
	firing.NextTimerAt = ptrTime(at)
	if f, _ := d.Detect(at, tenant, []NodeObservation{firing}); len(f) != 0 {
		t.Fatal("timer at the instant flagged overdue")
	}
	late := firing
	late.NextTimerAt = ptrTime(at.Add(-time.Nanosecond))
	if f, _ := d.Detect(at, tenant, []NodeObservation{late}); len(f) != 1 {
		t.Fatal("overdue timer invisible")
	}
	// Lease at the instant is already gone.
	lapsed := base
	lapsed.LastProgressAt = at.Add(-time.Minute)
	lapsed.LeaseExpiresAt = ptrTime(at)
	if f, _ := d.Detect(at, tenant, []NodeObservation{lapsed}); len(f) != 1 {
		t.Fatal("lapsed lease invisible")
	}
	// Poison threshold is inclusive; one below is merely stuck.
	almost := base
	almost.State = NodeRetrying
	almost.LastProgressAt = at.Add(-time.Hour)
	almost.NextRetryAt = ptrTime(at.Add(-time.Hour))
	almost.Attempts = 4
	f, _ := d.Detect(at, tenant, []NodeObservation{almost})
	if len(f) != 1 || f[0].Poisoned {
		t.Fatalf("attempts=4: %+v, want stuck but not poisoned", f)
	}
	almost.Attempts = 5
	f, _ = d.Detect(at, tenant, []NodeObservation{almost})
	if len(f) != 1 || !f[0].Poisoned {
		t.Fatalf("attempts=5: %+v, want poisoned", f)
	}
	// Incident keys are deterministic across scans for open/link dedupe.
	again, _ := d.Detect(at, tenant, []NodeObservation{almost})
	if f[0].IncidentKey == "" || f[0].IncidentKey != again[0].IncidentKey {
		t.Fatal("incident key unstable across identical scans")
	}
	// States without an entry use the default budget with every kind.
	ready := base
	ready.State = NodeReady
	ready.LastProgressAt = at.Add(-3 * time.Hour)
	if f, _ := d.Detect(at, tenant, []NodeObservation{ready}); len(f) != 1 {
		t.Fatal("default-expectation state invisible past default budget")
	}
}
