package gateevidence

import (
	"testing"
	"time"
)

func mustParseDate(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(dateLayout, s)
	if err != nil {
		t.Fatalf("parse date %q: %v", s, err)
	}
	return ts
}

func baseManifest() P1AManifest {
	return P1AManifest{
		SchemaVersion:           1,
		Release:                 "P1A",
		TodoID:                  "NEXT-002",
		SignedDate:              "2026-09-03",
		FreshnessWindowDays:     14,
		ForbiddenImportPrefixes: []string{"github.com/monstercameron/human-capital-management-suite/internal/connectivity/writeadapters"},
	}
}

// TestP1AEvidenceCompilerRejectsMissingStaleOutOfManifestOrEffectfulEvidence
// is NEXT-003's named TEST. Each RED subtest proves one rejection category;
// the final subtests prove the GREEN path (a clean entry compiles OK, and a
// manifest of only clean entries compiles GATE_CLEAR).
func TestP1AEvidenceCompilerRejectsMissingStaleOutOfManifestOrEffectfulEvidence(t *testing.T) {
	now := mustParseDate(t, "2026-09-10")

	t.Run("RED: missing test function", func(t *testing.T) {
		m := baseManifest()
		m.Evidence = []EvidenceEntry{{TodoID: "X-001", Test: "TestDoesNotExist", Package: "testdata/cleanpkg"}}

		report, err := Compile(m, nil, CompileOptions{RepoRoot: ".", Now: now})
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		requireVerdict(t, report, "TestDoesNotExist", VerdictMissing)
		if report.Decision != GateDecisionBlocked {
			t.Errorf("Decision = %s, want %s", report.Decision, GateDecisionBlocked)
		}
	})

	t.Run("RED: missing result (test exists, no checked-in record)", func(t *testing.T) {
		m := baseManifest()
		m.Evidence = []EvidenceEntry{{TodoID: "X-002", Test: "TestCleanPackageProbe", Package: "testdata/cleanpkg"}}

		report, err := Compile(m, nil, CompileOptions{RepoRoot: ".", Now: now})
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		requireVerdict(t, report, "TestCleanPackageProbe", VerdictMissing)
	})

	t.Run("RED: non-PASS checked-in result", func(t *testing.T) {
		m := baseManifest()
		m.Evidence = []EvidenceEntry{{TodoID: "X-003", Test: "TestCleanPackageProbe", Package: "testdata/cleanpkg"}}
		results := []ResultRecord{{Test: "TestCleanPackageProbe", Package: "testdata/cleanpkg", Result: "FAIL", Timestamp: "2026-09-09"}}

		report, err := Compile(m, results, CompileOptions{RepoRoot: ".", Now: now})
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		requireVerdict(t, report, "TestCleanPackageProbe", VerdictMissing)
	})

	t.Run("RED: stale result older than the freshness window", func(t *testing.T) {
		m := baseManifest()
		m.Evidence = []EvidenceEntry{{TodoID: "X-004", Test: "TestCleanPackageProbe", Package: "testdata/cleanpkg"}}
		results := []ResultRecord{{Test: "TestCleanPackageProbe", Package: "testdata/cleanpkg", Result: "PASS", Timestamp: "2026-08-01"}}

		report, err := Compile(m, results, CompileOptions{RepoRoot: ".", Now: now})
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		requireVerdict(t, report, "TestCleanPackageProbe", VerdictStale)
	})

	t.Run("RED: out-of-manifest result (recorded against a different package)", func(t *testing.T) {
		m := baseManifest()
		m.Evidence = []EvidenceEntry{{TodoID: "X-005", Test: "TestCleanPackageProbe", Package: "testdata/cleanpkg"}}
		results := []ResultRecord{{Test: "TestCleanPackageProbe", Package: "testdata/someotherpkg", Result: "PASS", Timestamp: "2026-09-09"}}

		report, err := Compile(m, results, CompileOptions{RepoRoot: ".", Now: now})
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		requireVerdict(t, report, "TestCleanPackageProbe", VerdictOutOfManifest)
	})

	t.Run("RED: effect-bearing evidence (package imports a forbidden path)", func(t *testing.T) {
		m := baseManifest()
		m.Evidence = []EvidenceEntry{{TodoID: "X-006", Test: "TestEffectfulPackageProbe", Package: "testdata/effectfulpkg"}}
		results := []ResultRecord{{Test: "TestEffectfulPackageProbe", Package: "testdata/effectfulpkg", Result: "PASS", Timestamp: "2026-09-09"}}

		report, err := Compile(m, results, CompileOptions{RepoRoot: ".", Now: now})
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		requireVerdict(t, report, "TestEffectfulPackageProbe", VerdictEffectful)
	})

	t.Run("GREEN: a fresh, in-manifest, non-effectful PASS compiles OK", func(t *testing.T) {
		m := baseManifest()
		m.Evidence = []EvidenceEntry{{TodoID: "X-007", Test: "TestCleanPackageProbe", Package: "testdata/cleanpkg"}}
		results := []ResultRecord{{Test: "TestCleanPackageProbe", Package: "testdata/cleanpkg", Result: "PASS", Timestamp: "2026-09-09"}}

		report, err := Compile(m, results, CompileOptions{RepoRoot: ".", Now: now})
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		requireVerdict(t, report, "TestCleanPackageProbe", VerdictOK)
		if report.Decision != GateDecisionClear {
			t.Errorf("Decision = %s, want %s", report.Decision, GateDecisionClear)
		}
	})

	t.Run("GREEN: one bad entry blocks the whole gate even when others are clean", func(t *testing.T) {
		m := baseManifest()
		m.Evidence = []EvidenceEntry{
			{TodoID: "X-008", Test: "TestCleanPackageProbe", Package: "testdata/cleanpkg"},
			{TodoID: "X-009", Test: "TestEffectfulPackageProbe", Package: "testdata/effectfulpkg"},
		}
		results := []ResultRecord{
			{Test: "TestCleanPackageProbe", Package: "testdata/cleanpkg", Result: "PASS", Timestamp: "2026-09-09"},
			{Test: "TestEffectfulPackageProbe", Package: "testdata/effectfulpkg", Result: "PASS", Timestamp: "2026-09-09"},
		}

		report, err := Compile(m, results, CompileOptions{RepoRoot: ".", Now: now})
		if err != nil {
			t.Fatalf("Compile: %v", err)
		}
		if report.Decision != GateDecisionBlocked {
			t.Errorf("Decision = %s, want %s", report.Decision, GateDecisionBlocked)
		}
		requireVerdict(t, report, "TestCleanPackageProbe", VerdictOK)
		requireVerdict(t, report, "TestEffectfulPackageProbe", VerdictEffectful)
	})
}

