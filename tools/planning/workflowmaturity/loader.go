// Live snapshot loading for the maturity gate. Every input is read by
// calling the sibling package that already owns it (workflowdesignjoin,
// workflowexpansion, designownership, scenariomatrix, workflowarchetypes,
// intentcoverage); this file performs no independent parsing of design,
// ownership, scenario or coverage data - only assembly.
package workflowmaturity

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/designownership"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/intentcoverage"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/scenariomatrix"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowarchetypes"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowdesign"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowdesignjoin"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowexpansion"
)

// hrCatalogs is the WF-DISC-002 six-catalog list every intent's
// catalog-only status is checked against.
var hrCatalogs = []string{
	filepath.Join("planning", "workflows", "people", "catalog.md"),
	filepath.Join("planning", "workflows", "workforce", "catalog.md"),
	filepath.Join("planning", "workflows", "rewards", "catalog.md"),
	filepath.Join("planning", "workflows", "lifecycle", "catalog.md"),
	filepath.Join("planning", "workflows", "leave", "catalog.md"),
	filepath.Join("planning", "workflows", "hr-service", "catalog.md"),
}

// DefaultIntentCoverageAllowlist is the same owner-backed exception file
// tools/planning/intentcoverage/cmd/intentcoverage reads by default; the
// maturity gate reuses it rather than maintaining a second allowlist for
// the same orphans.
const DefaultIntentCoverageAllowlist = "tools/planning/intentcoverage/testdata/orphans.allowlist.json"

// LoadSnapshot reads every live input below root through its owning
// sibling package and assembles one Snapshot. allowlistPath is the
// intentcoverage orphan allowlist to apply (pass "" for none); relative
// paths resolve against root.
func LoadSnapshot(root, allowlistPath string) (Snapshot, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Snapshot{}, fmt.Errorf("resolve repository root: %w", err)
	}

	accepted, records, err := workflowdesignjoin.LoadSnapshot(root)
	if err != nil {
		return Snapshot{}, fmt.Errorf("load design join snapshot: %w", err)
	}
	recordReport := workflowdesign.ValidateRecords(records)
	join, joinFindings := workflowdesignjoin.JoinRecords(accepted, records)

	expansionSnapshot, err := workflowexpansion.LoadSnapshot(root)
	if err != nil {
		return Snapshot{}, fmt.Errorf("load expansion snapshot: %w", err)
	}
	recordByDefinition := make(map[string]workflowdesign.DesignRecord, len(records))
	for _, r := range records {
		recordByDefinition[r.Definition] = r
	}
	graphs := make(map[string]*workflowexpansion.Graph, len(accepted))
	graphFindings := make(map[string][]workflowexpansion.Finding)
	scenarios := make(map[string]*scenariomatrix.Matrix, len(accepted))
	scenarioFindings := make(map[string][]scenariomatrix.Finding)
	for _, definition := range accepted {
		record, ok := recordByDefinition[definition]
		if !ok {
			continue
		}
		graph, findings := workflowexpansion.ExpandSnapshot(expansionSnapshot, definition, workflowexpansion.Delta{})
		graphs[definition] = graph
		if len(findings) > 0 {
			graphFindings[definition] = findings
		}
		profile := expansionSnapshot.Profiles[record.DomainProfile]
		matrix, sFindings := scenariomatrix.Generate(record, graph, profile)
		scenarios[definition] = matrix
		if len(sFindings) > 0 {
			scenarioFindings[definition] = sFindings
		}
	}

	ownershipSnapshot, ownershipAccepted, err := designownership.LoadSnapshot(root)
	if err != nil {
		return Snapshot{}, fmt.Errorf("load ownership snapshot: %w", err)
	}
	ownership, ownershipFindings := designownership.Compile(ownershipAccepted, ownershipSnapshot)

	archetypeReport, err := workflowarchetypes.ScanCatalogs(root, hrCatalogs)
	if err != nil {
		return Snapshot{}, fmt.Errorf("scan HR catalogs: %w", err)
	}
	catalogMapped := make(map[string]bool, len(archetypeReport.Rows))
	for _, row := range archetypeReport.Rows {
		catalogMapped[row.Intent] = true
	}

	coverageSnapshot, testNames, err := intentcoverage.LoadRepository(root)
	if err != nil {
		return Snapshot{}, fmt.Errorf("load intent coverage repository: %w", err)
	}
	var allowlist []intentcoverage.AllowlistEntry
	if allowlistPath != "" {
		resolved := allowlistPath
		if !filepath.IsAbs(resolved) {
			resolved = filepath.Join(root, allowlistPath)
		}
		allowlist, err = intentcoverage.LoadAllowlist(resolved)
		if err != nil {
			return Snapshot{}, fmt.Errorf("load intent coverage allowlist: %w", err)
		}
	}
	coverageReport := intentcoverage.Reconcile(coverageSnapshot, intentcoverage.Options{Allowlist: allowlist, TestNames: testNames})

	return Snapshot{
		Accepted:             accepted,
		Records:              records,
		RecordFindings:       recordReport.Findings,
		Join:                 join,
		JoinFindings:         joinFindings,
		Graphs:               graphs,
		GraphFindings:        graphFindings,
		Ownership:            ownership,
		OwnershipFindings:    ownershipFindings,
		Scenarios:            scenarios,
		ScenarioFindings:     scenarioFindings,
		Decisions:            nil, // see doc.go: no live decision sidecar is wired in the repository yet.
		CatalogMappedIntents: catalogMapped,
		IntentNodes:          coverageReport.Intents,
		Orphans:              coverageReport.NewOrphans,
	}, nil
}

// LoadCatalogClaims reads planning/workflows/catalog.md below root and
// parses its hand-typed per-flow status claims. See doc.go: this document
// is deliberately never fed into LoadSnapshot/Reconcile - only compared
// against their output by CrossCheckCatalog.
func LoadCatalogClaims(root string) ([]CatalogClaim, error) {
	path := filepath.Join(root, "planning", "workflows", "catalog.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return ParseCatalogClaims(string(data)), nil
}
