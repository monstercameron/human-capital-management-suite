package scenario

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func compareAssumption(key, text string, kind ValueKind, unit string, refs ...string) Assumption {
	var value TypedValue
	switch kind {
	case ValueDecimal:
		scale := int32(0)
		if i := strings.IndexByte(text, '.'); i >= 0 {
			scale = int32(len(text) - i - 1)
		}
		value = DecimalValue(values.MustDecimal(text, scale, values.RoundingHalfEven))
	case ValueBoolean:
		value = BooleanValue(text == "true")
	default:
		value = TextValue(text)
	}
	return Assumption{Key: key, Value: value, Unit: unit, ProvenanceRefs: refs}
}

func compareBase(t *testing.T) ScenarioRevision {
	t.Helper()
	s, err := NewScenarioRevision(ScenarioRevision{
		ScenarioID: "scenario-1", Revision: 1, Owner: "workforce-planning", Scope: "north-america",
		Horizon: scenarioHorizon(t), BaselineSnapshotRef: "snapshot:2026-01", Author: "planner-1",
		AuthorityDisclaimer: "simulation only; does not mutate authoritative facts", Lifecycle: LifecycleDraft,
		Assumptions: []Assumption{
			compareAssumption("headcount.target", "12", ValueDecimal, "HEAD", "forecast:2026"),
			compareAssumption("attrition.rate", "0.05", ValueDecimal, "RATE", "benchmark:2025"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestTodo_SCENARIO_004: comparison normalizes baseline/horizon/units,
// partitions assumption differences and traces each metric to the exact
// provenance behind it. Incomparable revisions return a typed error,
// never a partial diff.
func TestTodo_SCENARIO_004(t *testing.T) {
	base := compareBase(t)
	child, err := base.Fork(
		compareAssumption("headcount.target", "14", ValueDecimal, "head", "plan:change-1"),
		compareAssumption("hiring.freeze", "true", ValueBoolean, "FLAG", "policy:2026-03"),
	)
	if err != nil {
		t.Fatal(err)
	}
	comparison, err := Compare(base, child)
	if err != nil {
		t.Fatal(err)
	}
	byKey := make(map[string]AssumptionDiff, len(comparison.Diffs))
	for _, diff := range comparison.Diffs {
		byKey[diff.Key] = diff
	}
	// headcount.target changed value (unit differs only by normalization).
	changed, ok := byKey["headcount.target"]
	if !ok || changed.Partition != PartitionChanged {
		t.Fatalf("headcount.target diff = %+v", changed)
	}
	if !changed.HasBaseline || !changed.HasCompared {
		t.Fatal("changed diff must carry both sides")
	}
	if len(changed.BaselineProvenance) != 1 || changed.BaselineProvenance[0] != "forecast:2026" {
		t.Fatalf("baseline provenance = %v", changed.BaselineProvenance)
	}
	if len(changed.ComparedProvenance) != 1 || changed.ComparedProvenance[0] != "plan:change-1" {
		t.Fatalf("compared provenance = %v", changed.ComparedProvenance)
	}
	// hiring.freeze is new; attrition.rate is untouched.
	if added, ok := byKey["hiring.freeze"]; !ok || added.Partition != PartitionAdded || added.HasBaseline || !added.HasCompared {
		t.Fatalf("hiring.freeze diff = %+v", added)
	}
	if same, ok := byKey["attrition.rate"]; !ok || same.Partition != PartitionUnchanged {
		t.Fatalf("attrition.rate diff = %+v", same)
	}
	if len(comparison.Diffs) != 3 {
		t.Fatalf("diff count = %d", len(comparison.Diffs))
	}
	if comparison.ScenarioID != "scenario-1" || comparison.BaselineSnapshotRef != "snapshot:2026-01" ||
		comparison.Horizon == "" || comparison.Scope != "north-america" || comparison.CanonicalDigest == "" {
		t.Fatalf("comparison is not fully bound: %+v", comparison)
	}
	if comparison.BaselineRevision != 1 || comparison.ComparedRevision != 2 {
		t.Fatalf("comparison revisions = %d/%d", comparison.BaselineRevision, comparison.ComparedRevision)
	}
	if err := comparison.Validate(); err != nil {
		t.Fatal(err)
	}

	// Seeded defect: a removed assumption must partition REMOVED, not vanish.
	pruned, err := NewScenarioRevision(ScenarioRevision{
		ScenarioID: "scenario-1", Revision: 2, ParentRevision: 1, ParentDigest: base.CanonicalDigest,
		Owner: "workforce-planning", Scope: "north-america", Horizon: scenarioHorizon(t),
		BaselineSnapshotRef: "snapshot:2026-01", Author: "planner-1",
		AuthorityDisclaimer: "simulation only; does not mutate authoritative facts", Lifecycle: LifecycleDraft,
		Assumptions: []Assumption{
			compareAssumption("headcount.target", "12", ValueDecimal, "HEAD", "forecast:2026"),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	removed, err := Compare(base, pruned)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, diff := range removed.Diffs {
		if diff.Key == "attrition.rate" {
			found = true
			if diff.Partition != PartitionRemoved || !diff.HasBaseline || diff.HasCompared {
				t.Fatalf("attrition.rate diff = %+v", diff)
			}
		}
	}
	if !found {
		t.Fatal("removed assumption is missing from the diff")
	}

	// GREEN clause: incomparable versions return a typed error.
	otherID := base
	otherID.ScenarioID = "scenario-2"
	otherID.CanonicalDigest = ""
	if _, err := Compare(base, otherID); !errors.Is(err, ErrIncomparable) {
		t.Fatalf("scenario id mismatch error = %v", err)
	}
	otherBaseline := base
	otherBaseline.BaselineSnapshotRef = "snapshot:2026-02"
	otherBaseline.CanonicalDigest = ""
	if _, err := Compare(base, otherBaseline); !errors.Is(err, ErrIncomparable) {
		t.Fatalf("baseline mismatch error = %v", err)
	}
	later := base
	start, err := values.NewLocalDate(2026, time.March, 1)
	if err != nil {
		t.Fatal(err)
	}
	end, err := values.NewLocalDate(2026, time.April, 1)
	if err != nil {
		t.Fatal(err)
	}
	later.Horizon, err = values.NewLocalDateInterval(start, end, values.CalendarRef{Ref: "us-federal", Version: "2026"})
	if err != nil {
		t.Fatal(err)
	}
	later.CanonicalDigest = ""
	if _, err := Compare(base, later); !errors.Is(err, ErrIncomparable) {
		t.Fatalf("horizon mismatch error = %v", err)
	}
}
