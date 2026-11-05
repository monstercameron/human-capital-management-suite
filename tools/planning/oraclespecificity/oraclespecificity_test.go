package oraclespecificity

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func oracleFixture(id, red, green string) string {
	return "- [ ] `" + id + "` **[P0][LUNA] Fixture.**\n" +
		"  - **TEST:** `Test" + id + "`.\n" +
		"  - **TEST MATRIX:** `PRIMARY=Test" + id + "`.\n" +
		"  - **RED:** " + red + ".\n" +
		"  - **GREEN:** " + green + ".\n" +
		"  - **REFACTOR:** preserves the oracle.\n" +
		"  - **Refs:** [Plan](plan.md).\n"
}

func checkCorpus(t *testing.T, corpus, file string) map[string]map[string]int {
	t.Helper()
	findings, err := CheckMarkdown(corpus, file)
	if err != nil {
		t.Fatal(err)
	}
	grouped := make(map[string]map[string]int)
	for _, finding := range findings {
		if grouped[finding.TodoID] == nil {
			grouped[finding.TodoID] = make(map[string]int)
		}
		grouped[finding.TodoID][finding.Code]++
		if finding.Line <= 0 {
			t.Errorf("%s carries no source line", finding.TodoID)
		}
	}
	return grouped
}

// TestTodoOracleSpecificityRejectsPlaceholderContractLanguage is the GOV-028
// primary oracle: every placeholder shape from the contract classifies with
// its exact code while specific oracles stay clean.
func TestTodoOracleSpecificityRejectsPlaceholderContractLanguage(t *testing.T) {
	strongRed := "replay of the seeded duplicate settlement returns typed ErrDuplicateSettlement"
	strongGreen := "returns the exact typed ErrDuplicateSettlement, persists exactly one ledger row and emits zero outbox events"
	corpus := strings.Join([]string{
		oracleFixture("PlaceholderViolates", "violates this contract", strongGreen),
		oracleFixture("PlaceholderMustFail", "must fail on the seeded defect", strongGreen),
		oracleFixture("PlaceholderFixture", "an invalid fixture is rejected", strongGreen),
		oracleFixture("PlaceholderDiagnostics", "returns diagnostics for the seeded defect", strongGreen),
		oracleFixture("PlaceholderWorks", strongRed, "works correctly after the fix"),
		oracleFixture("PlaceholderSucceeds", strongRed, "the operation succeeds"),
		oracleFixture("MissingProhibited", strongRed, "the ledger persists the settlement and the provider is notified of the posting"),
		oracleFixture("StrongSpecific", strongRed, strongGreen),
		oracleFixture("StrongDigest", "a reordered seed changes the canonical digest", "the golden bytes are byte-identical to the pinned canonical digest"),
		oracleFixture("StrongEmission", "a record validates while missing engines", "the contract requires every field, emits stable registries and produces the same canonical digest across repeated compilation"),
	}, "\n")
	grouped := checkCorpus(t, corpus, "fixture.md")
	expectations := map[string]string{
		"PlaceholderViolates":    PlaceholderRedOracle,
		"PlaceholderMustFail":    PlaceholderRedOracle,
		"PlaceholderFixture":     PlaceholderRedOracle,
		"PlaceholderDiagnostics": PlaceholderRedOracle,
		"PlaceholderWorks":       PlaceholderGreenOracle,
		"PlaceholderSucceeds":    PlaceholderGreenOracle,
		"MissingProhibited":      MissingProhibitedEffectOracle,
	}
	for id, want := range expectations {
		got := grouped[id]
		if got[want] == 0 {
			t.Errorf("%s: want code %s, got %+v", id, want, got)
		}
	}
	for _, id := range []string{"StrongSpecific", "StrongDigest", "StrongEmission"} {
		if got := grouped[id]; len(got) > 0 {
			t.Errorf("%s flagged placeholder: %+v", id, got)
		}
	}
	if got := grouped["MissingProhibited"]; got[PlaceholderRedOracle] > 0 || got[PlaceholderGreenOracle] > 0 {
		t.Errorf("MissingProhibited carries a placeholder code too: %+v", got)
	}
}

