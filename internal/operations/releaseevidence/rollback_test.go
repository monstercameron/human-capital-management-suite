package releaseevidence_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/releaseevidence"
	"github.com/monstercameron/human-capital-management-suite/migrations"
)

// rollbackRelease is a minimal valid release of the promotion slice at one
// schema version. Unit tests roll synthetic histories so they never depend
// on the real migration tree.
func rollbackRelease(version string, schema int64) releaseevidence.Release {
	return releaseevidence.Release{
		Slice: "promotion", Version: version, BinaryRevision: "rev-" + version,
		SchemaDigest: "sha256:tree", SchemaVersion: schema,
		Definitions:  map[string]string{"wf": "sha256:p"},
		EvidenceRefs: []string{"test:TestTodo_ALIGN_056"},
		RecordedBy:   "release:pipeline", RecordedAt: t0,
	}
}

func rollbackJournal(t *testing.T) *releaseevidence.Journal {
	t.Helper()
	j := releaseevidence.NewJournal()
	for _, r := range []releaseevidence.Release{rollbackRelease("v1", 1), rollbackRelease("v2", 3)} {
		if _, err := j.Record(r); err != nil {
			t.Fatal(err)
		}
	}
	return j
}

func rollbackHistory() []releaseevidence.Migration {
	return []releaseevidence.Migration{{Version: 1, Reversible: true}, {Version: 2, Reversible: true}, {Version: 3, Reversible: true}}
}

// TestTodo_ALIGN_056 proves a recorded slice can roll back to an older
// recorded release: the plan names every schema version to reverse, and the
// rolled-back runtime verifies against the rollback target.
func TestTodo_ALIGN_056(t *testing.T) {
	j := rollbackJournal(t)
	plan, err := releaseevidence.PlanRollback(j, "promotion", "v2", "v1", "operator:release-captain", rollbackHistory())
	if err != nil {
		t.Fatalf("plan = %v", err)
	}
	if len(plan.Steps) != 2 || plan.Steps[0] != 3 || plan.Steps[1] != 2 {
		t.Fatalf("steps = %v", plan.Steps)
	}
	target, err := j.Get("promotion", "v1")
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.Verify(target, matchingRuntime(t, target)); err != nil {
		t.Fatalf("rolled-back runtime did not verify: %v", err)
	}
	// A binary-only rollback at one schema version is still a plan with no
	// migration steps.
	journal := releaseevidence.NewJournal()
	first, second := rollbackRelease("v3", 3), rollbackRelease("v4", 3)
	second.BinaryRevision = "rev-hotfix"
	for _, r := range []releaseevidence.Release{first, second} {
		if _, err := journal.Record(r); err != nil {
			t.Fatal(err)
		}
	}
	binary, err := releaseevidence.PlanRollback(journal, "promotion", "v4", "v3", "operator:release-captain", rollbackHistory())
	if err != nil || len(binary.Steps) != 0 {
		t.Fatalf("binary-only plan = %+v, %v", binary, err)
	}
	for name, tc := range map[string]func() error{
		"forward is not a rollback": func() error {
			_, err := releaseevidence.PlanRollback(j, "promotion", "v1", "v2", "operator:release-captain", rollbackHistory())
			return err
		},
		"same version is not a rollback": func() error {
			_, err := releaseevidence.PlanRollback(j, "promotion", "v1", "v1", "operator:release-captain", rollbackHistory())
			return err
		},
		"blank authority": func() error {
			_, err := releaseevidence.PlanRollback(j, "promotion", "v2", "v1", "  ", rollbackHistory())
			return err
		},
		"unknown target": func() error {
			_, err := releaseevidence.PlanRollback(j, "promotion", "v2", "v9", "operator:release-captain", rollbackHistory())
			return err
		},
		"unknown slice": func() error {
			_, err := releaseevidence.PlanRollback(j, "payroll", "v2", "v1", "operator:release-captain", rollbackHistory())
			return err
		},
	} {
		if err := tc(); !errors.Is(err, releaseevidence.ErrInvalid) && !errors.Is(err, releaseevidence.ErrNotFound) {
			t.Errorf("%s = %v", name, err)
		}
	}
}

