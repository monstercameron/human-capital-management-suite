package intake

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/documentsecurity"
)

// TestTodo_REV_024_03 is the primary package-ownership assertion. Go's
// package resolver must expose one implementation for each capability: the
// retained security package used by intake, the canonical extract/redact
// engines, and the quarantine domain. The former lookalike package paths
// must no longer resolve.
func TestTodo_REV_024_03(t *testing.T) {
	_, source, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", ".."))
	module := "github.com/monstercameron/human-capital-management-suite"
	for _, path := range []string{
		"./internal/documentsecurity",
		"./internal/domains/asset/quarantine",
		"./internal/engines/docextract",
		"./internal/engines/docredact",
	} {
		cmd := exec.Command("go", "list", "-f", "{{.ImportPath}}", path)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err != nil || strings.TrimSpace(string(out)) != module+strings.TrimPrefix(path, ".") {
			t.Fatalf("canonical package %s: output=%q err=%v", path, strings.TrimSpace(string(out)), err)
		}
	}
	for _, path := range []string{"./internal/documentextract", "./internal/documentredact"} {
		cmd := exec.Command("go", "list", "-f", "{{.ImportPath}}", path)
		cmd.Dir = root
		out, err := cmd.CombinedOutput()
		if err == nil {
			t.Errorf("retired package %s still resolves as %q", path, strings.TrimSpace(string(out)))
		}
	}
}

// TestTodo_REV_024_03_Integration proves the retained documentsecurity
// package has a production consumer: intake releases a reference only for a
// safe scanned artifact, and that reference stops working when the scan is
// revoked. Extraction and redaction use their canonical engine packages.
func TestTodo_REV_024_03_Integration(t *testing.T) {
	registry := documentsecurity.NewRegistry()
	limits := documentsecurity.Limits{MaxBytes: 100, MaxDerivativeBytes: 100, MaxCompressionRatio: 10}
	upload := documentsecurity.Upload{
		ID: "rev-024-03", Name: "medical.pdf", ContentType: "application/pdf", Bytes: []byte("source bytes"),
	}
	_, err := registry.Scan(context.Background(), upload, limits, scanner{documentsecurity.Safe})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	ref, err := NewService(registry).Release(Request{
		ArtifactID: upload.ID, Purpose: "leave.review", SubjectID: "worker-1", RequestedBy: "specialist", Now: time.Unix(1, 0),
	})
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if err := ref.Evidence.Check(); err != nil {
		t.Fatalf("safe evidence rejected: %v", err)
	}

	if !registry.Revoke(upload.ID, "rescan required") {
		t.Fatal("artifact was not revoked")
	}
	if err := ref.Evidence.Check(); !errors.Is(err, ErrRevoked) {
		t.Fatalf("revoked evidence check = %v, want ErrRevoked", err)
	}
}
