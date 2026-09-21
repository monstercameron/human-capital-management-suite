// Package cleancheckout verifies the Go repository from the tree that a
// clean checkout would contain. It deliberately keeps the checkout in a
// temporary directory and treats the working tree as read-only input.
package cleancheckout

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/quality/cicd"
)

// Artifact describes output that is intentionally absent from a clean
// checkout and has a documented command that produces it.
type Artifact struct {
	Path    string `json:"path"`
	Command string `json:"command"`
	Owner   string `json:"owner"`
}

// BuildTimeArtifacts is the complete allowlist for files generated into the
// embedded progressive-enhancement asset directory. The marker file in that
// directory is tracked; these outputs are not.
var BuildTimeArtifacts = []Artifact{
	{
		Path:    "internal/humanwork/workspace/assets/journey.wasm",
		Command: "go run ./tools/uxqual/cmd/journeywasm -out internal/humanwork/workspace/assets",
		Owner:   "experience-and-transport",
	},
	{
		Path:    "internal/humanwork/workspace/assets/uxqual.wasm",
		Command: "GOOS=js GOARCH=wasm go build -o internal/humanwork/workspace/assets/uxqual.wasm ./tools/uxqual/cmd/uxqualwasm",
		Owner:   "experience-and-transport",
	},
	{
		Path:    "internal/humanwork/workspace/assets/wasm_exec.js",
		Command: "go run ./tools/uxqual/cmd/journeywasm -out internal/humanwork/workspace/assets",
		Owner:   "experience-and-transport",
	},
}

// Command is one command run from the disposable checkout.
type Command struct {
	Name string   `json:"name"`
	Args []string `json:"args"`
}

// PolicyCommands is the policy trio used by the root-module workflow. The
// race policy is kept as a separate step because the workflow also runs it
// independently of the three source/contract gates.
var PolicyCommands = []Command{
	{Name: "driftgate", Args: []string{"run", "./tools/policy/driftgate/cmd/driftgate", "-root", "."}},
	{Name: "apigate", Args: []string{"run", "./tools/policy/apigate/cmd/apigate", "-root", "."}},
	{Name: "substratecoverage", Args: []string{"run", "./tools/policy/substratecoverage/cmd/substratecoverage", "-root", "."}},
}

// RacePolicyCommand is the concurrency coverage gate in the root-module
// workflow.
var RacePolicyCommand = Command{Name: "racepolicy", Args: []string{"run", "./tools/policy/racepolicy/cmd/racepolicy", "-root", "."}}

// AllowlistEntry records a temporary owner allowance for a working-tree file.
// The cleancheckout package itself is allowlisted because its files are
// necessarily untracked while this lane is being developed; after commit the
// entry becomes inert.
type AllowlistEntry struct {
	PathPrefix string `json:"path_prefix"`
	Owner      string `json:"owner"`
	Reason     string `json:"reason"`
}

// OwnerAllowlist contains only lane-local working-tree allowances. Generated
// assets are classified from BuildTimeArtifacts so one table remains the
// source of their owner and regeneration command.
var OwnerAllowlist = []AllowlistEntry{
	{
		PathPrefix: "tools/policy/cleancheckout/",
		Owner:      "platform-foundation",
		Reason:     "new lane files are untracked until this change is committed",
	},
}

// Finding is a clean-checkout diagnostic. Allowlisted findings are reported
// for visibility but do not make Report.OK false.
type Finding struct {
	Code        string `json:"code"`
	Path        string `json:"path,omitempty"`
	Detail      string `json:"detail"`
	Owner       string `json:"owner,omitempty"`
	Command     string `json:"command,omitempty"`
	Allowlisted bool   `json:"allowlisted"`
}

// CheckResult records one command run in the disposable checkout.
type CheckResult struct {
	Name     string   `json:"name"`
	Args     []string `json:"args"`
	Passed   bool     `json:"passed"`
	ExitCode int      `json:"exit_code"`
	Output   string   `json:"output,omitempty"`
}

// ExportReport describes the tracked paths copied into a temporary checkout.
type ExportReport struct {
	Paths []string `json:"paths"`
}

