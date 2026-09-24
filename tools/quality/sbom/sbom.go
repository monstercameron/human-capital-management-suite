// Package sbom generates and validates the release SBOM for a Go binary.
//
// The release graph is read from the binary's embedded `go version -m`
// metadata. This is intentional: go.mod describes the build list, while the
// embedded metadata describes the modules that actually shipped in the
// artifact. Module licenses and source digests are read from the local Go
// module cache; no network access is used.
package sbom

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
)

const (
	// Schema is the stable schema identifier used by the release SBOM.
	Schema = "hcmnext.sbom.v1"
	// GeneratorVersion identifies both the format implementation and its
	// behavior. It is part of the signed release evidence.
	GeneratorVersion = "hcmnext-sbom/1.0.0"

	sha256Prefix = "sha256:"
)

// Document is a deterministic, digest-bound SBOM document.
type Document struct {
	Schema     string      `json:"schema"`
	Generator  Generator   `json:"generator"`
	Graph      GraphSource `json:"graph"`
	Subject    Subject     `json:"subject"`
	Components []Component `json:"components"`
}

// Generator identifies the SBOM implementation and Go toolchain that ran it.
type Generator struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Go      string `json:"go"`
}

// GraphSource records the authoritative graph input.
type GraphSource struct {
	Tool    string `json:"tool"`
	Version string `json:"version"`
}

// Subject is the signed-build subject to which all components are bound.
type Subject struct {
	Name   string `json:"name"`
	Digest string `json:"digest"`
}

// Component describes one module shipped in the binary. Hash is the Go
// module sum embedded by the compiler (h1:...), while SourceDigest is a
// deterministic SHA-256 digest of the cached source tree.
type Component struct {
	Type         string       `json:"type"`
	Name         string       `json:"name"`
	Version      string       `json:"version"`
	Hash         string       `json:"hash"`
	SourceDigest string       `json:"source_digest"`
	License      string       `json:"license"`
	LicenseFile  string       `json:"license_file,omitempty"`
	Main         bool         `json:"main,omitempty"`
	Replacement  *Replacement `json:"replacement,omitempty"`
}

