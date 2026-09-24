// Package testcoverage generates and checks the repository's per-file Go
// statement coverage inventories.
package testcoverage

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Module describes one Go module whose inventory is maintained in planning/.
type Module struct {
	Path string
	Name string
	Root string
	Log  string
}

// Exclusion records a file outside the per-file test rule with its concrete
// reason. Exclusions are exact repository-relative paths.
type Exclusion struct {
	Path   string
	Kind   string
	Reason string
}

// Repository modules and the two permitted command-wrapper exclusions.
var Modules = []Module{
	{Path: ".", Name: "root", Root: "github.com/monstercameron/human-capital-management-suite", Log: "planning/test_coverage_root.md"},
	{Path: "src/blocks/go", Name: "nested", Root: "github.com/monstercameron/human-capital-management-suite-executor", Log: "planning/test_coverage_nested.md"},
}

var Exclusions = []Exclusion{
	{Path: "tools/planning/cmd/pilotblueprint/main.go", Kind: "thin command wrapper", Reason: "All business behavior is delegated to the tested tools/planning/pilotblueprint, pilotprovider, and pilotjurisdiction libraries."},
	{Path: "tools/gen/librarystrategy/cmd/generatelibrarystrategy/main.go", Kind: "thin generator wrapper", Reason: "The command delegates its generation behavior to the tested tools/gen/librarystrategy package."},
	{Path: "tools/gen/schemaflux/cmd/modelgen/main.go", Kind: "thin generator wrapper", Reason: "The command delegates its generation behavior to the tested tools/gen/schemaflux package."},
}

const checkpointVersion = 1

type listedPackage struct {
	Dir            string
	ImportPath     string
	Name           string
	GoFiles        []string
	CgoFiles       []string
	IgnoredGoFiles []string
	TestGoFiles    []string
	XTestGoFiles   []string
}

type sourceRow struct {
	dir, file, importPath string
	sourceHash, testHash  string
	statements, covered   int
	tested, generated     bool
	testResult            string
}

type packageCheckpoint struct {
	Version   int               `json:"version"`
	Module    string            `json:"module"`
	Package   string            `json:"package"`
	InputHash string            `json:"inputHash"`
	Coverage  map[string][2]int `json:"coverage"`
	Passed    bool              `json:"passed"`
}

// Generate runs go list and go test -coverprofile for each package and
// returns a deterministic Markdown inventory based on the resulting profiles.
func Generate(ctx context.Context, repo string, module Module) ([]byte, error) {
	return generate(ctx, repo, module, "")
}