// Report is the complete clean-checkout evaluation. TempDir is deliberately
// not retained in the report because it is both ephemeral and nondeterministic.
type Report struct {
	TrackedFiles   int           `json:"tracked_files"`
	BuildArtifacts []Artifact    `json:"build_time_artifacts"`
	LiveFindings   []Finding     `json:"live_findings,omitempty"`
	Checks         []CheckResult `json:"checks"`
	NewGaps        []Finding     `json:"new_gaps,omitempty"`
}

// RequiredCommandPackages returns the seven approved composition roots required by
// CICD-001, copied from the existing quality contract.
func RequiredCommandPackages() []string {
	return append([]string(nil), cicd.RequiredCommands...)
}

// ExpectedBuildArtifacts returns a defensive copy of the artifact table.
func ExpectedBuildArtifacts() []Artifact {
	return append([]Artifact(nil), BuildTimeArtifacts...)
}

// OK reports whether the clean checkout and every required verification step
// passed, with no unallowlisted working-tree gap.
func (r Report) OK() bool { return len(r.NewGaps) == 0 }

// Digest returns a stable digest of the report.
func (r Report) Digest() string {
	data, _ := json.Marshal(r)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// ExportTrackedTree asks git for tracked paths and copies precisely those
// paths into destination. The git invocation is read-only; ignored and
// untracked working-tree files are never consulted for the copy.
func ExportTrackedTree(root, destination string) (ExportReport, error) {
	paths, err := trackedPaths(root)
	if err != nil {
		return ExportReport{}, err
	}
	return ExportPaths(root, destination, paths)
}

// ExportPaths copies the supplied repository-relative paths. It is exposed so
// tests can exercise the clean-copy contract with a deterministic fixture
// without creating a nested git repository.
func ExportPaths(root, destination string, paths []string) (ExportReport, error) {
	if err := os.MkdirAll(destination, 0o755); err != nil {
		return ExportReport{}, fmt.Errorf("create checkout: %w", err)
	}
	normalized := make([]string, 0, len(paths))
	seen := make(map[string]bool, len(paths))
	for _, raw := range paths {
		rel, err := normalizeRelativePath(raw)
		if err != nil {
			return ExportReport{}, fmt.Errorf("tracked path %q: %w", raw, err)
		}
		if seen[rel] {
			continue
		}
		seen[rel] = true
		source := filepath.Join(root, filepath.FromSlash(rel))
		info, err := os.Lstat(source)
		if err != nil {
			return ExportReport{}, fmt.Errorf("read tracked path %s: %w", rel, err)
		}
		if info.IsDir() {
			return ExportReport{}, fmt.Errorf("tracked path %s is a directory", rel)
		}
		data, err := os.ReadFile(source)
		if err != nil {
			return ExportReport{}, fmt.Errorf("read tracked path %s: %w", rel, err)
		}
		destinationPath := filepath.Join(destination, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(destinationPath), 0o755); err != nil {
			return ExportReport{}, fmt.Errorf("create parent for %s: %w", rel, err)
		}
		mode := info.Mode().Perm()
		if mode == 0 {
			mode = 0o644
		}
		if err := os.WriteFile(destinationPath, data, mode); err != nil {
			return ExportReport{}, fmt.Errorf("write tracked path %s: %w", rel, err)
		}
		normalized = append(normalized, rel)
	}
	sort.Strings(normalized)
	return ExportReport{Paths: normalized}, nil
}

// FindWorkingTreeGaps identifies build or policy inputs that exist in the
// working tree but are absent from the supplied tracked tree. Expected build
// artifacts are returned as allowlisted findings, never as new gaps.
func FindWorkingTreeGaps(root string, tracked []string) ([]Finding, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve working tree: %w", err)
	}
	trackedSet := make(map[string]bool, len(tracked))
	for _, raw := range tracked {
		rel, normalizeErr := normalizeRelativePath(raw)
		if normalizeErr != nil {
			return nil, normalizeErr
		}
		trackedSet[rel] = true
	}
	embedded, err := embeddedInputs(root, trackedSet)
	if err != nil {
		return nil, err
	}

	candidates := make(map[string]bool)
	for rel := range embedded {
		candidates[rel] = true
	}
	walkErr := filepath.WalkDir(root, func(fullPath string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if fullPath != root && skipWorkingTreeDir(entry.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, relErr := filepath.Rel(root, fullPath)
		if relErr != nil {
			return relErr
		}
		rel = filepath.ToSlash(rel)
		if buildInputPath(rel) {
			candidates[rel] = true
		}
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("scan working tree: %w", walkErr)
	}

	findings := make([]Finding, 0)
	for rel := range candidates {
		if trackedSet[rel] {
			continue
		}
		fullPath := filepath.Join(root, filepath.FromSlash(rel))
		if _, statErr := os.Stat(fullPath); statErr != nil {
			if os.IsNotExist(statErr) {
				continue
			}
			return nil, fmt.Errorf("stat working-tree input %s: %w", rel, statErr)
		}
		findings = append(findings, classifyFinding(rel))
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Path != findings[j].Path {
			return findings[i].Path < findings[j].Path
		}
		return findings[i].Code < findings[j].Code
	})
	return findings, nil
}

// Evaluate creates a disposable tracked-tree checkout, runs all authoritative
// root Go checks there, and reports live-tree gaps without modifying root.
func Evaluate(root string) (Report, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Report{}, fmt.Errorf("resolve root: %w", err)
	}
	tempDir, err := os.MkdirTemp("", "human-capital-management-suite-cleancheckout-")
	if err != nil {
		return Report{}, fmt.Errorf("create temporary checkout: %w", err)
	}
	defer os.RemoveAll(tempDir)

	exported, err := ExportTrackedTree(root, tempDir)
	if err != nil {
		return Report{}, err
	}
	workingFindings, err := FindWorkingTreeGaps(root, exported.Paths)
	if err != nil {
		return Report{}, err
	}
	report := Report{
		TrackedFiles:   len(exported.Paths),
		BuildArtifacts: ExpectedBuildArtifacts(),
		LiveFindings:   workingFindings,
	}
	for _, finding := range workingFindings {
		if !finding.Allowlisted {
			report.NewGaps = append(report.NewGaps, finding)
		}
	}

	for _, command := range verificationCommands() {
		result := runCommand(tempDir, command)
		report.Checks = append(report.Checks, result)
		if !result.Passed {
			report.NewGaps = append(report.NewGaps, Finding{
				Code:   "clean-checkout-command-failed",
				Path:   command.Name,
				Detail: "authoritative clean-checkout verification command failed",
			})
		}
	}

	for _, command := range RequiredCommandPackages() {
		if !commandPackageExists(tempDir, command) {
			finding := Finding{
				Code:   "missing-required-command",
				Path:   command,
				Detail: "CICD-001 requires this composition root in the clean checkout",
			}
			report.NewGaps = append(report.NewGaps, finding)
		}
	}
	sort.Slice(report.NewGaps, func(i, j int) bool {
		if report.NewGaps[i].Code != report.NewGaps[j].Code {
			return report.NewGaps[i].Code < report.NewGaps[j].Code
		}
		return report.NewGaps[i].Path < report.NewGaps[j].Path
	})
	return report, nil
}

