package rlsparity_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRLSParityOwnershipRegistryRow is ALIGN-013 owner/class closure.
// The A-F classification audit (2026-09-24) reopened ALIGN-013 because no
// accountable owner/class could be confirmed for this package from the
// authoritative specification-owner rows. This test pins the resolution:
// tools/policy/rlsparity must carry a registry row naming one accountable
// owner role, a known registry status and a review trigger, matching the
// established planning/policy tool rows.
func TestRLSParityOwnershipRegistryRow(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "planning", "specs", "specification-ownership-registry.md"))
	if err != nil {
		t.Fatalf("read specification-ownership-registry.md: %v", err)
	}
	row, ok := findOwnershipRow(string(raw), "tools/policy/rlsparity")
	if !ok {
		t.Fatalf("specification-ownership-registry.md has no row for tools/policy/rlsparity; resolve the owner/class before closing ALIGN-013")
	}
	if row.owner == "" {
		t.Fatalf("rlsparity registry row names no accountable owner role")
	}
	switch row.status {
	case "DEFINED", "ACTIVE_REGISTER", "REFERENCE", "DRAFT_CONTRACT":
	default:
		t.Fatalf("rlsparity registry row has unknown status %q", row.status)
	}
	if row.trigger == "" {
		t.Fatalf("rlsparity registry row names no review trigger")
	}
}

type ownershipRow struct{ owner, status, trigger string }

// findOwnershipRow extracts the owner/status/trigger cells of the table row
// whose first cell names target (backtick-quoted, as in the registry).
func findOwnershipRow(doc, target string) (ownershipRow, bool) {
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
		return ownershipRow{
			owner:   cells[2],
			status:  strings.Trim(cells[3], "`"),
			trigger: cells[4],
		}, true
	}
	return ownershipRow{}, false
}
