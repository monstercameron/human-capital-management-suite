package federalbaseline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// validBaseline is a minimal, internally-consistent stand-in for
// us-federal.md's Section 1 summary bullets: just the four sentences
// ParseBaseline depends on, in their real shape, with the real figures.
func validBaseline() string {
	return `## 1. Summary for Human Capital Management Suite

- WARN Act (60 days' notice) applies to employers 100+ employees; triggered by plant closing 50+ or mass layoff 50+ employees or 33%+ of workforce.
- FLSA minimum wage $7.25/hour (unchanged since 2009), overtime required at 1.5x after 40 hours/week.
- FLSA: retain payroll 3 years, supplementary time records 2 years.
- FMLA: 12 weeks/year unpaid leave, 1,250 hours in 12 months, for employers 50+ within 75 miles.
`
}

func TestFederalBaselineIsStatedOnce(t *testing.T) {
	t.Run("RED_baseline_missing_a_required_sentence_is_reported", func(t *testing.T) {
		broken := "## 1. Summary for Human Capital Management Suite\n\nNothing useful here.\n"
		_, violations := ParseBaseline(broken)
		if len(violations) != 4 {
			t.Fatalf("ParseBaseline on a baseline with no summary sentences returned %d violation(s), want 4:\n%s", len(violations), joinViolations(violations))
		}
		for _, v := range violations {
			if v.Kind != "missing_baseline_field" {
				t.Errorf("violation kind = %q, want missing_baseline_field", v.Kind)
			}
		}
	})

	t.Run("RED_state_file_disagrees_on_WARN_employer_threshold", func(t *testing.T) {
		state := "No state mini-WARN. Federal WARN Act applies: employers with 50+ employees must provide 60 days' written notice of mass layoffs.\n"
		violations := Check(validBaseline(), map[string]string{"example.md": state})
		if !hasDisagreement(violations, "WARN_EMPLOYER_THRESHOLD", "50", "100") {
			t.Fatalf("expected a WARN_EMPLOYER_THRESHOLD disagreement (50 vs 100), got:\n%s", joinViolations(violations))
		}
	})

	t.Run("RED_state_file_disagrees_on_WARN_notice_days", func(t *testing.T) {
		state := "No state mini-WARN. Federal WARN Act applies: employers with 100+ employees must provide 30 days' written notice of mass layoffs.\n"
		violations := Check(validBaseline(), map[string]string{"example.md": state})
		if !hasDisagreement(violations, "WARN_NOTICE_DAYS", "30", "60") {
			t.Fatalf("expected a WARN_NOTICE_DAYS disagreement (30 vs 60), got:\n%s", joinViolations(violations))
		}
	})

	t.Run("RED_state_file_disagrees_on_FLSA_minimum_wage", func(t *testing.T) {
		state := "No state minimum wage statute; federal FLSA minimum of $8.00/hour applies.\n"
		violations := Check(validBaseline(), map[string]string{"example.md": state})
		if !hasDisagreement(violations, "FLSA_MIN_WAGE", "8.00", "7.25") {
			t.Fatalf("expected an FLSA_MIN_WAGE disagreement (8.00 vs 7.25), got:\n%s", joinViolations(violations))
		}
	})

	t.Run("RED_state_file_disagrees_on_FLSA_retention_years", func(t *testing.T) {
		state := "Employers must retain payroll records for at least 5 years per federal FLSA (29 U.S.C. § 211).\n"
		violations := Check(validBaseline(), map[string]string{"example.md": state})
		if !hasDisagreement(violations, "FLSA_RETENTION_PAYROLL_YEARS", "5", "3") {
			t.Fatalf("expected an FLSA_RETENTION_PAYROLL_YEARS disagreement (5 vs 3), got:\n%s", joinViolations(violations))
		}
	})

	t.Run("RED_state_file_disagrees_on_FMLA_thresholds", func(t *testing.T) {
		state := "Federal FMLA applies to covered employers (50+ employees within 40 miles).\n"
		violations := Check(validBaseline(), map[string]string{"example.md": state})
		if !hasDisagreement(violations, "FMLA_MILE_RADIUS", "40", "75") {
			t.Fatalf("expected an FMLA_MILE_RADIUS disagreement (40 vs 75), got:\n%s", joinViolations(violations))
		}
	})

	t.Run("GREEN_consistent_state_file_reports_nothing", func(t *testing.T) {
		state := "Federal WARN Act applies to employers with 100+ employees requiring 60 days' written notice. " +
			"Federal FLSA minimum of $7.25/hour applies. Employers must retain payroll records for at least 3 years per federal FLSA. " +
			"Federal FMLA applies to covered employers (50+ employees within 75 miles).\n"
		violations := Check(validBaseline(), map[string]string{"example.md": state})
		if len(violations) != 0 {
			t.Fatalf("expected no violations for a fully-consistent state file, got:\n%s", joinViolations(violations))
		}
	})

	t.Run("baseline_file_is_never_compared_against_itself", func(t *testing.T) {
		// A caller that (incorrectly) includes us-federal.md's own
		// content in the stateFiles map must not have it flagged: it
		// restates its own figures by definition.
		violations := Check(validBaseline(), map[string]string{"us-federal.md": validBaseline()})
		if len(violations) != 0 {
			t.Fatalf("us-federal.md compared against itself produced violations:\n%s", joinViolations(violations))
		}
	})
}

