// Command plancheck runs the GOV-001/003/005/006/007/008/009/010/011/012/013/014/015/016/017
// governance checkers against the real repository tree from the command
// line, independent of `go test`. Each subcommand exits non-zero and
// prints every unresolved violation it finds; a checker with a known
// allow-list (currently only the evidence-freshness checker) treats an
// allow-listed violation as non-fatal.
//
// This does not change tools/planning/cmd/todoregistry's behavior; it is
// a separate binary.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/atomicity"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/authoritygate"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/boundarytests"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/coveragematrix"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/deferredimports"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/dependencygraph"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/depthvocab"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/evidence"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/federalbaseline"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/gateevidence"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/legalmatrix"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/manifest"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/plancontradiction"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/progress"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/researchquestions"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/scopeexchange"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/tddcontract"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/terminology"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/todogovernance"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/todoregistry"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/traceability"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/depedge"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/garbagedrawer"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/importgraph"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/layout"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: plancheck <manifest|scopeexchange|traceability|depthvocab|atomicity|evidence|deferredimports|terminology|plancontradiction|legalmatrix|federalbaseline|researchquestions|p1aevidence|authoritygate|coveragematrix|boundarytests|progress|dependencygraph|tddcontract|garbagedrawer|reachability> [-live] [root]")
		os.Exit(2)
	}

	cmd := os.Args[1]

	// p1aevidence accepts an optional -live flag in addition to the
	// positional root argument every other subcommand takes; strip it out
	// here so the flag can appear before or after root without disturbing
	// any other subcommand's argument parsing.
	var live bool
	var positional []string
	for _, a := range os.Args[2:] {
		if a == "-live" {
			live = true
			continue
		}
		positional = append(positional, a)
	}
	root := "."
	if len(positional) >= 1 {
		root = positional[0]
	}

	var err error
	switch cmd {
	case "manifest":
		err = runManifest()
	case "scopeexchange":
		err = runScopeExchange()
	case "traceability":
		err = runTraceability(root)
	case "depthvocab":
		err = runDepthVocab(root)
	case "atomicity":
		err = runAtomicity(root)
	case "evidence":
		err = runEvidence(root)
	case "deferredimports":
		err = runDeferredImports(root)
	case "terminology":
		err = runTerminology(root)
	case "plancontradiction":
		err = runPlanContradiction(root)
	case "legalmatrix":
		err = runLegalMatrix(root)
	case "federalbaseline":
		err = runFederalBaseline(root)
	case "researchquestions":
		err = runResearchQuestions(root)
	case "p1aevidence":
		err = runP1AEvidence(root, live)
	case "authoritygate":
		err = runAuthorityGate(root)
	case "coveragematrix":
		err = runCoverageMatrix(root)
	case "boundarytests":
		err = runBoundaryTests(root)
	case "progress":
		err = runProgress(root)
	case "dependencygraph":
		err = runDependencyGraph(root)
	case "tddcontract":
		err = runTDDContract(root)
	case "garbagedrawer":
		err = runGarbageDrawer(root)
	case "reachability":
		err = runReachability(root)
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand %q\n", cmd)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// runManifest and runScopeExchange have no repository data source to check
// yet: no delivery-manifest or scope-exchange record file exists in the
// tree (GOV-001/GOV-006 define the schema and validation rules; a future
// generator or manual manifest file is what these subcommands would then
// validate). They report that plainly rather than fabricating a check
// against nothing. The libraries themselves are fully covered by
// manifest.TestDeliveryManifestRejectsMissingRequiredFields and
// scopeexchange.TestScopeExchangeRejectsUnfundedAddition.
func runManifest() error {
	required := manifest.Validate(manifest.Manifest{})
	fmt.Printf("manifest: no delivery-manifest data source is wired into the repository yet (%d required fields defined); see tools/planning/manifest for the schema and manifest.Validate/CanonicalDigest for the library API\n", len(required))
	return nil
}

func runScopeExchange() error {
	required := scopeexchange.Validate(scopeexchange.Exchange{Requirement: manifest.Manifest{Gate: "GATE_A"}})
	fmt.Printf("scopeexchange: no scope-exchange record source is wired into the repository yet (%d required elements defined for a Gate A/B exchange); see tools/planning/scopeexchange for the schema and scopeexchange.Validate for the library API\n", len(required))
	return nil
}

func readTodos(root string) ([]todoregistry.Todo, error) {
	content, err := os.ReadFile(filepath.Join(root, "planning", "todos.md"))
	if err != nil {
		return nil, fmt.Errorf("read planning/todos.md: %w", err)
	}
	todos, parseErrs := todoregistry.ParseTodos(string(content))
	if len(parseErrs) > 0 {
		return nil, fmt.Errorf("parse planning/todos.md: %v", parseErrs)
	}
	return todos, nil
}

func walkMarkdown(root string, visit func(path, content string) error) error {
	dir := filepath.Join(root, "planning")
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return visit(path, string(content))
	})
}