// TestCompileLiveModeUsesInjectedRunGoTest proves Live:true routes through
// CompileOptions.RunGoTest instead of the checked-in results file, and that
// its returned ResultRecord is still subject to the same freshness and
// pass/fail rules as a checked-in one.
func TestCompileLiveModeUsesInjectedRunGoTest(t *testing.T) {
	now := mustParseDate(t, "2026-09-10")
	var calledPkg, calledTest string

	m := baseManifest()
	m.Evidence = []EvidenceEntry{{TodoID: "X-010", Test: "TestCleanPackageProbe", Package: "testdata/cleanpkg"}}

	fake := func(repoRoot, pkg, name string, now time.Time) (ResultRecord, error) {
		calledPkg, calledTest = pkg, name
		return ResultRecord{Test: name, Package: pkg, Result: "PASS", Timestamp: now.Format(dateLayout)}, nil
	}

	report, err := Compile(m, []ResultRecord{{Test: "TestCleanPackageProbe", Package: "testdata/cleanpkg", Result: "FAIL", Timestamp: "2020-01-01"}}, CompileOptions{
		RepoRoot:  ".",
		Now:       now,
		Live:      true,
		RunGoTest: fake,
	})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if calledPkg != "testdata/cleanpkg" || calledTest != "TestCleanPackageProbe" {
		t.Fatalf("RunGoTest called with (%q, %q), want (testdata/cleanpkg, TestCleanPackageProbe)", calledPkg, calledTest)
	}
	// The checked-in FAIL/stale record must be ignored entirely in Live
	// mode - only the fake's fresh PASS counts.
	requireVerdict(t, report, "TestCleanPackageProbe", VerdictOK)
}

