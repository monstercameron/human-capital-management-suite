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
