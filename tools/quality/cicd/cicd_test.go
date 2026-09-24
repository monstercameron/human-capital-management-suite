package cicd_test

import (
	"reflect"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/quality/cicd"
)

func TestTodo_CICD_001(t *testing.T) {
	if got := cicd.Check(cicd.CleanPipeline()); len(got) != 0 {
		t.Fatalf("clean pipeline rejected: %v", got)
	}
	bad := cicd.CleanPipeline()
	bad.Commands = bad.Commands[:len(bad.Commands)-1]
	bad.DeferredPackages = []string{"internal/workflow", "internal/transaction"}
	bad.LegacyNodeAuthoritative = true
	if got := cicd.Check(bad); len(got) == 0 {
		t.Fatal("incomplete pipeline accepted")
	}
}

// TestTodo_CICD_001_Golden pins the exact command matrix and stable finding
// codes used in CI annotations.
func TestTodo_CICD_001_Golden(t *testing.T) {
	want := []string{"cmd/hcmnext", "cmd/worker", "cmd/projector", "cmd/migrate", "cmd/hcmctl", "cmd/scheduler", "cmd/frontenddev"}
	if !reflect.DeepEqual(cicd.RequiredCommands, want) {
		t.Fatalf("required command matrix = %v, want %v", cicd.RequiredCommands, want)
	}
	p := cicd.CleanPipeline()
	p.Commands = []string{"cmd/hcmnext", "cmd/hcmnext", "cmd/unknown"}
	got := cicd.Check(p)
	wantCodes := []string{"duplicate-command", "missing-command", "missing-command", "missing-command", "missing-command", "missing-command", "missing-command", "unknown-command"}
	if len(got) != len(wantCodes) {
		t.Fatalf("findings = %v, want codes %v", got, wantCodes)
	}
	for i, finding := range got {
		if finding.Code != wantCodes[i] {
			t.Fatalf("finding %d code = %q, want %q", i, finding.Code, wantCodes[i])
		}
	}
}

// TestTodo_CICD_001_Property checks that every required command is necessary:
// removing any one entry from an otherwise clean matrix must fail.
func TestTodo_CICD_001_Property(t *testing.T) {
	for i, command := range cicd.RequiredCommands {
		p := cicd.CleanPipeline()
		p.Commands = append([]string(nil), p.Commands[:i]...)
		p.Commands = append(p.Commands, cicd.RequiredCommands[i+1:]...)
		findings := cicd.Check(p)
		found := false
		for _, finding := range findings {
			if finding.Code == "missing-command" && finding.Detail == command {
				found = true
			}
		}
		if !found {
			t.Errorf("removing %s did not produce its missing-command finding: %v", command, findings)
		}
	}
}

// TestTodo_CICD_001_Race exercises the checker concurrently. Check is pure and
// must not share mutable state between parallel verification jobs.
func TestTodo_CICD_001_Race(t *testing.T) {
	const workers = 32
	start := make(chan struct{})
	results := make(chan []cicd.Finding, workers)
	for i := 0; i < workers; i++ {
		go func() {
			<-start
			results <- cicd.Check(cicd.CleanPipeline())
		}()
	}
	close(start)
	for i := 0; i < workers; i++ {
		if got := <-results; len(got) != 0 {
			t.Errorf("concurrent clean pipeline rejected: %v", got)
		}
	}
}
