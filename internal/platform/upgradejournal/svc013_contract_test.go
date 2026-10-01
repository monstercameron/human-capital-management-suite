package upgradejournal

import (
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/platform/schemaupgrade"
)

func TestTodo_SVC_013_Contract(t *testing.T) {
	path := t.TempDir() + "\\journal.json"
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	coord, err := OpenCoordinator(path, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := coord.Start(rev00302Plan()); err != nil {
		t.Fatal(err)
	}
	state, err := coord.RunToContract(rev00302Rows(), 3)
	if err != nil {
		t.Fatal(err)
	}
	if state.Phase != schemaupgrade.PhaseContracted {
		t.Fatalf("phase=%s, want CONTRACTED", state.Phase)
	}
	reloaded, err := coord.Load()
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Phase != schemaupgrade.PhaseContracted || len(reloaded.History) != 6 {
		t.Fatalf("reloaded phase=%s history=%d, want CONTRACTED/6", reloaded.Phase, len(reloaded.History))
	}
}
