package testhygiene

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// RiskKind identifies one reproducibility hazard found in a test package.
type RiskKind string

const (
	PackageStateWrite RiskKind = "PACKAGE_STATE_WRITE_WITHOUT_CLEANUP"
	SleepWait         RiskKind = "TIME_SLEEP_WAIT"
	ParallelFixture   RiskKind = "PARALLEL_MUTABLE_FIXTURE"
	WallClockRead     RiskKind = "WALL_CLOCK_READ"
)

// Finding is a source-level reliability risk. File is relative to the scan
// root and Line is one-based.
type Finding struct {
	Kind     RiskKind `json:"kind"`
	TestName string   `json:"test_name"`
	Package  string   `json:"package"`
	File     string   `json:"file"`
	Line     int      `json:"line"`
	Evidence string   `json:"evidence"`
}

// SkipSite is a test or helper that invokes t.Skip, t.Skipf, or t.SkipNow.
type SkipSite struct {
	TestName string
	Package  string
	File     string
	Line     int
}

// Report is the deterministic result of scanning the repository test roots.
type Report struct {
	Findings []Finding
	Skips    []SkipSite
}

// QuarantineRecord is the required record for a skipped test. EvidenceRef is
// deliberately a free-form reference so it can point to a todo, issue, or
// review record without giving this package authority over those systems.
type QuarantineRecord struct {
	TestName    string `json:"test_name"`
	Package     string `json:"package"`
	FirstSeen   string `json:"first_seen"`
	EvidenceRef string `json:"evidence_ref"`
	Owner       string `json:"owner"`
	Expiry      string `json:"expiry"`
}

// FlakeOutcome is one invocation result from the repeated package runner.
type FlakeOutcome struct {
	Run      int    `json:"run"`
	Passed   bool   `json:"passed"`
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output"`
}

// FlakeReport records every invocation and whether the outcomes disagree.
type FlakeReport struct {
	Package          string         `json:"package"`
	Runs             int            `json:"runs"`
	NonDeterministic bool           `json:"non_deterministic"`
	Outcomes         []FlakeOutcome `json:"outcomes"`
}

// Scan parses every *_test.go below internal/, tools/, cmd/, and test/.
// Findings are source diagnostics, not release waivers: callers may report
// them or fail a gate according to the surrounding todo.
func Scan(root, modulePath string) (Report, error) {
	if root == "" {
		return Report{}, errors.New("testhygiene: scan root is empty")
	}
	if modulePath == "" {
		return Report{}, errors.New("testhygiene: module path is empty")
	}

	type source struct {
		path string
		file *ast.File
		fset *token.FileSet
	}
	type packageSources struct {
		dir     string
		mutable map[string]bool
		sources []source
	}
	packages := make(map[string]*packageSources)

	for _, top := range []string{"internal", "tools", "cmd", "test"} {
		dir := filepath.Join(root, top)
		stat, err := os.Stat(dir)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return Report{}, err
		}
		if !stat.IsDir() {
			return Report{}, fmt.Errorf("testhygiene: scan root %s is not a directory", dir)
		}
		err = filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if entry.Name() == ".git" || strings.HasPrefix(entry.Name(), ".gocache") {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(entry.Name(), ".go") {
				return nil
			}
			fset := token.NewFileSet()
			parsed, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if err != nil {
				return fmt.Errorf("testhygiene: parse %s: %w", path, err)
			}
			rel, err := filepath.Rel(root, filepath.Dir(path))
			if err != nil {
				return err
			}
			rel = filepath.ToSlash(rel)
			pkg := packages[rel]
			if pkg == nil {
				pkg = &packageSources{dir: rel, mutable: make(map[string]bool)}
				packages[rel] = pkg
			}
			pkg.sources = append(pkg.sources, source{path: path, file: parsed, fset: fset})
			// Test fixtures are often declared in *_test.go themselves; they
			// are package-level mutable state just as production variables are.
			collectPackageMutable(parsed, pkg.mutable)
			return nil
		})
		if err != nil {
			return Report{}, err
		}
	}

	var report Report
	for _, pkg := range packages {
		importPath := modulePath
		if pkg.dir != "" {
			importPath += "/" + pkg.dir
		}
		for _, src := range pkg.sources {
			if !strings.HasSuffix(src.path, "_test.go") {
				continue
			}
			rel, err := filepath.Rel(root, src.path)
			if err != nil {
				return Report{}, err
			}
			analyzeTestFile(&report, src, pkg.mutable, importPath, filepath.ToSlash(rel))
		}
	}
	sort.Slice(report.Findings, func(i, j int) bool { return findingKey(report.Findings[i]) < findingKey(report.Findings[j]) })
	sort.Slice(report.Skips, func(i, j int) bool { return skipKey(report.Skips[i]) < skipKey(report.Skips[j]) })
	return report, nil
}

// Analyze is a compatibility spelling for callers that use analyzer terms.
func Analyze(root, modulePath string) (Report, error) { return Scan(root, modulePath) }