// Check evaluates root and returns an error when an unallowlisted gap exists.
func Check(root string) (Report, error) {
	report, err := Evaluate(root)
	if err != nil {
		return Report{}, err
	}
	if len(report.NewGaps) == 0 {
		return report, nil
	}
	return report, fmt.Errorf("cleancheckout: %d new gap(s)", len(report.NewGaps))
}

// JSON returns the deterministic machine-readable report.
func (r Report) JSON() ([]byte, error) { return json.MarshalIndent(r, "", "  ") }

// Text renders a concise report for local and CI logs.
func (r Report) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "cleancheckout: tracked files exported: %d\n", r.TrackedFiles)
	b.WriteString("cleancheckout: build-time artifacts expected outside a clean checkout:\n")
	for _, artifact := range r.BuildArtifacts {
		fmt.Fprintf(&b, "  - %s (owner=%s; produce with %s)\n", artifact.Path, artifact.Owner, artifact.Command)
	}
	fmt.Fprintf(&b, "cleancheckout: checks: %d\n", len(r.Checks))
	for _, check := range r.Checks {
		status := "PASS"
		if !check.Passed {
			status = "FAIL"
		}
		fmt.Fprintf(&b, "  - %s: %s\n", check.Name, status)
		if !check.Passed && check.Output != "" {
			fmt.Fprintf(&b, "    output: %s\n", strings.TrimSpace(check.Output))
		}
	}
	fmt.Fprintf(&b, "cleancheckout: live-tree findings: %d\n", len(r.LiveFindings))
	for _, finding := range r.LiveFindings {
		status := "NEW"
		if finding.Allowlisted {
			status = "ALLOWLISTED"
		}
		fmt.Fprintf(&b, "  - %s %s: %s", status, finding.Path, finding.Detail)
		if finding.Command != "" {
			fmt.Fprintf(&b, "; produce with %s", finding.Command)
		}
		b.WriteByte('\n')
	}
	if len(r.NewGaps) == 0 {
		b.WriteString("cleancheckout: PASS (no new gaps)\n")
	} else {
		fmt.Fprintf(&b, "cleancheckout: FAIL (%d new gap(s))\n", len(r.NewGaps))
		for _, finding := range r.NewGaps {
			fmt.Fprintf(&b, "  - %s %s: %s\n", finding.Code, finding.Path, finding.Detail)
		}
	}
	fmt.Fprintf(&b, "cleancheckout: report digest %s\n", r.Digest())
	return b.String()
}

