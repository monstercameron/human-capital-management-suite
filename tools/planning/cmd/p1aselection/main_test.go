package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const repoRoot = "../../../.."

var pinned = time.Date(2026, 9, 13, 0, 0, 0, 0, time.UTC)

func TestRunReportsIncompleteLiveSelectionsWithoutFailing(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-root", repoRoot}, &stdout, &stderr, pinned); err != nil {
		t.Fatalf("run: %v\nstderr: %s", err, stderr.String())
	}
	out := stdout.String()
	for _, want := range []string{
		"selection completeness: INCOMPLETE (as of 2026-09-13)",
		"binding SELECT-002     ready=false",
		"slot slo          filled=false",
		"REASON: TOPOLOGY-001: no deployable topology decision artifact",
		"P1B template activatable=false",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output does not contain %q:\n%s", want, out)
		}
	}
	if stderr.Len() != 0 {
		t.Errorf("live P1B template reported defects: %s", stderr.String())
	}

	stdout.Reset()
	if err := run([]string{"-root", repoRoot, "-require-complete"}, &stdout, &stderr, pinned); err == nil || !strings.Contains(err.Error(), "INCOMPLETE") {
		t.Fatalf("-require-complete on the live repository = %v, want an INCOMPLETE failure", err)
	}

	stdout.Reset()
	if err := run([]string{"-root", repoRoot, "-json"}, &stdout, &stderr, pinned); err != nil {
		t.Fatal(err)
	}
	var report struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil || report.Status != "INCOMPLETE" {
		t.Fatalf("-json output = %q (%v)", stdout.String(), err)
	}
}

func TestRunFailsOnTamperedOrUnloadableDocuments(t *testing.T) {
	root := t.TempDir()
	copyTree := func(rel string) []byte {
		b, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		dst := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dst, b, 0o644); err != nil {
			t.Fatal(err)
		}
		return b
	}
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-root", root}, &stdout, &stderr, pinned); err == nil {
		t.Fatal("run succeeded without a signing key fixture")
	}
	copyTree("tools/planning/gateevidence/testdata/dev-signing-key.yaml")
	if err := run([]string{"-root", root}, &stdout, &stderr, pinned); err == nil {
		t.Fatal("run succeeded without a P1A manifest")
	}
	manifest := copyTree("definitions/planning/gates/p1a-manifest.yaml")
	if err := run([]string{"-root", root}, &stdout, &stderr, pinned); err == nil {
		t.Fatal("run succeeded without a P1B template")
	}
	copyTree("definitions/planning/gates/p1b-template.yaml")
	// Every bound selection is absent from this tree: the bindings cannot
	// be recomputed, which must fail rather than report.
	if err := run([]string{"-root", root}, &stdout, &stderr, pinned); err == nil || !strings.Contains(err.Error(), "stale selection binding") {
		t.Fatalf("run with unresolvable bindings = %v, want a stale-binding failure", err)
	}

	tampered := bytes.Replace(manifest, []byte("freshness_window_days: 30"), []byte("freshness_window_days: 31"), 1)
	if err := os.WriteFile(filepath.Join(root, "definitions", "planning", "gates", "p1a-manifest.yaml"), tampered, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"-root", root}, &stdout, &stderr, pinned); err == nil || !strings.Contains(err.Error(), "invalid or untrusted") {
		t.Fatalf("run with a tampered manifest = %v, want an untrusted-manifest failure", err)
	}
	if err := run([]string{"-bogus"}, &stdout, &stderr, pinned); err == nil {
		t.Fatal("unknown flag accepted")
	}
}
