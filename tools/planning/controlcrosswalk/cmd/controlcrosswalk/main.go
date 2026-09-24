// Command controlcrosswalk validates and regenerates the security-control
// crosswalk against definitions/planning/todo-registry.json (GOV-030).
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/controlcrosswalk"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/traceability"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("controlcrosswalk", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "repository root")
	fixture := fs.String("fixture", filepath.Join("tools", "planning", "controlcrosswalk", "testdata", "security-control-crosswalk.yaml"), "YAML crosswalk seed")
	registry := fs.String("registry", filepath.Join("definitions", "planning", "todo-registry.json"), "generated todo registry")
	format := fs.String("format", "summary", "output format: summary or json")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	fixturePath := *fixture
	if !filepath.IsAbs(fixturePath) {
		fixturePath = filepath.Join(*root, fixturePath)
	}
	registryPath := *registry
	if !filepath.IsAbs(registryPath) {
		registryPath = filepath.Join(*root, registryPath)
	}
	definition, err := controlcrosswalk.Load(fixturePath)
	if err != nil {
		fmt.Fprintf(stderr, "controlcrosswalk: %v\n", err)
		return 2
	}
	todos, err := controlcrosswalk.LoadRegistry(registryPath)
	if err != nil {
		fmt.Fprintf(stderr, "controlcrosswalk: %v\n", err)
		return 2
	}
	testNames, err := scanEvidenceTests(*root)
	if err != nil {
		fmt.Fprintf(stderr, "controlcrosswalk: scan test names: %v\n", err)
		return 2
	}
	revision, err := controlcrosswalk.Regenerate(definition, todos, testNames)
	if err != nil {
		fmt.Fprintf(stderr, "controlcrosswalk: %v\n", err)
		return 1
	}
	if err := validateOwnerPackages(*root, revision); err != nil {
		fmt.Fprintf(stderr, "controlcrosswalk: %v\n", err)
		return 1
	}
	if err := controlcrosswalk.Verify(revision); err != nil {
		fmt.Fprintf(stderr, "controlcrosswalk: %v\n", err)
		return 1
	}
	if *format == "json" {
		data, err := revision.JSON()
		if err != nil {
			fmt.Fprintf(stderr, "controlcrosswalk: %v\n", err)
			return 1
		}
		if _, err := stdout.Write(data); err != nil {
			fmt.Fprintf(stderr, "controlcrosswalk: write JSON: %v\n", err)
			return 1
		}
		return 0
	}
	if *format != "summary" {
		fmt.Fprintf(stderr, "controlcrosswalk: unsupported format %q (want summary or json)\n", *format)
		return 2
	}
	fmt.Fprintf(stdout, "%s\n", revision.Explain())
	fmt.Fprintln(stdout, "controlcrosswalk: PASS")
	return 0
}

func validateOwnerPackages(root string, revision controlcrosswalk.Revision) error {
	rootPath, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	rootPath, err = filepath.EvalSymlinks(rootPath)
	if err != nil {
		return fmt.Errorf("resolve repository root: %w", err)
	}
	for _, control := range revision.Controls {
		owner := filepath.FromSlash(control.OwnerPackage)
		if filepath.IsAbs(owner) || !filepath.IsLocal(owner) {
			return fmt.Errorf("%s.owner_package: PACKAGE_OUTSIDE_ROOT", control.ID)
		}
		packagePath, err := filepath.EvalSymlinks(filepath.Join(rootPath, owner))
		if err != nil {
			return fmt.Errorf("%s.owner_package: PACKAGE_NOT_FOUND", control.ID)
		}
		relative, err := filepath.Rel(rootPath, packagePath)
		if err != nil || !filepath.IsLocal(relative) {
			return fmt.Errorf("%s.owner_package: PACKAGE_OUTSIDE_ROOT", control.ID)
		}
		entries, err := os.ReadDir(packagePath)
		if err != nil {
			return fmt.Errorf("%s.owner_package: PACKAGE_NOT_READABLE", control.ID)
		}
		foundGoSource := false
		for _, entry := range entries {
			name := entry.Name()
			if !entry.IsDir() && strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
				foundGoSource = true
				break
			}
		}
		if !foundGoSource {
			return fmt.Errorf("%s.owner_package: NOT_GO_PACKAGE", control.ID)
		}
	}
	return nil
}

func scanEvidenceTests(root string) (map[string]bool, error) {
	names := make(map[string]bool)
	for _, sourceRoot := range []string{"internal", "tools", "cmd", "gen"} {
		rootNames, err := traceability.ScanTestNames(filepath.Join(root, sourceRoot))
		if err != nil {
			return nil, err
		}
		for name := range rootNames {
			names[name] = true
		}
	}
	return names, nil
}
