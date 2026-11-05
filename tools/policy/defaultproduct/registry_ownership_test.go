package defaultproduct

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDefaultproductOwnershipRegistryRow is ALIGN-045 owner/class closure.
// The A-F classification audit (2026-09-24) reopened ALIGN-045 because no
// accountable owner/class could be confirmed for this package from the
// authoritative specification-owner rows. This test pins the resolution:
// tools/policy/defaultproduct must carry a registry row naming one
// accountable owner role, a known registry status and a review trigger,
// matching the sibling policy-tool row (tools/policy/rlsparity).
func TestDefaultproductOwnershipRegistryRow(t *testing.T) {
	root := defaultproductRepoRoot(t)
	raw, err := os.ReadFile(filepath.Join(root, "planning", "specs", "specification-ownership-registry.md"))
	if err != nil {
		t.Fatalf("read specification-ownership-registry.md: %v", err)
	}
	row, ok := findDefaultproductRegistryRow(string(raw), "tools/policy/defaultproduct")
	if !ok {
		t.Fatalf("specification-ownership-registry.md has no row for tools/policy/defaultproduct; resolve the owner/class before closing ALIGN-045")
	}
	if row.owner == "" {
		t.Fatalf("defaultproduct registry row names no accountable owner role")
	}
	switch row.status {
	case "DEFINED", "ACTIVE_REGISTER", "REFERENCE", "DRAFT_CONTRACT":
	default:
		t.Fatalf("defaultproduct registry row has unknown status %q", row.status)
	}
	if row.trigger == "" {
		t.Fatalf("defaultproduct registry row names no review trigger")
	}
}

type defaultproductRegistryRow struct{ owner, status, trigger string }

// findDefaultproductRegistryRow extracts the owner/status/trigger cells of
// the table row whose first cell names target (backtick-quoted, as in the
// registry).
func findDefaultproductRegistryRow(doc, target string) (defaultproductRegistryRow, bool) {
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
		return defaultproductRegistryRow{
			owner:   cells[2],
			status:  strings.Trim(cells[3], "`"),
			trigger: cells[4],
		}, true
	}
	return defaultproductRegistryRow{}, false
}

// defaultproductRepoRoot returns the repository root by walking up from the
// test working directory until go.mod and the ownership registry are found.
func defaultproductRepoRoot(t *testing.T) string {
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
