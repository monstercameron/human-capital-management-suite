package depedge_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/depedge"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/internal/repopath"
)

func TestTodo_REV_101_01(t *testing.T) {
	p := loadPolicy(t)
	pkgs, err := repopath.ListPackages(repopath.RootDir())
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, pkg := range pkgs {
		if !strings.HasPrefix(pkg.ImportPath, p.Module+"/internal/") && !strings.HasPrefix(pkg.ImportPath, p.Module+"/cmd/") {
			continue
		}
		for _, imported := range pkg.Imports {
			checked++
			if v := p.CheckEdge(pkg.ImportPath, imported); v != nil && v.Rule == depedge.RuleProductImportsTools {
				t.Errorf("release package import %s -> %s violates %s", v.Importer, v.Imported, v.Rule)
			}
		}
	}
	if checked == 0 {
		t.Fatal("package listing contained no internal or cmd import edges")
	}
}

func TestTodo_REV_101_01_Golden(t *testing.T) {
	p := loadPolicy(t)
	registered := false
	for _, name := range rev10102RuleNames(t) {
		if name == depedge.RuleProductImportsTools {
			registered = true
			break
		}
	}
	if !registered {
		t.Fatalf("dependency policy does not register %q", depedge.RuleProductImportsTools)
	}
	mod := p.Module
	cases := []struct {
		from, to string
	}{
		{mod + "/internal/humanwork/workspace", mod + "/tools/uxqual/contract"},
		{mod + "/internal/customobject", mod + "/tools/gen/schemaflux"},
		{mod + "/cmd/hcmnext", mod + "/tools/uxqual/qual"},
		{mod + "/tools/uxqual/render/gwc", mod + "/internal/experience/workspacecontract"},
		{mod + "/internal/kernel", "golang.org/x/text/language"},
	}
	var got strings.Builder
	for _, c := range cases {
		v := p.CheckEdge(c.from, c.to)
		if v == nil {
			fmt.Fprintf(&got, "%s -> %s: allowed\n", c.from, c.to)
		} else {
			fmt.Fprintf(&got, "%s -> %s: %s\n", c.from, c.to, v.Rule)
		}
	}
	want, err := os.ReadFile(filepath.Join(repopath.RootDir(), "tools", "policy", "depedge", "testdata", "rev101_01_edges.golden.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != string(want) {
		t.Fatalf("edge verdicts differ:\n got:\n%s\nwant:\n%s", got.String(), string(want))
	}
}
