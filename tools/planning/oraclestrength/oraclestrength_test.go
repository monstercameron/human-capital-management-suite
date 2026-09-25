package oraclestrength

import (
	"os"
	"strings"
	"testing"
)

func readTestFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func weakFixture(id, red, green string) string {
	return "- [ ] `" + id + "` **[P0][LUNA] Fixture.**\n" +
		"  - **TEST:** `Test" + id + "`.\n" +
		"  - **TEST MATRIX:** `PRIMARY=Test" + id + "`.\n" +
		"  - **RED:** " + red + ".\n" +
		"  - **GREEN:** " + green + ".\n" +
		"  - **REFACTOR:** preserves the oracle.\n" +
		"  - **Refs:** [Plan](plan.md).\n"
}

func checkIDs(findings []Finding) map[string][]Finding {
	grouped := make(map[string][]Finding)
	for _, finding := range findings {
		grouped[finding.TodoID] = append(grouped[finding.TodoID], finding)
	}
	return grouped
}

func hasReason(findings []Finding, want string) bool {
	for _, finding := range findings {
		if strings.Contains(finding.Message, want) {
			return true
		}
	}
	return false
}

// TestOracleStrengthRejectsExecutionOnlyAssertions is the GOV-021 primary
// oracle: every weak fixture below must classify WEAK_ORACLE while the
// strong fixtures stay clean.
func TestOracleStrengthRejectsExecutionOnlyAssertions(t *testing.T) {
	strongTyped := weakFixture("StrongTyped",
		"replay of the seeded duplicate settlement returns typed ErrDuplicateSettlement",
		"returns the exact typed ErrDuplicateSettlement, persists exactly one ledger row and emits zero outbox events")
	strongDigest := weakFixture("StrongDigest",
		"a reordered registry seed changes the canonical digest",
		"the golden bytes are byte-identical to the pinned canonical digest across repeated compilation")
	strongEmission := weakFixture("StrongEmission",
		"a record validates while missing engines",
		"the contract requires every field, emits stable registries and produces the same canonical digest across repeated compilation")
	corpus := strings.Join([]string{
		weakFixture("WeakPanic",
			"panics on the seeded defect",
			"completes without panic"),
		weakFixture("WeakNonNil",
			"returns nil for the seeded defect",
			"returns a non-nil result"),
		weakFixture("WeakStatus",
			"returns 500 for the seeded defect",
			"returns status 200"),
		weakFixture("WeakMock",
			"the provider is not called for the seeded defect",
			"the mock provider is invoked"),
		weakFixture("WeakCoverage",
			"the seeded defect is present",
			"line coverage reaches 80 percent"),
		weakFixture("WeakEffects",
			"duplicate settlement persists twice for the seeded replay",
			"the ledger persists the settlement"),
		weakFixture("WeakSnapshot",
			"a reordered seed changes the output",
			"the golden snapshot matches the recorded output"),
		weakFixture("WeakEitherOr",
			"rejects the seeded defect with typed ErrStaleSnapshot",
			"returns either the cached value or a fresh computation"),
		weakFixture("WeakNoCase",
			"misbehaves on bad input",
			"returns the exact typed result with zero ledger writes"),
		strongTyped, strongDigest, strongEmission,
	}, "\n")
	findings, err := CheckMarkdown(corpus, "fixture.md")
	if err != nil {
		t.Fatal(err)
	}
	grouped := checkIDs(findings)
	for _, id := range []string{"WeakPanic", "WeakNonNil", "WeakStatus", "WeakMock", "WeakCoverage", "WeakEffects", "WeakSnapshot", "WeakEitherOr", "WeakNoCase"} {
		got := grouped[id]
		if len(got) == 0 {
			t.Errorf("%s accepted as a strong oracle", id)
			continue
		}
		for _, finding := range got {
			if finding.Code != WeakOracle {
				t.Errorf("%s code = %s, want %s", id, finding.Code, WeakOracle)
			}
			if finding.Line <= 0 {
				t.Errorf("%s carries no source line", id)
			}
		}
	}
	for _, id := range []string{"StrongTyped", "StrongDigest", "StrongEmission"} {
		if got := grouped[id]; len(got) > 0 {
			t.Errorf("%s flagged weak: %+v", id, got)
		}
	}
	if !hasReason(grouped["WeakPanic"], "panic") {
		t.Error("panic-only oracle lacks its reason")
	}
	if !hasReason(grouped["WeakCoverage"], "coverage") {
		t.Error("coverage-shortcut oracle lacks its reason")
	}
	if !hasReason(grouped["WeakEffects"], "prohibited") {
		t.Error("effect-without-prohibition oracle lacks its reason")
	}
	if !hasReason(grouped["WeakSnapshot"], "snapshot") {
		t.Error("unstable-snapshot oracle lacks its reason")
	}
	if !hasReason(grouped["WeakEitherOr"], "either") {
		t.Error("contradictory-output oracle lacks its reason")
	}
	if !hasReason(grouped["WeakNoCase"], "concrete") {
		t.Error("case-free RED oracle lacks its reason")
	}
}

