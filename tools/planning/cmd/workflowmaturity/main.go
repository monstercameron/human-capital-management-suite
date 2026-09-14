// Command workflowmaturity reports the WF-DISC-012 maturity gate over the
// accepted BusinessIntent definitions: for each one it prints the status
// tools/planning/intentcoverage independently claims, the status this gate
// actually allows once workflow-design evidence (join, expanded graph,
// ownership resolution, adversarial scenarios, decisions and todo/test/
// evidence traceability) is accounted for, and every named blocker that
// caps it. It also cross-checks planning/workflows/catalog.md's hand-typed
// "**Status**: `EXISTING`" flow claims against that same generated
// evidence and prints every disagreement - see
// tools/planning/workflowmaturity's doc.go for why that document is read
// only for comparison, never as an input to the gate.
//
// It exits non-zero only on a load failure or a self-validation mismatch
// (Validate finding a report that claims more than fresh evidence
// supports); it does not exit non-zero merely because some accepted
// definition is not yet CONTRACTED - that is the gate's normal, expected
// output while WF-DISC-009's ownership register is still being filled in,
// and is reported, not hidden.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/workflowmaturity"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("workflowmaturity", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "repository root")
	allowlistPath := flags.String("allowlist", workflowmaturity.DefaultIntentCoverageAllowlist, "intentcoverage orphan allowlist to reuse")
	outPath := flags.String("out", "", "optional report JSON output path")
	if err := flags.Parse(args); err != nil {
		return 2
	}

	snap, err := workflowmaturity.LoadSnapshot(*root, *allowlistPath)
	if err != nil {
		fmt.Fprintf(stderr, "workflowmaturity: %v\n", err)
		return 2
	}
	report := workflowmaturity.Reconcile(snap)

	if mismatches := workflowmaturity.Validate(snap, report); len(mismatches) > 0 {
		for _, m := range mismatches {
			fmt.Fprintln(stderr, "SELF-VALIDATION FAILURE:", m)
		}
		return 2
	}

	for _, r := range report.Results {
		fmt.Fprintf(stdout, "%s (%s): claimed=%s allowed=%s capped=%v\n", r.Definition, r.Intent, r.ClaimedStatus, r.AllowedStatus, r.Capped)
		for _, b := range r.Blockers {
			fmt.Fprintf(stdout, "    %s: %s\n", b.Code, b.Detail)
		}
	}

	claims, err := workflowmaturity.LoadCatalogClaims(*root)
	if err != nil {
		fmt.Fprintf(stderr, "workflowmaturity: %v\n", err)
		return 2
	}
	disagreements := workflowmaturity.CrossCheckCatalog(report, claims)
	fmt.Fprintf(stdout, "\nplanning/workflows/catalog.md cross-check: %d EXISTING claim(s) not supported by generated evidence\n", len(disagreements))
	for _, d := range disagreements {
		fmt.Fprintf(stdout, "  %s (%q) claims EXISTING for %s but evidence allows at most %s\n", d.FlowID, d.Title, d.Definition, d.Evidence)
	}

	data, err := report.JSON()
	if err != nil {
		fmt.Fprintf(stderr, "workflowmaturity: %v\n", err)
		return 2
	}
	if *outPath != "" {
		outResolved := *outPath
		if !filepath.IsAbs(outResolved) {
			outResolved = filepath.Join(*root, outResolved)
		}
		if err := os.WriteFile(outResolved, data, 0o644); err != nil {
			fmt.Fprintf(stderr, "workflowmaturity: write report: %v\n", err)
			return 2
		}
	}

	fmt.Fprintf(stdout, "\nworkflowmaturity: total=%d blocked=%d digest=%s\n", report.TotalDefinitions, report.BlockedCount, report.Digest)
	return 0
}
