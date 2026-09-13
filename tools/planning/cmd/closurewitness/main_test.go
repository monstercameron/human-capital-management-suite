package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/closurewitness"
)

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test working directory")
		}
		dir = parent
	}
}

func fixedNow() time.Time { return time.Date(2026, 9, 13, 23, 30, 0, 0, time.UTC) }

func TestRunRejectsBadArguments(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run([]string{"-format", "yaml"}, &stdout, &stderr, fixedNow); err == nil || !strings.Contains(err.Error(), "unknown -format") {
		t.Fatalf("bad format: err = %v", err)
	}
	if err := run([]string{"-no-such-flag"}, &stdout, &stderr, fixedNow); err == nil {
		t.Fatal("an unknown flag was accepted")
	}
	if err := run([]string{"-root", t.TempDir()}, &stdout, &stderr, fixedNow); err == nil {
		t.Fatal("a root without registries produced a report")
	}
	root := repoRoot(t)
	if err := run([]string{"-root", root, "-as-of", "13/09/2026"}, &stdout, &stderr, fixedNow); !errors.Is(err, closurewitness.ErrInvalidAsOf) {
		t.Fatalf("bad as-of: err = %v, want ErrInvalidAsOf", err)
	}
}

// TestRunEmitsTheLiveReport drives the command over the live registries:
// the JSON format is the canonical report dated by the injected clock, the
// summary names the same digest, and -strict fails exactly when closure is
// incomplete.
func TestRunEmitsTheLiveReport(t *testing.T) {
	root := repoRoot(t)
	var jsonOut bytes.Buffer
	if err := run([]string{"-root", root, "-format", "json"}, &jsonOut, &bytes.Buffer{}, fixedNow); err != nil {
		t.Fatalf("json run: %v", err)
	}
	var report closurewitness.Report
	if err := json.Unmarshal(jsonOut.Bytes(), &report); err != nil {
		t.Fatalf("json output does not parse: %v", err)
	}
	if report.AsOf != "2026-09-13" || report.SchemaVersion != closurewitness.SchemaVersion || len(report.Witnesses) == 0 {
		t.Fatalf("report as_of=%s schema=%d witnesses=%d", report.AsOf, report.SchemaVersion, len(report.Witnesses))
	}

	var summary bytes.Buffer
	err := run([]string{"-root", root, "-as-of", "2026-09-13", "-strict"}, &summary, &bytes.Buffer{}, fixedNow)
	if !strings.Contains(summary.String(), "digest="+report.Digest) {
		t.Fatalf("summary does not name the JSON report's digest %s:\n%s", report.Digest, summary.String())
	}
	switch report.Result {
	case closurewitness.ResultComplete:
		if err != nil {
			t.Fatalf("-strict failed a complete report: %v", err)
		}
	default:
		if !errors.Is(err, errIncomplete) {
			t.Fatalf("-strict over an incomplete report: err = %v, want %v", err, errIncomplete)
		}
	}
}
