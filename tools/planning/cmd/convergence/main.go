// Command convergence is CLOSE-002's single convergence gate. It runs the
// design, slice and implementation gap compilers over the concrete
// selection facts, prints the deduplicated gap register (-format json), the
// proposed atomic todos (-format proposals) or a summary (-format summary),
// and proves the fixed point: an unchanged re-run is byte-identical and
// adopting every proposal adds no gap identity.
//
// With -baseline it diffs against a previously emitted JSON report and
// prints the exact bounded delta. -require-fixed-point exits non-zero when
// the fixed point fails or the baseline gains identities; -strict exits
// non-zero while any selected-scope gap, unknown or finding remains.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/convergence"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr, time.Now); err != nil {
		fmt.Fprintln(os.Stderr, "convergence:", err)
		os.Exit(1)
	}
}

var (
	errUnresolved  = errors.New(convergence.ResultUnresolved)
	errNotFixed    = errors.New("the gap register did not reach a fixed point")
	errNewIdentity = errors.New("the register gained gap identities relative to the baseline")
)

func run(args []string, stdout, stderr io.Writer, now func() time.Time) error {
	flags := flag.NewFlagSet("convergence", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "repository root")
	asOf := flags.String("as-of", "", "YYYY-MM-DD date the selections and waivers are judged against (default: today, UTC)")
	format := flags.String("format", "summary", "output format: summary, json or proposals")
	baseline := flags.String("baseline", "", "previously emitted JSON report to diff against")
	strict := flags.Bool("strict", false, "exit non-zero while any selected-scope gap, unknown or finding remains")
	requireFixed := flags.Bool("require-fixed-point", false, "exit non-zero unless the fixed point holds and the baseline gains no identity")
	if err := flags.Parse(args); err != nil {
		return err
	}
	switch *format {
	case "summary", "json", "proposals":
	default:
		return fmt.Errorf("unknown -format %q (want summary, json or proposals)", *format)
	}
	date := *asOf
	if date == "" {
		date = now().UTC().Format("2006-01-02")
	}
	snap, err := convergence.LoadSnapshot(*root, date)
	if err != nil {
		return err
	}
	return emit(snap, *format, *baseline, *strict, *requireFixed, stdout, stderr)
}

func emit(snap convergence.Snapshot, format, baseline string, strict, requireFixed bool, stdout, stderr io.Writer) error {
	fp := convergence.RunToFixedPoint(snap, 4)
	report := fp.First
	var delta *convergence.Delta
	if baseline != "" {
		content, err := os.ReadFile(baseline)
		if err != nil {
			return fmt.Errorf("read baseline: %w", err)
		}
		var prev convergence.Report
		if err := json.Unmarshal(content, &prev); err != nil {
			return fmt.Errorf("parse baseline: %w", err)
		}
		d := convergence.Diff(prev, report)
		delta = &d
	}

	switch format {
	case "json":
		out, err := convergence.MarshalReport(report)
		if err != nil {
			return err
		}
		if _, err := stdout.Write(out); err != nil {
			return err
		}
	case "proposals":
		out, err := json.MarshalIndent(report.Proposals, "", "  ")
		if err != nil {
			return err
		}
		if _, err := stdout.Write(append(out, '\n')); err != nil {
			return err
		}
	default:
		fmt.Fprint(stdout, summary(report, fp))
	}
	if delta != nil {
		fmt.Fprintf(stderr, "baseline delta: added=%d removed=%d resolution_changed=%d\n", len(delta.Added), len(delta.Removed), len(delta.ResolutionChanged))
		for _, id := range delta.Added {
			fmt.Fprintln(stderr, "  ADDED:", id)
		}
		for _, id := range delta.Removed {
			fmt.Fprintln(stderr, "  REMOVED:", id)
		}
	}
	for _, reason := range fp.Reasons {
		fmt.Fprintln(stderr, "FIXED POINT:", reason)
	}
	switch {
	case requireFixed && !fp.Stable:
		return errNotFixed
	case requireFixed && delta != nil && len(delta.Added) > 0:
		return errNewIdentity
	case strict && report.Result != convergence.ResultResolved:
		return errUnresolved
	}
	return nil
}

func summary(r convergence.Report, fp convergence.FixedPoint) string {
	out := fmt.Sprintf("convergence: %s digest=%s\n", r.Result, r.Digest)
	out += fmt.Sprintf("  observations=%d gaps=%d deduplicated=%d selected_unresolved=%d\n", r.Totals.Observations, r.Totals.Gaps, r.Totals.Deduplicated, r.Totals.SelectedUnresolved)
	out += fmt.Sprintf("  scope %s\n", counts(r.Totals.ByScope))
	out += fmt.Sprintf("  resolution %s\n", counts(r.Totals.ByResolution))
	out += fmt.Sprintf("  facts=%d inert=%d proposals=%d unknowns=%d findings=%d\n", r.Totals.Facts, r.Totals.InertFacts, r.Totals.Proposals, r.Totals.Unknowns, r.Totals.Findings)
	for _, f := range r.Facts {
		if f.Inert {
			out += fmt.Sprintf("  INERT FACT %s (owner %s)\n", f.ID, f.Owner)
		}
	}
	for _, u := range r.Unknowns {
		if u.Code != convergence.UnknownOwnerless {
			out += fmt.Sprintf("  UNKNOWN %s %s\n", u.Code, u.Ref)
		}
	}
	out += fmt.Sprintf("  fixed point: stable=%v passes=%d\n", fp.Stable, len(fp.Passes))
	return out
}

func counts[K ~string](m map[K]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, string(k))
	}
	sort.Strings(keys)
	out := ""
	for i, k := range keys {
		if i > 0 {
			out += " "
		}
		out += fmt.Sprintf("%s=%d", k, m[K(k)])
	}
	return out
}
