// Package testlayout implements ARCH-GO-016's source-tree policy. It keeps
// ordinary tests close to their package and makes shared test assets auditable
// without putting test-only dependencies into production code.
package testlayout

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Violation is a stable, path-relative ARCH-GO-016 finding.
type Violation struct {
	Path    string
	Kind    string
	Message string
}

func (v Violation) String() string {
	return fmt.Sprintf("%s: %s (%s)", v.Path, v.Message, v.Kind)
}

// FixtureMetadata is the sidecar format for an asset shared under test/.
// The sidecar lives beside the asset and is named <asset>.fixture.json.
type FixtureMetadata struct {
	Owner          string            `json:"owner"`
	Packages       []string          `json:"packages"`
	Clock          string            `json:"clock"`
	SourceVersions map[string]string `json:"source_versions"`
	Oracle         string            `json:"oracle"`
	Digest         string            `json:"digest"`
}

type directory struct {
	production bool
	testFiles  []string
	docs       []string
}

var sharedAssetKinds = map[string]bool{
	"fixtures":    true,
	"golden":      true,
	"conformance": true,
	"fault":       true,
	"fuzz":        true,
	"integration": true,
}

// Check scans root without executing Go programs or test harnesses. All
// diagnostics use paths relative to root, so the same checkout has the same
// output on every operating system.
func Check(root string) ([]Violation, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("testlayout: resolve root: %w", err)
	}
	module, err := readModule(absRoot)
	if err != nil {
		return nil, err
	}

	directories := map[string]*directory{}
	var findings []Violation
	err = filepath.WalkDir(absRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			if path != absRoot && (entry.Name() == ".git" || entry.Name() == "node_modules" || entry.Name() == "testdata" || entry.Name() == ".artifacts") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") {
			return nil
		}

		rel, err := relative(absRoot, path)
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ParseComments|parser.ImportsOnly)
		if err != nil {
			return fmt.Errorf("testlayout: parse %s: %w", rel, err)
		}
		dir := directories[filepath.ToSlash(filepath.Dir(rel))]
		if dir == nil {
			dir = &directory{}
			directories[filepath.ToSlash(filepath.Dir(rel))] = dir
		}
		if strings.HasSuffix(entry.Name(), "_test.go") {
			dir.testFiles = append(dir.testFiles, rel)
			if file.Doc != nil {
				dir.docs = append(dir.docs, file.Doc.Text())
			}
			return nil
		}

		dir.production = true
		if inTopLevelTest(rel) {
			findings = append(findings, Violation{
				Path: rel, Kind: "production_code_under_test_root",
				Message: "production Go source lives under top-level test/; that tree is reserved for shared test assets and harnesses",
			})
		}
		for _, imported := range file.Imports {
			path := strings.Trim(imported.Path.Value, "\"")
			if path == module+"/test" || strings.HasPrefix(path, module+"/test/") {
				findings = append(findings, Violation{
					Path: rel, Kind: "production_test_dependency",
					Message: fmt.Sprintf("production Go source imports %q; production code must not depend on top-level test packages", path),
				})
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("testlayout: scan source tree: %w", err)
	}

	for _, dir := range directories {
		if len(dir.testFiles) == 0 || dir.production || !inTopLevelTest(dir.testFiles[0]) || documentedCrossSystem(dir.docs) {
			continue
		}
		for _, testFile := range dir.testFiles {
			findings = append(findings, Violation{
				Path: testFile, Kind: "detached_test",
				Message: "ordinary test file is detached from its package; top-level test/ is reserved for documented cross-system harnesses",
			})
		}
	}

	assetFindings, err := checkSharedAssets(absRoot)
	if err != nil {
		return nil, err
	}
	findings = append(findings, assetFindings...)
	sort.Slice(findings, func(i, j int) bool { return findings[i].String() < findings[j].String() })
	return findings, nil
}

func readModule(root string) (string, error) {
	data, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("testlayout: read go.mod: %w", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "module" {
			return fields[1], nil
		}
	}
	return "", fmt.Errorf("testlayout: go.mod has no module directive")
}

func checkSharedAssets(root string) ([]Violation, error) {
	testRoot := filepath.Join(root, "test")
	if _, err := os.Stat(testRoot); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("testlayout: stat test root: %w", err)
	}

	var findings []Violation
	err := filepath.WalkDir(testRoot, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), ".fixture.json") {
			return nil
		}
		rel, err := relative(root, path)
		if err != nil {
			return err
		}
		if !sharedAssetPath(rel) {
			return nil
		}
		metadataPath := path + ".fixture.json"
		metadataRel, err := relative(root, metadataPath)
		if err != nil {
			return err
		}
		metadata, err := readFixtureMetadata(metadataPath)
		if err != nil || !validMetadata(metadata, path) {
			findings = append(findings, Violation{
				Path: metadataRel, Kind: "invalid_fixture_metadata",
				Message: fmt.Sprintf("fixture metadata must name owner, applicable packages, clock, source_versions, oracle, and a SHA-256 digest matching %s", rel),
			})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("testlayout: scan shared assets: %w", err)
	}
	return findings, nil
}

func readFixtureMetadata(path string) (FixtureMetadata, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return FixtureMetadata{}, err
	}
	var metadata FixtureMetadata
	if err := json.Unmarshal(data, &metadata); err != nil {
		return FixtureMetadata{}, err
	}
	return metadata, nil
}

func validMetadata(metadata FixtureMetadata, assetPath string) bool {
	if strings.TrimSpace(metadata.Owner) == "" || len(metadata.Packages) == 0 || strings.TrimSpace(metadata.Oracle) == "" || len(metadata.SourceVersions) == 0 {
		return false
	}
	if _, err := time.Parse(time.RFC3339, metadata.Clock); err != nil {
		return false
	}
	for _, pkg := range metadata.Packages {
		if strings.TrimSpace(pkg) == "" {
			return false
		}
	}
	for key, version := range metadata.SourceVersions {
		if strings.TrimSpace(key) == "" || strings.TrimSpace(version) == "" {
			return false
		}
	}
	const prefix = "sha256:"
	if !strings.HasPrefix(metadata.Digest, prefix) {
		return false
	}
	want, err := hex.DecodeString(strings.TrimPrefix(metadata.Digest, prefix))
	if err != nil || len(want) != sha256.Size {
		return false
	}
	data, err := os.ReadFile(assetPath)
	if err != nil {
		return false
	}
	got := sha256.Sum256(data)
	return string(want) == string(got[:])
}

func relative(root, path string) (string, error) {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func inTopLevelTest(rel string) bool {
	return rel == "test" || strings.HasPrefix(rel, "test/")
}

func sharedAssetPath(rel string) bool {
	parts := strings.Split(rel, "/")
	return len(parts) >= 3 && parts[0] == "test" && sharedAssetKinds[parts[1]]
}

func documentedCrossSystem(docs []string) bool {
	for _, doc := range docs {
		lower := strings.ToLower(doc)
		for _, marker := range []string{"cross-system", "cross system", "cross-package", "cross package", "conformance suite", "integration suite", "composed cell"} {
			if strings.Contains(lower, marker) {
				return true
			}
		}
	}
	return false
}
