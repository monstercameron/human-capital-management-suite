// Live snapshot loading. Every row is read through the package that
// already owns its registry; this file parses no registry format of its
// own except the checked-in capability-coverage.yaml, whose row type
// tools/planning/coveragematrix does not export. A file-backed registry
// that is missing marks its edge class UNSOURCED instead of loading as
// empty, so an absent registry can never masquerade as a clean one.
package closurewitness

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/capability/binding"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/definitions"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/modelbinding"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/manifest"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/coveragematrix"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/evidence"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/intentmanifests"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/productslice"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/scopeceiling"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/todoregistry"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/traceability"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowmaturity"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/enginecoverage"
)

// handlerScanRoots mirrors internal/capability/binding's live-tree scan
// roots (livetree_test.go): internal/transport holds the typed RPC
// methods and internal/intent/app holds the composition cell that binds
// BOOTSTRAP capabilities to real handlers.
var handlerScanRoots = []string{"internal/transport", "internal/intent/app"}

// LoadSnapshot reads every registry below root into a snapshot judged as
// of asOf (YYYY-MM-DD). It never mutates the repository.
func LoadSnapshot(root, asOf string) (Snapshot, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Snapshot{}, fmt.Errorf("closurewitness: resolve root: %w", err)
	}
	snap := Snapshot{AsOf: asOf}
	steps := []func(string, *Snapshot) error{
		loadSource,
		loadCeiling,
		loadSlices,
		loadModels,
		loadEngines,
		loadBindings,
		loadEndpoints,
		loadScenarios,
		loadTodos,
		loadCoverage,
		loadTests,
		loadWaivers,
	}
	for _, step := range steps {
		if err := step(root, &snap); err != nil {
			return Snapshot{}, err
		}
	}
	sort.Slice(snap.Unsourced, func(i, j int) bool { return classRank(snap.Unsourced[i]) < classRank(snap.Unsourced[j]) })
	return snap, nil
}

// present reports whether a registry file exists; any error other than
// not-exist is returned so an unreadable registry fails loudly.
func present(path string) (bool, error) {
	_, err := os.Stat(path)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	return false, fmt.Errorf("closurewitness: stat %s: %w", path, err)
}

func loadSource(root string, snap *Snapshot) error {
	descriptors, err := intentmanifests.LoadIntentManifestYAML(filepath.Join(root, filepath.FromSlash(RegistryDescriptors)))
	if err != nil {
		return fmt.Errorf("closurewitness: load source descriptors: %w", err)
	}
	for _, d := range descriptors {
		raw, err := json.Marshal(d)
		if err != nil {
			return fmt.Errorf("closurewitness: digest descriptor %s: %w", d.IntentTypeID, err)
		}
		sum := sha256.Sum256(raw)
		snap.Descriptors = append(snap.Descriptors, DescriptorRow{
			Definition:  fmt.Sprintf("%s/v%d", d.IntentTypeID, d.Version),
			DisplayName: d.DisplayName,
			Phase:       d.Phase,
			RowDigest:   "sha256:" + hex.EncodeToString(sum[:]),
		})
	}
	for _, d := range definitions.All() {
		snap.Catalog = append(snap.Catalog, CatalogRow{Definition: d.Ref.String(), DisplayName: d.DisplayName, Release: d.Release.String()})
	}
	return nil
}

func loadCeiling(root string, snap *Snapshot) error {
	path := filepath.Join(root, filepath.FromSlash(RegistryCeiling))
	ok, err := present(path)
	if err != nil {
		return err
	}
	if !ok {
		snap.Unsourced = append(snap.Unsourced, ClassPhaseGate)
		return nil
	}
	m, err := scopeceiling.LoadManifest(path)
	if err != nil {
		return fmt.Errorf("closurewitness: load scope ceiling: %w", err)
	}
	for _, item := range m.Intents {
		snap.Ceiling = append(snap.Ceiling, CeilingRow{Definition: item.ID, Gate: string(item.Gate), Disposition: string(item.Disposition)})
	}
	return nil
}

