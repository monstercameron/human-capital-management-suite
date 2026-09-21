package rowbatch

// REV-089-01: the PERFOPT-004 batching rollout must reach every row-at-a-time
// store, not just the four hot paths. This test scans internal/data for Exec
// calls inside loops; meritstore emission and leavestore segments adopt
// dbport.ExecAll, and every remaining site is a recorded, justified
// exception.
//
// RED: internal/data/meritstore/emission.go issues one QueryRow plus one
// INSERT per child inside `for _, child := range children`, and
// internal/data/leavestore/store.go issues one INSERT per segment in a loop;
// neither references dbport.ExecAll.

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func rowbatchRepoRoot(t *testing.T) string {
	t.Helper()
	working, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	root, err := filepath.Abs(filepath.Join(working, "..", "..", ".."))
	if err != nil {
		t.Fatalf("repo root: %v", err)
	}
	return root
}

func TestTodo_REV_089_01(t *testing.T) {
	t.Run("LoopExecDetected", func(t *testing.T) {
		src := "package x\nfunc f(tx T) {\n\tfor _, r := range rows {\n\t\t_, err := tx.Exec(ctx, \"INSERT INTO t VALUES ($1)\", r)\n\t\t_ = err\n\t}\n}\n"
		found, err := ScanFile("internal/data/example/store.go", []byte(src))
		if err != nil {
			t.Fatalf("scan snippet: %v", err)
		}
		if len(found) != 1 || found[0].Line != 4 {
			t.Fatalf("snippet findings = %+v, want one finding at line 4", found)
		}
	})

	t.Run("StraightLineExecIgnored", func(t *testing.T) {
		src := "package x\nfunc f(tx T) {\n\t_, err := tx.Exec(ctx, \"INSERT INTO t VALUES ($1)\", 1)\n\t_ = err\n}\n"
		found, err := ScanFile("internal/data/example/store.go", []byte(src))
		if err != nil {
			t.Fatalf("scan snippet: %v", err)
		}
		if len(found) != 0 {
			t.Fatalf("straight-line findings = %+v, want none", found)
		}
	})

	t.Run("FuncLiteralIgnored", func(t *testing.T) {
		src := "package x\nfunc f(tx T) {\n\tfor _, r := range rows {\n\t\tfn := func() {\n\t\t\ttx.Exec(ctx, \"INSERT INTO t VALUES ($1)\", r)\n\t\t}\n\t\t_ = fn\n\t}\n}\n"
		found, err := ScanFile("internal/data/example/store.go", []byte(src))
		if err != nil {
			t.Fatalf("scan snippet: %v", err)
		}
		if len(found) != 0 {
			t.Fatalf("func-literal findings = %+v, want none", found)
		}
	})

	t.Run("ConvertedStoresClean", func(t *testing.T) {
		for _, file := range []string{
			"internal/data/meritstore/emission.go",
			"internal/data/leavestore/store.go",
		} {
			src, err := os.ReadFile(filepath.Join(rowbatchRepoRoot(t), filepath.FromSlash(file)))
			if err != nil {
				t.Fatalf("read %s: %v", file, err)
			}
			found, err := ScanFile(file, src)
			if err != nil {
				t.Fatalf("scan %s: %v", file, err)
			}
			if len(found) != 0 {
				t.Fatalf("%s still issues row-at-a-time Exec: %+v", file, found)
			}
		}
	})

	t.Run("SweepExhaustive", func(t *testing.T) {
		findings, err := ScanTree(rowbatchRepoRoot(t))
		if err != nil {
			t.Fatalf("scan tree: %v", err)
		}
		var lines []string
		for _, f := range Unexcused(findings) {
			lines = append(lines, f.File+":"+strconv.Itoa(f.Line))
		}
		if len(lines) != 0 {
			t.Fatalf("unexcused row-at-a-time Exec sites: %v", lines)
		}
	})

	t.Run("ExceptionsPinned", func(t *testing.T) {
		raw, err := os.ReadFile(filepath.Join("testdata", "rowbatch_exceptions.golden"))
		if err != nil {
			t.Fatalf("read golden: %v", err)
		}
		want := strings.TrimSpace(string(raw))
		if got := CanonicalExceptions(); got != want {
			t.Fatalf("exception table drifted:\n got: %q\nwant: %q", got, want)
		}
	})
}
