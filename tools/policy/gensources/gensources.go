// Package gensources enforces the ownership boundary between authored source,
// wire-generated output, and internally generated output (ARCH-GO-014 and
// ARCH-GO-015).
//
// The checker is intentionally read-only. It walks the repository roots whose
// ownership is part of the contract, classifies files from their stable path
// and generator header, and compares the model generator's committed output
// with a fresh in-memory render. A finding is data until the caller chooses to
// fail a test or CI check.
package gensources

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/gen/modelgen"
)

// Classification is the ownership class assigned to one discovered file.
type Classification string

const (
	// Authored is source maintained by a human or a source fixture owner.
	Authored Classification = "AUTHORED"
	// WireGenerated is generated API/wire output, such as protoc-gen-go files.
	WireGenerated Classification = "WIRE_GENERATED"
	// InternalGenerated is generated semantic, manifest, or migration output.
	InternalGenerated Classification = "INTERNAL_GENERATED"
)

// File is one deterministic classification record.
type File struct {
	Path           string
	Classification Classification
	Generator      string
	Owner          string
}

// Violation is one ownership or drift failure.
type Violation struct {
	Path   string
	Rule   string
	Detail string
}

func (v Violation) Error() string {
	if v.Path == "" {
		return v.Rule + ": " + v.Detail
	}
	return v.Path + ": " + v.Rule + ": " + v.Detail
}

// Report is the complete deterministic result of a repository scan.
type Report struct {
	Files      []File
	Counts     map[Classification]int
	Violations []Violation
}

// Clean reports whether the scan found no ownership or drift violation.
func (r Report) Clean() bool { return len(r.Violations) == 0 }

// Count returns the number of files with classification c.
func (r Report) Count(c Classification) int { return r.Counts[c] }

// Error returns all findings as one stable error, or nil for a clean report.
func (r Report) Error() error {
	if r.Clean() {
		return nil
	}
	parts := make([]string, len(r.Violations))
	for i, v := range r.Violations {
		parts[i] = v.Error()
	}
	return fmt.Errorf("gensources: %s", strings.Join(parts, "; "))
}

// Config controls the authored definition allowlist. The default is the
// repository contract: each current definitions owner root is explicitly
// named rather than allowing an arbitrary new definitions subtree.
type Config struct {
	// AuthoredAllowlist maps a definitions path prefix to its accountable
	// owner. Prefixes use forward slashes and are relative to repository root.
	AuthoredAllowlist map[string]string
}

// DefaultConfig returns a copy of the current authored definitions contract.
func DefaultConfig() Config {
	return Config{AuthoredAllowlist: map[string]string{
		"definitions/api/":          "experience-and-transport",
		"definitions/architecture/": "platform-foundation",
		"definitions/governance/":   "governance-and-trust",
		"definitions/legal/":        "governance-and-trust",
		"definitions/operations/":   "operations-and-assurance",
		"definitions/planning/":     "platform-foundation",
		"definitions/runtime/":      "workflow-runtime",
		"definitions/storage/":      "data-and-ledger",
		"definitions/supply-chain/": "platform-foundation",
		"definitions/telemetry/":    "operations-and-assurance",
		"definitions/toolchain/":    "platform-foundation",
		"definitions/ux/":           "experience-and-transport",
	}}
}

var generatedByPattern = regexp.MustCompile(`(?i)generated\s+by\s+([A-Za-z0-9._/-]+)`)
var generatedKeyPattern = regexp.MustCompile(`(?m)^generated_by:\s*([^\s#]+)`)
var generatedLegacyKeyPattern = regexp.MustCompile(`(?m)^generatedby:\s*([^\s#]+)`)

// Scan scans schema/, definitions/, gen/, tools/gen/**/testdata, and the
// SchemaFlux generated output under root. It never writes. The report remains useful for a partially migrated
// tree: all files are classified even when violations are present.
func Scan(root string) (Report, error) { return ScanWithConfig(root, DefaultConfig()) }

