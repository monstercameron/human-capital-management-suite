package legalmatrix

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// validFixtureSpec is a minimal, internally-consistent three-state,
// two-column-per-table stand-in for the real Section 5 matrix. It exists
// so the RED/GREEN subtests below can flip exactly one thing at a time
// instead of editing a fifty-row fixture. It deliberately reuses real
// column headers (NOTICE, WAGE_FLOOR, BREACH, LOCAL) so the package's
// built-in alias maps apply unmodified, including the LOCAL column's
// two-part total.
func validFixtureSpec() string {
	return `## 5. Per-State Configuration Matrix

Every cell is derived from that state's research file.

` + "```text" + `
Y   the research asserts a state-level rule of this kind
L   the research asserts only a local (sub-state) rule; no state rule
P   the state preempts local rules of this kind
F   the research asserts no state rule; the federal baseline applies
?   the research is uncertain, self-contradictory, or marked "verify"
` + "```" + `

### 5.1 Table A — kinds the promotion and base-pay flow consumes

| State | NOTICE | WAGE_FLOOR |
| ----- | ------ | ---------- |
| AL    | F      | F          |
| AK    | Y      | Y          |
| AZ    | F      | Y          |

Column totals (` + "`Y`" + ` + ` + "`L`" + `, excluding ` + "`P`" + `, ` + "`F`" + ` and ` + "`?`" + `):

` + "```text" + `
NOTICE          1      WAGE_FLOOR 2
` + "```" + `

### 5.2 Table B — kinds the flow declares but usually does not trigger

| State | BREACH | LOCAL |
| ----- | ------ | ----- |
| AL    | Y 45d  | L     |
| AK    | Y      | P     |
| AZ    | ?      | F     |

Column totals (` + "`Y`" + ` + ` + "`L`" + `, excluding ` + "`P`" + `, ` + "`F`" + ` and ` + "`?`" + `):

` + "```text" + `
BREACH 2      LOCAL overlay 1      LOCAL preempted 1
` + "```" + `

## 6. Evaluation Semantics

Trailing prose that must not be mistaken for another table.
`
}

func fixtureResearchFiles() []string {
	return []string{"alabama.md", "alaska.md", "arizona.md"}
}

func fixtureReadme() string {
	return "- [Legal rule packs and state configuration](legal-rule-packs-and-state-configuration.md)\n"
}

func fixtureRegistry() string {
	return "| `legal-rule-packs-and-state-configuration.md` | Legal and Regulatory Content owner | `DRAFT_CONTRACT` | Before any rule-pack release |\n"
}

func hasKind(violations []Violation, kind string) bool {
	for _, v := range violations {
		if v.Kind == kind {
			return true
		}
	}
	return false
}