func verificationCommands() []Command {
	commands := []Command{
		{Name: "go build ./...", Args: []string{"build", "./..."}},
		{Name: "go vet ./...", Args: []string{"vet", "./..."}},
	}
	commands = append(commands, PolicyCommands...)
	commands = append(commands, RacePolicyCommand)
	return commands
}

func runCommand(root string, command Command) CheckResult {
	result := CheckResult{Name: command.Name, Args: append([]string(nil), command.Args...), ExitCode: -1}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "go", command.Args...)
	cmd.Dir = root
	cmd.Env = cleanEnvironment(os.Environ())
	output, err := cmd.CombinedOutput()
	result.Output = limitOutput(output)
	if err == nil {
		result.Passed = true
		result.ExitCode = 0
		return result
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exitErr.ExitCode()
	}
	return result
}

func cleanEnvironment(environment []string) []string {
	cleaned := make([]string, 0, len(environment)+1)
	for _, value := range environment {
		if strings.HasPrefix(strings.ToUpper(value), "GOWORK=") {
			continue
		}
		cleaned = append(cleaned, value)
	}
	return append(cleaned, "GOWORK=off")
}

func commandPackageExists(root, rel string) bool {
	info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil || !info.IsDir() {
		return false
	}
	entries, err := os.ReadDir(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") {
			return true
		}
	}
	return false
}

func trackedPaths(root string) ([]string, error) {
	cmd := exec.Command("git", "-C", root, "ls-files", "-z")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	output, err := cmd.Output()
	if err != nil {
		if stderr.Len() > 0 {
			return nil, fmt.Errorf("git ls-files: %w: %s", err, strings.TrimSpace(stderr.String()))
		}
		return nil, fmt.Errorf("git ls-files: %w", err)
	}
	paths := make([]string, 0)
	for _, raw := range bytes.Split(output, []byte{0}) {
		if len(raw) == 0 {
			continue
		}
		rel, err := normalizeRelativePath(string(raw))
		if err != nil {
			return nil, err
		}
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	return paths, nil
}

func embeddedInputs(root string, tracked map[string]bool) (map[string]bool, error) {
	inputs := make(map[string]bool)
	paths := make([]string, 0, len(tracked))
	for rel := range tracked {
		if strings.HasSuffix(rel, ".go") {
			paths = append(paths, rel)
		}
	}
	sort.Strings(paths)
	fset := token.NewFileSet()
	for _, rel := range paths {
		fullPath := filepath.Join(root, filepath.FromSlash(rel))
		file, err := parser.ParseFile(fset, fullPath, nil, parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", rel, err)
		}
		for _, group := range file.Comments {
			for _, comment := range group.List {
				text := strings.TrimSpace(strings.TrimPrefix(comment.Text, "//"))
				if !strings.HasPrefix(text, "go:embed ") {
					continue
				}
				patterns := strings.Fields(strings.TrimSpace(strings.TrimPrefix(text, "go:embed ")))
				for _, pattern := range patterns {
					if err := collectEmbedPattern(filepath.Dir(fullPath), pattern, root, inputs); err != nil {
						return nil, fmt.Errorf("collect embed %s: %w", rel, err)
					}
				}
			}
		}
	}
	return inputs, nil
}

func collectEmbedPattern(sourceDir, pattern, root string, inputs map[string]bool) error {
	all := strings.HasPrefix(pattern, "all:")
	if all {
		pattern = strings.TrimPrefix(pattern, "all:")
	}
	if strings.HasPrefix(pattern, "/") || strings.Contains(pattern, "..") {
		return fmt.Errorf("unsafe embed pattern %q", pattern)
	}
	target := filepath.Join(sourceDir, filepath.FromSlash(pattern))
	if all {
		info, err := os.Stat(target)
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("all: pattern %q is not a directory", pattern)
		}
		return filepath.WalkDir(target, func(fullPath string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				return nil
			}
			return addRelativeInput(root, fullPath, inputs)
		})
	}
	matches, err := filepath.Glob(target)
	if err != nil {
		return err
	}
	for _, match := range matches {
		info, statErr := os.Stat(match)
		if statErr != nil {
			return statErr
		}
		if !info.IsDir() {
			if err := addRelativeInput(root, match, inputs); err != nil {
				return err
			}
		}
	}
	return nil
}

