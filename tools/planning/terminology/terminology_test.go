package terminology

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCanonicalTerms is the primary red/green test for GOV-012.
func TestCanonicalTerms(t *testing.T) {
	t.Run("Workforce Access acting as Platform IAM is rejected", func(t *testing.T) {
		v := CheckCanonicalTerms("Workforce Access authenticates every Human Capital Management Suite operator session.")
		assertHasRule(t, v, "Platform IAM confused with Workforce Access")
	})
	t.Run("Platform IAM acting as Workforce Access is rejected", func(t *testing.T) {
		v := CheckCanonicalTerms("Platform IAM provisions the customer-worker accounts for the pilot.")
		assertHasRule(t, v, "Platform IAM confused with Workforce Access")
	})
	t.Run("Candidate used as an exclusive Person state is rejected", func(t *testing.T) {
		v := CheckCanonicalTerms("A person is either a Candidate or a Worker, never both.")
		assertHasRule(t, v, "Candidate used as an exclusive Person state")
	})
	t.Run("external observation labeled a domain fact is rejected", func(t *testing.T) {
		v := CheckCanonicalTerms("The connector's external observation is recorded as domain fact once ingested.")
		assertHasRule(t, v, "external observation labeled a domain fact")
	})

	t.Run("the correct distinguishing prose is not rejected", func(t *testing.T) {
		correct := []string{
			"PLATFORM IAM authenticates and authorizes Human Capital Management Suite users, services, agents, operators, workloads, sessions, and production administration.",
			"WORKFORCE ACCESS PRODUCT governs customer-worker accounts, applications, devices, entitlements, provisioning, deprovisioning, and access reviews.",
			"`Candidate`, `Worker`, and `Former Worker` are not mutually exclusive Person states.",
			"Ledger events classify their assertion authority as transaction fact, domain fact, external observation, claim, or correction.",
			"transactions and external observations rather than claiming the observed amount as its own domain fact.",
		}
		for _, line := range correct {
			if v := CheckCanonicalTerms(line); len(v) != 0 {
				t.Errorf("correct prose incorrectly flagged: %q -> %v", line, v)
			}
		}
	})

	t.Run("a checker's own RED/GREEN field description is not rejected", func(t *testing.T) {
		line := "  - **RED:** `TestCanonicalTerms` catches Platform IAM confused with Workforce Access, Candidate used as exclusive Person state, or external observation labeled domain fact."
		if v := CheckCanonicalTerms(line); len(v) != 0 {
			t.Errorf("spec-description RED field incorrectly flagged: %v", v)
		}
	})

	t.Run("real planning corpus has zero canonical-term violations", func(t *testing.T) {
		root := filepath.Join("..", "..", "..", "planning")
		var total int
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, ".md") {
				return nil
			}
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, v := range CheckCanonicalTerms(string(content)) {
				total++
				t.Errorf("%s: %s", path, v)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk planning/: %v", err)
		}
		if total != 0 {
			t.Fatalf("found %d canonical-term violations in the real planning corpus", total)
		}
	})
}

func assertHasRule(t *testing.T, violations []Violation, rule string) {
	t.Helper()
	for _, v := range violations {
		if v.Rule == rule {
			return
		}
	}
	t.Errorf("expected a violation for rule %q, got %v", rule, violations)
}

// TestTodo_GOV_012_Golden pins the exact violation format.
func TestTodo_GOV_012_Golden(t *testing.T) {
	v := CheckCanonicalTerms("A person is either a Candidate or a Worker.")
	if len(v) != 1 {
		t.Fatalf("expected exactly one violation, got %v", v)
	}
	const want = `line 1: Candidate used as an exclusive Person state: "A person is either a Candidate"`
	if got := v[0].String(); got != want {
		t.Errorf("violation message changed:\n got:  %s\n want: %s", got, want)
	}
}

// TestTodo_GOV_012_Conformance re-runs the real-corpus scan in isolation so
// it can be targeted directly (`go test -run TestTodo_GOV_012_Conformance`)
// without the rest of the primary test's fixtures.
func TestTodo_GOV_012_Conformance(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	findings, err := ScanRepository(root)
	if err != nil {
		t.Fatalf("scan planning/schema/definitions: %v", err)
	}
	for _, finding := range findings {
		t.Errorf("%s: %s", finding.Path, finding.Violation)
	}
	if len(findings) != 0 {
		t.Fatalf("found %d canonical-term violations in the governed corpus", len(findings))
	}
}

func TestTodo_GOV_012_NegativeFixtures(t *testing.T) {
	fixture, err := os.ReadFile(filepath.Join("testdata", "negative.md"))
	if err != nil {
		t.Fatal(err)
	}
	violations := CheckCanonicalTerms(string(fixture))
	want := []string{
		"Platform IAM confused with Workforce Access",
		"Platform IAM confused with Workforce Access",
		"Candidate used as an exclusive Person state",
		"Candidate used as an exclusive Person state",
		"external observation labeled a domain fact",
	}
	if len(violations) != len(want) {
		t.Fatalf("fixture produced %d violations, want %d: %v", len(violations), len(want), violations)
	}
	for i := range want {
		if violations[i].Rule != want[i] {
			t.Errorf("violation %d rule = %q, want %q", i, violations[i].Rule, want[i])
		}
	}
}

func TestTodo_GOV_012_GlossaryLinks(t *testing.T) {
	got := LinkGlossaryTerms("Platform IAM authenticates users. `Candidate` is a role.\nSee [Person](existing.md).")
	want := "[Platform IAM](planning/plan.md#94-identity-and-permission-contract) authenticates users. `Candidate` is a role.\nSee [Person](existing.md)."
	if got != want {
		t.Fatalf("glossary links differ:\n got: %s\nwant: %s", got, want)
	}
	if len(Glossary[1].Aliases) != 1 || Glossary[1].Aliases[0].Version == "" {
		t.Fatalf("registered alias must be explicitly versioned: %+v", Glossary[1].Aliases)
	}
}
