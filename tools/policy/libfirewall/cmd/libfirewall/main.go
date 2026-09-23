// Command libfirewall runs the REV-101-05 library firewall against a Go
// module tree: every direct third-party import edge in the tree must sit
// inside its dependency-roles.yaml row's allowed_import_roots.
//
// Exit code is 0 with no violations, 1 with at least one, 2 on a usage or
// scan error.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/libfirewall"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("libfirewall", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "path to the Go module root to scan")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	report, err := libfirewall.Evaluate(*root)
	if err != nil {
		fmt.Fprintf(stderr, "libfirewall: %v\n", err)
		return 2
	}
	for _, v := range report.Violations {
		fmt.Fprintf(stdout, "%s imports %s (role %s), allowed roots: %v\n",
			v.Importer, v.ImportedPath, v.Role, v.AllowedRoots)
	}
	if len(report.Violations) > 0 {
		return 1
	}
	return 0
}
