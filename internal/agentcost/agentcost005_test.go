package agentcost

import (
	"os"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/agentmodel"
)

// The RED state of AGENTCOST-005: no lever has been costed against another.
// These tests pin the comparison of the decision models with the cost levers
// that need no new vendor, measured from AGENTCOST-001.

func TestTodo_AGENTCOST_005(t *testing.T) {
	levers := Agentcost005Levers()
	if len(levers) != 9 {
		t.Fatalf("levers = %d, want 7 without a new vendor plus 2 hosted", len(levers))
	}
	var shippable, hosted int
	for _, lever := range levers {
		if lever.Title == "" || lever.SavingBasis == "" || lever.Quality == "" || lever.Latency == "" || lever.Effort == "" || lever.Risk == "" {
			t.Fatalf("%s: saving, quality, latency, effort or risk missing", lever.ID)
		}
		if lever.EffortScore < 1 || lever.EffortScore > 5 {
			t.Fatalf("%s: effort score %d outside 1..5", lever.ID, lever.EffortScore)
		}
		if lever.VendorNeeded {
			hosted++
			if lever.Condition == "" {
				t.Fatalf("%s: a hosted lever with no adoption condition", lever.ID)
			}
		} else {
			shippable++
			if lever.MonthlySavingMicros < 0 {
				t.Fatalf("%s: negative saving", lever.ID)
			}
		}
	}
	if shippable != 7 || hosted != 2 {
		t.Fatalf("shippable %d hosted %d", shippable, hosted)
	}
	// Ranked by saving per unit of effort, best first.
	for i := 1; i < len(levers); i++ {
		if levers[i-1].SavingPerEffort < levers[i].SavingPerEffort {
			t.Fatalf("lever %s outranks %s on saving per effort", levers[i].ID, levers[i-1].ID)
		}
	}
	// The figures come from the AGENTCOST-001 seed: the in-process screen
	// saves 60 percent of scaled screening cost by construction.
	var screening int64
	for _, line := range Agentcost001SeededLines() {
		if line.Stage == agentmodel.CallStageScreening {
			screening += line.CostMicros
		}
	}
	var screen *CostLever
	for i := range levers {
		if levers[i].ID == "in-process-screen" {
			screen = &levers[i]
		}
	}
	if screen == nil {
		t.Fatal("the in-process screen is missing")
	}
	if want := screening * Agentcost005ScaleTo500() * 60 / 100; screen.MonthlySavingMicros != want {
		t.Fatalf("in-process screen saving %d, want 60 percent of scaled screening %d", screen.MonthlySavingMicros, want)
	}
	// The hosted decision models appear in the recommended order only where
	// they beat the in-deployment options: blocked today, they come last
	// with their conditions.
	order := Agentcost005RecommendedOrder()
	if len(order) != len(levers) {
		t.Fatalf("recommended order covers %d of %d levers", len(order), len(levers))
	}
	byID := map[string]CostLever{}
	for _, lever := range levers {
		byID[lever.ID] = lever
	}
	seenHosted := false
	for _, id := range order {
		lever, ok := byID[id]
		if !ok {
			t.Fatalf("recommended lever %q is not costed", id)
		}
		if lever.VendorNeeded {
			seenHosted = true
		} else if seenHosted {
			t.Fatalf("shippable lever %s is ordered after a hosted one", id)
		}
	}
}

func TestTodo_AGENTCOST_005_Golden(t *testing.T) {
	const golden = "testdata/agentcost005_levers.golden"
	got := RenderAgentcost005Record()
	if os.Getenv("AGENTCOST_GOLDEN_UPDATE") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ReplaceAll(string(want), "\r\n", "\n") != got {
		t.Fatal("lever comparison changed; review it and regenerate with AGENTCOST_GOLDEN_UPDATE=1")
	}
	for _, text := range []string{"in-process", "classifier", "caching", "batch", "routing", "trimming", "jev", "decisions api", "ranked", "saving per", "recommended order", "agentcost-001"} {
		if !strings.Contains(strings.ToLower(got), text) {
			t.Fatalf("the comparison does not cover %q", text)
		}
	}
}
