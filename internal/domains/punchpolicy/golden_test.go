package punchpolicy

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// update rewrites the committed golden files instead of comparing against
// them. Run `go test ./internal/domains/punchpolicy/ -run Golden -update`
// after a deliberate change to a golden-covered shape.
var update = flag.Bool("update", false, "rewrite the punchpolicy golden files")

func assertGolden(t *testing.T, name, rendered string) {
	t.Helper()
	path := filepath.Join("testdata", "golden", name)
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create the golden directory: %v", err)
		}
		if err := os.WriteFile(path, []byte(rendered), 0o644); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s (run with -update to create it): %v", path, err)
	}
	if got := rendered; got != strings.ReplaceAll(string(want), "\r\n", "\n") {
		t.Fatalf("golden %s does not match.\n--- want ---\n%s\n--- got ---\n%s", name, want, got)
	}
}
