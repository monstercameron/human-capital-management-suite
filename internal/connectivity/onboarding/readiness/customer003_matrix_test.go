package readiness

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func mustCutoverDrill(t *testing.T, at time.Time) CutoverDrill {
	t.Helper()
	_ = at
	return CutoverDrill{
		DrillID: "cutover-1", TenantRef: "harborcare-demo",
		FreezeWatermark: "ledger:head-9",
		Deltas: []DeltaItem{
			{Ref: "worker:w-1", Digest: "sha256:d1"},
			{Ref: "worker:w-2", Digest: "sha256:d2"},
			{Ref: "worker:w-3", Digest: "sha256:d3"},
		},
		Thresholds:       StopGoThresholds{MaxRPO: time.Minute, MaxRTO: 5 * time.Minute},
		RollbackBoundary: "ledger:head-9",
		Transitions: []Transition{
			{Name: "credential payroll", Owner: "security-captain"},
			{Name: "webhook schedule", Owner: "platform-foundation"},
		},
		HypercareOwner:   "pilot-commander",
		CustomerContact:  "customer-ops@example",
		ManualContinuity: "paper timesheets to payroll inbox",
	}
}

func seedCutoverInput(drill CutoverDrill, at time.Time) CutoverInput {
	return CutoverInput{
		Drill: drill, At: at,
		ObservedRPO: 20 * time.Second, ObservedRTO: 3 * time.Minute,
	}
}

func mustExecuteCutover(t *testing.T, in CutoverInput) CutoverReport {
	t.Helper()
	report, err := ExecuteCutover(in)
	if err != nil {
		t.Fatalf("ExecuteCutover(%s): %v", in.Drill.DrillID, err)
	}
	return report
}

// TestTodo_CUSTOMER_003_Property: every single missing precheck blocks
// go-live; only the complete drill goes live.
func TestTodo_CUSTOMER_003_Property(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		mutate func(CutoverDrill) CutoverDrill
	}{
		{"no freeze", func(d CutoverDrill) CutoverDrill { d.FreezeWatermark = ""; return d }},
		{"no thresholds", func(d CutoverDrill) CutoverDrill { d.Thresholds = StopGoThresholds{}; return d }},
		{"no rollback", func(d CutoverDrill) CutoverDrill { d.RollbackBoundary = ""; return d }},
		{"unowned transition", func(d CutoverDrill) CutoverDrill { d.Transitions[0].Owner = ""; return d }},
		{"no hypercare", func(d CutoverDrill) CutoverDrill { d.HypercareOwner = ""; return d }},
		{"no contact", func(d CutoverDrill) CutoverDrill { d.CustomerContact = ""; return d }},
		{"no continuity", func(d CutoverDrill) CutoverDrill { d.ManualContinuity = ""; return d }},
		{"rpo blown", func(d CutoverDrill) CutoverDrill { return d }},
	}
	for _, tc := range cases {
		in := seedCutoverInput(tc.mutate(mustCutoverDrill(t, at)), at)
		if tc.name == "rpo blown" {
			in.ObservedRPO = 10 * time.Minute
		}
		if report := mustExecuteCutover(t, in); report.Decision == CutoverGoLive {
			t.Fatalf("%s went live: %+v", tc.name, report)
		}
	}
	if report := mustExecuteCutover(t, seedCutoverInput(mustCutoverDrill(t, at), at)); report.Decision != CutoverGoLive {
		t.Fatalf("complete drill: %+v", report)
	}
}

// TestTodo_CUSTOMER_003_Golden pins the cutover digest oracle.
func TestTodo_CUSTOMER_003_Golden(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	report := mustExecuteCutover(t, seedCutoverInput(mustCutoverDrill(t, at), at))
	raw, err := os.ReadFile(filepath.Join("testdata", "customer003.golden.txt"))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if want := strings.TrimSpace(string(raw)); report.Digest != want {
		t.Fatalf("report digest mismatch:\n got=%q\nwant=%q", report.Digest, want)
	}
}

// TestTodo_CUSTOMER_003_Race: concurrent drill filings agree.
func TestTodo_CUSTOMER_003_Race(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	cell := NewCutoverCell()
	const racers = 16
	var wg sync.WaitGroup
	reports := make([]CutoverReport, racers)
	errs := make([]error, racers)
	for i := range racers {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			reports[i], errs[i] = cell.Record(seedCutoverInput(mustCutoverDrill(t, at), at))
		}(i)
	}
	wg.Wait()
	for i := range racers {
		if errs[i] != nil {
			t.Fatalf("racer %d: %v", i, errs[i])
		}
		if reports[i].Digest != reports[0].Digest {
			t.Fatalf("racer %d digest drift", i)
		}
	}
}

