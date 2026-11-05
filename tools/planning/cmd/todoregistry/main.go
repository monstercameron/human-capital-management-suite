// Command todoregistry regenerates definitions/planning/todo-registry.json
// from planning/todos.md (GOV-002).
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/todoregistry"
)

func main() {
	outputPath := flag.String("output", filepath.Join("definitions", "planning", "todo-registry.json"), "path to output JSON file")
	markdownPathFlag := flag.String("markdown", filepath.Join("planning", "archive", "todos-2026-10-22.md"), "path to the todos markdown file")
	knownDefectsPath := flag.String("known-defects", filepath.Join("definitions", "planning", "known-defects.yaml"), "path to the known-defects allow-list")
	flag.Parse()

	content, err := os.ReadFile(*markdownPathFlag)
	if err != nil {
		log.Fatalf("failed to read %s: %v", *markdownPathFlag, err)
	}

	todos, parseErrs := todoregistry.ParseTodos(string(content))
	if len(parseErrs) > 0 {
		log.Printf("parse errors occurred:")
		for _, err := range parseErrs {
			log.Printf("  %v", err)
		}
		os.Exit(1)
	}

	unresolved := todoregistry.ResolveDependencies(todos)
	if len(unresolved) > 0 {
		knownDefects, err := todoregistry.LoadKnownDefects(*knownDefectsPath)
		if err != nil {
			log.Fatalf("failed to load known-defects allow-list: %v", err)
		}
		allowed := make(map[string]bool, len(knownDefects))
		for _, d := range knownDefects {
			allowed[d.From+"|"+d.To] = true
		}

		var unexpected int
		for _, u := range unresolved {
			if allowed[u.From+"|"+u.To] {
				continue
			}
			unexpected++
			if unexpected <= 10 {
				log.Printf("unresolved dependency: %s depends on %s (not found)", u.From, u.To)
			}
		}
		if unexpected > 10 {
			log.Printf("  ... and %d more", unexpected-10)
		}
		if unexpected > 0 {
			log.Fatalf("%d unresolved dependencies are not allow-listed in %s", unexpected, *knownDefectsPath)
		}
	}

	jsonBytes, err := todoregistry.ToJSON(todos)
	if err != nil {
		log.Fatalf("failed to generate JSON: %v", err)
	}

	outputDir := filepath.Dir(*outputPath)
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		log.Fatalf("failed to create output directory %s: %v", outputDir, err)
	}

	if err := os.WriteFile(*outputPath, jsonBytes, 0o644); err != nil {
		log.Fatalf("failed to write %s: %v", *outputPath, err)
	}

	fmt.Printf("Generated %s with %d todos\n", *outputPath, len(todos))
}
