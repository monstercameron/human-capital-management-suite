// Live snapshot loading. Every compiler runs through the loader its own
// package already exports; this file adds only the selection facts and
// their downstream consumers, which no existing package enumerates.
//
// Selected owners are the INCLUDED intents of the signed P1A manifest and
// the selecting todos it binds (plus NEXT-002 for the manifest itself).
// Selected facts are those intents and the bound artifacts. A fact's
// consumers are the closure-witness SLICE/MODEL/ENDPOINT/TEST edges of the
// definition, the THREAT-001 slices covering it, and downstream product
// source (internal/, cmd/, test/), product-slices.yaml and the threat
// register carrying a bound artifact's path or digest. Governance tooling
// under tools/ is deliberately not a downstream consumer: a planning
// compiler reading a selection changes no slice, model, API, threat or test.
//
// Rejections have no machine-readable source in the repository yet, so the
// live snapshot carries none rather than inventing one.
package convergence

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/closurewitness"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/designclosure"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/gateevidence"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/gateevidence/selectionbind"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/intentcoverage"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/threatregister"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/todogovernance"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowmaturity"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/provenance"
)

// Repository paths the loader reads.
const (
	TrustedKeyFixture  = "tools/planning/gateevidence/testdata/dev-signing-key.yaml"
	ProductSlicesPath  = "definitions/planning/product-slices.yaml"
	ThreatRegisterPath = "definitions/planning/gates/threat-001-register.yaml"
)

// Fact kinds.
const (
	FactSelectedIntent    = "SELECTED_INTENT"
	FactSelectionArtifact = "SELECTION_ARTIFACT"
)

// downstreamSourceRoots are the product source roots scanned for selection
// artifact tokens.
var downstreamSourceRoots = []string{"cmd", "internal", "test"}

// witnessConsumerLayer maps closure-witness edge classes that are
// downstream consumers of a selected intent onto their layer.
var witnessConsumerLayer = map[closurewitness.EdgeClass]string{
	closurewitness.ClassSlice:    LayerSlice,
	closurewitness.ClassModel:    LayerModel,
	closurewitness.ClassEndpoint: LayerAPI,
	closurewitness.ClassTest:     LayerTest,
}

// bindingGapContract maps BIND-001 binding-gap kinds onto contracts so the
// allowlisted closing owner todo can claim the gap.
var bindingGapContract = map[string]Contract{
	"UNCLAIMED_CAPABILITY":     ContractCapability,
	"CLAIM_WITHOUT_CAPABILITY": ContractCapability,
	"NO_WIRE_METHOD":           ContractEndpoint,
	"AMBIGUOUS_WIRE_METHOD":    ContractEndpoint,
	"UNKNOWN_WIRE_METHOD":      ContractEndpoint,
	"STREAMING_WIRE_METHOD":    ContractEndpoint,
	"WIRE_METHOD_UNBOUND":      ContractEndpoint,
	"NO_HANDLER":               ContractHandler,
	"AMBIGUOUS_HANDLER":        ContractHandler,
	"MISSING_HANDLER_SYMBOL":   ContractHandler,
	"HANDLER_BOUND_TWICE":      ContractHandler,
	"NO_MODEL_BINDING":         ContractModel,
}

// LoadSnapshot runs every compiler over the live repository below root as
// of asOf (YYYY-MM-DD) and assembles the convergence snapshot. It never
// mutates the repository.
func LoadSnapshot(root, asOf string) (Snapshot, error) {
	now, err := time.Parse("2006-01-02", asOf)
	if err != nil {
		return Snapshot{}, fmt.Errorf("convergence: as-of must be a YYYY-MM-DD date: %w", err)
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return Snapshot{}, fmt.Errorf("convergence: resolve root: %w", err)
	}
	var snap Snapshot

	manifest, err := gateevidence.LoadP1AManifest(filepath.Join(root, filepath.FromSlash(gateevidence.P1AManifestPath)))
	if err != nil {
		return Snapshot{}, fmt.Errorf("convergence: load P1A manifest: %w", err)
	}
	phases, err := loadTodos(root, &snap)
	if err != nil {
		return Snapshot{}, err
	}
	selectedIntents := selectOwners(*manifest, phases, &snap)

	if err := loadSelection(root, now, *manifest, &snap); err != nil {
		return Snapshot{}, err
	}
	witnessSnap, err := loadClosureWitness(root, asOf, selectedIntents, &snap)
	if err != nil {
		return Snapshot{}, err
	}
	for _, d := range witnessSnap.Descriptors {
		snap.KnownOwners = append(snap.KnownOwners, d.Definition)
	}
	if err := loadDesignClosure(root, &snap); err != nil {
		return Snapshot{}, err
	}
	if err := loadIntentCoverage(root, &snap); err != nil {
		return Snapshot{}, err
	}
	if err := loadWorkflowMaturity(root, &snap); err != nil {
		return Snapshot{}, err
	}
	if err := loadThreatConsumers(root, witnessSnap, &snap); err != nil {
		return Snapshot{}, err
	}
	if err := loadArtifactConsumers(root, &snap); err != nil {
		return Snapshot{}, err
	}
	sort.Strings(snap.KnownOwners)
	return snap, nil
}

