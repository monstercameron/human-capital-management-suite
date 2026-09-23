// Command quality is the TOOL-011 authoritative quality gate: it runs
// gofmt -l, go vet ./..., go tool staticcheck ./..., and the registry-driven
// frontend localization/accessibility matrix over the root
// module and exits non-zero if any of them reports a problem. Run it with
// `go run ./tools/quality` from the repository root.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/quality/decomposition"
	"github.com/monstercameron/human-capital-management-suite/tools/quality/testhygiene"
	"gopkg.in/yaml.v3"
)

// findRepoRoot walks up from the working directory looking for go.mod, so
// `go run ./tools/quality` behaves the same whether invoked from the
// repository root or from a subdirectory.
func findRepoRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if info, statErr := os.Stat(filepath.Join(dir, "go.mod")); statErr == nil && !info.IsDir() {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no go.mod found walking up from %s", dir)
		}
		dir = parent
	}
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) > 0 && args[0] == "decomposition" {
		return runDecomposition(args[1:], stdout, stderr)
	}
	if len(args) > 0 && args[0] == "testhygiene" {
		return runTesthygiene(args[1:], stdout, stderr)
	}
	root, err := findRepoRoot()
	if err != nil {
		fmt.Fprintf(stderr, "quality: %v\n", err)
		return 2
	}

	ok := true

	fmt.Println("== gofmt -l ==")
	violations, err := gofmtViolations(root)
	if err != nil {
		fmt.Fprintf(stderr, "quality: gofmt check failed to run: %v\n", err)
		ok = false
	} else if len(violations) > 0 {
		ok = false
		fmt.Println("gofmt reported unformatted files:")
		for _, v := range violations {
			fmt.Printf("  - %s\n", v)
		}
	} else {
		fmt.Println("no formatting drift")
	}

	fmt.Println("== go vet ./... ==")
	if out, passed := runGoVet(root, "./..."); !passed {
		ok = false
		fmt.Print(out)
	} else {
		fmt.Println("go vet: clean")
	}

	fmt.Println("== go tool staticcheck ./... ==")
	if out, passed := runStaticcheck(root, "./..."); !passed {
		ok = false
		fmt.Print(out)
	} else {
		fmt.Println("staticcheck: clean")
	}

	fmt.Println("== frontend i18n + accessibility ==")
	if out, passed := runFrontendExperienceGate(root); !passed {
		ok = false
		fmt.Print(out)
	} else {
		fmt.Println("frontend i18n + accessibility: clean")
	}

	if !ok {
		fmt.Fprintln(stderr, "quality: FAILED")
		return 1
	}
	fmt.Println("quality: PASSED")
	return 0
}