// TestLegalMatrixCoversEveryResearchFile is LEGAL-008's PRIMARY test. It
// first proves the real repository's spec and research corpus are clean
// (the actual acceptance condition), then table-drives every RED
// condition named in the todo against the synthetic fixture above to prove
// Check actually detects each one rather than passing by omission.
func TestLegalMatrixCoversEveryResearchFile(t *testing.T) {
	t.Run("real repository has zero violations", func(t *testing.T) {
		root := repoRoot(t)

		specContent, err := os.ReadFile(filepath.Join(root, "planning", "specs", "legal-rule-packs-and-state-configuration.md"))
		if err != nil {
			t.Fatalf("read spec: %v", err)
		}
		readmeContent, err := os.ReadFile(filepath.Join(root, "planning", "specs", "README.md"))
		if err != nil {
			t.Fatalf("read specs/README.md: %v", err)
		}
		registryContent, err := os.ReadFile(filepath.Join(root, "planning", "specs", "specification-ownership-registry.md"))
		if err != nil {
			t.Fatalf("read specification-ownership-registry.md: %v", err)
		}
		researchFiles := realResearchFiles(t, root)

		violations := Check(string(specContent), researchFiles, string(readmeContent), string(registryContent))
		if len(violations) != 0 {
			t.Fatalf("expected zero violations against the real repository, got %d:\n%s", len(violations), joinViolations(violations))
		}
	})

	t.Run("valid fixture has no violation besides its deliberate three-row size", func(t *testing.T) {
		// The fixture only has three rows (not fifty) so both tables
		// always report row_count; that is the one expected violation
		// when everything else about the fixture is internally
		// consistent.
		violations := Check(validFixtureSpec(), fixtureResearchFiles(), fixtureReadme(), fixtureRegistry())
		for _, v := range violations {
			if v.Kind != "row_count" {
				t.Errorf("unexpected violation on the otherwise-valid fixture: %s", v)
			}
		}
		if !hasKind(violations, "row_count") {
			t.Fatalf("expected row_count violations for the three-row fixture, got:\n%s", joinViolations(violations))
		}
	})

	t.Run("matrix row has no research file", func(t *testing.T) {
		files := []string{"alabama.md", "alaska.md"} // arizona.md missing
		violations := Check(validFixtureSpec(), files, fixtureReadme(), fixtureRegistry())
		if !hasKind(violations, "missing_research_file") {
			t.Fatalf("expected a missing_research_file violation, got:\n%s", joinViolations(violations))
		}
	})

	t.Run("research file has no matrix row", func(t *testing.T) {
		files := append(append([]string{}, fixtureResearchFiles()...), "colorado.md")
		violations := Check(validFixtureSpec(), files, fixtureReadme(), fixtureRegistry())
		if !hasKind(violations, "orphan_research_file") {
			t.Fatalf("expected an orphan_research_file violation, got:\n%s", joinViolations(violations))
		}
	})

	t.Run("cell holds a value outside the legend", func(t *testing.T) {
		spec := strings.Replace(validFixtureSpec(), "| AZ    | F      | Y          |", "| AZ    | X      | Y          |", 1)
		violations := Check(spec, fixtureResearchFiles(), fixtureReadme(), fixtureRegistry())
		if !hasKind(violations, "bad_cell") {
			t.Fatalf("expected a bad_cell violation, got:\n%s", joinViolations(violations))
		}
	})

	t.Run("stated column total disagrees with the counted cells", func(t *testing.T) {
		spec := strings.Replace(validFixtureSpec(), "NOTICE          1      WAGE_FLOOR 2", "NOTICE          5      WAGE_FLOOR 2", 1)
		violations := Check(spec, fixtureResearchFiles(), fixtureReadme(), fixtureRegistry())
		if !hasKind(violations, "bad_total") {
			t.Fatalf("expected a bad_total violation, got:\n%s", joinViolations(violations))
		}
	})

	t.Run("LOCAL column's preempted total disagrees with the counted cells", func(t *testing.T) {
		spec := strings.Replace(validFixtureSpec(), "LOCAL overlay 1      LOCAL preempted 1", "LOCAL overlay 1      LOCAL preempted 9", 1)
		violations := Check(spec, fixtureResearchFiles(), fixtureReadme(), fixtureRegistry())
		if !hasKind(violations, "bad_total") {
			t.Fatalf("expected a bad_total violation for the LOCAL preempted total, got:\n%s", joinViolations(violations))
		}
	})

	t.Run("spec absent from specs/README.md", func(t *testing.T) {
		violations := Check(validFixtureSpec(), fixtureResearchFiles(), "no reference here", fixtureRegistry())
		if !hasKind(violations, "missing_readme_index") {
			t.Fatalf("expected a missing_readme_index violation, got:\n%s", joinViolations(violations))
		}
	})

	t.Run("spec absent from specification-ownership-registry.md", func(t *testing.T) {
		violations := Check(validFixtureSpec(), fixtureResearchFiles(), fixtureReadme(), "no row here")
		if !hasKind(violations, "missing_ownership_row") {
			t.Fatalf("expected a missing_ownership_row violation, got:\n%s", joinViolations(violations))
		}
	})

	t.Run("ownership row names no accountable role or review trigger", func(t *testing.T) {
		registry := "| `legal-rule-packs-and-state-configuration.md` | TBD | `DRAFT_CONTRACT` | TBD |\n"
		violations := Check(validFixtureSpec(), fixtureResearchFiles(), fixtureReadme(), registry)
		if !hasKind(violations, "missing_owner") {
			t.Errorf("expected a missing_owner violation, got:\n%s", joinViolations(violations))
		}
		if !hasKind(violations, "missing_review_trigger") {
			t.Errorf("expected a missing_review_trigger violation, got:\n%s", joinViolations(violations))
		}
	})
}