// loadTodos reads the backlog lifecycle and returns each todo's phase.
func loadTodos(root string, snap *Snapshot) (map[string]string, error) {
	_, records, parseErrs, err := todogovernance.LoadMarkdown(filepath.Join(root, "planning", "todos.md"))
	if err != nil {
		return nil, fmt.Errorf("convergence: load backlog: %w", err)
	}
	if len(parseErrs) > 0 {
		return nil, fmt.Errorf("convergence: planning/todos.md has %d structural parse errors, e.g. %v", len(parseErrs), parseErrs[0])
	}
	phases := map[string]string{}
	for _, rec := range records {
		t := rec.Todo
		phases[t.ID] = t.Phase
		snap.Todos = append(snap.Todos, TodoRef{ID: t.ID, Phase: t.Phase, Done: t.Done, Retired: t.Retired})
		snap.KnownOwners = append(snap.KnownOwners, t.ID)
		if t.Done || t.Retired {
			continue
		}
		for _, tok := range strings.Split(rec.IntentContext["DIRECT"], ",") {
			if tok = strings.TrimSpace(tok); tok != "" && tok != "none" {
				snap.Claims = append(snap.Claims, Claim{TodoID: t.ID, Owner: tok, Contract: ContractTodo, Source: "planning/todos.md DIRECT"})
			}
		}
	}
	return phases, nil
}

// selectOwners records the P1A intents and selecting todos as selected
// owners and facts, returning the selected intent set.
func selectOwners(m gateevidence.P1AManifest, phases map[string]string, snap *Snapshot) map[string]bool {
	intents := map[string]bool{}
	for _, it := range m.Intents {
		if it.Disposition != "INCLUDED" {
			continue
		}
		intents[it.ID] = true
		snap.Selected = append(snap.Selected, SelectedOwner{Owner: it.ID, Kind: OwnerKindIntent, Phase: m.Release})
		snap.Facts = append(snap.Facts, Fact{ID: "intent:" + it.ID, Kind: FactSelectedIntent, Owner: it.ID, Tokens: []string{it.ID}})
	}
	todos := append([]string{SelectionManifestOwner}, gateevidence.RequiredSelectionBindingTodoIDs...)
	for _, id := range todos {
		snap.Selected = append(snap.Selected, SelectedOwner{Owner: id, Kind: OwnerKindSelection, Phase: phases[id]})
	}
	for _, b := range m.SelectionBindings {
		snap.Facts = append(snap.Facts, Fact{ID: "selection:" + b.TodoID, Kind: FactSelectionArtifact, Owner: b.TodoID, Tokens: []string{b.Path, b.Digest}})
	}
	return intents
}

func loadSelection(root string, now time.Time, m gateevidence.P1AManifest, snap *Snapshot) error {
	priv, err := provenance.LoadSigningKeyFixture(filepath.Join(root, filepath.FromSlash(TrustedKeyFixture)))
	if err != nil {
		return fmt.Errorf("convergence: load trusted key: %w", err)
	}
	trusted := hex.EncodeToString(priv.Public().(ed25519.PublicKey))
	report, err := selectionbind.Evaluate(m, selectionbind.Options{Root: root, Now: now, TrustedPublicKey: trusted})
	if err != nil {
		return fmt.Errorf("convergence: evaluate selections: %w", err)
	}
	obs, unknowns := FromSelectionBind(report)
	snap.Observations = append(snap.Observations, obs...)
	snap.Unknowns = append(snap.Unknowns, unknowns...)
	return nil
}

func loadClosureWitness(root, asOf string, selected map[string]bool, snap *Snapshot) (closurewitness.Snapshot, error) {
	ws, err := closurewitness.LoadSnapshot(root, asOf)
	if err != nil {
		return closurewitness.Snapshot{}, fmt.Errorf("convergence: load closure witness: %w", err)
	}
	report, err := closurewitness.Compile(ws)
	if err != nil {
		return closurewitness.Snapshot{}, fmt.Errorf("convergence: compile closure witness: %w", err)
	}
	snap.Observations = append(snap.Observations, FromClosureWitness(report)...)
	snap.Unknowns = append(snap.Unknowns, ClosureWitnessUnknowns(ws)...)
	for _, w := range report.Witnesses {
		if !selected[w.Definition] {
			continue
		}
		for _, e := range w.Edges {
			layer, ok := witnessConsumerLayer[e.Class]
			if !ok {
				continue
			}
			other := e.To
			if other == w.Definition {
				other = e.From
			}
			snap.Consumers = append(snap.Consumers, ConsumerRef{Layer: layer, Ref: e.Registry + "#" + other, Tokens: []string{w.Definition}})
		}
	}
	definitionOf := map[string]string{}
	for _, c := range ws.Claims {
		definitionOf[c.CapabilityID] = c.DefinitionRef
	}
	for _, g := range ws.BindingGaps {
		contract, ok := bindingGapContract[g.Kind]
		owner := definitionOf[g.CapabilityID]
		if !ok || owner == "" || g.OwnerTodo == "" {
			continue
		}
		snap.Claims = append(snap.Claims, Claim{TodoID: g.OwnerTodo, Owner: owner, Contract: contract, Source: "internal/capability/binding allowlist " + g.Kind})
	}
	return ws, nil
}