// ScanWithConfig is Scan with an explicit authored definitions allowlist.
func ScanWithConfig(root string, cfg Config) (Report, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return Report{}, fmt.Errorf("gensources: absolute root: %w", err)
	}
	report := Report{Counts: map[Classification]int{
		Authored:          0,
		WireGenerated:     0,
		InternalGenerated: 0,
	}}
	var paths []string
	for _, relRoot := range []string{"schema", "definitions", "gen"} {
		if err := collectFiles(filepath.Join(root, relRoot), func(path string) bool { return true }, &paths); err != nil {
			return Report{}, err
		}
	}
	toolsGen := filepath.Join(root, "tools", "gen")
	if err := collectFiles(toolsGen, func(path string) bool {
		rel, _ := filepath.Rel(root, path)
		return isToolTestdataPath(filepath.ToSlash(rel))
	}, &paths); err != nil {
		return Report{}, err
	}
	sort.Strings(paths)
	for _, path := range paths {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return Report{}, err
		}
		rel = filepath.ToSlash(rel)
		data, err := os.ReadFile(path)
		if err != nil {
			return Report{}, fmt.Errorf("gensources: read %s: %w", rel, err)
		}
		file, violations := classify(root, rel, data, cfg)
		report.Files = append(report.Files, file)
		report.Counts[file.Classification]++
		report.Violations = append(report.Violations, violations...)
	}
	report.Violations = append(report.Violations, checkModelDrift(root, report.Files)...)
	report.Violations = append(report.Violations, checkSchemaFluxGeneratedHeaders(root)...)
	sort.Slice(report.Violations, func(i, j int) bool {
		if report.Violations[i].Path != report.Violations[j].Path {
			return report.Violations[i].Path < report.Violations[j].Path
		}
		if report.Violations[i].Rule != report.Violations[j].Rule {
			return report.Violations[i].Rule < report.Violations[j].Rule
		}
		return report.Violations[i].Detail < report.Violations[j].Detail
	})
	return report, nil
}

func checkSchemaFluxGeneratedHeaders(root string) []Violation {
	generatedRoot := filepath.Join(root, "tools", "gen", "schemaflux", "generated")
	var violations []Violation
	_ = filepath.Walk(generatedRoot, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil || info == nil || info.IsDir() {
			return walkErr
		}
		data, err := os.ReadFile(path)
		if err != nil {
			rel, _ := filepath.Rel(root, path)
			violations = append(violations, Violation{Path: filepath.ToSlash(rel), Rule: "generated-file-unreadable", Detail: err.Error()})
			return nil
		}
		if generatedBy(data) == "" {
			rel, _ := filepath.Rel(root, path)
			violations = append(violations, Violation{Path: filepath.ToSlash(rel), Rule: "hand-edit-in-generated-root", Detail: "files under tools/gen/schemaflux/generated must carry a generator header"})
		}
		return nil
	})
	return violations
}

// CheckRepository is a convenience for policy tests that want a report even
// when the repository is not yet clean. An unreadable tree is represented as
// one root-level finding.
func CheckRepository(root string) Report {
	report, err := Scan(root)
	if err != nil {
		return Report{Counts: map[Classification]int{}, Violations: []Violation{{Rule: "scan", Detail: err.Error()}}}
	}
	return report
}

// Validate is the fail-closed convenience form of Scan.
func Validate(root string) error {
	report, err := Scan(root)
	if err != nil {
		return err
	}
	return report.Error()
}

