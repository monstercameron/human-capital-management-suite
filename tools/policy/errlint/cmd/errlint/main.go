// Command errlint flags blanked call results (`_ = f()`) in the named Go
// files, excluding statements inside deferred rollbacks. A finding names
// file, line, enclosing function and the blanked statement.
//
// Exit code is 0 with no findings, 1 with at least one, 2 on a usage or
// scan error.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/errlint"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("errlint", flag.ContinueOnError)
	fs.SetOutput(stderr)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	files := fs.Args()
	if len(files) == 0 {
		fmt.Fprintln(stderr, "errlint: no files named")
		return 2
	}
	for _, file := range files {
		if !errlint.Exists(file) {
			fmt.Fprintf(stderr, "errlint: no such file: %s\n", file)
			return 2
		}
	}
	report, err := errlint.EvaluateFiles(files)
	if err != nil {
		fmt.Fprintf(stderr, "errlint: %v\n", err)
		return 2
	}
	violations := report.FilterExceptions().Violations()
	for _, v := range violations {
		fmt.Fprintf(stdout, "%s:%d: %s: %s\n", v.File, v.Line, v.Function, v.Text)
	}
	if len(violations) > 0 {
		return 1
	}
	return 0
}