// runDecomposition is an explicit quality subcommand. It requires a supplied
// decision record; an omitted record is never treated as an empty passing
// decision. The record's process map is the submitted ownership inventory and
// is checked by the same decomposition contract used by tests and reviewers.
func runDecomposition(args []string, stdout, stderr io.Writer) int {
	var decisionPath, scanRoot, indexPath string
	for i := 0; i < len(args); i += 2 {
		if i+1 >= len(args) {
			fmt.Fprintln(stderr, "quality decomposition: usage: -decision <json-file> or -root <root> -index <json-file>")
			return 2
		}
		switch args[i] {
		case "-decision":
			decisionPath = args[i+1]
		case "-root":
			scanRoot = args[i+1]
		case "-index":
			indexPath = args[i+1]
		default:
			fmt.Fprintln(stderr, "quality decomposition: unknown option")
			return 2
		}
	}
	if scanRoot != "" || indexPath != "" {
		if scanRoot == "" || indexPath == "" || decisionPath != "" {
			fmt.Fprintln(stderr, "quality decomposition: usage: -root <root> -index <json-file>")
			return 2
		}
		inventory, err := loadProcessInventory(filepath.Join(scanRoot, "definitions", "architecture", "process-roles.yaml"))
		if err == nil {
			err = decomposition.ScanRootWithInventory(scanRoot, indexPath, inventory)
		}
		if err != nil {
			fmt.Fprintf(stderr, "quality decomposition: REJECTED: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "quality decomposition: PASS: %s\n", scanRoot)
		return 0
	}
	if decisionPath == "" {
		fmt.Fprintln(stderr, "quality decomposition: usage: -decision <json-file> or -root <root> -index <json-file>")
		return 2
	}
	root, err := findRepoRoot()
	if err != nil {
		fmt.Fprintf(stderr, "quality decomposition: repository: %v\n", err)
		return 2
	}
	decision, err := decomposition.ReadFile(decisionPath)
	if err == nil {
		err = decomposition.Check(decision)
	}
	if err == nil {
		err = decomposition.VerifyEvidence(root, decision)
	}
	if err == nil {
		var inventory map[string][]string
		inventory, err = loadProcessInventory(filepath.Join(root, "definitions", "architecture", "process-roles.yaml"))
		if err == nil {
			err = decomposition.CheckInventory(decision, inventory)
		}
	}
	if err != nil {
		fmt.Fprintf(stderr, "quality decomposition: REJECTED: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "quality decomposition: PASS: %s\n", decisionPath)
	return 0
}

// runTesthygiene is the REV-103-03 hygiene lint. It scans root (default: the
// repository root) for alias tests, _Race tests without concurrency and
// Golden tests that skip, prints the deterministic report and exits non-zero
// when any violation remains. It is an explicit subcommand rather than part
// of the default quality run because the wider tree still carries
// pre-existing violations owned by other todos; wiring it into the default
// run happens once that backlog is cleared.
func runTesthygiene(args []string, stdout, stderr io.Writer) int {
	root := ""
	for i := 0; i < len(args); i += 2 {
		if i+1 >= len(args) {
			fmt.Fprintln(stderr, "quality testhygiene: usage: [-root <dir>]")
			return 2
		}
		switch args[i] {
		case "-root":
			root = args[i+1]
		default:
			fmt.Fprintln(stderr, "quality testhygiene: unknown option")
			return 2
		}
	}
	if root == "" {
		var err error
		root, err = findRepoRoot()
		if err != nil {
			fmt.Fprintf(stderr, "quality testhygiene: %v\n", err)
			return 2
		}
	}
	violations, err := testhygiene.CheckTree(root)
	if err != nil {
		fmt.Fprintf(stderr, "quality testhygiene: REJECTED: %v\n", err)
		return 1
	}
	if len(violations) > 0 {
		fmt.Fprint(stdout, testhygiene.FormatReport(violations))
		fmt.Fprintf(stderr, "quality testhygiene: REJECTED: %d violation(s)\n", len(violations))
		return 1
	}
	fmt.Fprintln(stdout, "quality testhygiene: PASS")
	return 0
}

func loadProcessInventory(path string) (map[string][]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read process inventory: %w", err)
	}
	var manifest struct {
		Processes []struct {
			Command  string   `yaml:"command"`
			Packages []string `yaml:"semantic_packages"`
		} `yaml:"processes"`
	}
	if err := yaml.Unmarshal(b, &manifest); err != nil {
		return nil, fmt.Errorf("parse process inventory: %w", err)
	}
	inventory := make(map[string][]string, len(manifest.Processes))
	for _, process := range manifest.Processes {
		process.Command = strings.TrimSpace(process.Command)
		if process.Command == "" {
			return nil, errors.New("parse process inventory: process command is required")
		}
		if _, exists := inventory[process.Command]; exists {
			return nil, fmt.Errorf("parse process inventory: duplicate process command %q", process.Command)
		}
		seen := make(map[string]bool, len(process.Packages))
		for _, raw := range process.Packages {
			pkg := strings.TrimSpace(raw)
			if pkg == "" {
				return nil, fmt.Errorf("parse process inventory: process %q has an empty semantic package", process.Command)
			}
			if seen[pkg] {
				return nil, fmt.Errorf("parse process inventory: process %q repeats semantic package %q", process.Command, pkg)
			}
			seen[pkg] = true
			inventory[process.Command] = append(inventory[process.Command], pkg)
		}
	}
	return inventory, nil
}