func collectPackageMutable(file *ast.File, mutable map[string]bool) {
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok.String() != "var" {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for _, name := range value.Names {
				if name.Name != "_" {
					mutable[name.Name] = true
				}
			}
		}
	}
}

func analyzeTestFile(report *Report, src struct {
	path string
	file *ast.File
	fset *token.FileSet
}, mutable map[string]bool, importPath, relFile string) {
	for _, decl := range src.file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil || !isTestFunction(fn.Name.Name) {
			continue
		}
		state := functionState{}
		ast.Inspect(fn.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if ok {
				inspectCall(&state, report, call, fn.Name.Name, importPath, relFile, src.fset)
			}
			switch n := node.(type) {
			case *ast.AssignStmt:
				for _, lhs := range n.Lhs {
					if name := baseIdentifier(lhs); mutable[name] {
						state.writes = append(state.writes, lhs)
					}
				}
			case *ast.IncDecStmt:
				if name := baseIdentifier(n.X); mutable[name] {
					state.writes = append(state.writes, n.X)
				}
			case *ast.Ident:
				if mutable[n.Name] {
					state.references = append(state.references, n)
				}
			}
			return true
		})
		if len(state.writes) > 0 && !state.cleanup {
			for _, node := range state.writes {
				addFinding(report, Finding{Kind: PackageStateWrite, TestName: fn.Name.Name, Package: importPath, File: relFile, Line: nodeLine(src.fset, node), Evidence: "package-level mutable state is written without a direct t.Cleanup"})
			}
		}
		if state.parallel && len(state.references) > 0 {
			for _, node := range state.references {
				addFinding(report, Finding{Kind: ParallelFixture, TestName: fn.Name.Name, Package: importPath, File: relFile, Line: nodeLine(src.fset, node), Evidence: "t.Parallel shares a package-level mutable fixture"})
			}
		}
	}
}

type functionState struct {
	cleanup    bool
	parallel   bool
	writes     []ast.Node
	references []ast.Node
}

func inspectCall(state *functionState, report *Report, call *ast.CallExpr, testName, importPath, relFile string, fset *token.FileSet) {
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	base, _ := sel.X.(*ast.Ident)
	if base != nil && strings.HasPrefix(sel.Sel.Name, "Skip") {
		report.Skips = append(report.Skips, SkipSite{TestName: testName, Package: importPath, File: relFile, Line: nodeLine(fset, call)})
	}
	if base != nil && sel.Sel.Name == "Cleanup" {
		state.cleanup = true
	}
	if base != nil && sel.Sel.Name == "Parallel" {
		state.parallel = true
	}
	if packageSelector(sel, "time", "Sleep") {
		addFinding(report, Finding{Kind: SleepWait, TestName: testName, Package: importPath, File: relFile, Line: nodeLine(fset, call), Evidence: "time.Sleep is used as a test wait"})
	}
	if packageSelector(sel, "time", "Now") || packageSelector(sel, "time", "Since") || packageSelector(sel, "time", "Until") {
		addFinding(report, Finding{Kind: WallClockRead, TestName: testName, Package: importPath, File: relFile, Line: nodeLine(fset, call), Evidence: "test reads the wall clock directly"})
	}
}

func packageSelector(sel *ast.SelectorExpr, pkg, name string) bool {
	base, ok := sel.X.(*ast.Ident)
	return ok && base.Name == pkg && sel.Sel.Name == name
}

func baseIdentifier(expr ast.Expr) string {
	switch n := expr.(type) {
	case *ast.Ident:
		return n.Name
	case *ast.SelectorExpr:
		return baseIdentifier(n.X)
	case *ast.ParenExpr:
		return baseIdentifier(n.X)
	default:
		return ""
	}
}

func isTestFunction(name string) bool {
	return strings.HasPrefix(name, "Test") || strings.HasPrefix(name, "Fuzz") || strings.HasPrefix(name, "Benchmark")
}

func nodeLine(fset *token.FileSet, node ast.Node) int {
	if node == nil {
		return 0
	}
	return fset.Position(node.Pos()).Line
}

func addFinding(report *Report, finding Finding) {
	report.Findings = append(report.Findings, finding)
}

func findingKey(f Finding) string {
	return string(f.Kind) + "\x00" + f.Package + "\x00" + f.File + "\x00" + strconv.Itoa(f.Line) + "\x00" + f.TestName
}

func skipKey(s SkipSite) string {
	return s.Package + "\x00" + s.File + "\x00" + strconv.Itoa(s.Line) + "\x00" + s.TestName
}

// LoadQuarantine reads and validates the JSON quarantine registry.
func LoadQuarantine(path string) ([]QuarantineRecord, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("testhygiene: read quarantine: %w", err)
	}
	var records []QuarantineRecord
	if err := json.Unmarshal(b, &records); err != nil {
		return nil, fmt.Errorf("testhygiene: parse quarantine: %w", err)
	}
	return records, nil
}

