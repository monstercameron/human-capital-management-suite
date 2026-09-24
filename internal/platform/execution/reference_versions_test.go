package execution

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/hireexec"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/version"
)

// TestTodo_WF_HIRE_001_Published proves New employee hire is published as a
// reference workflow, idempotently, with a fixture that reproduces its plan,
// and that the shipped set a standard deployment publishes does not include
// a workflow nothing serves.
func TestTodo_WF_HIRE_001_Published(t *testing.T) {
	store := version.NewRegistry()
	first, err := PublishReferenceVersions(store, releaseAt)
	if err != nil || len(first) != 2 {
		t.Fatalf("PublishReferenceVersions = %+v, %v", first, err)
	}
	fixtures := ShippedFixtures()
	wantVersions := []string{hireexec.SemanticVersionV1_0, hireexec.SemanticVersion}
	wantFixtures := []string{FixtureHireCompile, FixtureHireCompileV1_1}
	for i, hire := range first {
		if hire.WorkflowID != hireexec.WorkflowID || hire.SemanticVersion != wantVersions[i] || hire.Status != version.StatusDraft {
			t.Fatalf("published %s %s as %s", hire.WorkflowID, hire.SemanticVersion, hire.Status)
		}
		if len(hire.FixtureRefs) != 1 || hire.FixtureRefs[0] != wantFixtures[i] {
			t.Fatalf("%s fixtures = %v, want [%s]", hire.SemanticVersion, hire.FixtureRefs, wantFixtures[i])
		}
		fixture := fixtures[wantFixtures[i]]
		if fixture == nil {
			t.Fatalf("fixture %q is not registered", wantFixtures[i])
		}
		if err := fixture(hire); err != nil {
			t.Fatalf("the %s plan does not reproduce: %v", hire.SemanticVersion, err)
		}
	}
	again, err := PublishReferenceVersions(store, releaseAt)
	if err != nil || len(again) != len(first) || again[0].CompiledPlanDigest != first[0].CompiledPlanDigest || again[1].CompiledPlanDigest != first[1].CompiledPlanDigest {
		t.Fatalf("second publication = %+v, %v; want the same version", again, err)
	}
	shipped, err := PublishShippedVersions(version.NewRegistry(), releaseAt)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range shipped {
		if v.WorkflowID == hireexec.WorkflowID {
			t.Fatal("a standard deployment publishes a workflow nothing serves")
		}
	}
}