// Replacement preserves module replacement-path metadata from go's build
// information when a replacement is present.
type Replacement struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// Generate creates an SBOM for binaryPath. root is the repository root and is
// used to identify the main module. Generation is offline and never invokes a
// command that can download modules.
func Generate(root, binaryPath string) (Document, error) {
	if strings.TrimSpace(binaryPath) == "" {
		return Document{}, errors.New("sbom: binary path is required")
	}
	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return Document{}, fmt.Errorf("sbom: resolving root: %w", err)
		}
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return Document{}, fmt.Errorf("sbom: resolving root: %w", err)
	}
	binaryPath, err = filepath.Abs(binaryPath)
	if err != nil {
		return Document{}, fmt.Errorf("sbom: resolving binary: %w", err)
	}
	info, err := os.Stat(binaryPath)
	if err != nil || info.IsDir() {
		if err == nil {
			err = errors.New("path is a directory")
		}
		return Document{}, fmt.Errorf("sbom: binary: %w", err)
	}

	metadata, err := readMetadata(binaryPath)
	if err != nil {
		return Document{}, err
	}
	mainPath := metadata.mainPath
	if mainPath == "" {
		mainPath, err = rootModule(root)
		if err != nil {
			return Document{}, err
		}
	}
	artifactDigest, err := fileDigest(binaryPath)
	if err != nil {
		return Document{}, err
	}

	cache, err := goEnv("GOMODCACHE")
	if err != nil {
		return Document{}, err
	}
	components := make([]Component, 0, len(metadata.modules)+1)
	mainComponent := Component{
		Type: "go-module", Name: mainPath, Version: metadata.mainVersion,
		Hash: sha256Prefix + artifactDigest, License: "NOASSERTION", Main: true,
	}
	if mainComponent.Version == "" {
		mainComponent.Version = "(devel)"
	}
	// The main module is represented by the signed artifact itself. Hashing
	// the mutable checkout here would make a release document change when an
	// unrelated working-tree file changes between generation and validation.
	// The binary's subject digest is the immutable source/build result that
	// shipped and is independently bound below.
	mainComponent.SourceDigest = sha256Prefix + artifactDigest
	components = append(components, mainComponent)
	for _, m := range metadata.modules {
		if m.path == mainPath {
			continue
		}
		c := Component{Type: "go-module", Name: m.path, Version: m.version, Hash: m.sum}
		if c.Version == "" {
			return Document{}, fmt.Errorf("sbom: embedded module %s lacks version", m.path)
		}
		lookupPath, lookupVersion := m.path, m.version
		if m.replacement != nil {
			lookupPath, lookupVersion = m.replacement.path, m.replacement.version
		}
		moduleDir, err := moduleDirectory(root, cache, lookupPath, lookupVersion)
		if err != nil {
			return Document{}, err
		}
		c.SourceDigest, err = treeDigest(moduleDir)
		if err != nil {
			return Document{}, err
		}
		// Local replacements have no h1 sum in build metadata. Bind them to
		// their deterministic source digest instead of emitting an unbound
		// placeholder hash. SourceDigest must be computed first.
		if c.Hash == "" {
			c.Hash = c.SourceDigest
		}
		c.License, c.LicenseFile, err = licenseFor(moduleDir)
		if err != nil {
			return Document{}, err
		}
		if m.replacement != nil {
			c.Replacement = &Replacement{Name: m.replacement.path, Version: m.replacement.version}
		}
		components = append(components, c)
	}
	sort.Slice(components, func(i, j int) bool {
		if components[i].Name != components[j].Name {
			return components[i].Name < components[j].Name
		}
		return components[i].Version < components[j].Version
	})
	return Document{
		Schema:     Schema,
		Generator:  Generator{Name: "hcmnext-sbom", Version: GeneratorVersion, Go: runtime.Version()},
		Graph:      GraphSource{Tool: "go version -m", Version: metadata.goVersion},
		Subject:    Subject{Name: filepath.Base(binaryPath), Digest: sha256Prefix + artifactDigest},
		Components: components,
	}, nil
}

// Marshal returns canonical compact JSON. Struct field order and sorted
// components are deliberately stable, making this suitable for signing.
func (d Document) Marshal() ([]byte, error) { return json.Marshal(d) }

// Parse decodes and validates the JSON shape of an SBOM.
func Parse(data []byte) (Document, error) {
	var d Document
	if err := json.Unmarshal(data, &d); err != nil {
		return Document{}, fmt.Errorf("sbom: parse JSON: %w", err)
	}
	if err := Validate(d); err != nil {
		return Document{}, err
	}
	return d, nil
}

// Validate checks required metadata, hashes, licenses, uniqueness and
// deterministic ordering. It does not access the filesystem.
func Validate(d Document) error {
	if d.Schema != Schema {
		return fmt.Errorf("sbom: schema %q is not %q", d.Schema, Schema)
	}
	if d.Generator.Name == "" || d.Generator.Version == "" || d.Generator.Go == "" {
		return errors.New("sbom: generator name, version and Go version are required")
	}
	if d.Graph.Tool != "go version -m" || d.Graph.Version == "" {
		return errors.New("sbom: graph must record go version -m and its version")
	}
	if d.Subject.Name == "" || !validDigest(d.Subject.Digest) {
		return errors.New("sbom: subject name and sha256 digest are required")
	}
	if len(d.Components) == 0 {
		return errors.New("sbom: no shipped components")
	}
	var prevName, prevVersion string
	seen := map[string]bool{}
	mainCount := 0
	for _, c := range d.Components {
		if c.Type != "go-module" || c.Name == "" || c.Version == "" {
			return fmt.Errorf("sbom: incomplete component %q", c.Name)
		}
		if c.Hash == "" || c.SourceDigest == "" || c.License == "" {
			return fmt.Errorf("sbom: component %q is missing version/hash/source digest/license", c.Name)
		}
		if !validModuleHash(c.Hash) {
			return fmt.Errorf("sbom: component %q has malformed module hash", c.Name)
		}
		if !validDigest(c.SourceDigest) {
			return fmt.Errorf("sbom: component %q has malformed source digest", c.Name)
		}
		if seen[c.Name+"@"+c.Version] {
			return fmt.Errorf("sbom: duplicate component %s@%s", c.Name, c.Version)
		}
		seen[c.Name+"@"+c.Version] = true
		if prevName != "" && (c.Name < prevName || (c.Name == prevName && c.Version < prevVersion)) {
			return fmt.Errorf("sbom: components are not in deterministic order (%s@%s before %s@%s)", prevName, prevVersion, c.Name, c.Version)
		}
		prevName, prevVersion = c.Name, c.Version
		if c.Main {
			mainCount++
		}
	}
	if mainCount != 1 {
		return fmt.Errorf("sbom: want exactly one main component, got %d", mainCount)
	}
	return nil
}

