package racepolicy_test

import (
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/racepolicy"
)

const concurrentSource = `package pkga

import "sync"

var mu sync.Mutex

func Do() {
	mu.Lock()
	defer mu.Unlock()
}
`

// TestTodo_TOOL_012 is the TOOL-012 primary test for this package's own
// mechanism (declared-concurrent-package detection plus the Linux
// race-runnability check), proven against synthetic fixtures so it passes
// deterministically on this host without -race and without depending on
// the live repository's own ever-changing file set (see
// TestTodo_TOOL_012_Golden for the live-repository snapshot).
func TestTodo_TOOL_012(t *testing.T) {
	t.Run("RED: a concurrent package whose only test is excluded by !race is a violation", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "pkga/a.go", concurrentSource)
		writeFile(t, root, "pkga/a_test.go", `//go:build !race

package pkga

import "testing"

func TestDo(t *testing.T) { Do() }
`)
		report, err := racepolicy.Evaluate(root, "example.com/mod")
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if len(report.Findings) != 1 {
			t.Fatalf("Findings = %+v, want exactly one concurrent package declared", report.Findings)
		}
		if !report.Findings[0].Violation() {
			t.Fatalf("Findings[0] = %+v, want a violation: a //go:build !race test file is excluded from the CI race context", report.Findings[0])
		}
		violations := report.Violations()
		if len(violations) != 1 || violations[0].Package.ImportPath != "example.com/mod/pkga" {
			t.Errorf("Violations() = %+v, want exactly example.com/mod/pkga", violations)
		}
	})

	t.Run("GREEN: a race-tagged test resolves in the CI race context", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "pkga/a.go", concurrentSource)
		writeFile(t, root, "pkga/a_test.go", `//go:build race

package pkga

import "testing"

func TestDo(t *testing.T) { Do() }
`)
		report, err := racepolicy.Evaluate(root, "example.com/mod")
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if len(report.Findings) != 1 || report.Findings[0].Violation() {
			t.Fatalf("Findings = %+v, want one package with a Linux-raceable test", report.Findings)
		}
	})

	t.Run("RED: a concurrent package with no test file at all is a violation", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "pkga/worker_linux.go", concurrentSource)

		report, err := racepolicy.Evaluate(root, "example.com/mod")
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if len(report.Violations()) != 1 {
			t.Fatalf("Violations() = %+v, want exactly one (no test file exists)", report.Violations())
		}
	})

	t.Run("GREEN: a concurrent package with an ordinary test file passes", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "pkga/a.go", concurrentSource)
		writeFile(t, root, "pkga/a_test.go", `package pkga

import "testing"

func TestDo(t *testing.T) { Do() }
`)
		report, err := racepolicy.Evaluate(root, "example.com/mod")
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if len(report.Findings) != 1 {
			t.Fatalf("Findings = %+v, want exactly one concurrent package declared", report.Findings)
		}
		if report.Findings[0].Violation() {
			t.Fatalf("Findings[0] = %+v, want no violation: an ordinary test file resolves under linux/amd64 and would run under -race on CI", report.Findings[0])
		}
		if len(report.Violations()) != 0 {
			t.Errorf("Violations() = %+v, want none", report.Violations())
		}
	})

	t.Run("GREEN: a non-concurrent package is never declared, so it cannot be a violation", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "pkgb/b.go", `package pkgb

func Do() {}
`)
		report, err := racepolicy.Evaluate(root, "example.com/mod")
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if len(report.Findings) != 0 {
			t.Fatalf("Findings = %+v, want none (pkgb uses no concurrency primitive)", report.Findings)
		}
	})

	t.Run("GREEN: an external test package (_test suffix) still counts as a raceable test", func(t *testing.T) {
		root := t.TempDir()
		writeFile(t, root, "pkga/a.go", concurrentSource)
		writeFile(t, root, "pkga/a_test.go", `package pkga_test

import (
	"testing"

	"example.com/mod/pkga"
)

func TestDo(t *testing.T) { pkga.Do() }
`)
		report, err := racepolicy.Evaluate(root, "example.com/mod")
		if err != nil {
			t.Fatalf("Evaluate: %v", err)
		}
		if len(report.Violations()) != 0 {
			t.Errorf("Violations() = %+v, want none (an XTestGoFiles entry still resolves under linux/amd64)", report.Violations())
		}
	})
}

// TestTodo_TOOL_012_Golden pins Evaluate's result against this repository's
// own live tree: it is a snapshot proving the mechanism at scale (every
// concurrent package it finds, and exactly which ones currently violate
// the policy), and forces a deliberate update — rather than silent staleness
// — whenever that set changes. See doc.go's "Known live-repository finding"
// section is empty because the security test wave closed the previously
// recorded internal/authn/federation gap.
func TestTodo_TOOL_012_Golden(t *testing.T) {
	root := repopath.RootDir()
	modulePath := repopath.ModulePath(root)

	report, err := racepolicy.Evaluate(root, modulePath)
	if err != nil {
		t.Fatalf("Evaluate(%s): %v", root, err)
	}
	if len(report.Findings) == 0 {
		t.Fatal("Evaluate found zero concurrent packages in the live repository; expected at least the known transaction/workflow/queue/cache/connector suites TOOL-012 names")
	}
	t.Logf("Evaluate declared %d concurrent packages", len(report.Findings))

	wantViolations := []string{}
	violations := report.Violations()
	if len(violations) != len(wantViolations) {
		names := make([]string, len(violations))
		for i, v := range violations {
			names[i] = v.Package.ImportPath + ": " + v.Detail
		}
		t.Fatalf("Violations() = %v, want exactly %v\n(if this is a NEW package, add a test suite for it and update this golden list; if it is %s having been fixed, remove it from this golden list)", names, wantViolations, wantViolations)
	}
	for i, v := range violations {
		if v.Package.ImportPath != wantViolations[i] {
			t.Errorf("Violations()[%d] = %s, want %s", i, v.Package.ImportPath, wantViolations[i])
		}
	}
}