func loadSlices(root string, snap *Snapshot) error {
	path := filepath.Join(root, filepath.FromSlash(RegistrySlices))
	ok, err := present(path)
	if err != nil {
		return err
	}
	if !ok {
		snap.Unsourced = append(snap.Unsourced, ClassSlice)
		return nil
	}
	registry, err := productslice.LoadRegistryYAML(path)
	if err != nil {
		return fmt.Errorf("closurewitness: load product slices: %w", err)
	}
	verified := registry.VerifyDigest() == nil
	for _, s := range registry.Slices {
		snap.Slices = append(snap.Slices, SliceRow{
			SliceID: s.SliceID, Version: s.Version,
			Intents:        append([]string(nil), s.BusinessIntents...),
			Capabilities:   append([]string(nil), s.Capabilities...),
			DigestVerified: verified,
		})
	}
	return nil
}

func loadModels(_ string, snap *Snapshot) error {
	table, err := modelbinding.BindCatalog()
	if err != nil {
		return fmt.Errorf("closurewitness: bind model catalog: %w", err)
	}
	for _, b := range table.Bindings {
		row := ModelBindingRow{Definition: b.Definition.String()}
		for _, e := range b.Entities {
			row.Entities = append(row.Entities, e.Ref())
		}
		snap.ModelBindings = append(snap.ModelBindings, row)
	}
	for _, g := range table.Gaps {
		snap.ModelGaps = append(snap.ModelGaps, ModelGapRow{Definition: g.Definition.String(), Element: g.Element, Detail: g.Detail})
	}
	return nil
}

func loadEngines(root string, snap *Snapshot) error {
	report, err := enginecoverage.Scan(root, enginecoverage.Options{})
	if err != nil {
		return fmt.Errorf("closurewitness: scan engine coverage: %w", err)
	}
	for _, r := range report.Responsibilities {
		row := EngineRow{Intent: r.Intent, Computation: r.Computation, Package: r.Package}
		for _, f := range report.Findings {
			byRow := f.Intent == r.Intent && f.Computation == r.Computation && f.Package == r.Package
			byPackage := f.Intent == "" && f.Package == r.Package
			if byRow || byPackage {
				row.Findings = append(row.Findings, f.Kind)
			}
		}
		snap.Engines = append(snap.Engines, row)
	}
	return nil
}

func loadBindings(root string, snap *Snapshot) error {
	sources := map[string]string{}
	for _, rel := range handlerScanRoots {
		base := filepath.Join(root, filepath.FromSlash(rel))
		walkErr := filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(d.Name(), ".go") || strings.HasSuffix(d.Name(), "_test.go") {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			relPath, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			sources[filepath.ToSlash(relPath)] = string(data)
			return nil
		})
		if walkErr != nil {
			return fmt.Errorf("closurewitness: read handler sources below %s: %w", rel, walkErr)
		}
	}
	if len(sources) == 0 {
		return fmt.Errorf("closurewitness: handler scan roots %v hold no Go source; symbol checks would pass vacuously", handlerScanRoots)
	}
	index, err := binding.ScanHandlerSymbols(sources)
	if err != nil {
		return fmt.Errorf("closurewitness: scan handler symbols: %w", err)
	}
	table, err := binding.Build(index)
	if err != nil {
		return fmt.Errorf("closurewitness: build binding table: %w", err)
	}
	registry, err := capability.NewBootstrapRegistry()
	if err != nil {
		return fmt.Errorf("closurewitness: build capability registry: %w", err)
	}
	for _, rec := range registry.List() {
		snap.Capabilities = append(snap.Capabilities, CapabilityRow{CapabilityID: rec.Definition.ID, Version: rec.Definition.Version})
	}
	for _, c := range binding.Claims() {
		snap.Claims = append(snap.Claims, ClaimRow{CapabilityID: c.CapabilityID, Version: c.CapabilityVersion, DefinitionRef: c.DefinitionRef})
	}
	for _, e := range table.Entries {
		snap.BindingEntries = append(snap.BindingEntries, BindingEntryRow{CapabilityID: e.Capability.ID, Wire: e.Wire.Ref(), Handler: e.Handler.Ref()})
	}
	allow := binding.AllowlistByGapID()
	for _, g := range table.Gaps {
		snap.BindingGaps = append(snap.BindingGaps, BindingGapRow{
			Kind: string(g.Kind), CapabilityID: g.Capability, Subject: g.Subject, OwnerTodo: allow[g.ID()].OwnerTodo,
		})
	}
	return nil
}