func generate(ctx context.Context, repo string, module Module, checkpointDir string) ([]byte, error) {
	var err error
	repo, err = filepath.Abs(repo)
	if err != nil {
		return nil, err
	}
	moduleRoot := filepath.Join(repo, module.Path)
	packages, err := listPackages(ctx, moduleRoot)
	if err != nil {
		return nil, err
	}
	rows, err := inventoryRowsForPackages(ctx, repo, module, packages)
	if err != nil {
		return nil, err
	}
	rowsByPackage := make(map[string][]sourceRow, len(packages))
	for _, row := range rows {
		rowsByPackage[row.importPath] = append(rowsByPackage[row.importPath], row)
	}
	coverageByPackage := make(map[string]map[string][2]int, len(packages))
	statusByPackage := make(map[string]string, len(packages))
	for _, p := range packages {
		packageRows := rowsByPackage[p.ImportPath]
		if len(packageRows) == 0 {
			continue
		}
		if len(p.TestGoFiles)+len(p.XTestGoFiles) == 0 {
			coverageByPackage[p.ImportPath] = map[string][2]int{}
			statusByPackage[p.ImportPath] = "no tests"
			continue
		}
		inputHash, err := packageInputHash(packageRows, p)
		if err != nil {
			return nil, fmt.Errorf("hash coverage inputs for %s: %w", p.ImportPath, err)
		}
		checkpointPath := ""
		if checkpointDir != "" {
			checkpointPath = checkpointFile(checkpointDir, module, p.ImportPath, inputHash)
			cached, ok, err := readCheckpoint(checkpointPath, module, p.ImportPath, inputHash)
			if err != nil {
				return nil, fmt.Errorf("coverage checkpoint for %s: %w", p.ImportPath, err)
			}
			if ok {
				coverageByPackage[p.ImportPath] = cached.Coverage
				statusByPackage[p.ImportPath] = "pass"
				continue
			}
		}
		profile, passed, testOutput, err := coverPackage(ctx, moduleRoot, p.ImportPath)
		if err != nil {
			if checkpointDir != "" {
				_, _ = writeFailureLog(checkpointDir, module, p.ImportPath, inputHash, testOutput)
			}
			return nil, fmt.Errorf("coverage for %s: %w", p.ImportPath, err)
		}
		coverage, err := parseProfile(profile)
		if err != nil {
			return nil, fmt.Errorf("coverage profile for %s: %w", p.ImportPath, err)
		}
		coverageByPackage[p.ImportPath] = coverage
		statusByPackage[p.ImportPath] = "fail"
		if passed {
			statusByPackage[p.ImportPath] = "pass"
			if checkpointPath != "" {
				checkpoint := packageCheckpoint{Version: checkpointVersion, Module: module.Path, Package: p.ImportPath, InputHash: inputHash, Coverage: coverage, Passed: true}
				if err := writeCheckpoint(checkpointPath, checkpoint); err != nil {
					return nil, fmt.Errorf("write coverage checkpoint for %s: %w", p.ImportPath, err)
				}
			}
		} else if checkpointDir != "" {
			if logPath, logErr := writeFailureLog(checkpointDir, module, p.ImportPath, inputHash, testOutput); logErr == nil {
				statusByPackage[p.ImportPath] = "fail: " + filepath.ToSlash(logPath)
			}
		}
	}
	for i := range rows {
		counts := countsForFile(coverageByPackage[rows[i].importPath], rows[i].file)
		rows[i].statements, rows[i].covered = counts[0], counts[1]
		rows[i].testResult = statusByPackage[rows[i].importPath]
	}
	return render(module, repo, rows), nil
}

func packageInputHash(rows []sourceRow, p listedPackage) (string, error) {
	h := sha256.New()
	io.WriteString(h, p.ImportPath+"\n")
	files := packageGoFiles(p)
	for _, name := range files {
		digest, err := hashFile(filepath.Join(p.Dir, name))
		if err != nil {
			return "", err
		}
		io.WriteString(h, name+" "+digest+"\n")
	}
	if len(rows) > 0 {
		io.WriteString(h, "tests "+rows[0].testHash+"\n")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func checkpointFile(dir string, module Module, importPath, inputHash string) string {
	sum := sha256.Sum256([]byte(importPath + "\n" + inputHash))
	return filepath.Join(dir, module.Name, hex.EncodeToString(sum[:])+".json")
}

func readCheckpoint(path string, module Module, importPath, inputHash string) (packageCheckpoint, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return packageCheckpoint{}, false, nil
	}
	if err != nil {
		return packageCheckpoint{}, false, err
	}
	var cached packageCheckpoint
	if err := json.Unmarshal(data, &cached); err != nil {
		return packageCheckpoint{}, false, err
	}
	if cached.Version != checkpointVersion || cached.Module != module.Path || cached.Package != importPath || cached.InputHash != inputHash || !cached.Passed {
		return packageCheckpoint{}, false, nil
	}
	return cached, true, nil
}

func writeCheckpoint(path string, checkpoint packageCheckpoint) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.Marshal(checkpoint)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "checkpoint-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func countsForFile(profile map[string][2]int, name string) [2]int {
	for path, counts := range profile {
		if filepath.Base(filepath.FromSlash(path)) == name {
			return counts
		}
	}
	return [2]int{}
}

// Check validates the checkout's file and test digests against both logs.
func Check(ctx context.Context, repo string, modules []Module) error {
	var problems []string
	for _, m := range modules {
		rows, err := inventoryRows(ctx, repo, m)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", m.Log, err))
			continue
		}
		path := filepath.Join(repo, filepath.FromSlash(m.Log))
		got, err := os.ReadFile(path)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", m.Log, err))
			continue
		}
		if err := checkLog(got, rows, m); err != nil {
			problems = append(problems, fmt.Sprintf("%s is stale: %v; run go run ./tools/planning/cmd/testcoverage -write", m.Log, err))
		}
	}
	if len(problems) != 0 {
		return errors.New(strings.Join(problems, "\n"))
	}
	return nil
}

