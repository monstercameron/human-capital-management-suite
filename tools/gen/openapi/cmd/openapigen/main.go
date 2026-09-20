// Command openapigen runs the INTAPI-008 generator, writing
// schema/openapi/rpcs.openapi.yaml from the compiled Protobuf descriptors.
// Run it from the repository root:
//
//	go run ./tools/gen/openapi/cmd/openapigen
//
// -check compares instead of writing and exits non-zero on drift.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/monstercameron/human-capital-management-suite/tools/gen/openapi"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run parses args, then writes or checks the document. It returns the
// process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("openapigen", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "repository root")
	out := fs.String("out", openapi.DefaultOutputPath, "output path, relative to -root unless absolute")
	check := fs.Bool("check", false, "fail if the output differs from a fresh generation instead of writing it")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *check {
		if err := openapi.Check(*root, *out); err != nil {
			fmt.Fprintln(stderr, "openapigen:", err)
			return 1
		}
		fmt.Fprintln(stdout, "openapigen: up to date:", *out)
		return 0
	}
	if err := openapi.Write(*root, *out); err != nil {
		fmt.Fprintln(stderr, "openapigen:", err)
		return 1
	}
	fmt.Fprintln(stdout, "openapigen: wrote", *out)
	return 0
}