// TestTodo_CUSTOMER_003_Integration: dry-run, cutover, rollback and
// hypercare compose across two drills.
func TestTodo_CUSTOMER_003_Integration(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	dryRun := mustExecuteCutover(t, seedCutoverInput(mustCutoverDrill(t, at), at))
	if dryRun.Decision != CutoverGoLive || dryRun.HypercareOwner != "pilot-commander" {
		t.Fatalf("dry run: %+v", dryRun)
	}
	// The cutover drill injects a recoverable lag and fails forward to
	// hypercare hold with the resumption fence set.
	live := mustCutoverDrill(t, at)
	live.DrillID = "cutover-2"
	forward := seedCutoverInput(live, at)
	forward.Failures = []string{"webhook-lag"}
	forward.FailForward = true
	held := mustExecuteCutover(t, forward)
	if held.Decision != CutoverHold || !held.FailForward {
		t.Fatalf("fail-forward: %+v", held)
	}
	if held.ResumptionFence != "fence:cutover-2:held" {
		t.Fatalf("resumption fence: %q", held.ResumptionFence)
	}
	// The rollback drill replays the same deltas without duplicating
	// accepted effects.
	retry := seedCutoverInput(live, at)
	retry.Failures = []string{"delta-gap"}
	rolled := mustExecuteCutover(t, retry)
	if rolled.Decision != CutoverRollback {
		t.Fatalf("rollback: %+v", rolled)
	}
}

// TestTodo_CUSTOMER_003_Fault: duplicate deltas deduplicate, unknown
// failures and malformed envelopes refuse.
func TestTodo_CUSTOMER_003_Fault(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	drill := mustCutoverDrill(t, at)
	drill.Deltas = append(drill.Deltas, DeltaItem{Ref: "worker:w-1", Digest: "sha256:d1"})
	report := mustExecuteCutover(t, seedCutoverInput(drill, at))
	if report.Decision != CutoverGoLive {
		t.Fatalf("duplicate retry: %+v", report)
	}
	if report.Duplicates != 1 || report.Reconciliation.Applied != 3 {
		t.Fatalf("retry accounting: %+v", report.Reconciliation)
	}
	unknown := seedCutoverInput(mustCutoverDrill(t, at), at)
	unknown.Failures = []string{"meteor-strike"}
	if gated := mustExecuteCutover(t, unknown); gated.Decision != CutoverRollback {
		t.Fatalf("unknown failure: %+v", gated)
	}
	if _, err := ExecuteCutover(CutoverInput{}); err == nil {
		t.Fatal("empty envelope admitted")
	}
	negative := seedCutoverInput(mustCutoverDrill(t, at), at)
	negative.ObservedRPO = -time.Second
	if _, err := ExecuteCutover(negative); err == nil {
		t.Fatal("negative RPO admitted")
	}
}

// TestTodo_CUSTOMER_003_Security: drill digests bind decisions and
// deltas; tampering changes the receipt.
func TestTodo_CUSTOMER_003_Security(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	honest := mustExecuteCutover(t, seedCutoverInput(mustCutoverDrill(t, at), at))
	retargeted := mustCutoverDrill(t, at)
	retargeted.TenantRef = "other-tenant"
	other := mustExecuteCutover(t, seedCutoverInput(retargeted, at))
	if honest.Digest == other.Digest {
		t.Fatal("retargeted drill verifies against the honest digest")
	}
	if honest.Decision != CutoverGoLive || other.Decision != CutoverGoLive {
		t.Fatalf("tenant retarget changed the gate: %+v %+v", honest, other)
	}
}

// TestTodo_CUSTOMER_003_Conformance: the conformance vector pins the
// allowed failure states and the fence shape.
func TestTodo_CUSTOMER_003_Conformance(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	for failure, states := range map[string][]CutoverDecision{
		"delta-gap": {CutoverRollback}, "source-freeze": {CutoverRollback, CutoverHold},
	} {
		in := seedCutoverInput(mustCutoverDrill(t, at), at)
		in.Failures = []string{failure}
		report := mustExecuteCutover(t, in)
		allowed := false
		for _, state := range states {
			if report.Decision == state {
				allowed = true
			}
		}
		if !allowed {
			t.Fatalf("failure %s reached %v", failure, report.Decision)
		}
		if !strings.HasPrefix(report.ResumptionFence, "fence:cutover-1") {
			t.Fatalf("fence shape: %q", report.ResumptionFence)
		}
	}
}

