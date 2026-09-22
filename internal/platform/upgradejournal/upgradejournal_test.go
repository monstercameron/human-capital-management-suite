// REV-003-02 acceptance suite against the durable adapter (not the
// in-memory protocol type): every test below opens a Coordinator over a
// real journal file, and the integration/recovery/fault cases prove a
// killed process resumes from that file without duplicating history.
package upgradejournal

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/schemaupgrade"
)

func rev00302Plan() schemaupgrade.Plan {
	return schemaupgrade.Plan{
		ID:                        "rev-003-02-upgrade",
		FromVersion:               1,
		ToVersion:                 2,
		Compatibility:             schemaupgrade.CompatibilityFull,
		SourceDigest:              strings.Repeat("1", 64),
		TargetDigest:              strings.Repeat("2", 64),
		RollbackBoundary:          schemaupgrade.RollbackBeforeContract,
		RequiredAdoptionWatermark: 3,
		BackfillBatchSize:         2,
		Binaries: []schemaupgrade.Binary{
			{Name: "old", Reads: []int{1, 2}, Writes: []int{1}},
			{Name: "new", Reads: []int{1, 2}, Writes: []int{1, 2}},
		},
	}
}

func rev00302Rows() []schemaupgrade.Row {
	return []schemaupgrade.Row{{Key: "a", Digest: "da"}, {Key: "b", Digest: "db"}, {Key: "c", Digest: "dc"}}
}

