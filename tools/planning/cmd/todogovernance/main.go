// Command todogovernance runs the GOV-016, GOV-017 and GOV-025 backlog
// governance validators against the real registry, markdown and
// BusinessIntent catalog, prints every finding, and exits non-zero if any
// are reported.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/todogovernance"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout))
}

func run(args []string, out io.Writer) int {
	fs := flag.NewFlagSet("todogovernance", flag.ContinueOnError)
	markdownPath := fs.String("markdown", filepath.Join("planning", "archive", "todos-2026-10-22.md"), "path to the todos markdown file")
	registryPath := fs.String("registry", filepath.Join("definitions", "planning", "todo-registry.json"), "path to the compiled todo registry JSON")
	catalogPath := fs.String("catalog", filepath.Join("planning", "specs", "business-intent-catalog.md"), "path to the BusinessIntent catalog")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	registryTodos, err := todogovernance.LoadRegistry(*registryPath)
	if err != nil {
		log.Printf("failed to load registry %s: %v", *registryPath, err)
		return 2
	}

	_, records, parseErrs, err := todogovernance.LoadMarkdown(*markdownPath)
	if err != nil {
		log.Printf("failed to load markdown %s: %v", *markdownPath, err)
		return 2
	}

	catalogDefinitions, err := todogovernance.LoadCatalogDefinitions(*catalogPath)
	if err != nil {
		log.Printf("failed to load catalog %s: %v", *catalogPath, err)
		return 2
	}

	var findings []todogovernance.Finding
	findings = append(findings, todogovernance.ValidateDependencies(registryTodos, todogovernance.RawDependsByID(records))...)
	findings = append(findings, todogovernance.ValidateTDD(records, parseErrs)...)
	findings = append(findings, todogovernance.ValidateIntentContext(records, catalogDefinitions)...)
	findings = append(findings, todogovernance.ValidateTestMatrixApplicability(records)...)

	if len(findings) == 0 {
		fmt.Fprintln(out, "todogovernance: no GOV-016/GOV-017/GOV-018/GOV-025 violations found")
		return 0
	}

	for _, f := range findings {
		fmt.Fprintln(out, f.String())
	}
	fmt.Fprintf(out, "todogovernance: %d violation(s) found\n", len(findings))
	return 1
}
