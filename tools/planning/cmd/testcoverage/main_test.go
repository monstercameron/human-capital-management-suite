package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/testcoverage"
)

func TestTodo_REV_103_04(t *testing.T) {
	if err := run([]string{"-unknown"}); err == nil {
		t.Fatal("unknown command flag was accepted")
	}
}

func TestTodo_REV_103_04_GeneratesAndChecksBothInventories(t *testing.T) {
	root := t.TempDir()
	for _, module := range testcoverage.Modules {
		moduleRoot := filepath.Join(root, filepath.FromSlash(module.Path))
		if err := os.MkdirAll(moduleRoot, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(moduleRoot, "go.mod"), []byte("module "+module.Root+"\n\ngo 1.26.0\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(moduleRoot, "sample.go"), []byte("package fixture\nfunc Value() int { return 7 }\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(moduleRoot, "sample_test.go"), []byte("package fixture\nimport \"testing\"\nfunc TestValue(t *testing.T) { if Value() != 7 { t.Fatal(Value()) } }\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := run([]string{"-root", root, "-write"}); err != nil {
		t.Fatalf("write mode failed: %v", err)
	}
	if err := run([]string{"-root", root}); err != nil {
		t.Fatalf("check mode rejected generated inventories: %v", err)
	}
	for _, module := range testcoverage.Modules {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(module.Log)))
		if err != nil {
			t.Fatalf("read %s: %v", module.Log, err)
		}
		if !strings.Contains(string(data), "| sample.go | "+module.Root+" | 1 | 1 | 100.0% | true | pass |") {
			t.Fatalf("%s lacks measured per-file coverage:\n%s", module.Log, data)
		}
	}
}

func TestRunTargetsOneModulePackageForScheduledCoverage(t *testing.T) {
	root := t.TempDir()
	moduleRoot := testcoverage.Modules[0].Root
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module "+moduleRoot+"\n\ngo 1.26.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sample.go"), []byte("package fixture\nfunc Value() int { return 7 }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "sample_test.go"), []byte("package fixture\nimport \"testing\"\nfunc TestValue(t *testing.T) { if Value() != 7 { t.Fatal(Value()) } }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"-root", root, "-write", "-module", "root", "-package", moduleRoot}); err != nil {
		t.Fatalf("single-package coverage command failed: %v", err)
	}
	checkpointDir := filepath.Join(root, ".artifacts", "coverage", "testcoverage", "root")
	entries, err := os.ReadDir(checkpointDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || filepath.Ext(entries[0].Name()) != ".json" {
		t.Fatalf("single-package command should save one checkpoint, got %v", entries)
	}
}

func TestRunTargetsPackageListForBulkCheckpointing(t *testing.T) {
	root := t.TempDir()
	module := testcoverage.Modules[0]
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.test/bulk\n\ngo 1.26.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"alpha", "beta"} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "sample.go"), []byte("package "+name+"\nfunc Value() int { return 7 }\n"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "sample_test.go"), []byte("package "+name+"\nimport \"testing\"\nfunc TestValue(t *testing.T) { if Value() != 7 { t.Fatal(Value()) } }\n"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	listPath := filepath.Join(root, "packages.txt")
	contents := "# selected module packages\nexample.test/bulk/alpha\n\nexample.test/bulk/beta\n"
	if err := os.WriteFile(listPath, []byte(contents), 0644); err != nil {
		t.Fatal(err)
	}
	if err := run([]string{"-root", root, "-write", "-module", "root", "-package-list", listPath, "-workers", "2"}); err != nil {
		t.Fatalf("bulk package coverage command failed: %v", err)
	}
	checkpointDir := filepath.Join(root, ".artifacts", "coverage", "testcoverage", module.Name)
	entries, err := os.ReadDir(checkpointDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("bulk package command should save two checkpoints, got %d", len(entries))
	}
	if err := run([]string{"-root", root, "-write", "-module", "root", "-package", "example.test/bulk/alpha", "-package-list", listPath}); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("combined single and bulk selection was accepted: %v", err)
	}
}

func TestRunWritesOnlyRequestedModuleInventory(t *testing.T) {
	root := t.TempDir()
	module := testcoverage.Modules[1]
	moduleRoot := filepath.Join(root, filepath.FromSlash(module.Path))
	if err := os.MkdirAll(moduleRoot, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleRoot, "go.mod"), []byte("module "+module.Root+"\n\ngo 1.26.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleRoot, "sample.go"), []byte("package fixture\nfunc Value() int { return 7 }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(moduleRoot, "sample_test.go"), []byte("package fixture\nimport \"testing\"\nfunc TestValue(t *testing.T) { if Value() != 7 { t.Fatal(Value()) } }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	outputPath := filepath.Join(".artifacts", "coverage", "test_coverage_nested.md")
	if err := run([]string{"-root", root, "-write", "-module", "nested", "-output", outputPath}); err != nil {
		t.Fatalf("module-scoped inventory write failed: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(outputPath)))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(got), "| sample.go | "+module.Root+" | 1 | 1 | 100.0% | true | pass |") {
		t.Fatalf("module-scoped inventory lacks measured coverage:\n%s", got)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(module.Log))); !os.IsNotExist(err) {
		t.Fatalf("candidate write unexpectedly created tracked inventory (stat error %v)", err)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(testcoverage.Modules[0].Log))); !os.IsNotExist(err) {
		t.Fatalf("module-scoped write unexpectedly created root inventory (stat error %v)", err)
	}
}