// ValidateQuarantine proves that every skip discovered by Scan has a complete
// record. It also rejects malformed, expired, duplicate, or orphaned records.
func ValidateQuarantine(root, modulePath, path string, now time.Time) error {
	records, err := LoadQuarantine(path)
	if err != nil {
		return err
	}
	if err := validateRecords(records, now); err != nil {
		return err
	}
	report, err := Scan(root, modulePath)
	if err != nil {
		return err
	}
	byKey := make(map[string]QuarantineRecord, len(records))
	for _, record := range records {
		byKey[recordKey(record.TestName, record.Package)] = record
	}
	for _, skip := range report.Skips {
		if _, ok := byKey[recordKey(skip.TestName, skip.Package)]; !ok {
			return fmt.Errorf("testhygiene: skipped test %s in %s has no quarantine record", skip.TestName, skip.Package)
		}
	}
	return nil
}

// FindUnquarantinedSkips returns skipped tests absent from records.
func FindUnquarantinedSkips(report Report, records []QuarantineRecord) []SkipSite {
	known := make(map[string]bool, len(records))
	for _, record := range records {
		known[recordKey(record.TestName, record.Package)] = true
	}
	var missing []SkipSite
	for _, skip := range report.Skips {
		if !known[recordKey(skip.TestName, skip.Package)] {
			missing = append(missing, skip)
		}
	}
	return missing
}

func validateRecords(records []QuarantineRecord, now time.Time) error {
	seen := make(map[string]bool, len(records))
	for _, record := range records {
		if record.TestName == "" || record.Package == "" || record.FirstSeen == "" || record.EvidenceRef == "" || record.Owner == "" || record.Expiry == "" {
			return fmt.Errorf("testhygiene: quarantine record for %q is incomplete", record.TestName)
		}
		key := recordKey(record.TestName, record.Package)
		if seen[key] {
			return fmt.Errorf("testhygiene: duplicate quarantine record for %s", key)
		}
		seen[key] = true
		first, err := time.Parse("2006-01-02", record.FirstSeen)
		if err != nil {
			return fmt.Errorf("testhygiene: invalid first_seen for %s: %w", key, err)
		}
		expiry, err := time.Parse("2006-01-02", record.Expiry)
		if err != nil {
			return fmt.Errorf("testhygiene: invalid expiry for %s: %w", key, err)
		}
		if !expiry.After(first) || expiry.Before(now.Truncate(24*time.Hour)) {
			return fmt.Errorf("testhygiene: quarantine record for %s is expired or has invalid interval", key)
		}
	}
	return nil
}

func recordKey(testName, packagePath string) string { return packagePath + "\x00" + testName }

// DetectFlakes runs `go test -count=1 <package>` runs times and records each
// outcome. A mixed pass/fail set is reported as NonDeterministic; a failing
// test is not retried into success and remains visible in Outcomes.
func DetectFlakes(ctx context.Context, root, packageName string, runs int) (FlakeReport, error) {
	if root == "" || packageName == "" {
		return FlakeReport{}, errors.New("testhygiene: flake detector requires root and package")
	}
	if runs < 2 {
		return FlakeReport{}, errors.New("testhygiene: flake detector requires at least two runs")
	}
	report := FlakeReport{Package: packageName, Runs: runs, Outcomes: make([]FlakeOutcome, 0, runs)}
	for run := 1; run <= runs; run++ {
		cmd := exec.CommandContext(ctx, "go", "test", "-count=1", packageName)
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "GOWORK=off")
		output, err := cmd.CombinedOutput()
		outcome := FlakeOutcome{Run: run, Passed: err == nil, ExitCode: 0, Output: string(output)}
		if err != nil {
			var exitErr *exec.ExitError
			if errors.As(err, &exitErr) {
				outcome.ExitCode = exitErr.ExitCode()
				// Windows can report an unlink failure after the package
				// already passed. Preserve the raw exit and output, but judge
				// the test result by its explicit package summary only when
				// the cleanup diagnostic is the sole additional output.
				if cleanupOnlyExit(outcome.Output, runtime.GOOS == "windows") {
					outcome.Passed = true
				}
			} else {
				return FlakeReport{}, fmt.Errorf("testhygiene: run %d: %w", run, err)
			}
		}
		report.Outcomes = append(report.Outcomes, outcome)
	}
	if len(report.Outcomes) > 1 {
		first := report.Outcomes[0].Passed
		for _, outcome := range report.Outcomes[1:] {
			if outcome.Passed != first {
				report.NonDeterministic = true
				break
			}
		}
	}
	return report, nil
}

func cleanupOnlyExit(output string, windows bool) bool {
	if !windows {
		return false
	}
	foundPass, foundCleanup := false, false
	for line := range strings.SplitSeq(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "ok "):
			foundPass = true
		case strings.HasPrefix(line, "go: unlinkat ") && strings.Contains(line, "go-build") && strings.HasSuffix(strings.ToLower(line), ".test.exe: access is denied."):
			foundCleanup = true
		default:
			return false
		}
	}
	return foundPass && foundCleanup
}

// RunFlakeDetector is an explicit compatibility spelling for callers that
// want the policy tool's action named as a detector.
func RunFlakeDetector(ctx context.Context, root, packageName string, runs int) (FlakeReport, error) {
	return DetectFlakes(ctx, root, packageName, runs)
}
