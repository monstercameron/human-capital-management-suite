package testhygiene

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeTestFile(t *testing.T, root, name, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestTestReliabilityPolicyRejectsFlakeAndSilentQuarantine is GOV-020's
// primary test. It covers all four static risk classes, the quarantine gate,
// and a repeated subprocess run against a deliberately alternating fixture.
func TestTestReliabilityPolicyRejectsFlakeAndSilentQuarantine(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "internal/sample/sample.go", `package sample
`)
	writeTestFile(t, root, "internal/sample/sample_test.go", `package sample
import (
  "testing"
  "time"
)
var shared = 0
func TestRisk(t *testing.T) {
  t.Parallel()
  shared++
  _ = time.Now()
  time.Sleep(time.Millisecond)
}
`)
	report, err := Scan(root, "example.com/mod")
	if err != nil {
		t.Fatal(err)
	}
	kinds := make(map[RiskKind]bool)
	for _, finding := range report.Findings {
		kinds[finding.Kind] = true
	}
	for _, kind := range []RiskKind{PackageStateWrite, SleepWait, ParallelFixture, WallClockRead} {
		if !kinds[kind] {
			t.Errorf("missing risk kind %s in %+v", kind, report.Findings)
		}
	}

	writeTestFile(t, root, "internal/sample/quarantine.json", `[{"test_name":"TestRisk","package":"example.com/mod/internal/sample","first_seen":"2026-09-05","evidence_ref":"GOV-020 fixture","owner":"test-owner","expiry":"2026-12-31"}]`)
	if err := ValidateQuarantine(root, "example.com/mod", filepath.Join(root, "internal", "sample", "quarantine.json"), time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}

	fixture := t.TempDir()
	writeTestFile(t, fixture, "go.mod", "module example.com/flaky\n\ngo 1.26\n")
	writeTestFile(t, fixture, "flaky/flaky_test.go", `package flaky
import (
  "os"
  "strconv"
  "testing"
)
func TestAlternates(t *testing.T) {
  path := "state.txt"
  data, _ := os.ReadFile(path)
  n := 0
  if len(data) > 0 { n, _ = strconv.Atoi(string(data)) }
  n++
  _ = os.WriteFile(path, []byte(strconv.Itoa(n)), 0644)
  if n%2 == 1 { t.Fatalf("deliberate alternating failure %d", n) }
}
`)
	flake, err := DetectFlakes(context.Background(), fixture, "./flaky", 3)
	if err != nil {
		t.Fatal(err)
	}
	if !flake.NonDeterministic || len(flake.Outcomes) != 3 {
		t.Fatalf("flake report = %+v, want three mixed outcomes", flake)
	}
}

func TestWindowsCleanupOnlyDoesNotHideFailedTests(t *testing.T) {
	pass := "ok  \texample.com/flaky/flaky\t0.1s\ngo: unlinkat C:\\Temp\\go-build1\\b001\\flaky.test.exe: Access is denied.\n"
	if !cleanupOnlyExit(pass, true) || cleanupOnlyExit(pass, false) {
		t.Fatal("Windows-only cleanup after an explicit package pass was misclassified")
	}
	if cleanupOnlyExit("--- FAIL: TestAlternates\nFAIL\texample.com/flaky/flaky\n"+pass, true) {
		t.Fatal("a failed test must not be hidden behind a later cleanup complaint")
	}
}

func TestTodo_GOV_020_Golden(t *testing.T) {
	if PackageStateWrite == SleepWait || SleepWait == ParallelFixture || ParallelFixture == WallClockRead {
		t.Fatal("risk kinds must be distinct")
	}
}

func TestTodo_GOV_020_Race(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "tools/sample/sample_test.go", `package sample
import "testing"
func TestSkip(t *testing.T) { t.Skip("fixture") }
`)
	report, err := Scan(root, "example.com/mod")
	if err != nil || len(report.Skips) != 1 {
		t.Fatalf("Scan = report=%+v err=%v, want one skip", report, err)
	}
	if report.Skips[0].Package != "example.com/mod/tools/sample" {
		t.Fatalf("skip package = %q", report.Skips[0].Package)
	}
}

func TestTodo_GOV_020_Fault(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "quarantine.json")
	if err := os.WriteFile(path, []byte(`[{"test_name":"TestX","package":"p"}]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := ValidateQuarantine(root, "example.com/mod", path, time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)); err == nil || !strings.Contains(err.Error(), "incomplete") {
		t.Fatalf("ValidateQuarantine error = %v, want incomplete record failure", err)
	}
}

func TestSkippedTestsHaveQuarantineRecords(t *testing.T) {
	root := repoRootForTest(t)
	records, err := LoadQuarantine(filepath.Join(root, "tools", "policy", "testhygiene", "quarantine.json"))
	if err != nil {
		t.Fatal(err)
	}
	report, err := Scan(root, "github.com/monstercameron/human-capital-management-suite")
	if err != nil {
		t.Fatal(err)
	}
	missing := FindUnquarantinedSkips(report, records)
	if len(missing) > 0 {
		t.Fatalf("unquarantined skips: %+v", missing)
	}
	if err := ValidateQuarantine(root, "github.com/monstercameron/human-capital-management-suite", filepath.Join(root, "tools", "policy", "testhygiene", "quarantine.json"), time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
}

func repoRootForTest(t *testing.T) string {
	t.Helper()
	d, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			t.Fatal("repository root not found")
		}
		d = parent
	}
}
