package todogovernance

import (
	"fmt"
	"strings"
	"testing"
)

func applicabilityFixture(id, phase, title, role, matrix string) string {
	name := "Test" + strings.ReplaceAll(id, "-", "")
	return fmt.Sprintf("- [ ] `%s` **[%s][LUNA] %s.**\n"+
		"  - **Depends:** none.\n"+
		"  - **INTENT CONTEXT:** `ROLE=%s; SETS=BI.ALL; DIRECT=none; WHY=fixture`.\n"+"  - **TEST:** `%s`.\n"+"  - **TEST MATRIX:** `%s`.\n"+"  - **RED:** fixture red.\n  - **GREEN:** fixture green.\n"+"  - **REFACTOR:** fixture refactor.\n  - **Refs:** [fixture](fixture.md).\n",
		id, phase, title, role, name, matrix)
}

func parseApplicabilityFixture(t *testing.T, content string) []Record {
	t.Helper()
	records, errs := ParseRecords(content)
	if len(errs) != 0 {
		t.Fatalf("unexpected parse errors: %v", errs)
	}
	return records
}

// TestTodoTestMatrixApplicability is the PRIMARY test declared by GOV-018.
// It proves the red cases for missing and meaningless classes, UNIT_ONLY
// without a reason and duplicate matrix names, then checks the live backlog
// against the reviewed gap allowlist.
func TestTodoTestMatrixApplicability(t *testing.T) {
	t.Run("MissingDerivedClass", func(t *testing.T) {
		records := parseApplicabilityFixture(t, applicabilityFixture("FX-018", "GATE_A", "Trust boundary", "GOVERNANCE", "PRIMARY=TestFX018"))
		findings := ValidateTestMatrixApplicability(records)
		requireFinding(t, findings, "FX-018", "GOV-018", CodeMissingApplicableTestClass)
	})

	t.Run("MeaninglessClass", func(t *testing.T) {
		records := parseApplicabilityFixture(t, applicabilityFixture("FX-019", "PHASE_2", "Plain note", "DOMAIN_SUPPORT", "PRIMARY=TestFX019; NOT_A_CLASS=TestNeverNeeded"))
		findings := ValidateTestMatrixApplicability(records)
		requireFinding(t, findings, "FX-019", "GOV-018", CodeMeaninglessTestClass)
	})

	t.Run("ArchitectureIsADeclaredClass", func(t *testing.T) {
		records := parseApplicabilityFixture(t, applicabilityFixture("FX-019A", "PHASE_2", "Package boundary", "DOMAIN_SUPPORT", "PRIMARY=TestFX019A; ARCHITECTURE=TestArchitecture"))
		if findings := ValidateTestMatrixApplicability(records); len(findings) != 0 {
			t.Fatalf("architecture fixture findings = %v", findings)
		}
	})

	t.Run("UnitOnlyNeedsEvaluatedReason", func(t *testing.T) {
		records := parseApplicabilityFixture(t, applicabilityFixture("FX-020", "PHASE_2", "Plain note", "DOMAIN_SUPPORT", "PRIMARY=TestFX020; UNIT_ONLY"))
		findings := ValidateTestMatrixApplicability(records)
		requireFinding(t, findings, "FX-020", "GOV-018", CodeUnitOnlyWithoutReason)
	})

	t.Run("DuplicateMatrixName", func(t *testing.T) {
		content := applicabilityFixture("FX-021", "PHASE_2", "Plain note", "DOMAIN_SUPPORT", "PRIMARY=TestFX021; PROPERTY=TestSame") +
			applicabilityFixture("FX-022", "PHASE_2", "Plain note", "DOMAIN_SUPPORT", "PRIMARY=TestFX022; PROPERTY=TestSame")
		records := parseApplicabilityFixture(t, content)
		findings := ValidateTestMatrixApplicability(records)
		requireFinding(t, findings, "FX-021", "GOV-018", CodeDuplicateMatrixTestName)
		requireFinding(t, findings, "FX-022", "GOV-018", CodeDuplicateMatrixTestName)
	})

	t.Run("RealCorpus", func(t *testing.T) {
		findings := ValidateTestMatrixApplicability(loadRealMarkdown(t))
		assertOnlyAllowlisted(t, findings, gov018Allowlist)
		t.Logf("GOV-018: %d live applicability gap(s), %d allowlisted", len(findings), len(gov018Allowlist))
	})
}

// TestTodo_GOV_018_Golden pins the complete derivation table, including
// predicate wording and class ordering, so a policy change is deliberate.
func TestTodo_GOV_018_Golden(t *testing.T) {
	got := RiskDerivationTable()
	want := []string{
		"phase:P0|phase is P0|GOLDEN",
		"phase:GATE_A|phase is GATE_A|GOLDEN",
		"tag:SECURITY|SECURITY tag in title or refs|SECURITY",
		"tag:CONFORMANCE|CONFORMANCE tag in title or refs|CONFORMANCE",
		"tag:PROPERTY|PROPERTY tag in title or refs|PROPERTY",
		"tag:FUZZ|FUZZ tag in title or refs|FUZZ",
		"tag:RACE|RACE tag in title or refs|RACE",
		"tag:FAULT|FAULT tag in title or refs|FAULT",
		"tag:BROWSER|BROWSER tag in title or refs|BROWSER",
		"tag:RECOVERY|RECOVERY tag in title or refs|RECOVERY",
		"tag:BENCHMARK|BENCHMARK tag in title or refs|BENCHMARK",
		"tag:MUTATION|MUTATION tag in title or refs|MUTATION",
		"tag:MODEL_BASED|MODEL_BASED tag in title or refs|MODEL_BASED",
		"tag:INTEGRATION|INTEGRATION tag in title or refs|INTEGRATION",
		"role:GOVERNANCE|INTENT CONTEXT ROLE is GOVERNANCE|GOLDEN",
		"role:CONFORMANCE|INTENT CONTEXT ROLE is CONFORMANCE|CONFORMANCE",
		"vocabulary:migration|migration, backfill, cutover or rollback vocabulary|FAULT,RECOVERY",
		"vocabulary:trust|trust, identity, authorization, secret, privacy or DLP vocabulary|SECURITY",
		"vocabulary:transport|transport, adapter, connector, provider, HTTP or gRPC vocabulary|INTEGRATION",
	}
	if len(got) != len(want) {
		t.Fatalf("derivation table length = %d, want %d", len(got), len(want))
	}
	for i, rule := range got {
		key := rule.ID + "|" + rule.Marker + "|" + strings.Join(rule.Classes, ",")
		if key != want[i] {
			t.Errorf("table[%d] = %q, want %q", i, key, want[i])
		}
	}
}

