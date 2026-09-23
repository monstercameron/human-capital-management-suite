// Command clockinject runs the REV-101-07 injected-clock policy against a
// Go module tree and reports every direct time.Now read in a non-test Go
// file under internal/engines or internal/domains that is neither the
// func() time.Time wall-default adapter idiom nor a site named in
// clockinject.DeclaredAdapters.
//
// Exit code is 0 with no violations, 1 with at least one, 2 on a usage or
// scan error.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/clockinject"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("clockinject", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "path to the Go module root to scan")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	report, err := clockinject.Evaluate(*root)
	if err != nil {
		fmt.Fprintf(stderr, "clockinject: %v\n", err)
		return 2
	}
	violations := report.Violations()
	for _, v := range violations {
		fmt.Fprintf(stdout, "%s:%d: %s: %s\n", v.File, v.Line, v.Function, v.Reason)
	}
	if len(violations) > 0 {
		return 1
	}
	return 0
}