func runTraceability(root string) error {
	todos, err := readTodos(root)
	if err != nil {
		return err
	}
	tests, err := traceability.ScanTestNames(root)
	if err != nil {
		return fmt.Errorf("scan test names: %w", err)
	}
	orphans := traceability.CheckTraceability(todos, tests)
	// REV-103-02: a tick is only evidence when its named TEST and TEST
	// MATRIX functions exist and its evidence names the command that ran
	// them. Ticked todos breaching that contract fail this subcommand
	// alongside the GOV-003 orphans above.
	ticked := traceability.CheckTickedTodos(todos, tests)
	if len(orphans) == 0 && len(ticked) == 0 {
		fmt.Println("traceability: OK")
		return nil
	}
	for _, o := range orphans {
		fmt.Println(o)
	}
	for _, f := range ticked {
		fmt.Println(f)
	}
	return fmt.Errorf("traceability: %d orphan(s), %d ticked todo gap(s)", len(orphans), len(ticked))
}

func runDepthVocab(root string) error {
	var total int
	err := walkMarkdown(root, func(path, content string) error {
		for _, v := range depthvocab.CheckDepthDeclarations(content) {
			total++
			fmt.Printf("%s: %s\n", path, v)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if total == 0 {
		fmt.Println("depthvocab: OK")
		return nil
	}
	return fmt.Errorf("depthvocab: %d violation(s)", total)
}

func runAtomicity(root string) error {
	todos, err := readTodos(root)
	if err != nil {
		return err
	}
	var total int
	for _, td := range todos {
		for _, v := range atomicity.CheckAtomicity(td.ID, td.Title, td.Green) {
			total++
			fmt.Println(v)
		}
	}
	if total == 0 {
		fmt.Println("atomicity: OK")
		return nil
	}
	return fmt.Errorf("atomicity: %d violation(s) (advisory - review before splitting todos)", total)
}

func runEvidence(root string) error {
	content, err := os.ReadFile(filepath.Join(root, "planning", "todos.md"))
	if err != nil {
		return fmt.Errorf("read planning/todos.md: %w", err)
	}
	knownGaps, err := evidence.LoadKnownGaps(filepath.Join(root, "definitions", "planning", "known-defects.yaml"))
	if err != nil {
		return fmt.Errorf("load known-defects.yaml: %w", err)
	}
	allowed := make(map[string]bool, len(knownGaps))
	for _, g := range knownGaps {
		allowed[g.ID+"|"+g.Issue] = true
	}

	var unresolved int
	for _, occ := range evidence.ScanEvidenceFields(string(content)) {
		for _, v := range evidence.CheckFreshness(occ.ID, occ.Label, occ.Body) {
			if allowed[v.ID+"|"+v.Issue] {
				continue
			}
			unresolved++
			fmt.Println(v)
		}
	}
	if unresolved == 0 {
		fmt.Println("evidence: OK")
		return nil
	}
	return fmt.Errorf("evidence: %d unallowed violation(s)", unresolved)
}

func runDeferredImports(root string) error {
	violations, err := deferredimports.ScanDir(filepath.Join(root, "internal"))
	if err != nil {
		return err
	}
	if len(violations) == 0 {
		fmt.Println("deferredimports: OK")
		return nil
	}
	for _, v := range violations {
		fmt.Println(v)
	}
	return fmt.Errorf("deferredimports: %d violation(s)", len(violations))
}

func runTerminology(root string) error {
	var total int
	err := walkMarkdown(root, func(path, content string) error {
		for _, v := range terminology.CheckCanonicalTerms(content) {
			total++
			fmt.Printf("%s: %s\n", path, v)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if total == 0 {
		fmt.Println("terminology: OK")
		return nil
	}
	return fmt.Errorf("terminology: %d violation(s)", total)
}

// listResearchFiles returns the basenames (e.g. "alabama.md") of every
// Markdown file directly inside planning/research/state-employment-law,
// excluding its README.md. It reuses walkMarkdown - the same
// planning/**/*.md walker readTodos's sibling subcommands already share -
// rather than adding a second directory scanner just to list one
// subdirectory's files.
func runLegalMatrix(root string) error {
	specPath := filepath.Join(root, "planning", "specs", "legal-rule-packs-and-state-configuration.md")
	specContent, err := os.ReadFile(specPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", specPath, err)
	}

	readmePath := filepath.Join(root, "planning", "specs", "README.md")
	readmeContent, err := os.ReadFile(readmePath)
	if err != nil {
		return fmt.Errorf("read %s: %w", readmePath, err)
	}

	registryPath := filepath.Join(root, "planning", "specs", "specification-ownership-registry.md")
	registryContent, err := os.ReadFile(registryPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", registryPath, err)
	}

	researchFiles, err := listResearchFiles(root)
	if err != nil {
		return fmt.Errorf("list state-employment-law research files: %w", err)
	}

	violations := legalmatrix.Check(string(specContent), researchFiles, string(readmeContent), string(registryContent))
	if len(violations) == 0 {
		fmt.Println("legalmatrix: OK")
		return nil
	}
	for _, v := range violations {
		fmt.Println(v)
	}
	return fmt.Errorf("legalmatrix: %d violation(s)", len(violations))
}

// runFederalBaseline validates LEGAL-017's single federal-law baseline
// against every state research file. The checker owns parsing only; this
// command owns the repository layout and reports all discovered conflicts in
// one invocation so a corpus edit cannot hide a second disagreement.
func runFederalBaseline(root string) error {
	researchDir := filepath.Join(root, "planning", "research", "state-employment-law")
	baselinePath := filepath.Join(researchDir, "us-federal.md")
	baseline, err := os.ReadFile(baselinePath)
	if err != nil {
		return fmt.Errorf("read %s: %w", baselinePath, err)
	}

	stateFiles, err := readStateResearchFiles(researchDir)
	if err != nil {
		return err
	}
	violations := federalbaseline.Check(string(baseline), stateFiles)
	if len(violations) == 0 {
		fmt.Println("federalbaseline: OK")
		return nil
	}
	for _, v := range violations {
		fmt.Println(v)
	}
	return fmt.Errorf("federalbaseline: %d violation(s)", len(violations))
}

// runResearchQuestions validates LEGAL-018's review-log closure and its
// release gates against the legal rule-pack contract and the state-research
// corpus. It deliberately reads the same on-disk sources a release review
// consumes instead of relying on a fixture or an inferred todo state.
func runResearchQuestions(root string) error {
	specPath := filepath.Join(root, "planning", "specs", "legal-rule-packs-and-state-configuration.md")
	spec, err := os.ReadFile(specPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", specPath, err)
	}

	researchDir := filepath.Join(root, "planning", "research", "state-employment-law")
	readmePath := filepath.Join(researchDir, "README.md")
	readme, err := os.ReadFile(readmePath)
	if err != nil {
		return fmt.Errorf("read %s: %w", readmePath, err)
	}
	stateFiles, err := readStateResearchFiles(researchDir)
	if err != nil {
		return err
	}

	violations := researchquestions.Check(string(spec), string(readme), stateFiles)
	if len(violations) == 0 {
		fmt.Println("researchquestions: OK")
		return nil
	}
	for _, v := range violations {
		fmt.Println(v)
	}
	return fmt.Errorf("researchquestions: %d violation(s)", len(violations))
}

// readStateResearchFiles loads the fifty per-state research files keyed by
// basename. README.md is the review log rather than a state source, and
// us-federal.md is the LEGAL-017 canonical baseline rather than a state file.
func readStateResearchFiles(researchDir string) (map[string]string, error) {
	entries, err := os.ReadDir(researchDir)
	if err != nil {
		return nil, fmt.Errorf("read state-employment-law directory %s: %w", researchDir, err)
	}

	files := make(map[string]string, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		if entry.Name() == "README.md" || entry.Name() == "us-federal.md" {
			continue
		}
		path := filepath.Join(researchDir, entry.Name())
		content, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		files[entry.Name()] = string(content)
	}
	return files, nil
}

func listResearchFiles(root string) ([]string, error) {
	researchDir := filepath.Join(root, "planning", "research", "state-employment-law")
	var files []string
	err := walkMarkdown(root, func(path, content string) error {
		if filepath.Dir(path) != researchDir {
			return nil
		}
		if filepath.Base(path) == "README.md" {
			return nil
		}
		files = append(files, filepath.Base(path))
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

func runPlanContradiction(root string) error {
	var total int
	err := walkMarkdown(root, func(path, content string) error {
		for _, v := range plancontradiction.CheckPlanningContradictions(content) {
			total++
			fmt.Printf("%s: %s\n", path, v)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if total == 0 {
		fmt.Println("plancontradiction: OK")
		return nil
	}
	return fmt.Errorf("plancontradiction: %d violation(s)", total)
}

// runP1AEvidence closes definitions/planning/gates/p1a-manifest.yaml's
// evidence entries against definitions/planning/gates/p1a-evidence-results.json
// (or, with live, a fresh `go test` run per entry) via gateevidence.Compile,
// prints the resulting NEXT-003 evidence-closure decision and every
// non-OK finding, and exits non-zero on GATE_BLOCKED. This is an
// evidence-closure verdict only, never the human-signed Gate A authority
// decision authoritygate.Record represents.
func runP1AEvidence(root string, live bool) error {
	manifestPath := filepath.Join(root, "definitions", "planning", "gates", "p1a-manifest.yaml")
	m, err := gateevidence.LoadP1AManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("load %s: %w", manifestPath, err)
	}

	resultsPath := filepath.Join(root, "definitions", "planning", "gates", "p1a-evidence-results.json")
	results, err := gateevidence.LoadResults(resultsPath)
	if err != nil {
		return fmt.Errorf("load %s: %w", resultsPath, err)
	}

	report, err := gateevidence.Compile(*m, results, gateevidence.CompileOptions{
		RepoRoot: root,
		Now:      time.Now(),
		Live:     live,
	})
	if err != nil {
		return fmt.Errorf("compile p1a evidence: %w", err)
	}

	fmt.Printf("p1aevidence: decision=%s manifest_todo=%s digest=sha256:%s as_of=%s\n",
		report.Decision, report.ManifestTodoID, report.ManifestDigest, report.AsOf)

	var nonOK int
	for _, f := range report.Findings {
		if f.Verdict == gateevidence.VerdictOK {
			continue
		}
		nonOK++
		fmt.Printf("%s %s (%s): %s - %s\n", f.TodoID, f.Test, f.Package, f.Verdict, f.Detail)
	}

	if report.Decision == gateevidence.GateDecisionBlocked {
		return fmt.Errorf("p1aevidence: GATE_BLOCKED (%d non-OK finding(s))", nonOK)
	}
	fmt.Println("p1aevidence: OK")
	return nil
}

// runAuthorityGate validates every recorded decision in
// definitions/planning/authority-gate-decision.yaml (structural GOV-010
// invariants and evidence-path resolution) and checks that no completed
// GATE_* todo has outrun a signed decision, skipping any violation
// allow-listed under known_authority_gate_gaps in known-defects.yaml.
func runAuthorityGate(root string) error {
	todos, err := readTodos(root)
	if err != nil {
		return err
	}

	decisionPath := filepath.Join(root, "definitions", "planning", "authority-gate-decision.yaml")
	records, err := authoritygate.Load(decisionPath)
	if err != nil {
		return fmt.Errorf("load %s: %w", decisionPath, err)
	}

	knownGaps, err := authoritygate.LoadKnownGaps(filepath.Join(root, "definitions", "planning", "known-defects.yaml"))
	if err != nil {
		return fmt.Errorf("load known-defects.yaml: %w", err)
	}
	allowed := make(map[string]bool, len(knownGaps))
	for _, g := range knownGaps {
		allowed[string(g.Gate)+"|"+g.Kind] = true
	}

	var all []authoritygate.Violation
	for _, r := range records {
		all = append(all, authoritygate.ValidateRecord(r)...)
		all = append(all, authoritygate.CheckEvidencePaths(r, root)...)
	}
	all = append(all, authoritygate.CheckUndecidedGates(todos, records)...)

	var unresolved int
	for _, v := range all {
		if allowed[string(v.Gate)+"|"+v.Kind] {
			continue
		}
		unresolved++
		fmt.Println(v)
	}
	if unresolved == 0 {
		fmt.Println("authoritygate: OK")
		return nil
	}
	return fmt.Errorf("authoritygate: %d unallowed violation(s)", unresolved)
}

// runCoverageMatrix regenerates the GOV-011 platform capability coverage
// matrix in check mode: it recomputes coveragematrix.Build over the real
// compiled-in capability/intent tables and the real todo corpus, fails on
// any evidence violation (a Done claim citing a nonexistent test), fails
// on any coverage violation (MISSING/IMPLIED) not allow-listed under
// known_coverage_gaps in known-defects.yaml, and fails if the recomputed
// matrix drifts from the checked-in
// definitions/planning/capability-coverage.yaml.
func runCoverageMatrix(root string) error {
	todosPath := filepath.Join(root, "planning", "todos.md")
	content, err := os.ReadFile(todosPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", todosPath, err)
	}
	todos, parseErrs := todoregistry.ParseTodos(string(content))
	if len(parseErrs) > 0 {
		return fmt.Errorf("parse %s: %v", todosPath, parseErrs)
	}
	contexts := coveragematrix.ParseIntentContexts(string(content))

	items, err := coveragematrix.AllItems()
	if err != nil {
		return fmt.Errorf("collect coverage items: %w", err)
	}
	existingTests, err := traceability.ScanTestNames(root)
	if err != nil {
		return fmt.Errorf("scan test names: %w", err)
	}

	entries, evidenceViolations := coveragematrix.Build(items, todos, contexts, existingTests)

	knownGaps, err := coveragematrix.LoadKnownGaps(filepath.Join(root, "definitions", "planning", "known-defects.yaml"))
	if err != nil {
		return fmt.Errorf("load known-defects.yaml: %w", err)
	}
	allowed := make(map[string]bool, len(knownGaps))
	for _, g := range knownGaps {
		allowed[g.ID] = true
	}

	var unresolved int
	for _, v := range evidenceViolations {
		unresolved++
		fmt.Println(v)
	}
	for _, v := range coveragematrix.CheckCoverage(entries) {
		if allowed[v.Item.ID] {
			continue
		}
		unresolved++
		fmt.Println(v)
	}

	want, err := coveragematrix.ToYAML(entries)
	if err != nil {
		return fmt.Errorf("render capability-coverage.yaml: %w", err)
	}
	coveragePath := filepath.Join(root, "definitions", "planning", "capability-coverage.yaml")
	got, err := os.ReadFile(coveragePath)
	if err != nil {
		return fmt.Errorf("read %s: %w", coveragePath, err)
	}
	if string(got) != string(want) {
		unresolved++
		fmt.Printf("%s: drifted from the recomputed coverage matrix (regenerate with HCMNEXT_UPDATE_GOLDEN=1 go test ./tools/planning/coveragematrix/...)\n", coveragePath)
	}

	if unresolved == 0 {
		fmt.Println("coveragematrix: OK")
		return nil
	}
	return fmt.Errorf("coveragematrix: %d unresolved issue(s)", unresolved)
}

// runBoundaryTests runs the GOV-013 architecture-boundary check against
// the real Go import graph: zero package-dependency-policy violations, no
// import cycle, and the coarse layer-to-layer edge set matching the
// committed testdata/layer-graph.golden.txt golden exactly.
func runBoundaryTests(root string) error {
	layoutManifest, err := layout.Load(filepath.Join(root, "definitions", "architecture", "repository-layout.yaml"))
	if err != nil {
		return fmt.Errorf("load repository-layout manifest: %w", err)
	}
	policy, err := depedge.Load(filepath.Join(root, "definitions", "architecture", "package-dependency-policy.yaml"))
	if err != nil {
		return fmt.Errorf("load package-dependency-policy manifest: %w", err)
	}
	graph, err := importgraph.Build(root, layoutManifest, policy)
	if err != nil {
		return fmt.Errorf("build import graph: %w", err)
	}

	var unresolved int
	for _, v := range graph.PolicyViolations {
		unresolved++
		fmt.Printf("forbidden dependency edge: %s -> %s violates %s\n", v.Importer, v.Imported, v.Rule)
	}
	if graph.Cycle != nil {
		unresolved++
		fmt.Printf("import cycle detected: %s\n", strings.Join(graph.Cycle, " -> "))
	}

	edges := boundarytests.LayerGraph(graph, policy)
	got := boundarytests.RenderGolden(edges)
	goldenPath := filepath.Join(root, "tools", "planning", "boundarytests", "testdata", "layer-graph.golden.txt")
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", goldenPath, err)
	}
	if diff, ok := boundarytests.GoldenDiff(string(want), got); !ok {
		unresolved++
		fmt.Printf("layer graph no longer matches %s (regenerate with HCMNEXT_UPDATE_GOLDEN=1 go test ./tools/planning/boundarytests/...):\n%s\n", goldenPath, diff)
	}

	if unresolved == 0 {
		fmt.Println("boundarytests: OK")
		return nil
	}
	return fmt.Errorf("boundarytests: %d unresolved issue(s)", unresolved)
}

// runProgress renders GOV-015's independent milestone counts from the live
// backlog. It intentionally does not fail merely because work remains: an
// incomplete report is the useful, truthful result. The COMPLETE count is
// calculated by progress.SummarizeMarkdown and never inferred from a checked
// Markdown box alone.
func runProgress(root string) error {
	path := filepath.Join(root, "planning", "todos.md")
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	report, err := progress.SummarizeMarkdown(string(content), time.Now())
	if err != nil {
		return fmt.Errorf("summarize %s: %w", path, err)
	}
	remaining := report.Count(progress.StageAuthored) - report.Count(progress.StageComplete)
	fmt.Printf("progress: %s remaining=%d\n", report, remaining)
	return nil
}

// runDependencyGraph validates raw dependency syntax and the resolved todo
// graph together. CheckMarkdown must receive the source Markdown rather than
// just readTodos' parsed result so prose and malformed range diagnostics keep
// their original line numbers.
func runDependencyGraph(root string) error {
	path := filepath.Join(root, "planning", "todos.md")
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	findings, err := dependencygraph.CheckMarkdown(string(content))
	if err != nil {
		return fmt.Errorf("check dependency graph: %w", err)
	}
	if len(findings) == 0 {
		fmt.Println("dependencygraph: OK")
		return nil
	}
	for _, finding := range findings {
		fmt.Println(finding)
	}
	return fmt.Errorf("dependencygraph: %d violation(s)", len(findings))
}

// runTDDContract validates GOV-017 against the authored planning corpus. It
// prints every source-located finding before returning an error, so a live
// corpus that has not yet satisfied red-first evidence cannot be mistaken for
// a passing command merely because the checker itself builds.
func runTDDContract(root string) error {
	path := filepath.Join(root, "planning", "todos.md")
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	// Use a repository-relative slash path in diagnostics so the stable
	// file/line contract does not vary between Windows and Unix runners.
	findings, err := tddcontract.CheckMarkdown(string(content), "planning/todos.md")
	if err != nil {
		return fmt.Errorf("check TDD contract: %w", err)
	}
	if len(findings) == 0 {
		fmt.Println("tddcontract: OK")
		return nil
	}
	for _, finding := range findings {
		fmt.Println(finding)
	}
	return fmt.Errorf("tddcontract: %d violation(s)", len(findings))
}

// runReachability adapts REV-103-01's binary-closure gate into plancheck:
// every ticked runtime todo must name a package linked into a shipped
// ./cmd/... binary. The closure comes from `go list -deps ./cmd/...` run
// against root; the todogovernance checker owns classification,
// exemptions and diagnostics, plancheck owns only command execution.
func runReachability(root string) error {
	todos, err := readTodos(root)
	if err != nil {
		return err
	}
	reachable, err := binaryClosure(root)
	if err != nil {
		return fmt.Errorf("list binary closure: %w", err)
	}
	findings := checkReachabilityTodos(todos, reachable)
	if len(findings) == 0 {
		fmt.Println("reachability: OK")
		return nil
	}
	for _, f := range findings {
		fmt.Println(f)
	}
	return fmt.Errorf("reachability: %d ticked runtime todo(s) name only packages no binary reaches", len(findings))
}

// binaryClosure runs `go list -deps ./cmd/...` in root and returns the
// normalized repo-relative package set.
func binaryClosure(root string) (map[string]bool, error) {
	cmd := exec.Command("go", "list", "-deps", "./cmd/...")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return todogovernance.NormalizeReachable(strings.Split(string(out), "\n")), nil
}

// checkReachabilityTodos is the exec-free seam between runReachability
// and the checker: command tests inject a synthetic closure here while
// production passes the live `go list` set.
func checkReachabilityTodos(todos []todoregistry.Todo, reachable map[string]bool) []todogovernance.ReachabilityFinding {
	return todogovernance.CheckReachability(todos, reachable)
}

// runGarbageDrawer adapts ARCH-GO-017's source-ownership policy into
// plancheck. The checker owns its deterministic default exception policy;
// plancheck owns only repository-root resolution and command diagnostics.
func runGarbageDrawer(root string) error {
	findings, err := garbagedrawer.Scan(root)
	if err != nil {
		return fmt.Errorf("scan garbage-drawer packages: %w", err)
	}
	if len(findings) == 0 {
		fmt.Println("garbagedrawer: OK")
		return nil
	}
	for _, finding := range findings {
		fmt.Println(finding)
	}
	return fmt.Errorf("garbagedrawer: %d violation(s)", len(findings))
}