func hasDisagreement(violations []Violation, kind, gotValue, wantValue string) bool {
	for _, v := range violations {
		if v.Kind != "federal_baseline_disagreement" {
			continue
		}
		if strings.Contains(v.Detail, kind+"="+gotValue) && strings.Contains(v.Detail, "states "+wantValue) {
			return true
		}
	}
	return false
}

func joinViolations(violations []Violation) string {
	var sb strings.Builder
	for _, v := range violations {
		sb.WriteString(v.String())
		sb.WriteString("\n")
	}
	return sb.String()
}

// TestTodo_LEGAL_017_Golden pins the real us-federal.md baseline at its
// currently-correct parsed figures and asserts the real fifty-state
// corpus scan currently reports zero disagreements. If this test starts
// failing, the failure message lists every disagreement found - that is
// the LEGAL-017 GREEN condition ("the contradiction scan reports zero
// disagreements") re-run against a live, still-edited corpus, not a
// frozen fixture.
func TestTodo_LEGAL_017_Golden(t *testing.T) {
	root := repoRoot(t)
	dir := filepath.Join(root, "planning", "research", "state-employment-law")

	baselinePath := filepath.Join(dir, "us-federal.md")
	baselineContent, err := os.ReadFile(baselinePath)
	if err != nil {
		t.Fatalf("read %s: %v", baselinePath, err)
	}

	b, violations := ParseBaseline(string(baselineContent))
	if len(violations) != 0 {
		t.Fatalf("ParseBaseline reported violations on the real us-federal.md: %v", violations)
	}

	want := Baseline{
		WARNNoticeDays:                  60,
		WARNEmployerThreshold:           100,
		FLSAMinWage:                     "7.25",
		FLSARetentionPayrollYears:       3,
		FLSARetentionSupplementaryYears: 2,
		FMLAWeeks:                       12,
		FMLAHours:                       "1,250",
		FMLAEmployerThreshold:           50,
		FMLAMileRadius:                  75,
	}
	if b != want {
		t.Fatalf("ParseBaseline(us-federal.md) = %+v, want %+v", b, want)
	}

	stateFiles := realStateFiles(t, dir)
	got := Check(string(baselineContent), stateFiles)
	if len(got) != 0 {
		t.Fatalf("federal-baseline scan found %d disagreement(s) in the real corpus:\n%s", len(got), joinViolations(got))
	}
}

// repoRoot returns the repository root by walking up from the current
// working directory until planning/todos.md is found (mirrors
// legalmatrix's repoRoot: each package keeps its own small copy rather
// than sharing a test-only helper package).
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

// realStateFiles reads every Markdown research file directly under dir
// except us-federal.md and README.md, keyed by basename.
func realStateFiles(t *testing.T, dir string) map[string]string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir %s: %v", dir, err)
	}
	files := make(map[string]string, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		if e.Name() == "README.md" || e.Name() == baselineFilename {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		files[e.Name()] = string(content)
	}
	return files
}

// FuzzFederalBaselineExtract proves ExtractRestatements and Check never
// panic on arbitrary input, however malformed.
func FuzzFederalBaselineExtract(f *testing.F) {
	f.Add(validBaseline())
	f.Add("")
	f.Add("federal WARN employers with employees")
	f.Add("federal FMLA 50+ employees within miles")
	f.Add("garbage \x00\xff\ndata\t\r\n")
	f.Add("$7.25 federal minimum wage federal FLSA requires years")

	f.Fuzz(func(t *testing.T, content string) {
		_ = ExtractRestatements("fuzz.md", content)
		_, _ = ParseBaseline(content)
		got := Check(content, map[string]string{"fuzz.md": content})
		for _, v := range got {
			if v.Kind == "" {
				t.Fatalf("Check returned a violation with an empty Kind for input %q", content)
			}
		}
	})
}
