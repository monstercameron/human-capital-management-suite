package intent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/engines/population"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// rev038ConsumerFiles are the exact production population-consumer call
// sites the DISPUTED triage flagged [mechanism-to-confirm], corrected at
// build time: the three production membership consumers must route snapshots
// through popscale, and none may call population.Resolve directly. Paths are
// relative to this package.
var rev038ConsumerRoutes = map[string]string{
	"batch.go": "popscaleSubjects(",
	filepath.Join("..", "domains", "crm", "campaign.go"):             "popscale.NewSession(",
	filepath.Join("..", "engines", "search", "population_action.go"): "popscale.NewSession(",
}

// TestTodo_REV_038_02_Callsites pins the corrected consumer mechanism at
// build time: none of the three production population consumers may call
// population.Resolve directly, and the batch compiler must resolve through
// popscale's paginated path. If another change reintroduces a direct
// unbounded Resolve call or removes the popscale routing, this test fails
// the build with the exact file.
func TestTodo_REV_038_02_Callsites(t *testing.T) {
	for file, route := range rev038ConsumerRoutes {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read consumer %s: %v", file, err)
		}
		if strings.Contains(string(data), "population.Resolve(") {
			t.Errorf("consumer %s calls population.Resolve directly: route large populations through popscale", file)
		}
		if !strings.Contains(string(data), route) {
			t.Errorf("consumer %s does not route population membership through %s", file, route)
		}
	}
}

func rev038LargeSnapshot(t *testing.T, n int) population.Snapshot {
	t.Helper()
	ids := make([]string, 0, n+2)
	for i := 0; i < n; i++ {
		ids = append(ids, fmt.Sprintf("s-%04d", i))
	}
	ids = append(ids, "denied:s-denied", "unknown:s-unknown")
	return population.Snapshot{
		DefinitionID:     "change-request/bulk",
		DefinitionDigest: "sha256:def",
		RevisionVersion:  "2026.1",
		SubjectIDs:       ids,
		Digest:           "sha256:frozen-large",
	}
}

func rev038LargeSpec(snapshot population.Snapshot) BatchSpec {
	return BatchSpec{
		DefinitionID:      "change-request/bulk",
		PopulationScope:   "population/acme",
		Snapshot:          snapshot,
		OperationTemplate: "leave-accrue",
		OperationVersion:  "v7",
		ChildFamily:       "change-request",
		PartitionSize:     25,
		Limits:            BatchLimits{MaxSubjects: 10000, MaxCost: 1000000, MaxRate: 50, CostPerChild: 3},
		CompletionPolicy:  "all-or-repair",
	}
}

// TestTodo_REV_038_02 proves the popscale-routed compiler preserves the
// batch contract over a representative large fixture: every subject appears
// in exactly one child, denied/unknown members are counted but never named,
// and the compile is deterministic.
func TestTodo_REV_038_02(t *testing.T) {
	spec := rev038LargeSpec(rev038LargeSnapshot(t, 500))
	first, err := CompileBatch(spec)
	if err != nil {
		t.Fatalf("CompileBatch: %v", err)
	}
	second, err := CompileBatch(spec)
	if err != nil {
		t.Fatalf("CompileBatch: %v", err)
	}
	if first.Digest != second.Digest {
		t.Fatal("popscale-routed compile is not deterministic")
	}
	if first.Counts.Eligible != 500 || len(first.Children) != 500 {
		t.Fatalf("eligible = %d children = %d, want 500 named children: %+v", first.Counts.Eligible, len(first.Children), first.Counts)
	}
	if first.Counts.Denied != 1 || first.Counts.Unknown != 1 {
		t.Fatalf("denied/unknown = %d/%d, want 1/1 counted but unnamed: %+v", first.Counts.Denied, first.Counts.Unknown, first.Counts)
	}
	seen := make(map[string]bool, len(first.Children))
	for _, child := range first.Children {
		if strings.HasPrefix(child.SubjectID, "denied:") || strings.HasPrefix(child.SubjectID, "unknown:") {
			t.Fatalf("denied/unknown member named as child: %s", child.SubjectID)
		}
		if seen[child.SubjectID] {
			t.Fatalf("subject %s compiled twice", child.SubjectID)
		}
		seen[child.SubjectID] = true
	}
}

// TestTodo_REV_038_02_Security proves the routed compiler fails closed: a
// membership-protected snapshot is refused outright, and a snapshot whose
// recorded count contradicts its membership is refused instead of served.
func TestTodo_REV_038_02_Security(t *testing.T) {
	protected := rev038LargeSnapshot(t, 10)
	protected.MembershipProtected = true
	if _, err := CompileBatch(rev038LargeSpec(protected)); err == nil {
		t.Fatal("CompileBatch accepted a membership-protected snapshot")
	}
	inconsistent := rev038LargeSnapshot(t, 10)
	inconsistent.Count = values.Value(4)
	if _, err := CompileBatch(rev038LargeSpec(inconsistent)); err == nil {
		t.Fatal("CompileBatch served a snapshot whose count contradicts its membership")
	}
}

// BenchmarkTodo_REV_038_02 compiles a representative 2000-subject batch
// through the popscale-routed path, pinning the ceiling the rollout proof
// declares.
func BenchmarkTodo_REV_038_02(b *testing.B) {
	snapshot := population.Snapshot{
		DefinitionID:     "change-request/bulk",
		DefinitionDigest: "sha256:def",
		RevisionVersion:  "2026.1",
		SubjectIDs:       make([]string, 0, 2000),
		Digest:           "sha256:frozen-bench",
	}
	for i := 0; i < 2000; i++ {
		snapshot.SubjectIDs = append(snapshot.SubjectIDs, fmt.Sprintf("bench-%04d", i))
	}
	spec := rev038LargeSpec(snapshot)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := CompileBatch(spec); err != nil {
			b.Fatalf("CompileBatch: %v", err)
		}
	}
}