// ValidateAgainstSubject verifies the SBOM's subject binding against the
// digest supplied by signed-build/provenance verification.
func ValidateAgainstSubject(d Document, signedSubject string) error {
	if err := Validate(d); err != nil {
		return err
	}
	if signedSubject == "" || d.Subject.Digest != signedSubject {
		return fmt.Errorf("sbom: subject digest %q does not match signed build subject %q", d.Subject.Digest, signedSubject)
	}
	return nil
}

// ValidateArtifact additionally re-reads the binary's embedded module graph,
// rejecting undeclared or omitted shipped modules and a mismatched artifact.
func ValidateArtifact(d Document, binaryPath string) error {
	if err := Validate(d); err != nil {
		return err
	}
	digest, err := fileDigest(binaryPath)
	if err != nil {
		return err
	}
	if d.Subject.Digest != sha256Prefix+digest {
		return errors.New("sbom: subject is not bound to artifact bytes")
	}
	m, err := readMetadata(binaryPath)
	if err != nil {
		return err
	}
	cache, err := goEnv("GOMODCACHE")
	if err != nil {
		return err
	}
	mainSeen := false
	for _, c := range d.Components {
		if c.Main {
			if mainSeen || c.Name != m.mainPath || c.Hash != d.Subject.Digest || c.SourceDigest != d.Subject.Digest {
				return errors.New("sbom: main component is not bound to the binary's main module and subject")
			}
			mainSeen = true
		}
	}
	if !mainSeen {
		return errors.New("sbom: main component is absent")
	}
	want := map[string]modInfo{}
	for _, x := range m.modules {
		want[x.path] = x
	}
	for _, c := range d.Components {
		if c.Main {
			continue
		}
		x, ok := want[c.Name]
		if !ok || x.version != c.Version {
			return fmt.Errorf("sbom: component %s@%s is not in binary module graph", c.Name, c.Version)
		}
		if x.sum != c.Hash {
			if x.sum != "" {
				return fmt.Errorf("sbom: component %s has hash %q, binary records %q", c.Name, c.Hash, x.sum)
			}
		}
		if x.replacement == nil && c.Replacement != nil {
			return fmt.Errorf("sbom: component %s contains unexpected replacement metadata", c.Name)
		}
		lookupPath, lookupVersion := c.Name, c.Version
		if x.replacement != nil {
			if c.Replacement == nil || c.Replacement.Name != x.replacement.path || c.Replacement.Version != x.replacement.version {
				return fmt.Errorf("sbom: component %s replacement metadata does not match binary", c.Name)
			}
			lookupPath, lookupVersion = x.replacement.path, x.replacement.version
			if isLocalReplacementPath(lookupPath) {
				if x.sum == "" && c.Hash != c.SourceDigest {
					return fmt.Errorf("sbom: local replacement %s hash does not match source digest", c.Name)
				}
				delete(want, c.Name)
				continue
			}
		}
		dir, err := moduleDirectory("", cache, lookupPath, lookupVersion)
		if err != nil {
			return err
		}
		sourceDigest, err := treeDigest(dir)
		if err != nil || sourceDigest != c.SourceDigest {
			if err != nil {
				return err
			}
			return fmt.Errorf("sbom: component %s source digest does not match module cache", c.Name)
		}
		if x.sum == "" && c.Hash != sourceDigest {
			return fmt.Errorf("sbom: local replacement %s hash does not match source digest", c.Name)
		}
		license, _, err := licenseFor(dir)
		if err != nil {
			return err
		}
		if license != c.License {
			return fmt.Errorf("sbom: component %s license does not match module cache", c.Name)
		}
		delete(want, c.Name)
	}
	for path, x := range want {
		return fmt.Errorf("sbom: binary module %s@%s is absent from SBOM", path, x.version)
	}
	return nil
}

