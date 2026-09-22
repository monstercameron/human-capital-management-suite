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
	if err != nil || len(first) != 1 {
		t.Fatalf("PublishReferenceVersions = %+v, %v", first, err)
	}
	hire := first[0]
	if hire.WorkflowID != hireexec.WorkflowID || hire.SemanticVersion != hireexec.SemanticVersion || hire.Status != version.StatusDraft {
		t.Fatalf("published %s %s as %s", hire.WorkflowID, hire.SemanticVersion, hire.Status)
	}
	if len(hire.FixtureRefs) != 1 || hire.FixtureRefs[0] != FixtureHireCompile {
		t.Fatalf("fixtures = %v", hire.FixtureRefs)
	}
	if err := ShippedFixtures()[FixtureHireCompile](hire); err != nil {
		t.Fatalf("the published plan does not reproduce: %v", err)
	}
	again, err := PublishReferenceVersions(store, releaseAt)
	if err != nil || again[0].CompiledPlanDigest != hire.CompiledPlanDigest {
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