func TestOracleStrengthRejectsMissingGreenOracle(t *testing.T) {
	fixture := "- [ ] `MissingGreen` **[P0][LUNA] Fixture.**\n" +
		"  - **TEST:** `TestMissingGreen`.\n" +
		"  - **RED:** replay of the seeded duplicate settlement returns typed ErrDuplicateSettlement.\n" +
		"  - **REFACTOR:** preserves the oracle.\n"
	findings, err := CheckMarkdown(fixture, "fixture.md")
	if err != nil {
		t.Fatal(err)
	}
	if !hasReason(findings, "GREEN oracle is missing") {
		t.Fatalf("todo without a GREEN oracle accepted: %+v", findings)
	}
}

func TestTodo_GOV_021_Property(t *testing.T) {
	strongRed := "replay of the seeded duplicate settlement returns typed ErrDuplicateSettlement"
	strongGreen := "returns the exact typed ErrDuplicateSettlement, persists exactly one ledger row and emits zero outbox events"
	cases := []struct {
		name   string
		red    string
		green  string
		reason string
	}{
		{"panic only", "panics on the seeded defect", "completes without panic", "panic"},
		{"nil only", "returns nil for the seeded defect", "returns a non-nil result", "nil"},
		{"status only", "returns 500 for the seeded defect", "returns status 200", "status 200"},
		{"mock only", "the provider is not called", "the mock provider interaction is verified", "mock"},
		{"coverage shortcut", "the seeded defect is present", "line coverage reaches 80 percent", "coverage"},
		{"coverage with mutant closure passes", "the seeded defect is present", "mutation coverage closes every seeded defect with zero survivors", ""},
		{"unbounded effect", "duplicate settlement persists twice", "the ledger persists the settlement", "prohibited"},
		{"bounded effect passes", strongRed, strongGreen, ""},
		{"unstable snapshot", "output changes", "the golden snapshot matches the recorded output", "snapshot"},
		{"digest snapshot passes", "a reordered seed changes the digest", "the golden bytes match the pinned canonical digest", ""},
		{"contradictory outputs", "rejects the defect", "returns either the cached value or a fresh one", "either"},
		{"single outcome passes", strongRed, strongGreen, ""},
		{"vague red", "misbehaves on bad input", strongGreen, "concrete"},
		{"seeded-defect red passes", "the seeded mutant survives the suite", strongGreen, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			findings, err := CheckMarkdown(weakFixture("Prop", tc.red, tc.green), "fixture.md")
			if err != nil {
				t.Fatal(err)
			}
			if tc.reason == "" {
				if len(findings) > 0 {
					t.Errorf("strong oracle flagged weak: %+v", findings)
				}
				return
			}
			if !hasReason(findings, tc.reason) {
				t.Errorf("expected a %q finding, got %+v", tc.reason, findings)
			}
		})
	}
}

func TestTodo_GOV_021_Golden(t *testing.T) {
	corpus := strings.Join([]string{
		weakFixture("GoldenPanic",
			"panics on the seeded defect",
			"completes without panic"),
		weakFixture("GoldenStrong",
			"replay of the seeded duplicate settlement returns typed ErrDuplicateSettlement",
			"returns the exact typed ErrDuplicateSettlement with zero outbox events"),
		weakFixture("GoldenSnapshot",
			"a reordered seed changes the output",
			"the golden snapshot matches the recorded output"),
	}, "\n")
	findings, err := CheckMarkdown(corpus, "golden.md")
	if err != nil {
		t.Fatal(err)
	}
	got, err := MarshalFindings(findings)
	if err != nil {
		t.Fatal(err)
	}
	const goldenPath = "testdata/golden.json"
	if os.Getenv("UPDATE_GOLDEN") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := readTestFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != strings.TrimSpace(string(want)) {
		t.Fatalf("golden mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestTodo_GOV_021_Mutation(t *testing.T) {
	strongRed := "replay of the seeded duplicate settlement returns typed ErrDuplicateSettlement"
	strongGreen := "returns the exact typed ErrDuplicateSettlement, persists exactly one ledger row and emits zero outbox events"
	before, err := CheckMarkdown(weakFixture("Mut", strongRed, strongGreen), "fixture.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(before) > 0 {
		t.Fatalf("strong oracle flagged weak: %+v", before)
	}
	stripped, err := CheckMarkdown(weakFixture("Mut",
		"replay of the seeded duplicate settlement returns typed ErrDuplicateSettlement",
		"the ledger persists the settlement"), "fixture.md")
	if err != nil {
		t.Fatal(err)
	}
	if !hasReason(stripped, "prohibited") {
		t.Fatalf("stripped prohibited-effect bound accepted: %+v", stripped)
	}
	seeded, err := CheckMarkdown(weakFixture("Mut",
		"misbehaves on bad input",
		"returns the exact typed result with zero ledger writes"), "fixture.md")
	if err != nil {
		t.Fatal(err)
	}
	if !hasReason(seeded, "concrete") {
		t.Fatalf("vague RED accepted: %+v", seeded)
	}
	repaired, err := CheckMarkdown(weakFixture("Mut",
		"the seeded mutant survives the suite",
		"returns the exact typed result with zero ledger writes"), "fixture.md")
	if err != nil {
		t.Fatal(err)
	}
	if len(repaired) > 0 {
		t.Fatalf("repaired oracle still weak: %+v", repaired)
	}
}
