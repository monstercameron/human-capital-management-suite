package depedge_test

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/depedge"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
)

func TestTodo_ARCH_GO_002_Golden(t *testing.T) {
	path := filepath.Join(repopath.RootDir(), "definitions", "architecture", "package-dependency-policy.yaml")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := fmt.Sprintf("%X", sha256.Sum256(content))
	const want = "7E05364AE0380E476186B3346DBCE701FDFCE264D423926CB67F4649B4E9B2D5"
	if got != want {
		t.Fatalf("package-dependency-policy.yaml SHA256 = %s, want %s", got, want)
	}
}

func TestTodo_ARCH_GO_002_Conformance(t *testing.T) {
	p := loadPolicy(t)
	mod := p.Module
	vectors := []struct{ from, to, rule string }{
		{mod + "/internal/kernel/value", mod + "/internal/engines/rules", depedge.RuleKernelUpward},
		{mod + "/internal/engines/rules", mod + "/internal/domains/people", depedge.RuleEngineImportsDomain},
		{mod + "/internal/workflow/runtime", mod + "/internal/domains/people/store", depedge.RuleWorkflowDomainPersist},
		{mod + "/internal/transport/http", mod + "/internal/data/postgres", depedge.RuleTransportImportsStore},
		{mod + "/internal/domains/people", mod + "/internal/connectivity/http/adapters", depedge.RuleBusinessConcreteAdapter},
		{mod + "/internal/domains/people", mod + "/internal/kernel/value", ""},
	}
	for _, vector := range vectors {
		v := p.CheckEdge(vector.from, vector.to)
		if vector.rule == "" {
			if v != nil {
				t.Errorf("CheckEdge(%q, %q) = %+v, want allowed", vector.from, vector.to, v)
			}
			continue
		}
		if v == nil || v.Rule != vector.rule {
			t.Errorf("CheckEdge(%q, %q) = %+v, want rule %q", vector.from, vector.to, v, vector.rule)
		}
	}
}

func TestTodo_ARCH_GO_002_Integration(t *testing.T) {
	p := loadPolicy(t)
	pkgs, err := repopath.ListPackages(repopath.RootDir())
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, pkg := range pkgs {
		for _, imported := range pkg.Imports {
			if violation := p.CheckEdge(pkg.ImportPath, imported); violation != nil {
				t.Errorf("real import edge %s -> %s violates %s", violation.Importer, violation.Imported, violation.Rule)
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("real package listing contained no import edges")
	}
}

func TestTodo_ARCH_GO_002_Mutation(t *testing.T) {
	p := loadPolicy(t)
	importer := p.Module + "/internal/workflow/runtime"
	imported := p.Module + "/internal/domains/people/store"
	if v := p.CheckEdge(importer, imported); v == nil || v.Rule != depedge.RuleWorkflowDomainPersist {
		t.Fatalf("baseline workflow persistence edge = %+v", v)
	}
	p.DomainPersistenceMarkers = nil
	if v := p.CheckEdge(importer, imported); v != nil {
		t.Fatalf("edge remained forbidden after removing its marker: %+v", v)
	}
}
