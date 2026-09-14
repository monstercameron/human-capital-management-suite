package covergate

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const sampleOutput = `ok  	github.com/monstercameron/human-capital-management-suite/internal/trust/sod	0.412s	coverage: 81.3% of statements
ok  	github.com/monstercameron/human-capital-management-suite/internal/trust/jit	(cached)	coverage: 64.0% of statements
?   	github.com/monstercameron/human-capital-management-suite/cmd/frontenddev	[no test files]
--- FAIL: TestSomething (0.00s)
    thing_test.go:12: boom
FAIL
FAIL	github.com/monstercameron/human-capital-management-suite/internal/domains/access	1.203s
FAIL	github.com/monstercameron/human-capital-management-suite/internal/broken	[build failed]
ok  	github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes	0.100s	coverage: [no statements]
go: unlinkat C:\Users\x\AppData\Local\Temp\go-build1\b001\sod.test.exe: Access is denied.
FAIL
`

func TestParseGoTestOutput_ClassifiesEveryPackageLineAndIgnoresNoise(t *testing.T) {
	got := ParseGoTestOutput(sampleOutput)
	want := map[string]Result{
		Module + "/internal/trust/sod":              {Status: "ok", Coverage: 81.3, HasCoverage: true},
		Module + "/internal/trust/jit":              {Status: "ok", Coverage: 64.0, HasCoverage: true},
		Module + "/cmd/frontenddev":                 {Status: "notests"},
		Module + "/internal/domains/access":         {Status: "fail"},
		Module + "/internal/broken":                 {Status: "fail"},
		Module + "/internal/engines/canonicalbytes": {Status: "ok", HasCoverage: false},
	}
	if len(got) != len(want) {
		t.Fatalf("parsed %d results, want %d: %+v", len(got), len(want), got)
	}
	for _, r := range got {
		if r.Package == Module+"/internal/domains/access" && (len(r.FailedTests) != 1 || r.FailedTests[0] != "TestSomething" || !strings.Contains(r.Line, "TestSomething")) {
			t.Fatalf("a failing package must carry the names of its failing tests, got %+v", r)
		}
		if r.Package == Module+"/internal/broken" && len(r.FailedTests) != 0 {
			t.Fatalf("a build failure must not inherit another package's failing tests, got %+v", r)
		}
	}
	for _, r := range got {
		w, ok := want[r.Package]
		if !ok {
			t.Fatalf("unexpected package %s", r.Package)
		}
		if r.Status != w.Status || r.HasCoverage != w.HasCoverage || r.Coverage != w.Coverage {
			t.Fatalf("%s = %+v, want %+v", r.Package, r, w)
		}
	}
}

func TestEvaluate_AppliesFloorAndExactUnexpiredExceptionsOnly(t *testing.T) {
	now := time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC)
	cfg := Config{Threshold: 70, Exceptions: []Exception{
		{Package: "internal/trust/jit", Kind: KindBelowFloor, Owner: "trust", Reason: "r", Expiry: "2026-12-31"},
		{Package: "cmd/frontenddev", Kind: KindNoTests, Owner: "fe", Reason: "r", Expiry: "2026-01-01"},
	}}
	findings, waived := Evaluate(cfg, ParseGoTestOutput(sampleOutput), now)
	kinds := map[string]string{}
	for _, f := range findings {
		kinds[f.Package] = f.Kind
	}
	if kinds["internal/domains/access"] != FindingTestFailure || kinds["internal/broken"] != FindingTestFailure {
		t.Fatalf("test failures must always be findings: %+v", findings)
	}
	if kinds["cmd/frontenddev"] != FindingNoTests {
		t.Fatalf("an expired no_tests exception must not waive: %+v", findings)
	}
	if _, ok := kinds["internal/trust/jit"]; ok {
		t.Fatalf("an active below_floor exception must waive: %+v", findings)
	}
	if len(waived) != 1 || waived[0].Package != "internal/trust/jit" {
		t.Fatalf("waived = %+v", waived)
	}
	if _, ok := kinds["internal/trust/sod"]; ok {
		t.Fatal("a package above the floor must not be a finding")
	}
	if _, ok := kinds["internal/engines/canonicalbytes"]; ok {
		t.Fatal("a package with no statements must not be a finding")
	}
	for i := 1; i < len(findings); i++ {
		if findings[i-1].Kind > findings[i].Kind || (findings[i-1].Kind == findings[i].Kind && findings[i-1].Package > findings[i].Package) {
			t.Fatalf("findings must be sorted by kind then package: %+v", findings)
		}
	}
}

