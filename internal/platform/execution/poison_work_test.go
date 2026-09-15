package execution

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/workflow/execute"
	"github.com/monstercameron/human-capital-management-suite/internal/workflow/runtime"
)

// TestComposedPoisonWorkIsDurableAndAccountable proves the policy serve hands
// the driver (WF-RUN-007) files through the durable PostgreSQL store, names an
// accountable owner and SLA, and is accepted by execute.New.
func TestComposedPoisonWorkIsDurableAndAccountable(t *testing.T) {
	policy := composedPoisonWork()
	if _, ok := policy.Store.(runtime.QuarantineStore); !ok {
		t.Fatalf("composed store = %T, want runtime.QuarantineStore", policy.Store)
	}
	if policy.Owner != "principal:workflow-operations" || policy.SLA != 4*time.Hour {
		t.Fatalf("composed policy = %+v", policy)
	}
	if composedPoisonWork() == policy {
		t.Fatal("composedPoisonWork shares one mutable policy across compositions")
	}
	if _, err := execute.New(execute.Options{DB: stubBeginner{}, Steps: promotionStepRunner{}, PoisonWork: policy}); err != nil {
		t.Fatalf("execute.New refused the composed policy: %v", err)
	}
}