// TestTodo_LEGAL_008_Golden pins the real spec's Section 5 matrix at its
// currently-correct shape and numbers, so an edit to the matrix that
// forgets to update a row, a cell or a column total is caught by an exact
// diff here, not just by the more general PRIMARY test above.
func TestTodo_LEGAL_008_Golden(t *testing.T) {
	root := repoRoot(t)
	specContent, err := os.ReadFile(filepath.Join(root, "planning", "specs", "legal-rule-packs-and-state-configuration.md"))
	if err != nil {
		t.Fatalf("read spec: %v", err)
	}

	m, violations := Parse(string(specContent))
	if len(violations) != 0 {
		t.Fatalf("Parse reported violations on the real spec: %v", violations)
	}

	if m.tableA == nil || m.tableB == nil {
		t.Fatalf("expected both tables to parse, tableA=%v tableB=%v", m.tableA, m.tableB)
	}
	if len(m.tableA.rows) != 50 {
		t.Fatalf("table A has %d rows, want 50", len(m.tableA.rows))
	}
	if len(m.tableB.rows) != 50 {
		t.Fatalf("table B has %d rows, want 50", len(m.tableB.rows))
	}

	wantColumnsA := []string{
		"NOTICE", "PAY_TRANSP", "FIELD_RESTR", "WAGE_FLOOR", "PAY_FREQ",
		"PAY_STMT", "LEAVE", "NON_COMPETE", "CLASSIFN", "PAY_EQUITY",
		"RETENTION", "PERSONNEL_FILE",
	}
	if !equalStrings(m.tableA.columns, wantColumnsA) {
		t.Fatalf("table A columns = %v, want %v", m.tableA.columns, wantColumnsA)
	}

	wantColumnsB := []string{
		"FINAL_PAY", "MINI_WARN", "SEP_FILING", "E_VERIFY", "DRUG_TEST",
		"ANTI_RETAL", "JOB_SEC", "BREACH", "AUTO_DEC", "LOCAL",
	}
	if !equalStrings(m.tableB.columns, wantColumnsB) {
		t.Fatalf("table B columns = %v, want %v", m.tableB.columns, wantColumnsB)
	}

	if m.tableA.rows[0].state != "AL" || m.tableA.rows[len(m.tableA.rows)-1].state != "WY" {
		t.Fatalf("table A row order starts %q ends %q, want AL...WY", m.tableA.rows[0].state, m.tableA.rows[len(m.tableA.rows)-1].state)
	}
	if m.tableB.rows[0].state != "AL" || m.tableB.rows[len(m.tableB.rows)-1].state != "WY" {
		t.Fatalf("table B row order starts %q ends %q, want AL...WY", m.tableB.rows[0].state, m.tableB.rows[len(m.tableB.rows)-1].state)
	}

	wantTotalsA := map[string]int{
		"NOTICE": 22, "PAY_TRANSPARENCY": 16, "FIELD_RESTRICTION": 20,
		"WAGE_FLOOR": 30, "PAY_FREQUENCY": 31, "PAY_STATEMENT": 18,
		"LEAVE": 28, "NON_COMPETE": 35, "CLASSIFICATION": 10,
		"PAY_EQUITY": 36, "RETENTION": 31, "PERSONNEL_FILE": 18,
	}
	for label, want := range wantTotalsA {
		if got := m.totalsA[label]; got != want {
			t.Errorf("table A stated total %s = %d, want %d", label, got, want)
		}
	}

	wantTotalsB := map[string]int{
		"FINAL_PAY_DEADLINE": 44, "MINI_WARN": 17, "SEPARATION_FILING": 9,
		"E_VERIFY": 17, "DRUG_TEST": 13, "ANTI_RETALIATION": 50,
		"JOB_SECURITY": 4, "BREACH": 49, "AUTOMATED_DECISION": 3,
		"LOCAL overlay": 16, "LOCAL preempted": 5,
	}
	for label, want := range wantTotalsB {
		if got := m.totalsB[label]; got != want {
			t.Errorf("table B stated total %q = %d, want %d", label, got, want)
		}
	}

	// Re-run the full cross-check against the real research directory and
	// registries: the golden numbers above are worthless if Check itself
	// finds something wrong with them.
	readmeContent, err := os.ReadFile(filepath.Join(root, "planning", "specs", "README.md"))
	if err != nil {
		t.Fatalf("read specs/README.md: %v", err)
	}
	registryContent, err := os.ReadFile(filepath.Join(root, "planning", "specs", "specification-ownership-registry.md"))
	if err != nil {
		t.Fatalf("read specification-ownership-registry.md: %v", err)
	}
	researchFiles := realResearchFiles(t, root)
	if got := Check(string(specContent), researchFiles, string(readmeContent), string(registryContent)); len(got) != 0 {
		t.Fatalf("Check found %d violation(s) against the pinned-good real spec:\n%s", len(got), joinViolations(got))
	}
}