// TestDefaultRunGoTestExecutesRealGoTest exercises the real `go test`
// invocation path (not the CompileOptions.Live gate itself, which the
// table test above already covers with a fake) against the harmless
// cleanpkg fixture, proving DefaultRunGoTest's command construction and
// PASS/FAIL classification work against the real go tool.
func TestDefaultRunGoTestExecutesRealGoTest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping a real go test subprocess invocation in -short mode")
	}
	now := mustParseDate(t, "2026-09-10")

	// The real toolchain invocation below is subject to transient Windows
	// process-spawn/file-lock pressure when many suites build in parallel
	// (the same unlinkat/Access-denied class AGENTS.md already tolerates
	// in gate output parsing). A broken command construction fails every
	// attempt, so retrying the invocation without touching any assertion
	// absorbs only infra flakes, never a product regression.
	var record ResultRecord
	var err error
	for attempt := 1; ; attempt++ {
		record, err = DefaultRunGoTest(".", "./testdata/cleanpkg", "TestCleanPackageProbe", now)
		if err == nil && record.Result == "PASS" {
			break
		}
		if attempt == 3 {
			t.Fatalf("DefaultRunGoTest: %v (result %q after 3 attempts)", err, record.Result)
		}
	}
	if record.Result != "PASS" {
		t.Errorf("Result = %s, want PASS", record.Result)
	}
	if record.Timestamp != "2026-09-10" {
		t.Errorf("Timestamp = %s, want 2026-09-10", record.Timestamp)
	}
}

func TestClassifyGoTestRunRequiresNamedPassAndRejectsOtherFailures(t *testing.T) {
	pass := "{\"Action\":\"pass\",\"Test\":\"TestCleanPackageProbe\"}\n{\"Action\":\"pass\"}\n"
	if got := classifyGoTestRun(pass, true, "TestCleanPackageProbe", false); got != "PASS" {
		t.Fatalf("named package pass = %q", got)
	}
	for _, output := range []string{"{\"Action\":\"pass\"}\n", pass + "{\"Action\":\"fail\"}\n"} {
		if got := classifyGoTestRun(output, true, "TestCleanPackageProbe", false); got != "FAIL" {
			t.Fatalf("incomplete/failed run = %q", got)
		}
	}
	cleanup := pass + "go: unlinkat C:\\Temp\\go-build1\\b001\\cleanpkg.test.exe: Access is denied.\n"
	if got := classifyGoTestRun(cleanup, false, "TestCleanPackageProbe", true); got != "PASS" {
		t.Fatalf("Windows post-pass cleanup = %q", got)
	}
	if got := classifyGoTestRun(cleanup, false, "TestCleanPackageProbe", false); got != "FAIL" {
		t.Fatalf("non-Windows error = %q", got)
	}
}

func requireVerdict(t *testing.T, report *Report, test string, want Verdict) {
	t.Helper()
	for _, f := range report.Findings {
		if f.Test == test {
			if f.Verdict != want {
				t.Errorf("finding for %s: verdict = %s, want %s (detail: %s)", test, f.Verdict, want, f.Detail)
			}
			return
		}
	}
	t.Fatalf("no finding for test %s", test)
}

// TestCompileRealP1AManifestAgainstCheckedInResultsIsGateClear compiles the
// real, signed definitions/planning/gates/p1a-manifest.yaml against the
// real checked-in definitions/planning/gates/p1a-evidence-results.json and
// proves it is GATE_CLEAR as of the manifest's own signed date - the same
// invocation `p1aevidence` would make in non-live mode.
func TestCompileRealP1AManifestAgainstCheckedInResultsIsGateClear(t *testing.T) {
	m := mustLoadP1AManifest(t)
	results, err := LoadResults(repoRoot + "/definitions/planning/gates/p1a-evidence-results.json")
	if err != nil {
		t.Fatalf("LoadResults: %v", err)
	}

	report, err := Compile(m, results, CompileOptions{RepoRoot: repoRoot, Now: mustParseDate(t, "2026-09-03")})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	if report.Decision != GateDecisionClear {
		for _, f := range report.Findings {
			if f.Verdict != VerdictOK {
				t.Logf("blocking finding: %+v", f)
			}
		}
		t.Fatalf("Decision = %s, want %s", report.Decision, GateDecisionClear)
	}
	if len(report.Findings) != len(m.Evidence) {
		t.Errorf("got %d findings, want %d (one per manifest evidence entry)", len(report.Findings), len(m.Evidence))
	}
}
