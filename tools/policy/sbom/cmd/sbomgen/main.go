// Command sbomgen generates the CycloneDX 1.5 SBOM for this repository's
// root Go module (TOOL-017) and writes it to -out (or stdout if -out is
// empty).
//
// This command deliberately never writes into definitions/ itself — that
// directory is outside tools/policy/sbom's file root. The expected release
// invocation is:
//
//	go run ./tools/policy/sbom/cmd/sbomgen -out definitions/supply-chain/sbom.cdx.json
//
// which the orchestrator (or a CI step with write access to definitions/)
// runs directly, or run without -root/-out overrides and have the caller
// move the stdout/temp-file result into place.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/sbom"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sbomgen", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "path to the Go module root to scan")
	out := fs.String("out", "", "path to write the CycloneDX JSON document (default: stdout)")
	rootVersion := fs.String("version", "", "override the root component's version (default: "+sbom.DefaultRootVersion+")")
	artifact := fs.String("artifact", "", "path to the exact release artifact; bind its SHA-256 to the CycloneDX root component")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	doc, err := sbom.Generate(*root, sbom.Options{RootVersion: *rootVersion, ArtifactPath: *artifact})
	if err != nil {
		fmt.Fprintf(stderr, "sbomgen: %v\n", err)
		return 1
	}

	completeness, err := sbom.ValidateCompleteness(doc, *root)
	if err != nil {
		fmt.Fprintf(stderr, "sbomgen: validating completeness: %v\n", err)
		return 1
	}
	if !completeness.Empty() {
		for _, e := range completeness.Errors() {
			fmt.Fprintf(stderr, "sbomgen: %s\n", e)
		}
		return 1
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "sbomgen: encoding document: %v\n", err)
		return 1
	}
	data = append(data, '\n')

	if *out == "" {
		if _, err := stdout.Write(data); err != nil {
			fmt.Fprintf(stderr, "sbomgen: writing stdout: %v\n", err)
			return 1
		}
		fmt.Fprintf(stderr, "sbomgen: license counts: %s\n", sbom.FormatLicenseCounts(doc))
		return 0
	}
	if err := os.WriteFile(*out, data, 0o644); err != nil {
		fmt.Fprintf(stderr, "sbomgen: writing %s: %v\n", *out, err)
		return 1
	}
	fmt.Fprintf(stdout, "sbomgen: wrote %s (%d components); license counts: %s\n", *out, len(doc.Components), sbom.FormatLicenseCounts(doc))
	return 0
}