func TestTodo_GOV_028_Property(t *testing.T) {
	strongRed := "replay of the seeded duplicate settlement returns typed ErrDuplicateSettlement"
	strongGreen := "returns the exact typed ErrDuplicateSettlement, persists exactly one ledger row and emits zero outbox events"
	cases := []struct {
		name  string
		red   string
		green string
		code  string
	}{
		{"violates the contract", "violates the contract on bad input", strongGreen, PlaceholderRedOracle},
		{"fails as expected", "fails as expected on the seeded defect", strongGreen, PlaceholderRedOracle},
		{"reports an error", "reports an error for the seeded defect", strongGreen, PlaceholderRedOracle},
		{"is rejected bare", "the request is rejected", strongGreen, PlaceholderRedOracle},
		{"redeemed by exact rejection", "violates the contract and returns the exact typed ErrStaleSnapshot", strongGreen, ""},
		{"redeemed by literal", "returns `ErrStaleSnapshot` for the seeded replay", strongGreen, ""},
		{"behaves as expected", strongRed, "behaves as expected", PlaceholderGreenOracle},
		{"is accepted bare", strongRed, "the request is accepted", PlaceholderGreenOracle},
		{"passes bare", strongRed, "all suites pass", PlaceholderGreenOracle},
		{"redeemed by counts", strongRed, "persists exactly one row and emits zero events", ""},
		{"redeemed by digest", strongRed, "the golden bytes match the pinned canonical digest", ""},
		{"ledger without bound", strongRed, "the ledger persists the settlement", MissingProhibitedEffectOracle},
		{"outbox with zero bound passes", strongRed, "the outbox emits zero events for the duplicate", ""},
		{"provider with refusal passes", strongRed, "the provider refuses the duplicate with typed ErrDuplicateSettlement", ""},
		{"human work without bound", strongRed, "an approval task is created and assigned to the manager", MissingProhibitedEffectOracle},
		{"pure computation passes", "divides by zero in the seeded fixture", "returns the exact quotient for every seeded pair", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			grouped := checkCorpus(t, oracleFixture("Prop", tc.red, tc.green), "fixture.md")
			got := grouped["Prop"]
			if tc.code == "" {
				if len(got) > 0 {
					t.Errorf("specific oracle flagged: %+v", got)
				}
				return
			}
			if got[tc.code] == 0 {
				t.Errorf("want code %s, got %+v", tc.code, got)
			}
		})
	}
}

