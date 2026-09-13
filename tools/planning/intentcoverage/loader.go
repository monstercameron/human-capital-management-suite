package intentcoverage

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/corpus"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/intentmanifests"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/todogovernance"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/traceability"
)

// LoadRepository reads the live accepted-intent catalog
// (definitions/governance/intent-conformance-descriptors.yaml, INTENT-009),
// the feature-intent coverage registry
// (definitions/governance/feature-intent-coverage.yaml), the workflow
// catalogue (planning/workflows/catalog.yaml, shared by import with
// tools/planning/corpus/GOV-024 rather than re-parsed here) and the
// planning backlog (planning/todos.md, via tools/planning/todogovernance)
// into one Snapshot, and scans the repository's *_test.go sources
// (tools/planning/traceability.ScanTestNames) for executable oracle names.
// It never mutates definitions/ or planning/.
func LoadRepository(root string) (Snapshot, map[string]bool, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Snapshot{}, nil, fmt.Errorf("resolve repository root: %w", err)
	}

	descriptors, err := intentmanifests.LoadIntentManifestYAML(filepath.Join(root, "definitions", "governance", "intent-conformance-descriptors.yaml"))
	if err != nil {
		return Snapshot{}, nil, err
	}
	if err := intentmanifests.ValidateIntentManifestYAML(descriptors); err != nil {
		return Snapshot{}, nil, fmt.Errorf("accepted intent catalog failed validation: %w", err)
	}

	var snap Snapshot
	knownIDs := make(map[string]bool, len(descriptors))
	for _, d := range descriptors {
		id := fmt.Sprintf("%s/v%d", d.IntentTypeID, d.Version)
		knownIDs[id] = true
		snap.Intents = append(snap.Intents, Intent{
			ID:              id,
			Namespace:       NamespaceBaseline,
			ConformanceOnly: d.ConformanceOnly,
			Model:           append([]string(nil), d.Entities...),
			Property:        append([]string(nil), d.Properties...),
			Governance:      append([]string(nil), d.Authority...),
		})
	}

	coverage, err := intentmanifests.LoadFeatureIntentCoverageYAML(filepath.Join(root, "definitions", "governance", "feature-intent-coverage.yaml"))
	if err != nil {
		return Snapshot{}, nil, err
	}
	for _, f := range coverage.Features {
		if !knownIDs[f.BoundIntentID] {
			continue
		}
		snap.Gaps = append(snap.Gaps, CapabilityGap{
			FeatureID:            f.FeatureID,
			IntentID:             f.BoundIntentID,
			Disposition:          string(f.Disposition),
			DispositionTarget:    f.DispositionTarget,
			DispositionRationale: f.DispositionRationale,
			Capability:           f.Capability,
			Governance:           f.GovernanceProfile,
		})
	}

	corpusSnapshot, err := corpus.LoadRepository(root)
	if err != nil {
		return Snapshot{}, nil, err
	}
	for _, w := range corpusSnapshot.Workflows {
		for _, ref := range w.Intents {
			if knownIDs[ref] {
				snap.Workflows = append(snap.Workflows, WorkflowBinding{WorkflowID: w.ID, IntentID: ref})
			}
		}
	}

	_, records, _, err := todogovernance.LoadMarkdown(filepath.Join(root, "planning", "todos.md"))
	if err != nil {
		return Snapshot{}, nil, err
	}
	for _, rec := range records {
		evidenceText := strings.Join(rec.EvidenceLines, "\n")
		snap.Todos = append(snap.Todos, TodoBinding{
			TodoID:            rec.Todo.ID,
			Direct:            directIntents(rec.IntentContext["DIRECT"]),
			Sets:              splitTrim(rec.IntentContext["SETS"]),
			Test:              rec.Todo.Test,
			TestMatrix:        rec.Todo.TestMatrix,
			Done:              rec.Todo.Done,
			EvidenceTestNames: traceability.ExtractEvidenceTestNames(evidenceText),
			Retired:           rec.Todo.Retired,
		})
	}

	testNames, err := scanRepoTestNames(root)
	if err != nil {
		return Snapshot{}, nil, err
	}
	return snap, testNames, nil
}

// scanRepoTestNames scans only the repository's Go source roots for
// executable test/fuzz/benchmark names, reusing
// tools/planning/traceability.ScanTestNames per root rather than walking the
// bare repository root directly. Other lanes create and delete their own
// ephemeral ".gocache-*" build-cache directories directly under the
// repository root while this runs concurrently; walking the root itself
// races those directories and can fail with a transient "file not found"
// even though nothing under the scanned roots changed. test/ is included
// because the acceptance, bootstrap, tunnel and workflow suites that ticked
// evidence cites live there; omitting it reported them as dangling.
func scanRepoTestNames(root string) (map[string]bool, error) {
	out := make(map[string]bool)
	for _, sub := range []string{"cmd", "gen", "internal", "test", "tools"} {
		dir := filepath.Join(root, sub)
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, err
		}
		names, err := traceability.ScanTestNames(dir)
		if err != nil {
			return nil, fmt.Errorf("scan executable test names below %s: %w", dir, err)
		}
		for name := range names {
			out[name] = true
		}
	}
	return out, nil
}

// directIntents extracts the exact accepted-intent id tokens from a
// todo's INTENT CONTEXT DIRECT field, dropping "none" and blanks.
func directIntents(raw string) []string {
	var out []string
	for _, tok := range strings.Split(raw, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" || tok == "none" {
			continue
		}
		out = append(out, tok)
	}
	sort.Strings(out)
	return out
}

// splitTrim splits a comma-separated field (e.g. INTENT CONTEXT SETS) into
// trimmed, non-empty tokens.
func splitTrim(raw string) []string {
	var out []string
	for _, tok := range strings.Split(raw, ",") {
		tok = strings.TrimSpace(tok)
		if tok != "" {
			out = append(out, tok)
		}
	}
	return out
}

// LoadAllowlist reads exact owner-backed exceptions. A missing file is an
// empty allowlist, which keeps new orphans failing closed.
func LoadAllowlist(path string) ([]AllowlistEntry, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read intentcoverage allowlist: %w", err)
	}
	var out []AllowlistEntry
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, fmt.Errorf("parse intentcoverage allowlist: %w", err)
	}
	for i, item := range out {
		if strings.TrimSpace(item.Kind) == "" || strings.TrimSpace(item.ID) == "" || strings.TrimSpace(item.Owner) == "" {
			return nil, fmt.Errorf("allowlist[%d]: kind, id, and owner are required", i)
		}
	}
	return out, nil
}

// WriteAllowlist writes a deterministic owner-backed baseline file.
func WriteAllowlist(path string, entries []AllowlistEntry) error {
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal intentcoverage allowlist: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create intentcoverage allowlist directory: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write intentcoverage allowlist: %w", err)
	}
	return nil
}
