package reliability_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/operations/reliability"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/slo"
)

func TestTodo_OPS_001_SLOContractReachable(t *testing.T) {
	if got := reliability.SLOContractVersion(); got != slo.Version() || got <= 0 {
		t.Fatalf("served SLO contract version=%d, package version=%d", got, slo.Version())
	}
}