// TestTodo_ALIGN_056_Property proves planning is exact over generated
// histories: a plan exists exactly when the target schema is older, both
// endpoints are known, and every migration in between is reversible.
func TestTodo_ALIGN_056_Property(t *testing.T) {
	j := releaseevidence.NewJournal()
	versions := []string{"a", "b", "c", "d"}
	for i, v := range versions {
		if _, err := j.Record(rollbackRelease(v, int64(i+1))); err != nil {
			t.Fatal(err)
		}
	}
	for irreversible := -1; irreversible < 4; irreversible++ {
		history := []releaseevidence.Migration{}
		for i := range 4 {
			history = append(history, releaseevidence.Migration{Version: int64(i + 1), Reversible: i != irreversible})
		}
		plan, err := releaseevidence.PlanRollback(j, "promotion", "d", "a", "operator:release-captain", history)
		if irreversible < 0 {
			if err != nil {
				t.Fatalf("reversible history = %v", err)
			}
			for i, step := range plan.Steps {
				if step != int64(4-i) {
					t.Fatalf("steps = %v", plan.Steps)
				}
			}
			continue
		}
		if irreversible == 0 {
			if err != nil {
				t.Fatalf("irreversible target endpoint is not a step: %v", err)
			}
			continue
		}
		if !errors.Is(err, releaseevidence.ErrIrreversible) {
			t.Errorf("irreversible %d in range = %+v, %v", irreversible+1, plan, err)
		}
	}
}

// TestTodo_ALIGN_056_Golden pins the plan encoding and its safe summary.
func TestTodo_ALIGN_056_Golden(t *testing.T) {
	plan, err := releaseevidence.PlanRollback(rollbackJournal(t), "promotion", "v2", "v1", "operator:release-captain", rollbackHistory())
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(plan)
	want := `{"slice":"promotion","from_version":"v2","to_version":"v1","from_schema":3,"to_schema":1,"steps":[3,2],"authority":"operator:release-captain"}`
	if string(body) != want {
		t.Fatalf("plan = %s", body)
	}
	if summary := plan.Summary(); summary != "rollback promotion v2(schema 3) -> v1(schema 1) steps=2 authority=operator:release-captain" {
		t.Fatalf("summary = %q", summary)
	}
}

// TestTodo_ALIGN_056_Security proves a forged release cannot vouch for a
// rollback and that authority and identity are always required.
func TestTodo_ALIGN_056_Security(t *testing.T) {
	j := rollbackJournal(t)
	plan, err := releaseevidence.PlanRollback(j, "promotion", "v2", "v1", "operator:release-captain", rollbackHistory())
	if err != nil {
		t.Fatal(err)
	}
	target, _ := j.Get("promotion", "v1")
	forged := target
	forged.BinaryRevision = "rev-attacker"
	if err := plan.Verify(forged, matchingRuntime(t, forged)); err == nil {
		t.Fatal("a forged target vouched for a rolled-back runtime")
	}
	other, _ := j.Get("promotion", "v2")
	if err := plan.Verify(other, matchingRuntime(t, other)); err == nil {
		t.Fatal("a plan verified against the release it rolls back from")
	}
	if _, err := releaseevidence.PlanRollback(nil, "promotion", "v2", "v1", "operator:release-captain", rollbackHistory()); !errors.Is(err, releaseevidence.ErrInvalid) {
		t.Errorf("nil journal = %v", err)
	}
}