func inventoryRows(ctx context.Context, repo string, module Module) ([]sourceRow, error) {
	var err error
	repo, err = filepath.Abs(repo)
	if err != nil {
		return nil, err
	}
	packages, err := listPackages(ctx, filepath.Join(repo, module.Path))
	if err != nil {
		return nil, err
	}
	return inventoryRowsForPackages(ctx, repo, module, packages)
}

func inventoryRowsForPackages(ctx context.Context, repo string, module Module, packages []listedPackage) ([]sourceRow, error) {
	rows := make([]sourceRow, 0)
	for _, p := range packages {
		tests := append(append([]string{}, p.TestGoFiles...), p.XTestGoFiles...)
		testHash, err := hashFiles(p.Dir, tests)
		if err != nil {
			return nil, err
		}
		files := packageGoFiles(p)
		for _, name := range files {
			full := filepath.Join(p.Dir, name)
			if isGenerated(full) {
				continue
			}
			rel, err := filepath.Rel(repo, full)
			if err != nil {
				return nil, err
			}
			rel = filepath.ToSlash(rel)
			if isExcluded(rel) {
				continue
			}
			sourceHash, err := hashFile(full)
			if err != nil {
				return nil, err
			}
			rows = append(rows, sourceRow{dir: filepath.ToSlash(filepath.Dir(rel)), file: filepath.Base(rel), importPath: p.ImportPath, sourceHash: sourceHash, testHash: testHash, tested: len(tests) > 0})
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].dir == rows[j].dir {
			return rows[i].file < rows[j].file
		}
		return rows[i].dir < rows[j].dir
	})
	return rows, nil
}

func hashFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func hashFiles(dir string, names []string) (string, error) {
	sort.Strings(names)
	h := sha256.New()
	for _, name := range names {
		digest, err := hashFile(filepath.Join(dir, name))
		if err != nil {
			return "", err
		}
		io.WriteString(h, name+" "+digest+"\n")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func checkLog(data []byte, rows []sourceRow, module Module) error {
	logged := map[string][5]string{}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "| ") || strings.Contains(line, "| ---") {
			continue
		}
		fields := strings.Split(strings.Trim(line, "|"), "|")
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}
		if len(fields) != 10 || fields[0] == "dir" {
			continue
		}
		logged[fields[0]+"/"+fields[1]] = [5]string{fields[7], fields[8], fields[9], fields[2], fields[6]}
	}
	if len(logged) != len(rows) {
		return fmt.Errorf("file rows differ: log has %d, checkout has %d", len(logged), len(rows))
	}
	for _, r := range rows {
		got, ok := logged[r.dir+"/"+r.file]
		if !ok {
			return fmt.Errorf("missing %s/%s", r.dir, r.file)
		}
		if got[1] != r.sourceHash || got[2] != r.testHash {
			return fmt.Errorf("source or test digest changed for %s/%s", r.dir, r.file)
		}
		if !r.tested {
			return fmt.Errorf("hand-written file %s/%s has no package tests or named exclusion", r.dir, r.file)
		}
		if got[3] != r.importPath || got[4] != strconv.FormatBool(r.tested) {
			return fmt.Errorf("package or test status changed for %s/%s", r.dir, r.file)
		}
		if got[0] != "pass" {
			return fmt.Errorf("coverage test run did not pass for %s/%s", r.dir, r.file)
		}
		if validation := validateCoverageFields(data, r.dir+"/"+r.file); validation != "" {
			return errors.New(validation)
		}
	}
	loggedExclusions, err := parseExclusions(data)
	if err != nil {
		return err
	}
	wantExclusions := moduleExclusions(module)
	if len(loggedExclusions) != len(wantExclusions) {
		return fmt.Errorf("exclusion rows differ: log has %d, checkout allows %d", len(loggedExclusions), len(wantExclusions))
	}
	for _, x := range wantExclusions {
		if got, ok := loggedExclusions[x.Path]; !ok {
			return fmt.Errorf("missing named exclusion %s", x.Path)
		} else if got != [2]string{x.Kind, x.Reason} {
			return fmt.Errorf("named exclusion changed for %s", x.Path)
		}
	}
	return nil
}