func loadEndpoints(_ string, snap *Snapshot) error {
	dispositions, err := manifest.BuildDefaultDispositionReport()
	if err != nil {
		return fmt.Errorf("closurewitness: build endpoint dispositions: %w", err)
	}
	for _, d := range dispositions.Intents {
		snap.IntentDispositions = append(snap.IntentDispositions, IntentDispositionRow{
			Definition: d.DefinitionRef, Category: string(d.Category), Justification: d.Justification,
			ServingEndpoints: append([]string(nil), d.ServingEndpoints...),
		})
	}
	m, err := manifest.Build()
	if err != nil {
		return fmt.Errorf("closurewitness: build endpoint manifest: %w", err)
	}
	for _, e := range m.Endpoints {
		snap.Endpoints = append(snap.Endpoints, EndpointRow{
			EndpointID: e.EndpointID, Disposition: string(e.Disposition),
			AcceptedDefinitions: append([]string(nil), e.AcceptedIntentDefinitionRefs...),
		})
	}
	return nil
}

func loadScenarios(root string, snap *Snapshot) error {
	maturity, err := workflowmaturity.LoadSnapshot(root, "")
	if err != nil {
		return fmt.Errorf("closurewitness: load scenario matrices: %w", err)
	}
	defs := map[string]bool{}
	for def := range maturity.Scenarios {
		defs[def] = true
	}
	for def := range maturity.ScenarioFindings {
		defs[def] = true
	}
	for _, def := range sortedKeys(defs) {
		row := ScenarioRow{Definition: def}
		if matrix := maturity.Scenarios[def]; matrix != nil {
			for _, s := range matrix.Scenarios {
				row.ScenarioIDs = append(row.ScenarioIDs, s.ID)
			}
			row.NotApplicable = len(matrix.NotApplicable)
		}
		for _, f := range maturity.ScenarioFindings[def] {
			row.Findings = append(row.Findings, f.Code+": "+f.Detail)
		}
		snap.Scenarios = append(snap.Scenarios, row)
	}
	return nil
}

func loadTodos(root string, snap *Snapshot) error {
	path := filepath.Join(root, filepath.FromSlash(RegistryTodos))
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("closurewitness: read %s: %w", path, err)
	}
	content := string(raw)
	todos, parseErrs := todoregistry.ParseTodos(content)
	if len(parseErrs) > 0 {
		return fmt.Errorf("closurewitness: %s has %d parse errors, e.g. %v", RegistryTodos, len(parseErrs), parseErrs[0])
	}
	dates := map[string][]string{}
	for _, occ := range evidence.ScanEvidenceFields(content) {
		if record, ok := evidence.ParseEvidenceField(occ.ID, occ.Label, occ.Body); ok {
			dates[occ.ID] = append(dates[occ.ID], record.Timestamp)
		}
	}
	for _, t := range todos {
		row := TodoRow{
			ID: t.ID, Done: t.Done, Retired: t.Retired, PrimaryTest: t.Test,
			EvidenceTests: traceability.ExtractEvidenceTestNames(t.Evidence),
			EvidenceDates: dates[t.ID],
		}
		if strings.TrimSpace(t.Evidence) != "" {
			sum := sha256.Sum256([]byte(t.Evidence))
			row.EvidenceDigest = "sha256:" + hex.EncodeToString(sum[:])
		}
		snap.Todos = append(snap.Todos, row)
	}
	contexts := coveragematrix.ParseIntentContexts(content)
	for _, item := range coveragematrix.IntentItems() {
		for _, c := range coveragematrix.ClaimsFor(item, contexts) {
			snap.TodoClaims = append(snap.TodoClaims, TodoClaimRow{Intent: item.ID, TodoID: c.TodoID, Kind: string(c.Kind)})
		}
	}
	ids := make([]string, 0, len(contexts))
	for id := range contexts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		ic := contexts[id]
		for _, token := range ic.Direct {
			snap.ContextTokens = append(snap.ContextTokens, ContextTokenRow{TodoID: id, Field: "DIRECT", Token: token})
		}
		for _, token := range ic.Intents {
			snap.ContextTokens = append(snap.ContextTokens, ContextTokenRow{TodoID: id, Field: "INTENTS", Token: token})
		}
	}
	return nil
}

