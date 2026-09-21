// Command storagemanifest regenerates the DB-002/003/004 model-to-storage
// manifests from the compiled model catalog and SQL migrations.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/monstercameron/human-capital-management-suite/tools/gen/storagemanifest"
)

func main() {
	os.Exit(run(os.Stdout, os.Stderr))
}

func run(out, errOut io.Writer) int {
	wd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(errOut, "storagemanifest: %v\n", err)
		return 1
	}
	root, err := storagemanifest.RepoRoot(wd)
	if err != nil {
		fmt.Fprintf(errOut, "storagemanifest: %v\n", err)
		return 1
	}
	generated, err := storagemanifest.BuildAll(filepath.Join(root, "migrations"))
	if err != nil {
		fmt.Fprintf(errOut, "storagemanifest: %v\n", err)
		return 1
	}
	if err := storagemanifest.WriteAll(root, generated); err != nil {
		fmt.Fprintf(errOut, "storagemanifest: %v\n", err)
		return 1
	}
	fmt.Fprintln(out, "storagemanifest: regenerated definitions/model manifests")
	return 0
}
