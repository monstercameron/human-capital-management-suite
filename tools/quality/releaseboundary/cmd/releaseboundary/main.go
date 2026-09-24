// Command releaseboundary creates the SBOM for a built release binary and
// refuses to publish it when its actual module graph crosses the P1B boundary.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/monstercameron/human-capital-management-suite/tools/quality/releaseboundary"
	"github.com/monstercameron/human-capital-management-suite/tools/quality/sbom"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "releaseboundary:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	fs := flag.NewFlagSet("releaseboundary", flag.ContinueOnError)
	root := fs.String("root", ".", "repository root")
	binary := fs.String("binary", "", "built release binary")
	out := fs.String("out", "", "path for the artifact-bound SBOM")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *binary == "" || *out == "" {
		return fmt.Errorf("-binary and -out are required")
	}
	document, err := sbom.Generate(*root, *binary)
	if err != nil {
		return fmt.Errorf("generate SBOM: %w", err)
	}
	if err := releaseboundary.CheckArtifact(document, *binary); err != nil {
		return err
	}
	data, err := document.Marshal()
	if err != nil {
		return fmt.Errorf("marshal SBOM: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := os.WriteFile(*out, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write SBOM: %w", err)
	}
	fmt.Printf("releaseboundary: %s passed (%d shipped Go modules); SBOM: %s\n", *binary, len(document.Components), *out)
	return nil
}
