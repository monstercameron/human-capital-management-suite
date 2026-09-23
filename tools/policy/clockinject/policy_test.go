package clockinject_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/clockinject"
)

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate the test source")
	}
	dir := filepath.Dir(thisFile)
	for {
		if info, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil && !info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("cannot find go.mod walking up from the test source")
		}
		dir = parent
	}
}

func TestDirectReadIsFlagged(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "internal/engines/a/a.go", `package a

import "time"

func Stamp() string { return time.Now().UTC().Format(time.RFC3339) }
`)
	report, err := clockinject.Evaluate(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Violations()) != 1 {
		t.Fatalf("violations = %+v, want exactly one", report.Violations())
	}
	got := report.Violations()[0]
	if got.File != "internal/engines/a/a.go" || got.Function != "Stamp" || got.Line != 5 {
		t.Fatalf("violation = %+v, want a.go Stamp line 5", got)
	}
}

func TestAdapterIdiomAndTestsAreClean(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "internal/domains/b/b.go", `package b

import "time"

func New(now ...func() time.Time) func() time.Time {
	clock := func() time.Time { return time.Now().UTC() }
	if len(now) > 0 && now[0] != nil {
		clock = now[0]
	}
	return clock
}
`)
	writeFile(t, root, "internal/domains/b/b_test.go", `package b

import (
	"testing"
	"time"
)

func TestWall(t *testing.T) { _ = time.Now() }
`)
	writeFile(t, root, "internal/engines/c/c.go", `package c

func Pure() int { return 1 }
`)
	report, err := clockinject.Evaluate(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Violations()) != 0 {
		t.Fatalf("violations = %+v, want none", report.Violations())
	}
}

func TestDeclaredAdapterIsHonored(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "internal/domains/pseudonym/revelation.go", `package pseudonym

import "time"

type RevelationPolicy struct{ Clock func() time.Time }

func (p RevelationPolicy) now() time.Time {
	if p.Clock != nil {
		return p.Clock().UTC()
	}
	return time.Now().UTC()
}
`)
	report, err := clockinject.Evaluate(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Violations()) != 0 {
		t.Fatalf("violations = %+v, want none: the now fallback is a declared adapter", report.Violations())
	}
}

func TestPackageLevelReadIsFlagged(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "internal/domains/d/d.go", `package d

import "time"

var startedAt = time.Now()
`)
	report, err := clockinject.Evaluate(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Violations()) != 1 {
		t.Fatalf("violations = %+v, want exactly one", report.Violations())
	}
	if got := report.Violations()[0]; got.Function != "<package>" {
		t.Fatalf("violation = %+v, want package-level function", got)
	}
}

// TestTodo_REV_101_07 is the PRIMARY contract test: the live engine and
// domain trees contain no direct wall-clock read outside the declared
// adapters, so every timestamp flows from an injected clock.
func TestTodo_REV_101_07(t *testing.T) {
	root := repoRoot(t)
	report, err := clockinject.Evaluate(root)
	if err != nil {
		t.Fatal(err)
	}
	if violations := report.Violations(); len(violations) != 0 {
		t.Fatalf("direct wall-clock reads remain: %+v", violations)
	}
}

// TestDeclaredAdaptersResolve proves the allowlist cannot rot: every
// declared adapter names a file that still exists in the checkout, so a
// deleted fallback does not linger as a blanket excuse.
func TestDeclaredAdaptersResolve(t *testing.T) {
	root := repoRoot(t)
	for key := range clockinject.DeclaredAdapters {
		sep := -1
		for i := len(key) - 1; i >= 0; i-- {
			if key[i] == ':' {
				sep = i
				break
			}
		}
		if sep < 0 {
			t.Fatalf("declared adapter %q has no file:function shape", key)
		}
		rel := filepath.FromSlash(key[:sep])
		info, err := os.Stat(filepath.Join(root, rel))
		if err != nil || info.IsDir() {
			t.Fatalf("declared adapter %q names nothing on disk: %v", key, err)
		}
	}
}
