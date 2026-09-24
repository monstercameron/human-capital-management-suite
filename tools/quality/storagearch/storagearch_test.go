package storagearch

import (
	"os"
	"path/filepath"
	"testing"
)

func fixture(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		p := filepath.Join(root, name)
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func hasCode(fs []Finding, code string) bool {
	for _, f := range fs {
		if f.Code == code {
			return true
		}
	}
	return false
}

func TestStoreAdaptersRejectBusinessOwnership(t *testing.T) {
	root := fixture(t, map[string]string{
		"internal/domains/people/port.go":  "package people\ntype Store interface { Save() error }\n",
		"internal/data/postgres/people.go": "package postgres\nimport \"github.com/jackc/pgx/v5\"\ntype Command struct{}\nfunc (s *Store) Execute() error { return nil }\ntype Store struct { c *pgx.Conn }\nfunc New(c *pgx.Conn) *Store { return &Store{c:c} }\n",
		"internal/workflow/use.go":         "package workflow\nimport _ \"github.com/monstercameron/human-capital-management-suite/internal/data/postgres\"\n",
	})
	fs := Check(root)
	for _, code := range []string{"adapter-business-authority", "driver-leak", "semantic-imports-technology"} {
		if !hasCode(fs, code) {
			t.Fatalf("missing %s in %#v", code, fs)
		}
	}
}

func TestSemanticPortsRemainAllowed(t *testing.T) {
	root := fixture(t, map[string]string{
		"internal/domains/people/port.go":  "package people\ntype Store interface { Save() error }\n",
		"internal/data/postgres/people.go": "package postgres\nimport \"github.com/monstercameron/human-capital-management-suite/internal/domains/people\"\ntype Store struct{}\nvar _ people.Store = (*Store)(nil)\n",
	})
	if fs := Check(root); len(fs) != 0 {
		t.Fatalf("unexpected findings: %#v", fs)
	}
}

func TestSemanticEdgesCannotImportAdapters(t *testing.T) {
	root := fixture(t, map[string]string{
		"internal/transport/http.go": "package transport\nimport _ \"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter\"\n",
		"internal/operations/use.go": "package operations\nimport _ \"github.com/monstercameron/human-capital-management-suite/internal/data/postgres\"\n",
	})
	fs := Check(root)
	if len(fs) != 1 || fs[0].Code != "semantic-imports-technology" {
		t.Fatalf("expected transport adapter violation, got %#v", fs)
	}
}

func TestPrivateDriverFieldsDoNotLeak(t *testing.T) {
	root := fixture(t, map[string]string{
		"internal/data/pgxadapter/adapter.go": `package pgxadapter
import "github.com/jackc/pgx/v5"
type Adapter struct { conn *pgx.Conn }
func New() *Adapter { return nil }
`,
	})
	if fs := Check(root); len(fs) != 0 {
		t.Fatalf("private implementation field reported as API leak: %#v", fs)
	}
}

func TestExportedDriverTypesAreRejected(t *testing.T) {
	root := fixture(t, map[string]string{
		"internal/data/cache/adapter.go": `package cache
import "github.com/jackc/pgx/v5"
type Adapter struct { Conn *pgx.Conn }
func Open(*pgx.Conn) *Adapter { return nil }
`,
	})
	fs := Check(root)
	count := 0
	for _, f := range fs {
		if f.Code == "driver-leak" {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("expected field and parameter leaks, got %#v", fs)
	}
}

func TestCheckIsDeterministic(t *testing.T) {
	root := fixture(t, map[string]string{"internal/data/cache/a.go": "package cache\ntype Invariant struct{}\n"})
	a, b := Check(root), Check(root)
	if len(a) != len(b) {
		t.Fatal("non-deterministic result")
	}
	if len(a) == 0 || a[0].String() != b[0].String() {
		t.Fatalf("results differ: %#v %#v", a, b)
	}
}

func TestTodo_ARCH_GO_025_Property(t *testing.T) {
	root := fixture(t, map[string]string{
		"internal/data/pgxadapter/adapter.go": `package pgxadapter
import "github.com/jackc/pgx/v5"
type Adapter struct { conn *pgx.Conn }
func New() *Adapter { return nil }
`,
	})
	if fs := Check(root); len(fs) != 0 {
		t.Fatalf("private driver state should stay encapsulated: %#v", fs)
	}
}

func TestTodo_ARCH_GO_025_Golden(t *testing.T) {
	root := fixture(t, map[string]string{
		"internal/domains/people/port.go":  "package people\ntype Store interface { Save() error }\n",
		"internal/data/postgres/people.go": "package postgres\nimport \"github.com/monstercameron/human-capital-management-suite/internal/domains/people\"\ntype Store struct{}\nvar _ people.Store = (*Store)(nil)\n",
	})
	if fs := Check(root); len(fs) != 0 {
		t.Fatalf("semantic storage port implementation should be allowed: %#v", fs)
	}
}

func TestTodo_ARCH_GO_025_Integration(t *testing.T) {
	root := fixture(t, map[string]string{
		"internal/transport/http.go": "package transport\nimport _ \"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter\"\n",
	})
	fs := Check(root)
	if len(fs) != 1 || fs[0].Code != "semantic-imports-technology" {
		t.Fatalf("semantic-to-adapter integration edge = %#v", fs)
	}
}

func TestTodo_ARCH_GO_025_Conformance(t *testing.T) {
	root := fixture(t, map[string]string{
		"internal/data/cache/adapter.go": `package cache
import "github.com/jackc/pgx/v5"
type Adapter struct { conn *pgx.Conn }
`,
	})
	if fs := Check(root); len(fs) != 0 {
		t.Fatalf("unexported adapter state should conform to the storage boundary: %#v", fs)
	}
}

func TestTodo_ARCH_GO_025_Mutation(t *testing.T) {
	root := fixture(t, map[string]string{
		"internal/data/cache/adapter.go": `package cache
import "github.com/jackc/pgx/v5"
type Adapter struct { Conn *pgx.Conn }
func Open(*pgx.Conn) *Adapter { return nil }
`,
	})
	fs := Check(root)
	count := 0
	for _, f := range fs {
		if f.Code == "driver-leak" {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("exported driver field and parameter should both be rejected, got %#v", fs)
	}
}

func TestTodo_ARCH_GO_025_Race(t *testing.T) {
	root := fixture(t, map[string]string{
		"internal/transport/http.go": "package transport\nimport _ \"github.com/monstercameron/human-capital-management-suite/internal/data/postgres\"\n",
		"internal/data/cache/adapter.go": `package cache
import "github.com/jackc/pgx/v5"
type Adapter struct { Conn *pgx.Conn }
`,
	})
	type result struct{ findings []Finding }
	const workers = 8
	results := make(chan result, workers)
	for i := 0; i < workers; i++ {
		go func() { results <- result{findings: Check(root)} }()
	}
	for i := 0; i < workers; i++ {
		got := <-results
		if len(got.findings) != 2 || !hasCode(got.findings, "semantic-imports-technology") || !hasCode(got.findings, "driver-leak") {
			t.Fatalf("concurrent storage scan = %#v", got.findings)
		}
	}
}