type modInfo struct {
	path, version, sum string
	replacement        *modInfo
}
type buildMetadata struct {
	goVersion, mainPath, mainVersion string
	modules                          []modInfo
}

func readMetadata(binaryPath string) (buildMetadata, error) {
	cmd := exec.Command("go", "version", "-m", binaryPath)
	cmd.Env = offlineEnv()
	out, err := cmd.Output()
	if err != nil {
		return buildMetadata{}, fmt.Errorf("sbom: go version -m: %w", err)
	}
	var result buildMetadata
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.Contains(line, ": go") && result.goVersion == "" {
			if i := strings.LastIndex(line, ": "); i >= 0 {
				result.goVersion = strings.TrimSpace(line[i+2:])
			}
			continue
		}
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case "path":
			result.mainPath = f[1]
		case "mod":
			result.mainPath = f[1]
			if len(f) >= 3 {
				result.mainVersion = f[2]
			}
		case "dep":
			if len(f) < 3 {
				return buildMetadata{}, fmt.Errorf("sbom: malformed dep metadata %q", line)
			}
			m := modInfo{path: f[1], version: f[2]}
			if len(f) >= 4 && strings.HasPrefix(f[3], "h1:") {
				m.sum = f[3]
			}
			result.modules = append(result.modules, m)
		case "=>":
			if len(f) >= 3 && len(result.modules) > 0 {
				result.modules[len(result.modules)-1].replacement = &modInfo{path: f[1], version: f[2]}
			}
		}
	}
	if result.goVersion == "" || result.mainPath == "" {
		return buildMetadata{}, errors.New("sbom: binary has no Go module metadata")
	}
	return result, nil
}

func rootModule(root string) (string, error) {
	b, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return "", fmt.Errorf("sbom: reading go.mod: %w", err)
	}
	f, err := modfile.Parse("go.mod", b, nil)
	if err != nil {
		return "", fmt.Errorf("sbom: parsing go.mod: %w", err)
	}
	if f.Module == nil || f.Module.Mod.Path == "" {
		return "", errors.New("sbom: go.mod has no module path")
	}
	return f.Module.Mod.Path, nil
}

func goEnv(name string) (string, error) {
	cmd := exec.Command("go", "env", name)
	cmd.Env = offlineEnv()
	b, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("sbom: go env %s: %w", name, err)
	}
	if strings.TrimSpace(string(b)) == "" {
		return "", fmt.Errorf("sbom: go env %s is empty", name)
	}
	return strings.TrimSpace(string(b)), nil
}

func offlineEnv() []string {
	env := make([]string, 0, len(os.Environ())+2)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "GOPROXY=") || strings.HasPrefix(kv, "GOSUMDB=") {
			continue
		}
		env = append(env, kv)
	}
	return append(env, "GOPROXY=off", "GOSUMDB=off")
}