func TestTodo_GOV_028_Golden(t *testing.T) {
	corpus := strings.Join([]string{
		oracleFixture("GoldenViolates", "violates this contract", "returns the exact typed ErrStaleSnapshot with zero outbox events"),
		oracleFixture("GoldenStrong", "replay of the seeded duplicate returns typed ErrDuplicateSettlement", "persists exactly one ledger row and emits zero outbox events"),
		oracleFixture("GoldenLedger", "the duplicate replay returns typed ErrDuplicateSettlement", "the ledger persists the settlement"),
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
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != strings.TrimSpace(string(want)) {
		t.Fatalf("golden mismatch\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestTodo_GOV_028_Mutation(t *testing.T) {
	red := "replay of the seeded duplicate settlement returns typed ErrDuplicateSettlement"
	green := "returns the exact typed ErrDuplicateSettlement, persists exactly one ledger row and emits zero outbox events"
	before := checkCorpus(t, oracleFixture("Mut", red, green), "fixture.md")
	if len(before["Mut"]) > 0 {
		t.Fatalf("specific oracle flagged: %+v", before["Mut"])
	}
	weakened := checkCorpus(t, oracleFixture("Mut", "must fail on the seeded defect", green), "fixture.md")
	if weakened["Mut"][PlaceholderRedOracle] == 0 {
		t.Fatalf("weakened RED accepted: %+v", weakened["Mut"])
	}
	unbounded := checkCorpus(t, oracleFixture("Mut", red, "the ledger persists the settlement"), "fixture.md")
	if unbounded["Mut"][MissingProhibitedEffectOracle] == 0 {
		t.Fatalf("unbounded ledger claim accepted: %+v", unbounded["Mut"])
	}
	repaired := checkCorpus(t, oracleFixture("Mut", red, green), "fixture.md")
	if len(repaired["Mut"]) > 0 {
		t.Fatalf("repaired oracle still flagged: %+v", repaired["Mut"])
	}
}

// TestTodo_GOV_028_Conformance runs the classifier over the live backlog:
// the scan must complete deterministically with located findings under
// known codes, and the ticked-strong P0 anchors must stay clean.
func TestTodo_GOV_028_Conformance(t *testing.T) {
	root := repoRoot(t)
	content, err := os.ReadFile(filepath.Join(root, "planning", "archive", "todos-2026-10-22.md"))
	if err != nil {
		t.Fatalf("read live backlog: %v", err)
	}
	first, err := CheckMarkdown(string(content), "planning/archive/todos-2026-10-22.md")
	if err != nil {
		t.Fatalf("live backlog check: %v", err)
	}
	second, err := CheckMarkdown(string(content), "planning/archive/todos-2026-10-22.md")
	if err != nil {
		t.Fatalf("live backlog recheck: %v", err)
	}
	if len(first) != len(second) {
		t.Fatalf("nondeterministic live scan: %d then %d findings", len(first), len(second))
	}
	counts := make(map[string]int)
	byTodo := make(map[string][]Finding)
	for i := range first {
		if first[i].String() != second[i].String() {
			t.Fatalf("nondeterministic finding: %q then %q", first[i], second[i])
		}
		switch first[i].Code {
		case PlaceholderRedOracle, PlaceholderGreenOracle, MissingProhibitedEffectOracle:
		default:
			t.Fatalf("unknown code %q", first[i].Code)
		}
		counts[first[i].Code]++
		byTodo[first[i].TodoID] = append(byTodo[first[i].TodoID], first[i])
	}
	t.Logf("live backlog specificity: %d findings %v", len(first), counts)
	for _, id := range []string{"GOV-021", "GOV-022", "GOV-023", "GOV-024", "GOV-025", "GOV-026", "GOV-027", "WF-DISC-001", "WF-DISC-002", "WF-DISC-003", "WF-DISC-004", "WF-DISC-005"} {
		if findings := byTodo[id]; len(findings) > 0 {
			t.Errorf("strong anchor %s flagged: %+v", id, findings)
		}
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("no go.mod found from %s", file)
		}
		dir = parent
	}
}

func FuzzTodo_GOV_028(f *testing.F) {
	seeds := []string{
		oracleFixture("SeedStrong",
			"replay of the seeded duplicate settlement returns typed ErrDuplicateSettlement",
			"returns the exact typed ErrDuplicateSettlement with zero outbox events"),
		oracleFixture("SeedWeak", "must fail", "works correctly"),
		oracleFixture("SeedEmpty", "", ""),
		"- [ ] `Broken",
		"",
		"  - **RED:** violates this contract.\n  - **GREEN:** succeeds.\n",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, corpus string) {
		first, err := CheckMarkdown(corpus, "fuzz.md")
		if err != nil {
			return
		}
		second, err := CheckMarkdown(corpus, "fuzz.md")
		if err != nil {
			t.Fatal("deterministic input errors nondeterministically")
		}
		if len(first) != len(second) {
			t.Fatalf("nondeterministic findings: %d then %d", len(first), len(second))
		}
		for i := range first {
			if first[i].String() != second[i].String() {
				t.Fatalf("nondeterministic finding: %q then %q", first[i], second[i])
			}
			if first[i].Line <= 0 || first[i].TodoID == "" {
				t.Fatalf("unlocated finding: %+v", first[i])
			}
			switch first[i].Code {
			case PlaceholderRedOracle, PlaceholderGreenOracle, MissingProhibitedEffectOracle:
			default:
				t.Fatalf("unknown code %q", first[i].Code)
			}
			if i > 0 && findingLess(first[i], first[i-1]) {
				t.Fatalf("findings not sorted: %q before %q", first[i-1], first[i])
			}
		}
	})
}
