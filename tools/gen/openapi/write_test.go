package openapi

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, dir, rel, content string) string {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestWriteThenCheckAndDrift(t *testing.T) {
	root := repoRoot(t)
	out := filepath.Join(t.TempDir(), "nested", "rpcs.openapi.yaml")
	if err := Write(root, out); err != nil {
		t.Fatalf("Write: %v", err)
	}
	want, err := Generate(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(out)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("written file differs from Generate (err=%v)", err)
	}
	if err := Check(root, out); err != nil {
		t.Fatalf("Check on a fresh file: %v", err)
	}
	// CRLF checkouts are not drift.
	if err := os.WriteFile(out, bytes.ReplaceAll(want, []byte("\n"), []byte("\r\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Check(root, out); err != nil {
		t.Fatalf("Check on a CRLF copy: %v", err)
	}
	if err := os.WriteFile(out, append(want, "# hand edit\n"...), 0o644); err != nil {
		t.Fatal(err)
	}
	err = Check(root, out)
	if !errors.Is(err, ErrDrift) || !strings.Contains(err.Error(), RegenerateCommand) {
		t.Fatalf("Check on an edited file = %v, want ErrDrift naming the regenerate command", err)
	}
	err = Check(root, filepath.Join(t.TempDir(), "missing.yaml"))
	if !errors.Is(err, ErrDrift) {
		t.Fatalf("Check on a missing file = %v, want ErrDrift", err)
	}
}

func TestWriteAndCheckFailWithoutProtoSources(t *testing.T) {
	empty := t.TempDir()
	if err := Write(empty, "out.yaml"); err == nil {
		t.Fatal("Write without schema/proto succeeded")
	}
	if err := Check(empty, "out.yaml"); err == nil || errors.Is(err, ErrDrift) {
		t.Fatalf("Check without schema/proto = %v, want a generation error", err)
	}
	if _, err := os.Stat(filepath.Join(empty, "out.yaml")); !os.IsNotExist(err) {
		t.Fatal("a failed Write left a file behind")
	}
}

func TestWriteFailsWhenTheDirectoryCannotBeCreated(t *testing.T) {
	root := repoRoot(t)
	blocker := writeFile(t, t.TempDir(), "file", "x")
	if err := Write(root, filepath.Join(blocker, "sub", "out.yaml")); err == nil {
		t.Fatal("Write under a regular file succeeded")
	}
}

func TestResolve(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "x.yaml")
	if resolve("root", abs) != abs {
		t.Fatal("absolute path was rebased")
	}
	if got := resolve("root", "a/b.yaml"); got != filepath.Join("root", "a", "b.yaml") {
		t.Fatalf("resolve = %s", got)
	}
}