// FuzzLegalMatrixParse proves the parser never panics on arbitrary input,
// however malformed: Parse and Check must always return, reporting
// violations rather than crashing.
func FuzzLegalMatrixParse(f *testing.F) {
	f.Add(validFixtureSpec())
	f.Add("")
	f.Add("### 5.1 Table A\n| State |\n| --- |\n| AL |")
	f.Add("### 5.1 Table A\n### 5.2 Table B\nColumn totals\n```text\nNOTICE\n```")
	f.Add("| a | b\n|-|-|-|\nnot a table at all")
	f.Add("garbage \x00\xff\ndata\t\r\n")
	f.Add("### 5.1 Table A\n| State | X |\n| --- | --- |\n|  |  |\n")

	f.Fuzz(func(t *testing.T, spec string) {
		m, violations := Parse(spec)
		if m == nil {
			t.Fatalf("Parse returned a nil Matrix for input %q", spec)
		}
		_ = violations

		got := Check(spec, []string{"alabama.md", "does-not-exist.md"}, spec, spec)
		for _, v := range got {
			if v.Kind == "" {
				t.Fatalf("Check returned a violation with an empty Kind for input %q", spec)
			}
		}
	})
}

// repoRoot returns the repository root by walking up from the current
// working directory (tools/planning/legalmatrix under `go test`) until
// planning/todos.md is found.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "planning", "archive", "todos-2026-10-22.md")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repository root (planning/archive/todos-2026-10-22.md) above %s", dir)
		}
		dir = parent
	}
}

// realResearchFiles lists the basenames of every Markdown research file
// directly under planning/research/state-employment-law, excluding
// README.md.
func realResearchFiles(t *testing.T, root string) []string {
	t.Helper()
	dir := filepath.Join(root, "planning", "research", "state-employment-law")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir %s: %v", dir, err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") || e.Name() == "README.md" {
			continue
		}
		files = append(files, e.Name())
	}
	sort.Strings(files)
	return files
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func joinViolations(vs []Violation) string {
	var b strings.Builder
	for _, v := range vs {
		b.WriteString(v.String())
		b.WriteByte('\n')
	}
	return b.String()
}