func parseExclusions(data []byte) (map[string][2]string, error) {
	lines := strings.Split(string(data), "\n")
	start := -1
	for i, line := range lines {
		if line == "## Exclusions" {
			start = i
			break
		}
	}
	if start < 0 || start+2 >= len(lines) || lines[start+1] != "" || lines[start+2] != "| file | kind | reason |" {
		return nil, errors.New("missing or malformed exclusions table")
	}
	if start+3 >= len(lines) || lines[start+3] != "| --- | --- | --- |" {
		return nil, errors.New("missing or malformed exclusions table separator")
	}
	result := make(map[string][2]string)
	for _, line := range lines[start+4:] {
		if line == "" {
			break
		}
		if !strings.HasPrefix(line, "| ") || !strings.HasSuffix(line, " |") {
			return nil, fmt.Errorf("malformed exclusion row %q", line)
		}
		fields := strings.Split(strings.Trim(line, "|"), "|")
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}
		if len(fields) != 3 || fields[0] == "" || fields[1] == "" || fields[2] == "" {
			return nil, fmt.Errorf("malformed exclusion row %q", line)
		}
		if _, exists := result[fields[0]]; exists {
			return nil, fmt.Errorf("duplicate exclusion row %s", fields[0])
		}
		result[fields[0]] = [2]string{fields[1], fields[2]}
	}
	return result, nil
}

func validateCoverageFields(data []byte, key string) string {
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "| ") {
			continue
		}
		fields := strings.Split(strings.Trim(line, "|"), "|")
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}
		if len(fields) != 10 || fields[0]+"/"+fields[1] != key {
			continue
		}
		statements, err1 := strconv.Atoi(fields[3])
		covered, err2 := strconv.Atoi(fields[4])
		if err1 != nil || err2 != nil || statements < 0 || covered < 0 || covered > statements {
			return "invalid statement counts for " + key
		}
		want := "n/a"
		if statements > 0 {
			want = fmt.Sprintf("%.1f%%", 100*float64(covered)/float64(statements))
		}
		if fields[5] != want {
			return "coverage percentage disagrees with counts for " + key
		}
		return ""
	}
	return "missing coverage row for " + key
}

// Write regenerates and writes each inventory.
func Write(ctx context.Context, repo string, modules []Module) error {
	for _, module := range modules {
		if err := WriteModule(ctx, repo, module); err != nil {
			return err
		}
	}
	return nil
}

// WriteModule regenerates one inventory from its module's current sources and
// the passing per-package checkpoints already on disk.
func WriteModule(ctx context.Context, repo string, module Module) error {
	return WriteModuleTo(ctx, repo, module, module.Log)
}