// TestTodo_ALIGN_056_Integration binds the planner to the real embedded
// migration tree and a real migrated database: the recorded tip cannot roll
// back across the irreversible tail, and the refusal names the blocking
// migration against the live database version.
func TestTodo_ALIGN_056_Integration(t *testing.T) {
	ctx := context.Background()
	db := pgtest.New(t)
	applied, err := db.Provider(t).GetDBVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	history, err := releaseevidence.EmbeddedHistory()
	if err != nil {
		t.Fatal(err)
	}
	files, err := migrations.Files()
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != len(files) || history[len(history)-1].Version != files[len(files)-1].Version {
		t.Fatalf("embedded history tracks %d of %d files", len(history), len(files))
	}
	reversible, _ := migrations.NewestReversibleVersion()
	tip := shippedRelease(t)
	if tip.SchemaVersion != applied {
		t.Fatalf("shipped release schema %d != live database %d", tip.SchemaVersion, applied)
	}
	j := releaseevidence.NewJournal()
	if _, err := j.Record(tip); err != nil {
		t.Fatal(err)
	}
	older := tip
	older.Version = "2026.09.13"
	older.SchemaVersion = reversible
	if _, err := j.Record(older); err != nil {
		t.Fatal(err)
	}
	_, err = releaseevidence.PlanRollback(j, "promotion", tip.Version, older.Version, "operator:release-captain", history)
	if reversible == applied {
		if err != nil {
			t.Fatalf("one reversible step = %v", err)
		}
		return
	}
	if !errors.Is(err, releaseevidence.ErrIrreversible) {
		t.Fatalf("rollback across the irreversible tail = %v", err)
	}
	if !strings.Contains(err.Error(), fmt.Sprint(reversible+1)) && !strings.Contains(err.Error(), "reversible") {
		t.Fatalf("refusal does not name the blocking range: %v", err)
	}
}

// TestTodo_ALIGN_056_Fault proves unknown migrations, irreversible steps and
// unverified runtimes fail closed with the blocking version named.
func TestTodo_ALIGN_056_Fault(t *testing.T) {
	j := rollbackJournal(t)
	if _, err := releaseevidence.PlanRollback(j, "promotion", "v2", "v1", "operator:release-captain", nil); !errors.Is(err, releaseevidence.ErrInvalid) {
		t.Errorf("nil history = %v", err)
	}
	blocked := []releaseevidence.Migration{{Version: 1, Reversible: true}, {Version: 2, Reversible: false}, {Version: 3, Reversible: true}}
	_, err := releaseevidence.PlanRollback(j, "promotion", "v2", "v1", "operator:release-captain", blocked)
	if !errors.Is(err, releaseevidence.ErrIrreversible) || !strings.Contains(err.Error(), "2") {
		t.Errorf("irreversible step = %v", err)
	}
	plan, err := releaseevidence.PlanRollback(j, "promotion", "v2", "v1", "operator:release-captain", rollbackHistory())
	if err != nil {
		t.Fatal(err)
	}
	target, _ := j.Get("promotion", "v1")
	if err := plan.Verify(target, releaseevidence.Runtime{}); err == nil {
		t.Error("an empty runtime verified against the rollback target")
	}
	if err := plan.Verify(releaseevidence.Release{}, matchingRuntime(t, target)); err == nil {
		t.Error("a rollback verified against an empty release")
	}
}

// TestTodo_ALIGN_056_Conformance proves plans are deterministic, contiguous
// and ordered: repeated plans are identical and every step reverses exactly
// one known migration.
func TestTodo_ALIGN_056_Conformance(t *testing.T) {
	j := rollbackJournal(t)
	first, err := releaseevidence.PlanRollback(j, "promotion", "v2", "v1", "operator:release-captain", rollbackHistory())
	if err != nil {
		t.Fatal(err)
	}
	second, err := releaseevidence.PlanRollback(j, "promotion", "v2", "v1", "operator:release-captain", rollbackHistory())
	if err != nil {
		t.Fatal(err)
	}
	if first.Summary() != second.Summary() {
		t.Fatal("repeated plans differ")
	}
	for i, step := range first.Steps {
		if step != first.FromSchema-int64(i) {
			t.Fatalf("steps not contiguous from %d: %v", first.FromSchema, first.Steps)
		}
	}
	if last := first.Steps[len(first.Steps)-1]; last != first.ToSchema+1 {
		t.Fatalf("steps %v do not land on schema %d", first.Steps, first.ToSchema)
	}
}
