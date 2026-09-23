package errlint_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/errlint"
)

func writeFile(t *testing.T, path, body string) {
	t.Helper()
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

// rev10306Files are the five files REV-103-06's RED names, relative to the
// repository root.
var rev10306Files = []string{
	"internal/data/operationstore/store.go",
	"internal/intent/app/pgstore/pgstore.go",
	"internal/trust/session/store.go",
	"internal/intent/app/journey_intervention.go",
	"internal/transport/evidence/server.go",
}

func TestBlankedCallIsFlagged(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "store.go")
	writeFile(t, path, `package store

import "context"

func Cancel(ctx context.Context) error {
	var fence int64
	_ = queryRow(ctx).Scan(&fence)
	return nil
}

func queryRow(ctx context.Context) Row { return nil }
`)
	_ = path
	report, err := errlint.EvaluateFiles([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Violations()) != 1 {
		t.Fatalf("violations = %+v, want exactly one", report.Violations())
	}
	got := report.Violations()[0]
	if got.Function != "method:Cancel" && got.Function != "Cancel" {
		t.Fatalf("violation = %+v, want the Cancel function", got)
	}
}

func TestMixedCaptureCheckingTheErrorIsClean(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "store.go")
	writeFile(t, path, `package store

import "context"

func Insert(ctx context.Context) error {
	if _, err := execInsert(ctx); err != nil {
		return err
	}
	return nil
}

func execInsert(ctx context.Context) (int64, error) { return 1, nil }
`)
	report, err := errlint.EvaluateFiles([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Violations()) != 0 {
		t.Fatalf("violations = %+v, want none: the row count is discarded but the error is checked", report.Violations())
	}
}

func TestDeferredRollbackIsClean(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "store.go")
	writeFile(t, path, `package store

import "context"

type Tx struct{}

func (Tx) Rollback(context.Context) error { return nil }

func Work(ctx context.Context, tx Tx) error {
	defer func() { _ = tx.Rollback(ctx) }()
	return nil
}

func Setup(ctx context.Context, tx Tx) error {
	if err := context.Cause(ctx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}
	return nil
}
`)
	report, err := errlint.EvaluateFiles([]string{path})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Violations()) != 0 {
		t.Fatalf("violations = %+v, want none: rollbacks decide nothing", report.Violations())
	}
}

// TestTodo_REV_103_06 is the PRIMARY contract test: the five files the RED
// names carry no blanked call outside a rollback, so the fence query, the
// obligation decodes, the denial evidence, the intervention commit and the
// export context can no longer be silently dropped.
func TestTodo_REV_103_06(t *testing.T) {
	root := repoRoot(t)
	files := make([]string, 0, len(rev10306Files))
	for _, rel := range rev10306Files {
		full := filepath.Join(root, filepath.FromSlash(rel))
		if !errlint.Exists(full) {
			t.Fatalf("gated file is missing: %s", full)
		}
		files = append(files, full)
	}
	report, err := errlint.EvaluateFiles(files)
	if err != nil {
		t.Fatal(err)
	}
	if violations := report.FilterExceptions().Violations(); len(violations) != 0 {
		t.Fatalf("blanked calls remain in the gated files: %+v", violations)
	}
}