func TestEvaluate_ExceptionsNeverWaiveAnotherKindOrAnotherPackage(t *testing.T) {
	now := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	cfg := Config{Threshold: 70, Exceptions: []Exception{
		{Package: "internal/trust/jit", Kind: KindNoTests, Owner: "trust", Reason: "r", Expiry: "2026-12-31"},
		{Package: "internal/trust", Kind: KindBelowFloor, Owner: "trust", Reason: "r", Expiry: "2026-12-31"},
	}}
	findings, _ := Evaluate(cfg, ParseGoTestOutput(sampleOutput), now)
	found := false
	for _, f := range findings {
		if f.Package == "internal/trust/jit" && f.Kind == FindingBelowFloor {
			found = true
		}
	}
	if !found {
		t.Fatalf("a no_tests exception or a parent-package exception must not waive below_floor for jit: %+v", findings)
	}
}

func TestConfigValidate_RefusesIncompleteOrWildcardExceptions(t *testing.T) {
	good := Exception{Package: "cmd/x", Kind: KindNoTests, Owner: "o", Reason: "r", Expiry: "2026-12-31"}
	cases := map[string]Config{
		"zero threshold":  {Threshold: 0},
		"over 100":        {Threshold: 101},
		"wildcard":        {Threshold: 70, Exceptions: []Exception{{Package: "cmd/...", Kind: KindNoTests, Owner: "o", Reason: "r", Expiry: "2026-12-31"}}},
		"unknown kind":    {Threshold: 70, Exceptions: []Exception{{Package: "cmd/x", Kind: "meh", Owner: "o", Reason: "r", Expiry: "2026-12-31"}}},
		"no owner":        {Threshold: 70, Exceptions: []Exception{{Package: "cmd/x", Kind: KindNoTests, Reason: "r", Expiry: "2026-12-31"}}},
		"bad expiry":      {Threshold: 70, Exceptions: []Exception{{Package: "cmd/x", Kind: KindNoTests, Owner: "o", Reason: "r", Expiry: "soon"}}},
		"duplicate entry": {Threshold: 70, Exceptions: []Exception{good, good}},
	}
	for name, cfg := range cases {
		if err := cfg.Validate(); err == nil {
			t.Errorf("%s: expected a validation error", name)
		}
	}
	if err := (Config{Threshold: 70, Exceptions: []Exception{good}}).Validate(); err != nil {
		t.Fatalf("complete config must validate: %v", err)
	}
}

func TestLoadConfig_ReadsTheCheckedInPolicy(t *testing.T) {
	root := repoRoot(t)
	cfg, err := LoadConfig(filepath.Join(root, DefaultConfigPath))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.Threshold < 70 {
		t.Fatalf("the checked-in floor must be at least 70%%, got %v", cfg.Threshold)
	}
}

