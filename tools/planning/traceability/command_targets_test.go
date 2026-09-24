package traceability

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/todoregistry"
)

func TestEvidenceCommandTargetsResolveRepositoryTargets(t *testing.T) {
	root := t.TempDir()
	packageDir := filepath.Join(root, "pkg", "real")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, "real.go"), []byte("package real\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"scripts":{"test:frontend":"vitest run"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "scripts", "sample.test.mjs"), []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	todos := []todoregistry.Todo{
		{ID: "GO-OK", Done: true, Evidence: "`go test -count=1 ./pkg/real/` PASS"},
		{ID: "NPM-OK", Done: true, Evidence: "`npm run test:frontend` PASS"},
		{ID: "NODE-OK", Done: true, Evidence: "`node --test scripts/sample.test.mjs` PASS"},
		{ID: "GO-MISSING", Done: true, Evidence: "`go test -count=1 ./pkg/missing/` PASS"},
		{ID: "NPM-MISSING", Done: true, Evidence: "`npm run test:go` PASS"},
		{ID: "NODE-MISSING", Done: true, Evidence: "`node --test scripts/missing.test.mjs` PASS"},
		{ID: "OPEN-MISSING", Done: false, Evidence: "`go test ./pkg/missing/`"},
		{ID: "RETIRED-MISSING", Done: true, Retired: true, Evidence: "`go test ./pkg/missing/`"},
	}

	findings := CheckEvidenceCommandTargets(root, todos)
	if len(findings) != 3 {
		t.Fatalf("CheckEvidenceCommandTargets() returned %v, want exactly 3 missing targets", findings)
	}
	wants := map[string]string{
		"GO-MISSING":   `Go package "./pkg/missing/" does not exist`,
		"NPM-MISSING":  `npm script "test:go" does not exist`,
		"NODE-MISSING": `Node test target "scripts/missing.test.mjs" does not exist`,
	}
	for _, finding := range findings {
		if finding.Kind != TickedMissingCommandTarget {
			t.Errorf("finding %s kind = %q, want %q", finding.ID, finding.Kind, TickedMissingCommandTarget)
		}
		want, ok := wants[finding.ID]
		if !ok {
			t.Errorf("unexpected finding %v", finding)
			continue
		}
		if finding.Detail != "Evidence command "+commandForID(finding.ID)+" has no repository target: "+want {
			t.Errorf("unexpected detail for %s: %q", finding.ID, finding.Detail)
		}
	}
}

func commandForID(id string) string {
	switch id {
	case "GO-MISSING":
		return `"go test -count=1 ./pkg/missing/"`
	case "NPM-MISSING":
		return `"npm run test:go"`
	case "NODE-MISSING":
		return `"node --test scripts/missing.test.mjs"`
	default:
		return ""
	}
}