// WriteModuleTo writes one generated inventory to an explicit repository path.
func WriteModuleTo(ctx context.Context, repo string, module Module, outputPath string) error {
	absRepo, err := filepath.Abs(repo)
	if err != nil {
		return err
	}
	checkpointDir := filepath.Join(absRepo, ".artifacts", "coverage", "testcoverage")
	data, err := generate(ctx, absRepo, module, checkpointDir)
	if err != nil {
		return err
	}
	if outputPath == "" {
		outputPath = module.Log
	}
	logPath := outputPath
	if !filepath.IsAbs(logPath) {
		logPath = filepath.Join(absRepo, filepath.FromSlash(logPath))
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(logPath), ".testcoverage-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := os.Rename(tmpPath, logPath); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// CheckpointPackage runs coverage for exactly one package and stores its
// digest-bound result for a later inventory assembly. This lets an operator
// schedule PostgreSQL-backed package runs through the repository's exclusive
// test lane without starting a full module test sweep.
func CheckpointPackage(ctx context.Context, repo string, module Module, importPath string) error {
	return CheckpointPackages(ctx, repo, module, []string{importPath})
}

// CheckpointPackages runs coverage for an explicit set of packages after a
// single module package listing. It is intended for bounded batches so large
// repositories do not pay the go-list cost once per package.
func CheckpointPackages(ctx context.Context, repo string, module Module, importPaths []string) error {
	return CheckpointPackagesWithWorkers(ctx, repo, module, importPaths, 1)
}

// CheckpointPackagesWithWorkers checkpoints an explicit package selection
// using a bounded number of independent go test processes. Package checkpoint
// files are content-addressed and atomically written, so workers never share
// an output file. The returned failures are ordered by import path.
func CheckpointPackagesWithWorkers(ctx context.Context, repo string, module Module, importPaths []string, workers int) error {
	if workers < 1 || workers > 4 {
		return fmt.Errorf("worker count must be between 1 and 4; got %d", workers)
	}
	absRepo, err := filepath.Abs(repo)
	if err != nil {
		return err
	}
	moduleRoot := filepath.Join(absRepo, module.Path)
	packages, err := listPackages(ctx, moduleRoot)
	if err != nil {
		return err
	}
	selectedByPath := make(map[string]listedPackage, len(packages))
	for _, pkg := range packages {
		selectedByPath[pkg.ImportPath] = pkg
	}
	unique := make(map[string]struct{}, len(importPaths))
	for _, importPath := range importPaths {
		if importPath == "" {
			return errors.New("package list contains an empty import path")
		}
		if _, ok := unique[importPath]; ok {
			return fmt.Errorf("package list contains duplicate import path %q", importPath)
		}
		unique[importPath] = struct{}{}
	}
	if len(unique) == 0 {
		return errors.New("package list is empty")
	}
	paths := make([]string, 0, len(unique))
	for path := range unique {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	failuresByPath := make([]string, len(paths))
	jobs := make(chan int)
	var wg sync.WaitGroup
	workerCount := min(workers, len(paths))
	for range workerCount {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for index := range jobs {
				failuresByPath[index] = checkpointSelectedPackage(ctx, absRepo, moduleRoot, module, selectedByPath, paths[index])
			}
		}()
	}
	for index := range paths {
		jobs <- index
	}
	close(jobs)
	wg.Wait()
	var failures []string
	for _, failure := range failuresByPath {
		if failure != "" {
			failures = append(failures, failure)
		}
	}
	if len(failures) > 0 {
		return errors.New(strings.Join(failures, "\n"))
	}
	return nil
}

func checkpointSelectedPackage(ctx context.Context, absRepo, moduleRoot string, module Module, selectedByPath map[string]listedPackage, importPath string) string {
	selected, ok := selectedByPath[importPath]
	if !ok {
		return fmt.Sprintf("package %q is not in module %s", importPath, module.Name)
	}
	if len(selected.TestGoFiles)+len(selected.XTestGoFiles) == 0 {
		return fmt.Sprintf("package %q has no Go tests to measure", importPath)
	}
	rows, err := inventoryRowsForPackages(ctx, absRepo, module, []listedPackage{selected})
	if err != nil {
		return fmt.Sprintf("inventory rows for %s: %v", importPath, err)
	}
	inputHash, err := packageInputHash(rows, selected)
	if err != nil {
		return fmt.Sprintf("hash coverage inputs for %s: %v", importPath, err)
	}
	checkpointDir := filepath.Join(absRepo, ".artifacts", "coverage", "testcoverage")
	checkpointPath := checkpointFile(checkpointDir, module, importPath, inputHash)
	if _, ok, err := readCheckpoint(checkpointPath, module, importPath, inputHash); err != nil {
		return fmt.Sprintf("read coverage checkpoint for %s: %v", importPath, err)
	} else if ok {
		return ""
	}
	profile, passed, testOutput, err := coverPackage(ctx, moduleRoot, importPath)
	if err != nil {
		logPath, logErr := writeFailureLog(filepath.Join(absRepo, ".artifacts", "coverage", "testcoverage"), module, importPath, inputHash, testOutput)
		if logErr != nil {
			return fmt.Sprintf("coverage for %s: %v; failed to write test output: %v", importPath, err, logErr)
		}
		return fmt.Sprintf("coverage for %s: %v; output: %s", importPath, err, filepath.ToSlash(logPath))
	}
	if !passed {
		logPath, logErr := writeFailureLog(filepath.Join(absRepo, ".artifacts", "coverage", "testcoverage"), module, importPath, inputHash, testOutput)
		if logErr != nil {
			return fmt.Sprintf("go test did not pass for %s; no coverage checkpoint was saved; failed to write output: %v", importPath, logErr)
		}
		return fmt.Sprintf("go test did not pass for %s; no coverage checkpoint was saved; output: %s", importPath, filepath.ToSlash(logPath))
	}
	coverage, err := parseProfile(profile)
	if err != nil {
		return fmt.Sprintf("coverage profile for %s: %v", importPath, err)
	}
	checkpoint := packageCheckpoint{Version: checkpointVersion, Module: module.Path, Package: importPath, InputHash: inputHash, Coverage: coverage, Passed: true}
	if err := writeCheckpoint(checkpointPath, checkpoint); err != nil {
		return fmt.Sprintf("write coverage checkpoint for %s: %v", importPath, err)
	}
	return ""
}

func listPackages(ctx context.Context, root string) ([]listedPackage, error) {
	cmd := exec.CommandContext(ctx, "go", "list", "-json", "./...")
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %w\n%s", err, strings.TrimSpace(stderr.String()))
	}
	dec := json.NewDecoder(bytes.NewReader(out))
	var result []listedPackage
	for {
		var p listedPackage
		if err := dec.Decode(&p); err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(root, p.Dir)
		if err != nil {
			return nil, fmt.Errorf("make package path relative to module root: %w", err)
		}
		if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || ignoredPackageDir(rel) {
			continue
		}
		result = append(result, p)
	}
	return result, nil
}

func ignoredPackageDir(dir string) bool {
	for _, part := range strings.Split(filepath.ToSlash(dir), "/") {
		if part == "node_modules" || part == ".artifacts" {
			return true
		}
	}
	return false
}

// packageGoFiles includes source files excluded by this host's build tags. They
// still belong to the checkout inventory even though this host cannot measure
// their statement coverage in the package's test run.
func packageGoFiles(p listedPackage) []string {
	files := append(append(append([]string{}, p.GoFiles...), p.CgoFiles...), p.IgnoredGoFiles...)
	sort.Strings(files)
	return files
}

func coverPackage(ctx context.Context, root, pkg string) (string, bool, []byte, error) {
	dir := filepath.Join(root, ".artifacts", "coverage")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", false, nil, err
	}
	f, err := os.CreateTemp(dir, "profile-*.out")
	if err != nil {
		return "", false, nil, err
	}
	path := f.Name()
	if err := f.Close(); err != nil {
		return "", false, nil, err
	}
	defer os.Remove(path)
	cmd := exec.CommandContext(ctx, "go", "test", "-count=1", "-coverprofile="+path, pkg)
	cmd.Dir = root
	output, testErr := cmd.CombinedOutput()
	if err := ctx.Err(); err != nil {
		return "", false, output, err
	}
	data, readErr := os.ReadFile(path)
	if len(data) == 0 {
		var exitErr *exec.ExitError
		if errors.As(testErr, &exitErr) {
			return "", false, output, fmt.Errorf("go test failed without a coverage profile: %w", testErr)
		}
		return "", false, output, fmt.Errorf("go test did not produce a coverage profile: %w", readErr)
	}
	return string(data), hasPassingTestResult(output, testErr), output, nil
}

