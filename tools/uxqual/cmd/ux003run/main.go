// Command ux003run computes the UX-003 release-gate result and appends the
// observed run to the governed journal. Routine go test and report generation
// do not write run records.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/forms"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/gwc"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/render/ssr"
	"github.com/monstercameron/human-capital-management-suite/tools/uxqual/wcag"
)

func main() {
	if err := run(os.Args[1:], os.Stdout); err != nil {
		fatalf("UX-003 run: %v", err)
	}
}

func run(args []string, output io.Writer) error {
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "..", "..", "..", ".."))
	flags := flag.NewFlagSet("ux003run", flag.ContinueOnError)
	flags.SetOutput(output)
	evidencePath := flags.String("evidence", filepath.Join(root, "definitions", "ux", "wcag", "ux-003-evidence.yaml"), "UX-003 evidence YAML")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %v", flags.Args())
	}
	if *evidencePath != "" { // LoadEvidence resolves the governed default; reject alternate evidence inputs.
		abs, _ := filepath.Abs(*evidencePath)
		defaultAbs, _ := filepath.Abs(filepath.Join(root, "definitions", "ux", "wcag", "ux-003-evidence.yaml"))
		if abs != defaultAbs {
			return fmt.Errorf("UX-003 run recording is bound to governed evidence %s", defaultAbs)
		}
	}
	record, failures, err := recordUX003Run()
	if err != nil {
		return err
	}
	fmt.Fprintf(output, "recorded %s sequence=%d gate_passed=%t digest=%s\n", record.ID, record.Sequence, record.GatePassed, record.Digest)
	if len(failures) != 0 {
		return fmt.Errorf("UX-003 gate failed: %s", strings.Join(failures, "; "))
	}
	return nil
}

func recordUX003Run() (wcag.RunRecord, []string, error) {
	fixture := forms.FixtureWithValidationError()
	ssrDoc, err := ssr.Render(fixture)
	if err != nil {
		return wcag.RunRecord{}, nil, fmt.Errorf("render SSR fixture: %w", err)
	}
	gwcDoc, err := gwc.Document(fixture)
	if err != nil {
		return wcag.RunRecord{}, nil, fmt.Errorf("render GWC fixture: %w", err)
	}
	ssrResults, gwcResults := wcag.Score(ssrDoc), wcag.Score(gwcDoc)
	evidence, err := wcag.LoadEvidence()
	if err != nil {
		return wcag.RunRecord{}, nil, fmt.Errorf("load UX-003 evidence: %w", err)
	}

	var failures []string
	for _, r := range ssrResults {
		if !r.Pass {
			failures = append(failures, "SSR "+r.Name+": "+r.Detail)
		}
	}
	if err := evidence.Validate(); err != nil {
		failures = append(failures, "evidence: "+err.Error())
	}
	if err := evidence.ReleaseReady(); err == nil {
		failures = append(failures, "pending manual scenarios unexpectedly passed release readiness")
	}
	broken := strings.Replace(ssrDoc, `aria-describedby="proposedCompensation-error"`, `aria-describedby="missing-error"`, 1)
	if forms.CheckErrorAssociation(broken, forms.ErroredFieldIDs).Pass {
		failures = append(failures, "error association mutation was not detected")
	}
	if wcag.CheckAccessibleAuth(strings.Replace(ssrDoc, "force_execute", "force_execute", 1) + "force_execute").Pass {
		failures = append(failures, "accessible-auth mutation was not detected")
	}
	passed := len(failures) == 0
	record, err := wcag.RecordUX003Run(ssrResults, gwcResults, evidence, passed)
	if err != nil {
		return wcag.RunRecord{}, nil, fmt.Errorf("record UX-003 run: %w", err)
	}
	return record, failures, nil
}

func fatalf(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...); os.Exit(1) }
