// Live repository loading for the design-closure register: accepted
// intents from the intent-conformance descriptors, DIRECT todo bindings
// and evidence from the planning backlog, workflow bindings from the
// corpus catalogue, open capability gaps from the feature-intent coverage
// registry, and executable test names from the scanned test sources.
// Deferral, rejection and selection records have no machine source yet:
// the snapshot carries them empty until NEXT-002/PHASE-001 create one,
// and the compiler keeps every unselected item visibly UNSELECTED rather
// than inventing a selection.
package designclosure

import (
	"crypto/sha256"
	"encoding/hex"
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

const descriptorsPath = "definitions/governance/intent-conformance-descriptors.yaml"

// LoadSnapshot reads the live repository below root into a closure
// snapshot with the accepted intent ids. It never mutates the repository.
func LoadSnapshot(root string) (Snapshot, []string, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Snapshot{}, nil, fmt.Errorf("resolve repository root: %w", err)
	}
	var snap Snapshot
	snap.Workflows = make(map[string][]string)
	snap.OpenGaps = make(map[string][]string)
	snap.Deferrals = make(map[string]Deferral)
	snap.Rejections = make(map[string]string)
	snap.Selections = make(map[string]string)

	descriptors, err := intentmanifests.LoadIntentManifestYAML(filepath.Join(root, descriptorsPath))
	if err != nil {
		return Snapshot{}, nil, err
	}
	if err := intentmanifests.ValidateIntentManifestYAML(descriptors); err != nil {
		return Snapshot{}, nil, fmt.Errorf("accepted intent catalog failed validation: %w", err)
	}
	var accepted []string
	for _, d := range descriptors {
		id := fmt.Sprintf("%s/v%d", d.IntentTypeID, d.Version)
		accepted = append(accepted, id)
		owner := ""
		if strings.TrimSpace(d.Family) != "" {
			owner = "family:" + d.Family
		}
		snap.Items = append(snap.Items, ItemInput{
			ID:              id,
			Source:          descriptorsPath + "#" + id,
			Owner:           owner,
			Phase:           d.Phase,
			ConformanceOnly: d.ConformanceOnly,
		})
	}

	_, records, parseErrs, err := todogovernance.LoadMarkdown(filepath.Join(root, "planning", "todos.md"))
	if err != nil {
		return Snapshot{}, nil, err
	}
	if len(parseErrs) > 0 {
		return Snapshot{}, nil, fmt.Errorf("planning/todos.md has %d structural parse errors, e.g. %v", len(parseErrs), parseErrs[0])
	}
	for _, rec := range records {
		evidenceText := strings.Join(rec.EvidenceLines, "\n")
		todo := TodoInput{
			ID:      rec.Todo.ID,
			Phase:   rec.Todo.Phase,
			Done:    rec.Todo.Done,
			Retired: rec.Todo.Retired,
			Direct:  directIntents(rec.IntentContext["DIRECT"]),
		}
		if rec.Todo.Test != "" {
			todo.TestNames = append(todo.TestNames, rec.Todo.Test)
		}
		for _, name := range rec.Todo.TestMatrix {
			if name = strings.TrimSpace(name); name != "" {
				todo.TestNames = append(todo.TestNames, name)
			}
		}
		todo.EvidenceTests = traceability.ExtractEvidenceTestNames(evidenceText)
		if strings.TrimSpace(evidenceText) != "" {
			sum := sha256.Sum256([]byte(evidenceText))
			todo.EvidenceDigest = "sha256:" + hex.EncodeToString(sum[:])
		}
		snap.Todos = append(snap.Todos, todo)
	}

	corpusSnapshot, err := corpus.LoadRepository(root)
	if err != nil {
		return Snapshot{}, nil, err
	}
	known := make(map[string]bool, len(accepted))
	for _, id := range accepted {
		known[id] = true
	}
	for _, workflow := range corpusSnapshot.Workflows {
		for _, ref := range workflow.Intents {
			if known[ref] {
				snap.Workflows[ref] = append(snap.Workflows[ref], workflow.ID)
			}
		}
	}

	coverage, err := intentmanifests.LoadFeatureIntentCoverageYAML(filepath.Join(root, "definitions", "governance", "feature-intent-coverage.yaml"))
	if err != nil {
		return Snapshot{}, nil, err
	}
	for _, feature := range coverage.Features {
		if !known[feature.BoundIntentID] {
			continue
		}
		if feature.Disposition == intentmanifests.DispositionMergedInto {
			continue
		}
		snap.OpenGaps[feature.BoundIntentID] = append(snap.OpenGaps[feature.BoundIntentID], feature.FeatureID)
	}

	snap.TestExists, err = scanTestNames(root)
	if err != nil {
		return Snapshot{}, nil, err
	}
	return snap, accepted, nil
}

// testSourceRoots are the Go source roots scanned for executable test
// names. test/ holds the acceptance, bootstrap, tunnel and workflow suites
// that ticked evidence cites; omitting it reported those tests as dangling.
// The bare repository root is not walked because other sessions create and
// delete build-cache directories directly under it.
var testSourceRoots = []string{"cmd", "gen", "internal", "test", "tools"}

// scanTestNames returns every executable test, fuzz and benchmark name
// declared below the present test source roots of root.
func scanTestNames(root string) (map[string]bool, error) {
	out := make(map[string]bool)
	for _, sub := range testSourceRoots {
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

// directIntents extracts the exact accepted-intent id tokens from a todo's
// INTENT CONTEXT DIRECT field, dropping "none" and blanks.
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