func hasPassingTestResult(output []byte, testErr error) bool {
	if !hasPassingSummary(output) {
		return false
	}
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "FAIL\t") || strings.HasPrefix(line, "FAIL ") || strings.HasPrefix(line, "--- FAIL:") {
			return false
		}
	}
	if testErr == nil {
		return true
	}
	// On Windows, go test can print a successful package summary and then
	// return an exit error only because it cannot remove the generated test
	// executable. The repository instructions explicitly treat this cleanup
	// diagnostic as a passing result.
	return runtime.GOOS == "windows" && bytes.Contains(output, []byte("go: unlinkat ")) && bytes.Contains(output, []byte("Access is denied"))
}

func writeFailureLog(checkpointDir string, module Module, importPath, inputHash string, output []byte) (string, error) {
	digest := sha256.Sum256([]byte(importPath + "\n" + inputHash))
	path := filepath.Join(checkpointDir, module.Name, "failures", hex.EncodeToString(digest[:])+".log")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "failure-*.tmp")
	if err != nil {
		return "", err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(output); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return "", err
	}
	return path, nil
}

func hasPassingSummary(out []byte) bool {
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "ok\t") || strings.HasPrefix(line, "ok ") {
			return true
		}
	}
	return false
}

// profile map keys are absolute source filenames; values are statement and
// covered-statement counts aggregated across all profile blocks.
func parseProfile(profile string) (map[string][2]int, error) {
	out := map[string][2]int{}
	scanner := bufio.NewScanner(strings.NewReader(profile))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" || strings.HasPrefix(line, "mode:") {
			continue
		}
		colon := strings.LastIndex(line, ":")
		if colon < 0 {
			return nil, fmt.Errorf("malformed profile row %q", line)
		}
		fields := strings.Fields(line[colon+1:])
		if len(fields) != 3 {
			return nil, fmt.Errorf("malformed profile counts %q", line)
		}
		statements, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil, err
		}
		count, err := strconv.Atoi(fields[2])
		if err != nil {
			return nil, err
		}
		path := filepath.ToSlash(line[:colon])
		old := out[path]
		old[0] += statements
		if count > 0 {
			old[1] += statements
		}
		out[path] = old
	}
	return out, scanner.Err()
}