// TestTodo_GOV_018_Race exercises the pure validator under concurrent calls;
// the policy itself has no shared mutable evaluation state.
func TestTodo_GOV_018_Race(t *testing.T) {
	records := parseApplicabilityFixture(t, applicabilityFixture("FX-023", "GATE_A", "Transport trust [RACE]", "CONFORMANCE", "PRIMARY=TestFX023; GOLDEN=TestGolden; SECURITY=TestSecurity; INTEGRATION=TestIntegration; CONFORMANCE=TestConformance; RACE=TestRace"))
	done := make(chan struct{}, 8)
	for i := 0; i < cap(done); i++ {
		go func() { _ = ValidateTestMatrixApplicability(records); done <- struct{}{} }()
	}
	for i := 0; i < cap(done); i++ {
		<-done
	}
}

func TestTodo_GOV_018_Integration(t *testing.T) {
	records := parseApplicabilityFixture(t, applicabilityFixture("FX-024", "PHASE_2", "Transport adapter", "DOMAIN_SUPPORT", "PRIMARY=TestFX024; INTEGRATION=TestIntegration"))
	if findings := ValidateTestMatrixApplicability(records); len(findings) != 0 {
		t.Fatalf("transport fixture findings = %v", findings)
	}
}

func TestTodo_GOV_018_Fault(t *testing.T) {
	records := parseApplicabilityFixture(t, applicabilityFixture("FX-025", "PHASE_2", "Migration rollback", "DOMAIN_SUPPORT", "PRIMARY=TestFX025; FAULT=TestFault; RECOVERY=TestRecovery"))
	if findings := ValidateTestMatrixApplicability(records); len(findings) != 0 {
		t.Fatalf("migration fixture findings = %v", findings)
	}
}

func TestTodo_GOV_018_Security(t *testing.T) {
	records := parseApplicabilityFixture(t, applicabilityFixture("FX-026", "PHASE_2", "Security trust", "DOMAIN_SUPPORT", "PRIMARY=TestFX026; SECURITY=TestSecurity"))
	if findings := ValidateTestMatrixApplicability(records); len(findings) != 0 {
		t.Fatalf("security fixture findings = %v", findings)
	}
}

func TestTodo_GOV_018_Conformance(t *testing.T) {
	records := parseApplicabilityFixture(t, applicabilityFixture("FX-027", "CONFORMANCE", "Parity", "CONFORMANCE", "PRIMARY=TestFX027; CONFORMANCE=TestConformance"))
	if findings := ValidateTestMatrixApplicability(records); len(findings) != 0 {
		t.Fatalf("conformance fixture findings = %v", findings)
	}
}

func TestTodo_GOV_018_Browser(t *testing.T) {
	records := parseApplicabilityFixture(t, applicabilityFixture("FX-028", "PHASE_2", "[BROWSER] interaction", "DOMAIN_SUPPORT", "PRIMARY=TestFX028; BROWSER=TestBrowser"))
	if findings := ValidateTestMatrixApplicability(records); len(findings) != 0 {
		t.Fatalf("browser fixture findings = %v", findings)
	}
}

func BenchmarkTodo_GOV_018(b *testing.B) {
	records, errs := ParseRecords(applicabilityFixture("FX-029", "PHASE_2", "Transport adapter", "DOMAIN_SUPPORT", "PRIMARY=TestFX029; INTEGRATION=TestIntegration"))
	if len(errs) != 0 {
		b.Fatal(errs)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ValidateTestMatrixApplicability(records)
	}
}

func TestTodo_GOV_018_Mutation(t *testing.T) {
	records := parseApplicabilityFixture(t, applicabilityFixture("FX-030", "PHASE_2", "[MUTATION] effect", "DOMAIN_SUPPORT", "PRIMARY=TestFX030; MUTATION=TestMutation"))
	if findings := ValidateTestMatrixApplicability(records); len(findings) != 0 {
		t.Fatalf("mutation fixture findings = %v", findings)
	}
}

func FuzzTodo_GOV_018(f *testing.F) {
	for _, seed := range []string{"", "SECURITY", "UNIT_ONLY(reason=not applicable: no external boundary)", "GOLDEN=x; MISSING=y", "`;=;unicode-—"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, matrix string) {
		if strings.ContainsAny(matrix, "\r\n`") {
			t.Skip()
		}
		records, _ := ParseRecords(applicabilityFixture("FZ-018", "PHASE_2", "fuzz fixture", "DOMAIN_SUPPORT", "PRIMARY=TestFZ018; "+matrix))
		_ = ValidateTestMatrixApplicability(records)
	})
}