func TestTodo_REV_003_02(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	coord, err := OpenCoordinator(dir+"/journal.json", func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coord.Start(rev00302Plan()); err != nil {
		t.Fatal(err)
	}
	state, err := coord.RunToCutover(rev00302Rows(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if state.Phase != schemaupgrade.PhaseCutover || !state.Checkpoint.Completed || !state.Shadow.Exact {
		t.Fatalf("phase=%s completed=%v exact=%v, want CUTOVER/completed/exact", state.Phase, state.Checkpoint.Completed, state.Shadow.Exact)
	}
	reloaded, err := coord.Load()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Phase != schemaupgrade.PhaseCutover || len(reloaded.History) != 5 {
		t.Fatalf("durable journal phase=%s history=%d, want CUTOVER and 5 events", reloaded.Phase, len(reloaded.History))
	}
}

func TestTodo_REV_003_02_Integration(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/journal.json"
	base := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	first, err := OpenCoordinator(path, func() time.Time { return base })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.Start(rev00302Plan()); err != nil {
		t.Fatal(err)
	}
	// Simulate a process kill: drop the coordinator, reopen over the same
	// journal file and resume to cutover without restarting the upgrade.
	resumed, err := OpenCoordinator(path, func() time.Time { return base.Add(time.Minute) })
	if err != nil {
		t.Fatal(err)
	}
	state, err := resumed.RunToCutover(rev00302Rows(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if state.Phase != schemaupgrade.PhaseCutover || len(state.History) != 5 {
		t.Fatalf("resumed phase=%s history=%d, want CUTOVER and 5 events", state.Phase, len(state.History))
	}
	// Re-running after completion is idempotent: no duplicate history.
	again, err := resumed.RunToCutover(rev00302Rows(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.History) != 5 || again.Phase != schemaupgrade.PhaseCutover {
		t.Fatalf("rerun phase=%s history=%d, want CUTOVER and 5 events", again.Phase, len(again.History))
	}
}

func TestTodo_REV_003_02_Recovery(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/journal.json"
	base := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	first, err := OpenCoordinator(path, func() time.Time { return base })
	if err != nil {
		t.Fatal(err)
	}
	state, err := first.Start(rev00302Plan())
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Expand(base.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := state.ResumeBackfill(rev00302Rows(), 1, base.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	// Persist a partial backfill, then "kill" the process mid-upgrade.
	raw, err := json.Marshal(state.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	killed := len(state.History)
	resumed, err := OpenCoordinator(path, func() time.Time { return base.Add(3 * time.Minute) })
	if err != nil {
		t.Fatal(err)
	}
	final, err := resumed.RunToCutover(rev00302Rows(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if !final.Checkpoint.Completed || final.Phase != schemaupgrade.PhaseCutover {
		t.Fatalf("recovered phase=%s completed=%v, want CUTOVER/completed", final.Phase, final.Checkpoint.Completed)
	}
	if len(final.History) <= killed {
		t.Fatalf("recovery appended no evidence: before=%d after=%d", killed, len(final.History))
	}
	if final.Checkpoint.Cursor != "c" {
		t.Fatalf("recovered cursor=%q, want %q", final.Checkpoint.Cursor, "c")
	}
}

func TestTodo_REV_003_02_Fault(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	// A coordinator with no journaled state refuses to run: resume requires
	// a durably recorded starting point, never a silent fresh start.
	missing, err := OpenCoordinator(dir+"/absent/journal.json", func() time.Time { return base })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := missing.RunToCutover(rev00302Rows(), 3); !errors.Is(err, schemaupgrade.ErrCheckpointMismatch) {
		t.Fatalf("missing journal error=%v, want checkpoint mismatch", err)
	}
	// A source that changes mid-backfill is refused, never re-baselined.
	path := dir + "/journal.json"
	coord, err := OpenCoordinator(path, func() time.Time { return base })
	if err != nil {
		t.Fatal(err)
	}
	state, err := coord.Start(rev00302Plan())
	if err != nil {
		t.Fatal(err)
	}
	if err := state.Expand(base.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := state.ResumeBackfill(rev00302Rows(), 1, base.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(state.Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	mutated := rev00302Rows()
	mutated[0].Digest = "changed"
	resumed, err := OpenCoordinator(path, func() time.Time { return base.Add(3 * time.Minute) })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resumed.RunToCutover(mutated, 3); !errors.Is(err, schemaupgrade.ErrCheckpointMismatch) {
		t.Fatalf("mutated source error=%v, want checkpoint mismatch", err)
	}
}

// TestTodo_REV_003_02_Edges proves the adapter's constructor and start
// guards are load bearing: an empty journal path and an invalid plan are
// refused with typed errors, a nil clock falls back to the system clock,
// and Started tells a resume from a fresh start.
func TestTodo_REV_003_02_Edges(t *testing.T) {
	if _, err := OpenCoordinator("", nil); !errors.Is(err, schemaupgrade.ErrInvalidPlan) {
		t.Fatalf("empty path error=%v, want invalid plan", err)
	}
	dir := t.TempDir()
	path := dir + "/journal.json"
	coord, err := OpenCoordinator(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if coord.Started() {
		t.Fatal("Started reports true before any journal exists")
	}
	if _, err := coord.Start(schemaupgrade.Plan{}); !errors.Is(err, schemaupgrade.ErrInvalidPlan) {
		t.Fatalf("invalid plan error=%v, want invalid plan", err)
	}
	if coord.Started() {
		t.Fatal("Started reports true after a refused start wrote nothing")
	}
	if _, err := coord.Start(rev00302Plan()); err != nil {
		t.Fatal(err)
	}
	if !coord.Started() {
		t.Fatal("Started reports false after a journaled start")
	}
}

// TestTodo_REV_003_02_ShadowRefusal proves the adapter's explicit-target
// path is load bearing: a new projection that differs from the source is
// refused with the typed shadow-mismatch error and the refusal is journaled,
// so a retried run observes the same refusal instead of a silent cutover.
func TestTodo_REV_003_02_ShadowRefusal(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/journal.json"
	base := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	coord, err := OpenCoordinator(path, func() time.Time { return base })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coord.Start(rev00302Plan()); err != nil {
		t.Fatal(err)
	}
	diverged := rev00302Rows()
	diverged[2].Digest = "other"
	if _, err := coord.RunToCutoverWithTarget(rev00302Rows(), diverged, 3); !errors.Is(err, schemaupgrade.ErrShadowMismatch) {
		t.Fatalf("diverged target error=%v, want shadow mismatch", err)
	}
	reloaded, err := coord.Load()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Phase != schemaupgrade.PhaseBackfilled {
		t.Fatalf("refused phase=%s, want BACKFILLED (no cutover on mismatch)", reloaded.Phase)
	}
}