func isGenerated(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for i := 0; i < 10 && scanner.Scan(); i++ {
		if strings.Contains(scanner.Text(), "Code generated") && strings.Contains(scanner.Text(), "DO NOT EDIT") {
			return true
		}
	}
	return false
}

func render(module Module, repo string, rows []sourceRow) []byte {
	var b strings.Builder
	handWritten, untested := 0, map[string]bool{}
	for _, r := range rows {
		handWritten++
		if !r.tested {
			untested[r.importPath] = true
		}
	}
	generated := generatedCount(repo, module)
	fmt.Fprintf(&b, "=== %s (%s) ===\nfiles(hand-written)=%d generated=%d hand-written-in-untested-pkgs=%d\n", module.Name, module.Root, handWritten, generated, len(untested))
	pkgs := make([]string, 0, len(untested))
	for p := range untested {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)
	fmt.Fprintf(&b, "untested pkgs(%d):", len(pkgs))
	if len(pkgs) > 0 {
		fmt.Fprintf(&b, " %s", strings.Join(pkgs, ", "))
	}
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "# Test-coverage file inventory - %s (%s) module\n\nhand-written=%d generated=%d\n\n## Hand-written files\n\n| dir | file | package | statements | covered | coverage | pkg-has-tests | go-test | source-sha256 | test-sha256 |\n| --- | --- | --- | ---: | ---: | ---: | --- | --- | --- | --- |\n", module.Name, module.Root, handWritten, generated)
	for _, r := range rows {
		coverage := "n/a"
		if r.statements > 0 {
			coverage = fmt.Sprintf("%.1f%%", 100*float64(r.covered)/float64(r.statements))
		}
		fmt.Fprintf(&b, "| %s | %s | %s | %d | %d | %s | %t | %s | %s | %s |\n", r.dir, r.file, r.importPath, r.statements, r.covered, coverage, r.tested, r.testResult, r.sourceHash, r.testHash)
	}
	fmt.Fprintln(&b, "\n## Exclusions\n\n| file | kind | reason |\n| --- | --- | --- |")
	for _, x := range moduleExclusions(module) {
		fmt.Fprintf(&b, "| %s | %s | %s |\n", x.Path, x.Kind, x.Reason)
	}
	return []byte(b.String())
}

func moduleExclusions(module Module) []Exclusion {
	if module.Path == "." {
		return Exclusions
	}
	var out []Exclusion
	prefix := strings.TrimSuffix(filepath.ToSlash(module.Path), "/") + "/"
	for _, x := range Exclusions {
		if strings.HasPrefix(x.Path, prefix) {
			out = append(out, x)
		}
	}
	return out
}

func isExcluded(path string) bool {
	for _, x := range Exclusions {
		if x.Path == path {
			return true
		}
	}
	return false
}

func generatedCount(repo string, module Module) int {
	packages, err := listPackages(context.Background(), filepath.Join(repo, module.Path))
	if err != nil {
		return 0
	}
	count := 0
	for _, p := range packages {
		for _, name := range packageGoFiles(p) {
			if isGenerated(filepath.Join(p.Dir, name)) {
				count++
			}
		}
	}
	return count
}