func addRelativeInput(root, fullPath string, inputs map[string]bool) error {
	rel, err := filepath.Rel(root, fullPath)
	if err != nil {
		return err
	}
	rel = filepath.ToSlash(rel)
	if rel == "." || strings.HasPrefix(rel, "../") {
		return fmt.Errorf("embed input %s escapes root", fullPath)
	}
	inputs[rel] = true
	return nil
}

func classifyFinding(rel string) Finding {
	for _, artifact := range BuildTimeArtifacts {
		if artifact.Path == rel {
			return Finding{
				Code:        "expected-build-artifact",
				Path:        rel,
				Detail:      "working tree contains output intentionally absent from a clean checkout",
				Owner:       artifact.Owner,
				Command:     artifact.Command,
				Allowlisted: true,
			}
		}
	}
	for _, entry := range OwnerAllowlist {
		if strings.HasPrefix(rel, entry.PathPrefix) {
			return Finding{
				Code:        "owner-allowlisted-working-file",
				Path:        rel,
				Detail:      entry.Reason,
				Owner:       entry.Owner,
				Allowlisted: true,
			}
		}
	}
	return Finding{
		Code:   "missing-tracked-file",
		Path:   rel,
		Detail: "working-tree build or policy input is absent from git's tracked tree",
	}
}

func buildInputPath(rel string) bool {
	if strings.HasSuffix(rel, ".go") || strings.HasSuffix(rel, ".proto") {
		return true
	}
	for _, prefix := range []string{"definitions/", "gen/", "schema/"} {
		if strings.HasPrefix(rel, prefix) {
			ext := strings.ToLower(filepath.Ext(rel))
			return ext == ".yaml" || ext == ".yml" || ext == ".json" || ext == ".proto" || ext == ".sql"
		}
	}
	return false
}

func skipWorkingTreeDir(name string) bool {
	if name == ".git" || name == "node_modules" {
		return true
	}
	if strings.HasPrefix(name, ".gocache") || strings.HasPrefix(name, ".go-build") {
		return true
	}
	switch name {
	case "bin", "build", "dist", "out", "coverage", "tmp", "temp", "test-results", "playwright-report":
		return true
	default:
		return false
	}
}

func normalizeRelativePath(raw string) (string, error) {
	cleanedSeparators := strings.ReplaceAll(raw, "\\", "/")
	if cleanedSeparators == "" || strings.HasPrefix(cleanedSeparators, "/") {
		return "", fmt.Errorf("path is not repository-relative")
	}
	cleaned := path.Clean(cleanedSeparators)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") {
		return "", fmt.Errorf("path escapes repository root")
	}
	return cleaned, nil
}

func limitOutput(output []byte) string {
	const max = 32 * 1024
	if len(output) <= max {
		return string(output)
	}
	return string(output[:max]) + "\n...[output truncated]"
}