func TestPackagesFromFiles_KeepsOnlyExistingMeasurableGoDirs(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"internal/a", "internal/b/testdata", "gen/go/x", "src/blocks/go/y", ".gotmp/z"} {
		if err := os.MkdirAll(filepath.Join(root, filepath.FromSlash(d)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got := packagesFromFiles(root, []string{
		"internal/a/a.go", "internal/a/a_test.go", "internal/b/testdata/fx.go", "gen/go/x/x.pb.go",
		"src/blocks/go/y/y.go", ".gotmp/z/z.go", "internal/missing/m.go", "README.md", "internal\\a\\b.go",
	})
	if len(got) != 1 || got[0] != "./internal/a" {
		t.Fatalf("packages = %v, want [./internal/a]", got)
	}
}

func TestAllPackages_ListsMeasurablePackagesFromARelativeRoot(t *testing.T) {
	root := repoRoot(t)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(cwd, root)
	if err != nil {
		t.Fatal(err)
	}
	pkgs, err := AllPackages(rel)
	if err != nil {
		t.Fatalf("all packages: %v", err)
	}
	seen := map[string]bool{}
	for _, p := range pkgs {
		seen[p] = true
		if strings.Contains(p, "/testdata/") || strings.HasPrefix(p, "./gen/") || strings.HasPrefix(p, "./src/blocks/go") {
			t.Fatalf("excluded directory listed: %s", p)
		}
	}
	if !seen["./tools/quality/covergate"] || !seen["./tools/quality/covergate/cmd/covergate"] {
		t.Fatalf("relative root must list this package and its command, got %d packages", len(pkgs))
	}
}

func TestReportFormat_NamesEveryFindingAndTheVerdict(t *testing.T) {
	r := Report{Threshold: 70, Packages: 2, Results: []Result{{HasCoverage: true}}, Findings: []Finding{{Package: "internal/x", Kind: FindingBelowFloor, Detail: "12.0% of statements covered, floor is 70%"}}}
	out := r.Format()
	if !strings.Contains(out, "GAP below_floor internal/x") || !strings.Contains(out, "FAIL 1 finding(s)") {
		t.Fatalf("format = %q", out)
	}
	if pass := (Report{Threshold: 70, Packages: 1}).Format(); !strings.Contains(pass, "PASS 1 package(s)") {
		t.Fatalf("pass format = %q", pass)
	}
}

func TestGate_MeasuresARealPackageAgainstThePolicy(t *testing.T) {
	root := repoRoot(t)
	report, err := Gate(root, DefaultConfigPath, []string{"./tools/quality/covergate/testdata/gatedpkg"}, time.Now(), 5*time.Minute)
	if err != nil {
		t.Fatalf("gate: %v", err)
	}
	if len(report.Results) != 1 || !report.Results[0].HasCoverage || report.Results[0].Coverage != 100 {
		t.Fatalf("gate must measure the fixture at 100%%: %+v", report.Results)
	}
	if len(report.Findings) != 0 {
		t.Fatalf("a fully covered fixture must produce no findings: %+v", report.Findings)
	}
	for _, f := range report.Findings {
		if f.Kind == FindingTestFailure {
			t.Fatalf("the fixture package must not fail: %+v", f)
		}
	}
}

func TestAppendUninstrumentedFindings_PropagatesProductUIFailure(t *testing.T) {
	var report Report
	out := "{\"Action\":\"run\",\"Package\":\"" + Module + "/internal/humanwork/productui\",\"Test\":\"TestInteractionLatencyGate\"}\n" +
		"{\"Action\":\"pass\",\"Package\":\"" + Module + "/internal/humanwork/productui\",\"Test\":\"TestInteractionLatencyGate\"}\n" +
		"{\"Action\":\"fail\",\"Package\":\"" + Module + "/internal/humanwork/productui\",\"Test\":\"TestUnexpectedFailure\"}\n" +
		"{\"Action\":\"run\",\"Package\":\"" + Module + "/internal/humanwork/productui\",\"Test\":\"TestTodo_UXAUDIT_008_Performance\"}\n" +
		"{\"Action\":\"pass\",\"Package\":\"" + Module + "/internal/humanwork/productui\",\"Test\":\"TestTodo_UXAUDIT_008_Performance\"}\n" +
		"{\"Action\":\"run\",\"Package\":\"" + Module + "/internal/humanwork/productui\",\"Test\":\"TestTodo_UXAUDIT_015_Performance\"}\n" +
		"{\"Action\":\"pass\",\"Package\":\"" + Module + "/internal/humanwork/productui\",\"Test\":\"TestTodo_UXAUDIT_015_Performance\"}\n" +
		"{\"Action\":\"run\",\"Package\":\"" + Module + "/internal/humanwork/productui\",\"Test\":\"TestTodo_WEB_039_InteractionP95\"}\n" +
		"{\"Action\":\"pass\",\"Package\":\"" + Module + "/internal/humanwork/productui\",\"Test\":\"TestTodo_WEB_039_InteractionP95\"}\n" +
		"{\"Action\":\"fail\",\"Package\":\"" + Module + "/internal/humanwork/productui\"}\n"
	if err := appendUninstrumentedFindings(&report, out, nil); err != nil {
		t.Fatalf("a parsed failing package must become a finding: %v", err)
	}
	if len(report.Findings) != 1 || report.Findings[0].Package != "internal/humanwork/productui" || report.Findings[0].Kind != FindingTestFailure {
		t.Fatalf("findings = %+v, want productui test failure", report.Findings)
	}
}

func TestAppendUninstrumentedFindings_RefusesMissingResult(t *testing.T) {
	if err := appendUninstrumentedFindings(&Report{}, "go: build failed\n", errors.New("exit status 1")); err == nil {
		t.Fatal("an uninstrumented run without a package result must fail the gate")
	}
}

func TestIncludesProductUIPackage_NormalizesExactAndBroadPatterns(t *testing.T) {
	for _, pkg := range []string{"./internal/humanwork/productui", Module + "/internal/humanwork/productui", "./...", "./internal/..."} {
		if !includesProductUIPackage([]string{pkg}) {
			t.Errorf("%q must include productui", pkg)
		}
	}
	if includesProductUIPackage([]string{"./cmd/..."}) {
		t.Fatal("unrelated command pattern must not include productui")
	}
}

func TestWindowsCleanupError_RejectsAdditionalFatalOutput(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows cleanup behavior is platform-specific")
	}
	err := errors.New("exit status 1")
	clean := "ok  	" + Module + "/internal/humanwork/productui\n" + "go: unlinkat C:\\go-build\\x.test.exe: Access is denied.\n"
	if !isWindowsTestCleanupError(err, clean) {
		t.Fatal("canonical cleanup output must be tolerated")
	}
	if isWindowsTestCleanupError(err, clean+"go: fatal tool error\n") {
		t.Fatal("additional fatal output must not be waived")
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test directory")
		}
		dir = parent
	}
}