// CheckTrackedArtifacts reports generated build output or demo persona media
// that is still tracked by git. Ignoring a path does not untrack it, so this
// check uses the index rather than the working tree or .gitignore rules.
func CheckTrackedArtifacts(root string) ([]Violation, error) {
	cmd := exec.Command("git", "-C", root, "ls-files", "-z")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gensources: git ls-files: %w", err)
	}
	var violations []Violation
	for _, raw := range bytes.Split(output, []byte{0}) {
		if len(raw) == 0 {
			continue
		}
		path := filepath.ToSlash(string(raw))
		if strings.HasPrefix(path, "internal/generated/schemaflux/") {
			violations = append(violations, Violation{Path: path, Rule: "generated-tool-output-in-runtime-root", Detail: "tool-only generated models belong under tools/, not internal/"})
		}
		if isBuildArtifact(path) {
			violations = append(violations, Violation{Path: path, Rule: "committed-build-artifact", Detail: "build output must be generated under .artifacts/"})
			continue
		}
		if strings.HasPrefix(path, "internal/humanwork/workspace/assets/") && isDemoPersonaMedia(path) {
			violations = append(violations, Violation{Path: path, Rule: "committed-demo-media", Detail: "demo persona media is supplied by the development seed, not embedded in the application"})
		}
	}
	sort.Slice(violations, func(i, j int) bool { return violations[i].Path < violations[j].Path })
	return violations, nil
}

func isBuildArtifact(path string) bool {
	if !strings.Contains(path, "/") {
		lower := strings.ToLower(path)
		if strings.HasSuffix(lower, ".test.exe") || strings.HasSuffix(lower, ".exe") || lower == "journeywasm" {
			return true
		}
	}
	return path == "internal/humanwork/workspace/assets/journey.wasm" ||
		path == "internal/humanwork/workspace/assets/journey.wasm.gz" ||
		path == "internal/humanwork/workspace/assets/uxqual.wasm" ||
		path == "internal/humanwork/workspace/assets/wasm_exec.js" ||
		path == "internal/humanwork/workspace/assets/wasm_exec.js.gz"
}

func isDemoPersonaMedia(path string) bool {
	name := strings.TrimPrefix(path, "internal/humanwork/workspace/assets/")
	return strings.HasPrefix(name, "person-") && (strings.HasSuffix(name, ".png") || strings.HasSuffix(name, ".jpg"))
}

func collectFiles(root string, include func(string) bool, paths *[]string) error {
	info, err := os.Stat(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("gensources: %s is not a directory", root)
	}
	return filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if info.IsDir() {
			return nil
		}
		if include(path) {
			*paths = append(*paths, path)
		}
		return nil
	})
}

func isToolTestdataPath(rel string) bool {
	parts := strings.Split(rel, "/")
	if len(parts) < 4 || parts[0] != "tools" || parts[1] != "gen" {
		return false
	}
	for _, part := range parts[3:] {
		if part == "testdata" {
			return true
		}
	}
	return false
}

func classify(root, rel string, data []byte, cfg Config) (File, []Violation) {
	generator := generatedBy(data)
	generated := generator != ""
	file := File{Path: rel, Generator: generator}
	var violations []Violation

	switch {
	case strings.HasPrefix(rel, "schema/"):
		if generated {
			file.Classification = generatedClassification(generator)
			violations = append(violations, Violation{Path: rel, Rule: "generated-header-in-authored-root", Detail: "schema/ is authored; generated output belongs under gen/"})
		} else {
			file.Classification = Authored
			file.Owner = "schema-authors"
		}
	case strings.HasPrefix(rel, "definitions/"):
		contractFile := strings.HasSuffix(strings.ToLower(rel), ".yaml") || strings.HasSuffix(strings.ToLower(rel), ".json")
		if generated {
			file.Classification = generatedClassification(generator)
			if contractFile {
				if generatedKeyPattern.Find(data) == nil {
					violations = append(violations, Violation{Path: rel, Rule: "generated-definition-missing-generated_by", Detail: "generated definitions must carry a generated_by: key"})
				}
				if !hasGeneratorDriftTest(root, generator) {
					violations = append(violations, Violation{Path: rel, Rule: "generated-definition-missing-drift-test", Detail: fmt.Sprintf("generator %q has no package drift test", generator)})
				}
			}
		} else {
			file.Classification = Authored
			file.Owner = ownerFor(rel, cfg.AuthoredAllowlist)
			if contractFile && file.Owner == "" {
				violations = append(violations, Violation{Path: rel, Rule: "definition-not-in-authored-allowlist", Detail: "authored YAML/JSON must name an owner in the definitions allowlist"})
			}
		}
		if strings.HasSuffix(strings.ToLower(rel), ".go") {
			violations = append(violations, Violation{Path: rel, Rule: "go-under-definitions", Detail: "definitions/ is machine-readable and may not contain Go"})
		}
	case strings.HasPrefix(rel, "gen/"):
		if generated {
			file.Classification = generatedClassification(generator)
		} else if rel == "gen/TOOLS.lock" {
			// buf's module lock is generator metadata, not source output. It is
			// authored and versioned, but a generated-code header has no meaning
			// for this checksum lockfile.
			file.Classification = Authored
			file.Owner = "buf-module-lock"
		} else {
			file.Classification = Authored
			violations = append(violations, Violation{Path: rel, Rule: "hand-edit-in-generated-root", Detail: "every file under gen/ must be generated and carry a generator header"})
		}
	case strings.HasPrefix(rel, "tools/gen/"):
		if generated {
			file.Classification = generatedClassification(generator)
		} else {
			file.Classification = Authored
			file.Owner = "generator-fixture-authors"
		}
	}
	return file, violations
}

