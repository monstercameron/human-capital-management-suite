// Command pilotblueprint reports on CUSTOMER-001's signed pilot
// implementation blueprint
// (definitions/planning/gates/customer-001-pilot-blueprint.yaml): it loads
// the blueprint, validates its structure, verifies its signature, prints a
// summary of its discovery-through-hypercare workstreams, and then
// instantiates it against today's real repository state - no design
// partner, the real checked-in SELECT-002 provider topology and SELECT-001
// jurisdiction profile - to report each workstream's readiness. It exits
// non-zero on any structural violation or signature failure, so it can gate
// a CI step exactly like `go run ./tools/planning/cmd/pilotprovider` does
// for SELECT-002's topology and `go run ./tools/planning/cmd/pilotjurisdiction`
// does for SELECT-001's profile.
//
// A BLOCKED readiness report is expected today for every workstream: no
// design partner has been selected, the checked-in provider topology is a
// structurally-marked placeholder, and the checked-in jurisdiction profile
// carries no named reviewer. This command does not treat that as a failure
// exit condition - it is this reusable blueprint template's own reporting
// tool, not a claim that a real pilot is ready. See
// tools/planning/pilotblueprint's doc.go for the full rationale.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotblueprint"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotjurisdiction"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotprovider"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "pilotblueprint:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("pilotblueprint", flag.ContinueOnError)
	flags.SetOutput(stderr)
	blueprintPath := flags.String("blueprint", "definitions/planning/gates/customer-001-pilot-blueprint.yaml", "path to the pilot implementation blueprint")
	providerPath := flags.String("provider-topology", "definitions/planning/gates/select-002-provider-topology.yaml", "path to the provider topology Instantiate checks the provider dependency against")
	jurisdictionPath := flags.String("jurisdiction-profile", "definitions/planning/gates/select-001-jurisdiction-profile.yaml", "path to the jurisdiction profile Instantiate checks the legal-review dependency against")
	if err := flags.Parse(args); err != nil {
		return err
	}

	bp, err := pilotblueprint.LoadBlueprint(*blueprintPath)
	if err != nil {
		return fmt.Errorf("loading %s: %w", *blueprintPath, err)
	}

	violations := bp.Validate()
	for _, v := range violations {
		fmt.Fprintln(stderr, "VIOLATION:", v.String())
	}

	verified, verr := pilotblueprint.VerifyBlueprintSignature(*bp)
	if verr != nil {
		fmt.Fprintln(stderr, "SIGNATURE ERROR:", verr)
	} else if !verified {
		fmt.Fprintln(stderr, "SIGNATURE INVALID")
	}

	digest, derr := bp.CanonicalDigest()
	if derr != nil {
		return fmt.Errorf("computing canonical digest: %w", derr)
	}

	fmt.Fprintf(stdout, "CUSTOMER-001 pilot blueprint %s (signed %s, template %s)\n", *blueprintPath, bp.SignedDate, bp.TemplateVersion)
	fmt.Fprintf(stdout, "  canonical_digest: %s\n", digest)
	fmt.Fprintf(stdout, "  signature verified: %v\n", verified)
	fmt.Fprintf(stdout, "  workstreams: %d\n", len(bp.Workstreams))

	provider, perr := pilotprovider.LoadTopology(*providerPath)
	jurisdiction, jerr := pilotjurisdiction.LoadProfile(*jurisdictionPath)
	if perr == nil && jerr == nil {
		report := pilotblueprint.Instantiate(*bp, pilotblueprint.CustomerFacts{}, *provider, *jurisdiction, time.Now())
		fmt.Fprintf(stdout, "\ninstantiated against today's real repository state (no design partner recorded):\n")
		fmt.Fprintf(stdout, "  overall: %s\n", report.Overall)
		for _, w := range report.Workstreams {
			fmt.Fprintf(stdout, "  %-14s %s\n", w.Kind, w.Status)
			for _, reason := range w.Reasons {
				fmt.Fprintf(stdout, "    - %s\n", reason)
			}
		}
	} else {
		fmt.Fprintln(stderr, "could not load provider topology/jurisdiction profile for instantiation:", perr, jerr)
	}

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
