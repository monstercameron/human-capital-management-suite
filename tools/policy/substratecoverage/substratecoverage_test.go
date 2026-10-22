package substratecoverage

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestTodo_SUBSTRATE_COVERAGE_001(t *testing.T) {
	root := t.TempDir()
	packageDir := filepath.Join(root, "internal", "data", "store")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "store.go"), []byte("package store\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "store_test.go"), []byte("package store\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	row := Responsibility{Name: "persistence", Package: "internal/data/store", Todo: "STORE-002", Owner: "data"}
	report, err := Scan(root, Options{Responsibilities: []Responsibility{row}})
	if err != nil || !report.OK() {
		t.Fatalf("clean substrate scan = %+v, %v", report, err)
	}
	if err := os.Remove(filepath.Join(packageDir, "store_test.go")); err != nil {
		t.Fatal(err)
	}
	report, err = Scan(root, Options{Responsibilities: []Responsibility{row}})
	if err != nil || report.OK() || !strings.Contains(report.Violations()[0].Detail, "no test file") {
		t.Fatalf("missing test scan = %+v, %v; want NO_TEST", report, err)
	}
}

func TestTodo_SUBSTRATE_COVERAGE_001_Golden(t *testing.T) {
	rows := append([]Responsibility(nil), Responsibilities...)
	report := Report{Responsibilities: rows}
	firstSubstrateDigest, secondSubstrateDigest := report.Digest(), report.Digest()
	if firstSubstrateDigest == "" || firstSubstrateDigest != secondSubstrateDigest {
		t.Fatal("substrate report digest is not stable")
	}
	if len(rows) != 10 {
		t.Fatalf("declared responsibilities = %d, want ten", len(rows))
	}
}

func TestTodo_SUBSTRATE_COVERAGE_001_Conformance(t *testing.T) {
	root := t.TempDir()
	if _, err := Scan(root, Options{Responsibilities: []Responsibility{{Name: "bad", Package: "external/store", Todo: "STORE-002", Owner: "data"}}}); err != nil {
		t.Fatal(err)
	}
	report, _ := Scan(root, Options{Responsibilities: []Responsibility{{Name: "bad", Package: "external/store", Todo: "STORE-002", Owner: "data"}}})
	if report.OK() || len(report.Violations()) == 0 {
		t.Fatal("package outside internal/ was accepted")
	}
}

func TestTodo_SUBSTRATE_COVERAGE_001_Integration(t *testing.T) {
	root := t.TempDir()
	packageDir := filepath.Join(root, "internal", "data", "outbox")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"outbox.go", "outbox_test.go"} {
		if err := os.WriteFile(filepath.Join(packageDir, name), []byte("package outbox\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	report, err := Scan(root, Options{Responsibilities: []Responsibility{}, Allowlist: []ImplicitEntry{{Package: "internal/data/outbox", Owner: "messaging", Reason: "supporting adapter"}}})
	if err != nil || !report.OK() {
		t.Fatalf("allowlisted implicit package = %+v, %v", report, err)
	}
	if len(report.Findings) != 1 || !report.Findings[0].Allowlisted {
		t.Fatalf("findings = %+v, want one allowlisted IMPLICIT finding", report.Findings)
	}
}

func TestTodo_SUBSTRATE_COVERAGE_001_ExactConsumerAllowlist(t *testing.T) {
	root := t.TempDir()
	want := map[string]string{
		"internal/agentsystem/scheduled":    "agent platform",
		"internal/application/crewschedule": "experience platform",
		"internal/collaboration/chatsearch": "collaboration platform",
		"internal/operations/telemetry":     "operations platform",
	}
	allowlist := make([]ImplicitEntry, 0, len(want))
	for _, entry := range ImplicitAllowlist {
		owner, expected := want[entry.Package]
		if !expected {
			continue
		}
		if entry.Owner != owner || entry.Reason == "" {
			t.Fatalf("allowlist entry for %s = %+v, want owner %q and an ownership reason", entry.Package, entry, owner)
		}
		allowlist = append(allowlist, entry)
		packageDir := filepath.Join(root, filepath.FromSlash(entry.Package))
		if err := os.MkdirAll(packageDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(packageDir, "consumer.go"), []byte("package consumer\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if len(allowlist) != len(want) {
		t.Fatalf("exact consumer allowlist entries = %d, want %d", len(allowlist), len(want))
	}
	report, err := Scan(root, Options{Responsibilities: []Responsibility{}, Allowlist: allowlist})
	if err != nil || !report.OK() || len(report.Findings) != len(want) {
		t.Fatalf("known exact consumers = %+v, %v; want only allowlisted findings", report, err)
	}
	for _, finding := range report.Findings {
		if !finding.Allowlisted || finding.Owner != want[finding.Package] || finding.Detail == "" {
			t.Errorf("consumer finding = %+v, want exact owner-bearing allowlist evidence", finding)
		}
	}

	unknownDir := filepath.Join(root, "internal", "application", "crewscheduleextra")
	if err := os.MkdirAll(unknownDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(unknownDir, "unknown.go"), []byte("package crewscheduleextra\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err = Scan(root, Options{Responsibilities: []Responsibility{}, Allowlist: allowlist})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Violations()) != 1 || report.Violations()[0].Package != "internal/application/crewscheduleextra" {
		t.Fatalf("new similarly named consumer = %+v, want one unallowlisted exact-path violation", report.Violations())
	}
}

func TestTodo_SUBSTRATE_COVERAGE_001_Security(t *testing.T) {
	root := t.TempDir()
	packageDir := filepath.Join(root, "internal", "data", "outbox-new")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "outbox.go"), []byte("package outboxnew\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	report, err := Scan(root, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if report.OK() {
		t.Fatal("new similarly named substrate package bypassed exact-path allowlist")
	}
}

func TestTodo_SUBSTRATE_COVERAGE_001_Race(t *testing.T) {
	root := t.TempDir()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = Scan(root, Options{Responsibilities: []Responsibility{}})
		}()
	}
	wg.Wait()
}

func FuzzTodo_SUBSTRATE_COVERAGE_001(f *testing.F) {
	f.Add("internal/data/store")
	f.Fuzz(func(t *testing.T, packagePath string) {
		if strings.Contains(packagePath, "\x00") {
			return
		}
		report, err := Scan(t.TempDir(), Options{Responsibilities: []Responsibility{{Name: "fuzz", Package: packagePath, Todo: "FUZZ-001", Owner: "test"}}})
		if err != nil {
			t.Fatal(err)
		}
		_ = report.Violations()
	})
}
