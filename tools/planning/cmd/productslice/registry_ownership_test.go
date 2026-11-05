package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/productslice"
)

// TestCmdOwnershipRegistryRow is ALIGN-001 owner/class closure for the
// generator command. The A-F classification audit (2026-09-24) reopened
// ALIGN-001 because no accountable owner/class could be confirmed from the
// authoritative specification-owner rows; the library row for
// tools/planning/productslice is pinned in that package, and this test pins
// the sibling row for tools/planning/cmd/productslice: one accountable
// owner role, a known registry status and a review trigger.
func TestCmdOwnershipRegistryRow(t *testing.T) {
	root, err := productslice.RepoRoot()
	if err != nil {
		t.Fatalf("RepoRoot: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "planning", "specs", "specification-ownership-registry.md"))
	if err != nil {
		t.Fatalf("read specification-ownership-registry.md: %v", err)
	}
	row, ok := findCmdRegistryRow(string(raw), "tools/planning/cmd/productslice")
	if !ok {
		t.Fatalf("specification-ownership-registry.md has no row for tools/planning/cmd/productslice; resolve the owner/class before closing ALIGN-001")
	}
	if row.owner == "" {
		t.Fatalf("cmd/productslice registry row names no accountable owner role")
	}
	switch row.status {
	case "DEFINED", "ACTIVE_REGISTER", "REFERENCE", "DRAFT_CONTRACT":
	default:
		t.Fatalf("cmd/productslice registry row has unknown status %q", row.status)
	}
	if row.trigger == "" {
		t.Fatalf("cmd/productslice registry row names no review trigger")
	}
}

type cmdRegistryRow struct{ owner, status, trigger string }

// findCmdRegistryRow extracts the owner/status/trigger cells of the table
// row whose first cell names target (backtick-quoted, as in the registry).
func findCmdRegistryRow(doc, target string) (cmdRegistryRow, bool) {
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
		return cmdRegistryRow{
			owner:   cells[2],
			status:  strings.Trim(cells[3], "`"),
			trigger: cells[4],
		}, true
	}
	return cmdRegistryRow{}, false
}
