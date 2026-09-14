package observetest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUninstrumentedAndCheckExemptions(t *testing.T) {
	root := t.TempDir()
	write := func(rel, src string) {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("a.go", `package a
import "context"
type S struct{}
func (s *S) Bare(ctx context.Context) error { return nil }
func Traced(ctx context.Context) { _, op := observe.Begin(ctx, "x"); _ = op }
func NoCtx() {}
func unexported(ctx context.Context) {}
func Other(ctx notcontext.Context) {}
`)
	write("a_test.go", "package a\nimport \"context\"\nfunc Skipped(ctx context.Context) {}\n")
	write("testdata/t.go", "package t\nimport \"context\"\nfunc Skipped(ctx context.Context) {}\n")
	write("gen/g.go", "package g\nimport \"context\"\nfunc Skipped(ctx context.Context) {}\n")
	write("sub/b.go", "package b\nimport \"context\"\nfunc Plain(c context.Context) {}\n")
	found, err := Uninstrumented(root, func(rel string) bool { return rel == "gen" })
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(found, ","); got != "a.go S.Bare,sub/b.go Plain" {
		t.Fatalf("found = %s", got)
	}
	missing, stale := CheckExemptions(found, map[string]string{"a.go S.Bare": "no-op", "gone.go X": "stale"})
	if strings.Join(missing, ",") != "sub/b.go Plain" || strings.Join(stale, ",") != "gone.go X" {
		t.Fatalf("missing = %v, stale = %v", missing, stale)
	}
	write("bad.go", "package a\nfunc (")
	if _, err := Uninstrumented(root, nil); err == nil {
		t.Fatal("unparsable file accepted")
	}
	if _, err := Uninstrumented(filepath.Join(root, "missing"), nil); err == nil {
		t.Fatal("missing root accepted")
	}
}