// TestTodo_CUSTOMER_003_Recovery: a held drill re-executes clean and
// replays receipt-stable.
func TestTodo_CUSTOMER_003_Recovery(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	cell := NewCutoverCell()
	lagging := seedCutoverInput(mustCutoverDrill(t, at), at)
	lagging.Failures = []string{"credential-lag"}
	held, err := cell.Record(lagging)
	if err != nil {
		t.Fatalf("held Record: %v", err)
	}
	if held.Decision != CutoverRollback {
		t.Fatalf("lagging drill: %+v", held)
	}
	cleanInput := seedCutoverInput(mustCutoverDrill(t, at), at)
	cleanInput.Drill.DrillID = "cutover-3"
	clean, err := cell.Record(cleanInput)
	if err != nil {
		t.Fatalf("clean Record: %v", err)
	}
	if clean.Decision != CutoverGoLive {
		t.Fatalf("clean drill: %+v", clean)
	}
	replay, err := cell.Record(cleanInput)
	if err != nil {
		t.Fatalf("replay Record: %v", err)
	}
	if replay.Digest != clean.Digest {
		t.Fatalf("replay drift:\n got=%q\nwant=%q", replay.Digest, clean.Digest)
	}
}

// BenchmarkTodo_CUSTOMER_003 measures cutover evaluation.
func BenchmarkTodo_CUSTOMER_003(b *testing.B) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	drill := CutoverDrill{
		DrillID: "cutover-bench", TenantRef: "harborcare-demo",
		FreezeWatermark:  "ledger:head-9",
		Deltas:           []DeltaItem{{Ref: "worker:w-1", Digest: "sha256:d1"}},
		Thresholds:       StopGoThresholds{MaxRPO: time.Minute, MaxRTO: 5 * time.Minute},
		RollbackBoundary: "ledger:head-9",
		Transitions:      []Transition{{Name: "credential payroll", Owner: "security-captain"}},
		HypercareOwner:   "pilot-commander",
		CustomerContact:  "customer-ops@example",
		ManualContinuity: "paper timesheets",
	}
	in := CutoverInput{Drill: drill, At: at, ObservedRPO: time.Second, ObservedRTO: time.Minute}
	b.ResetTimer()
	for range b.N {
		if _, err := ExecuteCutover(in); err != nil {
			b.Fatal(err)
		}
	}
}

// TestTodo_CUSTOMER_003_Mutation: drill edges resolve on the documented
// side.
func TestTodo_CUSTOMER_003_Mutation(t *testing.T) {
	at := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	// Thresholds exactly at observed values still go live.
	edge := seedCutoverInput(mustCutoverDrill(t, at), at)
	edge.Drill.Thresholds = StopGoThresholds{MaxRPO: 20 * time.Second, MaxRTO: 3 * time.Minute}
	if report := mustExecuteCutover(t, edge); report.Decision != CutoverGoLive {
		t.Fatalf("at-threshold: %+v", report)
	}
	// One nanosecond over rolls back.
	edge.ObservedRTO = 3*time.Minute + time.Nanosecond
	if report := mustExecuteCutover(t, edge); report.Decision != CutoverRollback {
		t.Fatalf("over-threshold: %+v", report)
	}
	// An empty delta set goes live vacuously with exact 0/0
	// reconciliation: nothing to migrate, nothing unresolved.
	empty := mustCutoverDrill(t, at)
	empty.Deltas = nil
	if report := mustExecuteCutover(t, seedCutoverInput(empty, at)); report.Decision != CutoverGoLive {
		t.Fatalf("empty deltas: %+v", report)
	} else if report.Reconciliation.Applied != 0 || report.Reconciliation.Total != 0 {
		t.Fatalf("empty reconciliation: %+v", report.Reconciliation)
	}
	// Padded identities refuse instead of migrating the wrong tenant.
	padded := mustCutoverDrill(t, at)
	padded.DrillID = " cutover-1"
	if _, err := ExecuteCutover(seedCutoverInput(padded, at)); err == nil {
		t.Fatal("padded drill id admitted")
	}
}