// coverageFile is the checked-in capability-coverage.yaml shape
// (tools/planning/coveragematrix ToYAML).
type coverageFile struct {
	Items []struct {
		ID     string   `yaml:"id"`
		Kind   string   `yaml:"kind"`
		State  string   `yaml:"state"`
		Claims []string `yaml:"claims"`
		Tests  []string `yaml:"tests"`
	} `yaml:"items"`
}

func loadCoverage(root string, snap *Snapshot) error {
	path := filepath.Join(root, filepath.FromSlash(RegistryCoverage))
	ok, err := present(path)
	if err != nil {
		return err
	}
	if !ok {
		snap.Unsourced = append(snap.Unsourced, ClassTodo)
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("closurewitness: read %s: %w", path, err)
	}
	return parseCoverage(raw, snap)
}

func parseCoverage(raw []byte, snap *Snapshot) error {
	var file coverageFile
	if err := yaml.Unmarshal(raw, &file); err != nil {
		return fmt.Errorf("closurewitness: parse %s: %w", RegistryCoverage, err)
	}
	for _, item := range file.Items {
		if item.Kind != string(coveragematrix.KindIntent) {
			continue
		}
		row := CoverageRow{Intent: item.ID, State: item.State, Tests: append([]string(nil), item.Tests...)}
		for _, claim := range item.Claims {
			todo, kind, found := strings.Cut(claim, ":")
			if found && kind == ClaimDirect {
				row.DirectTodos = append(row.DirectTodos, todo)
			}
		}
		snap.Coverage = append(snap.Coverage, row)
	}
	return nil
}

// testSourceRoots are the Go source roots scanned for executable test
// names. test/ holds the bootstrap, tunnel and workflow suites that ticked
// evidence cites; the bare repository root is not walked because other
// sessions create and delete build-cache directories directly under it.
var testSourceRoots = []string{"cmd", "gen", "internal", "test", "tools"}

func loadTests(root string, snap *Snapshot) error {
	snap.TestExists = map[string]bool{}
	for _, sub := range testSourceRoots {
		dir := filepath.Join(root, sub)
		ok, err := present(dir)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		names, err := traceability.ScanTestNames(dir)
		if err != nil {
			return fmt.Errorf("closurewitness: scan test names below %s: %w", dir, err)
		}
		for name := range names {
			snap.TestExists[name] = true
		}
	}
	return nil
}

func loadWaivers(root string, snap *Snapshot) error {
	path := filepath.Join(root, filepath.FromSlash(RegistryKnownDefects))
	ok, err := present(path)
	if err != nil {
		return err
	}
	if !ok {
		snap.Unsourced = append(snap.Unsourced, ClassEvidence)
		return nil
	}
	gaps, err := evidence.LoadKnownGaps(path)
	if err != nil {
		return fmt.Errorf("closurewitness: load evidence waivers: %w", err)
	}
	for _, g := range gaps {
		snap.Waivers = append(snap.Waivers, WaiverRow{TodoID: g.ID, Issue: g.Issue, Expiry: g.Expiry})
	}
	return nil
}