func moduleDirectory(root, cache, path, version string) (string, error) {
	// A local replacement is recorded by go as a filesystem path (often
	// ./submodule) and has no module-cache entry or h1 sum. Resolve it only
	// during generation, when the repository root is available.
	if isLocalReplacementPath(path) {
		if root == "" {
			return "", fmt.Errorf("sbom: local replacement %s cannot be resolved without repository root", path)
		}
		dir := path
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(root, filepath.FromSlash(path))
		}
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			if err == nil {
				err = errors.New("not a directory")
			}
			return "", fmt.Errorf("sbom: local replacement %s: %w", path, err)
		}
		return dir, nil
	}
	escaped, err := module.EscapePath(path)
	if err != nil {
		return "", fmt.Errorf("sbom: escaping module %s: %w", path, err)
	}
	dir := filepath.Join(cache, escaped+"@"+version)
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		if err == nil {
			err = errors.New("not a directory")
		}
		return "", fmt.Errorf("sbom: module cache entry %s@%s: %w", path, version, err)
	}
	return dir, nil
}

func isLocalReplacementPath(path string) bool {
	return filepath.IsAbs(path) || path == "." || strings.HasPrefix(path, "."+string(filepath.Separator)) || strings.HasPrefix(path, "./") || strings.HasPrefix(path, "../")
}

func fileDigest(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("sbom: reading %s: %w", path, err)
	}
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:]), nil
}

func treeDigest(root string) (string, error) {
	h := sha256.New()
	var paths []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, path)
		if rel == "." {
			return nil
		}
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if parts[0] == ".git" || parts[0] == "node_modules" {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.IsDir() {
			paths = append(paths, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("sbom: hashing source tree %s: %w", root, err)
	}
	sort.Strings(paths)
	for _, rel := range paths {
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%d:%s:%d:", len(rel), rel, len(b))
		h.Write(b)
	}
	return sha256Prefix + hex.EncodeToString(h.Sum(nil)), nil
}

func licenseFor(root string) (string, string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		n := strings.ToUpper(d.Name())
		if n == "LICENSE" || strings.HasPrefix(n, "LICENSE.") || strings.HasPrefix(n, "COPYING") || strings.HasPrefix(n, "NOTICE") {
			rel, _ := filepath.Rel(root, path)
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		return "", "", fmt.Errorf("sbom: finding license in %s: %w", root, err)
	}
	sort.Strings(files)
	if len(files) == 0 {
		return "NOASSERTION", "", nil
	}
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(files[0])))
	if err != nil {
		return "", "", err
	}
	text := strings.ToLower(string(b))
	id := "NOASSERTION"
	switch {
	case strings.Contains(text, "apache license"):
		id = "Apache-2.0"
	case strings.Contains(text, "permission is hereby granted, free of charge"):
		id = "MIT"
	case strings.Contains(text, "isc license"):
		id = "ISC"
	case strings.Contains(text, "mozilla public license"):
		id = "MPL-2.0"
	case strings.Contains(text, "redistribution and use in source and binary forms"):
		if strings.Contains(text, "neither the name") {
			id = "BSD-3-Clause"
		} else {
			id = "BSD-2-Clause"
		}
	case strings.Contains(text, "gnu general public license"):
		id = "GPL"
	}
	return id, files[0], nil
}

func validDigest(s string) bool {
	if !strings.HasPrefix(s, sha256Prefix) || len(s) != len(sha256Prefix)+64 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(s, sha256Prefix))
	return err == nil
}

func validModuleHash(s string) bool {
	if strings.HasPrefix(s, "h1:") {
		// Go module sums are base64-encoded SHA-256 values. Keep the check
		// intentionally strict so a placeholder cannot enter signed evidence.
		if len(s) != len("h1:")+44 {
			return false
		}
		for _, r := range s[len("h1:"):] {
			if !(r >= 'A' && r <= 'Z') && !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '+' && r != '/' && r != '=' {
				return false
			}
		}
		return true
	}
	return validDigest(s)
}

// EqualCanonical reports whether two documents have byte-identical canonical
// JSON representations.
func EqualCanonical(a, b Document) bool {
	x, e1 := a.Marshal()
	y, e2 := b.Marshal()
	return e1 == nil && e2 == nil && bytes.Equal(x, y)
}
