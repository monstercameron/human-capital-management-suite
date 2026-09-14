// Command pilotjurisdiction reports on SELECT-001's signed pilot
// jurisdiction profile
// (definitions/planning/gates/select-001-jurisdiction-profile.yaml): it
// loads the profile, validates its structure, verifies its signature, and
// prints a summary of its sources, scope and exclusions. It exits non-zero
// on any structural violation or signature failure, so it can gate a CI
// step exactly like `go run ./tools/planning/cmd/scopeceiling` does for
// PHASE-001's manifest.
//
// A non-zero exit on the checked-in profile is expected today: the profile
// carries exactly one violation by design, its empty reviewer.name (see
// tools/planning/pilotjurisdiction's doc.go). This command is a reporting
// and CI-gating tool for a future reviewed profile, not a claim that
// today's is complete.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotjurisdiction"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "pilotjurisdiction:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("pilotjurisdiction", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("profile", "definitions/planning/gates/select-001-jurisdiction-profile.yaml", "path to the jurisdiction profile")
	if err := flags.Parse(args); err != nil {
		return err
	}

	p, err := pilotjurisdiction.LoadProfile(*path)
	if err != nil {
		return fmt.Errorf("loading %s: %w", *path, err)
	}

	violations := p.Validate()
	for _, v := range violations {
		fmt.Fprintln(stderr, "VIOLATION:", v.String())
	}

	verified, verr := pilotjurisdiction.VerifyProfileSignature(*p)
	if verr != nil {
		fmt.Fprintln(stderr, "SIGNATURE ERROR:", verr)
	} else if !verified {
		fmt.Fprintln(stderr, "SIGNATURE INVALID")
	}

	digest, derr := p.CanonicalDigest()
	if derr != nil {
		return fmt.Errorf("computing canonical digest: %w", derr)
	}

	fmt.Fprintf(stdout, "SELECT-001 jurisdiction profile %s (signed %s)\n", *path, p.SignedDate)
	fmt.Fprintf(stdout, "  jurisdiction: %s-%s\n", p.Jurisdiction.Country, p.Jurisdiction.State)
	fmt.Fprintf(stdout, "  review_status: %s (reviewer named: %v)\n", p.ReviewStatus, p.Reviewer.Name != "")
	fmt.Fprintf(stdout, "  canonical_digest: %s\n", digest)
	fmt.Fprintf(stdout, "  signature verified: %v\n", verified)
	fmt.Fprintf(stdout, "  sources: %d, scope_items: %d, obligation_mappings: %d, exclusions: %d\n",
		len(p.Sources), len(p.ScopeItems), len(p.ObligationMappings), len(p.Exclusions))
	fmt.Fprintf(stdout, "  update_sla.review_cadence_days: %d, stop_reselect_thresholds: %d\n",
		p.UpdateSLA.ReviewCadenceDays, len(p.StopReselectThresholds))

	if len(violations) != 0 {
		return fmt.Errorf("%d structural violation(s)", len(violations))
	}
	if verr != nil {
		return verr
	}
	if !verified {
		return fmt.Errorf("signature did not verify")
	}
	return nil
}
