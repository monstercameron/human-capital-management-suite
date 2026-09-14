// Command closurewitness emits SLICE-016's per-intent closure witnesses
// from the live registries: one digest-backed witness per source-bound
// definition, printed as the canonical JSON report (-format json) or as the
// coverage summary generated from those witnesses (-format summary). With
// -strict it exits non-zero while any witness or orphan edge is
// SLICE_CLOSURE_INCOMPLETE, so it can gate a CI step.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/closurewitness"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr, time.Now); err != nil {
		fmt.Fprintln(os.Stderr, "closurewitness:", err)
		os.Exit(1)
	}
}

// errIncomplete is returned under -strict when closure is incomplete.
var errIncomplete = errors.New(closurewitness.ResultIncomplete)

func run(args []string, stdout, stderr io.Writer, now func() time.Time) error {
	flags := flag.NewFlagSet("closurewitness", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "repository root")
	asOf := flags.String("as-of", "", "YYYY-MM-DD date evidence waivers are judged against (default: today, UTC)")
	format := flags.String("format", "summary", "output format: summary or json")
	strict := flags.Bool("strict", false, "exit non-zero while closure is incomplete")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *format != "summary" && *format != "json" {
		return fmt.Errorf("unknown -format %q (want summary or json)", *format)
	}
	date := *asOf
	if date == "" {
		date = now().UTC().Format("2006-01-02")
	}

	snap, err := closurewitness.LoadSnapshot(*root, date)
	if err != nil {
		return err
	}
	report, err := closurewitness.Compile(snap)
	if err != nil {
		return err
	}
	if *format == "json" {
		out, err := closurewitness.MarshalReport(report)
		if err != nil {
			return err
		}
		if _, err := stdout.Write(out); err != nil {
			return err
		}
	} else {
		fmt.Fprint(stdout, closurewitness.Summary(report))
	}
	if *strict && report.Result != closurewitness.ResultComplete {
		return errIncomplete
	}
	return nil
}