func loadDesignClosure(root string, snap *Snapshot) error {
	ds, accepted, err := designclosure.LoadSnapshot(root)
	if err != nil {
		return fmt.Errorf("convergence: load design closure: %w", err)
	}
	register, findings := designclosure.Compile(accepted, ds)
	snap.Observations = append(snap.Observations, FromDesignClosure(register, findings)...)
	return nil
}

func loadIntentCoverage(root string, snap *Snapshot) error {
	is, testNames, err := intentcoverage.LoadRepository(root)
	if err != nil {
		return fmt.Errorf("convergence: load intent coverage: %w", err)
	}
	allowlist, err := intentcoverage.LoadAllowlist(filepath.Join(root, filepath.FromSlash(workflowmaturity.DefaultIntentCoverageAllowlist)))
	if err != nil {
		return fmt.Errorf("convergence: load intent coverage allowlist: %w", err)
	}
	report := intentcoverage.Reconcile(is, intentcoverage.Options{Allowlist: allowlist, TestNames: testNames})
	snap.Observations = append(snap.Observations, FromIntentCoverage(report, is)...)
	return nil
}

func loadWorkflowMaturity(root string, snap *Snapshot) error {
	ms, err := workflowmaturity.LoadSnapshot(root, workflowmaturity.DefaultIntentCoverageAllowlist)
	if err != nil {
		return fmt.Errorf("convergence: load workflow maturity: %w", err)
	}
	snap.Observations = append(snap.Observations, FromWorkflowMaturity(workflowmaturity.Reconcile(ms))...)
	return nil
}

// loadThreatConsumers makes every THREAT-001 slice a THREAT consumer of the
// intents its product slice names.
func loadThreatConsumers(root string, ws closurewitness.Snapshot, snap *Snapshot) error {
	register, err := threatregister.LoadRegister(filepath.Join(root, filepath.FromSlash(ThreatRegisterPath)))
	if err != nil {
		return fmt.Errorf("convergence: load threat register: %w", err)
	}
	intentsBySlice := map[string][]string{}
	for _, s := range ws.Slices {
		intentsBySlice[s.SliceID] = append(intentsBySlice[s.SliceID], s.Intents...)
	}
	for _, s := range register.Slices {
		tokens := intentsBySlice[s.SliceID]
		if len(tokens) == 0 {
			continue
		}
		snap.Consumers = append(snap.Consumers, ConsumerRef{Layer: LayerThreat, Ref: ThreatRegisterPath + "#" + s.SliceID, Tokens: append([]string(nil), tokens...)})
	}
	return nil
}

// loadArtifactConsumers scans downstream artifacts for the exact path and
// digest tokens of every bound selection artifact.
func loadArtifactConsumers(root string, snap *Snapshot) error {
	var tokens []string
	for _, f := range snap.Facts {
		if f.Kind == FactSelectionArtifact {
			tokens = append(tokens, f.Tokens...)
		}
	}
	scan := func(rel, layer string) error {
		content, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if errors.Is(err, fs.ErrNotExist) {
			return nil // vanished between walk and read, or absent registry
		}
		if err != nil {
			return fmt.Errorf("convergence: read %s: %w", rel, err)
		}
		if found := carried(string(content), tokens); len(found) > 0 {
			snap.Consumers = append(snap.Consumers, ConsumerRef{Layer: layer, Ref: rel, Tokens: found})
		}
		return nil
	}
	if err := scan(ProductSlicesPath, LayerSlice); err != nil {
		return err
	}
	if err := scan(ThreatRegisterPath, LayerThreat); err != nil {
		return err
	}
	for _, sub := range downstreamSourceRoots {
		base := filepath.Join(root, sub)
		err := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					return nil
				}
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			return scan(rel, sourceLayer(rel))
		})
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("convergence: scan %s: %w", sub, err)
		}
	}
	return nil
}

// sourceLayer classifies a downstream Go source file.
func sourceLayer(rel string) string {
	switch {
	case strings.HasSuffix(rel, "_test.go"), strings.HasPrefix(rel, "test/"):
		return LayerTest
	case strings.HasPrefix(rel, "internal/transport/"):
		return LayerAPI
	case strings.HasPrefix(rel, "internal/intent/modelbinding/"):
		return LayerModel
	}
	return LayerImplementation
}

// carried returns the sorted tokens content contains.
func carried(content string, tokens []string) []string {
	var found []string
	for _, tok := range tokens {
		if tok != "" && strings.Contains(content, tok) {
			found = append(found, tok)
		}
	}
	sort.Strings(found)
	return found
}