func generatedBy(data []byte) string {
	match := generatedByPattern.FindSubmatch(data)
	if len(match) > 1 {
		return strings.Trim(string(match[1]), "\"'")
	}
	match = generatedKeyPattern.FindSubmatch(data)
	if len(match) > 1 {
		return strings.Trim(string(match[1]), "\"'")
	}
	match = generatedLegacyKeyPattern.FindSubmatch(data)
	if len(match) > 1 {
		return strings.Trim(string(match[1]), "\"'")
	}
	return ""
}

func generatedClassification(generator string) Classification {
	if strings.Contains(strings.ToLower(generator), "protoc") || strings.Contains(strings.ToLower(generator), "buf") {
		return WireGenerated
	}
	return InternalGenerated
}

func ownerFor(rel string, allowlist map[string]string) string {
	keys := make([]string, 0, len(allowlist))
	for prefix := range allowlist {
		keys = append(keys, prefix)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	for _, prefix := range keys {
		if strings.HasPrefix(rel, prefix) {
			return allowlist[prefix]
		}
	}
	return ""
}

func hasGeneratorDriftTest(root, generator string) bool {
	if !strings.HasPrefix(generator, "tools/") {
		return false
	}
	parts := strings.Split(generator, "/")
	if len(parts) < 3 || parts[0] != "tools" {
		return false
	}
	// Definitions may name a generator package with a suffix such as
	// "(DB-002)"; generatedBy has already removed that suffix.
	packageDir := filepath.Join(append([]string{root}, parts...)...)
	for _, candidate := range []string{
		filepath.Join(packageDir, "*_test.go"),
		filepath.Join(packageDir, "drift", "*_test.go"),
	} {
		if matches, _ := filepath.Glob(candidate); len(matches) > 0 {
			for _, match := range matches {
				data, err := os.ReadFile(match)
				if err != nil {
					continue
				}
				text := strings.ToLower(string(data))
				if strings.Contains(text, "golden") || strings.Contains(text, "drift") {
					return true
				}
			}
		}
	}
	return false
}

func checkModelDrift(root string, files []File) []Violation {
	const rel = "gen/go/hcmnext/model/model_generated.go"
	for _, file := range files {
		if file.Path != rel || file.Classification != InternalGenerated {
			continue
		}
		fresh, err := modelgen.GenerateAll()
		if err != nil {
			return []Violation{{Path: rel, Rule: "generator-recompute-failed", Detail: err.Error()}}
		}
		want, ok := fresh[modelgen.OutputFile]
		if !ok {
			return []Violation{{Path: rel, Rule: "generator-recompute-failed", Detail: "modelgen produced no model output"}}
		}
		got, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return []Violation{{Path: rel, Rule: "generated-output-unreadable", Detail: err.Error()}}
		}
		if !bytes.Equal(got, want) {
			return []Violation{{Path: rel, Rule: "generated-drift", Detail: "committed model output differs from tools/gen/modelgen recomputed in memory"}}
		}
	}
	return nil
}
