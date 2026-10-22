package productui

import (
	"os"
	"path/filepath"
	"testing"
)

// goldenReviewCopy keeps the document a failing golden digest was computed over
// where a person can read it: under $HCMNEXT_ARTIFACTS/tmp (or the system temp
// directory) as golden-<name>.txt. A bare digest says only that something
// changed; the copy lets the next change be diffed against the last one before
// the digest is pinned again. It returns the path, or "" when nothing could be
// written.
func goldenReviewCopy(t *testing.T, name, document string) string {
	t.Helper()
	dir := os.TempDir()
	if root := os.Getenv("HCMNEXT_ARTIFACTS"); root != "" {
		dir = filepath.Join(root, "tmp")
	}
	path := filepath.Join(dir, "golden-"+name+".txt")
	if err := os.WriteFile(path, []byte(document), 0o644); err != nil {
		return ""
	}
	return path
}
