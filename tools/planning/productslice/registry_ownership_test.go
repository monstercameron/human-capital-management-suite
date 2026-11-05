package productslice

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestProductsliceOwnershipRegistryRow is ALIGN-005 owner/class closure.
// The A-F classification audit (2026-09-24) reopened ALIGN-005 because no
// accountable owner/class could be confirmed for this package from the
// authoritative specification-owner rows. This test pins the resolution:
// tools/planning/productslice must carry a registry row naming one
// accountable owner role, a known registry status and a review trigger,
// matching the sibling planning-tool rows (workflowdecisions,
// workflowregistry).
func TestProductsliceOwnershipRegistryRow(t *testing.T) {
	root := productsliceRepoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "planning", "specs", "specification-ownership-registry.md"))
	if err != nil {
		t.Fatalf("read specification-ownership-registry.md: %v", err)
	}
	row, ok := findRegistryRow(string(raw), "tools/planning/productslice")
	if !ok {
		t.Fatalf("specification-ownership-registry.md has no row for tools/planning/productslice; resolve the owner/class before closing ALIGN-005")
	}
	if row.owner == "" {
		t.Fatalf("productslice registry row names no accountable owner role")
	}
	switch row.status {
	case "DEFINED", "ACTIVE_REGISTER", "REFERENCE", "DRAFT_CONTRACT":
	default:
		t.Fatalf("productslice registry row has unknown status %q", row.status)
	}
	if row.trigger == "" {
		t.Fatalf("productslice registry row names no review trigger")
	}
}

type registryRow struct{ owner, status, trigger string }

// findRegistryRow extracts the owner/status/trigger cells of the table row
// whose first cell names target (backtick-quoted, as in the registry).
func findRegistryRow(doc, target string) (registryRow, bool) {
	for _, line := range strings.Split(doc, "\n") {
		cells := strings.Split(line, "|")
		if len(cells) < 5 {
			continue
		}
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		if cells[1] != "`"+target+"`" {
			continue
		}
		return registryRow{
			owner:   cells[2],
			status:  strings.Trim(cells[3], "`"),
			trigger: cells[4],
		}, true
	}
	return registryRow{}, false
}

// productsliceRepoRoot returns the repository root by walking up from the
// test working directory until go.mod and the ownership registry are found.
func productsliceRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "planning", "specs", "specification-ownership-registry.md")); err == nil {
				return dir
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("could not locate repository root above working directory")
		}
		dir = parent
	}
}
