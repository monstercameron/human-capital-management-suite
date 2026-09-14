// Command pilotprovider reports on SELECT-002's signed pilot provider
// topology (definitions/planning/gates/select-002-provider-topology.yaml):
// it loads the topology, validates its structure, verifies its signature,
// checks whether it could ever satisfy a real provider-selection gate, and
// prints a summary. It exits non-zero on any structural violation or
// signature failure, so it can gate a CI step exactly like
// `go run ./tools/planning/cmd/pilotjurisdiction` does for SELECT-001's
// profile and `go run ./tools/planning/cmd/scopeceiling` does for
// PHASE-001's manifest.
//
// The checked-in topology validates clean (it is a structurally complete,
// honestly marked placeholder) but never satisfies the real-selection gate,
// so a clean exit here is expected today even though no real vendor has
// been selected: see tools/planning/pilotprovider's doc.go for why.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotprovider"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "pilotprovider:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("pilotprovider", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("topology", "definitions/planning/gates/select-002-provider-topology.yaml", "path to the provider topology")
	if err := flags.Parse(args); err != nil {
		return err
	}

	tp, err := pilotprovider.LoadTopology(*path)
	if err != nil {
		return fmt.Errorf("loading %s: %w", *path, err)
	}

	violations := tp.Validate()
	for _, v := range violations {
		fmt.Fprintln(stderr, "VIOLATION:", v.String())
	}

	verified, verr := pilotprovider.VerifyTopologySignature(*tp)
	if verr != nil {
		fmt.Fprintln(stderr, "SIGNATURE ERROR:", verr)
	} else if !verified {
		fmt.Fprintln(stderr, "SIGNATURE INVALID")
	}

	digest, derr := tp.CanonicalDigest()
	if derr != nil {
		return fmt.Errorf("computing canonical digest: %w", derr)
	}

	satisfiesRealGate, gateViolations := tp.SatisfiesRealProviderSelectionGate()
	for _, v := range gateViolations {
		fmt.Fprintln(stderr, "REAL SELECTION GATE:", v.String())
	}

	fmt.Fprintf(stdout, "SELECT-002 provider topology %s (signed %s)\n", *path, tp.SignedDate)
	fmt.Fprintf(stdout, "  provider: %s (%s, %s, %s)\n", tp.Provider.VendorID, tp.Provider.Product, tp.Provider.Edition, tp.Provider.Region)
	fmt.Fprintf(stdout, "  selection_status: %s\n", tp.SelectionStatus)
	fmt.Fprintf(stdout, "  canonical_digest: %s\n", digest)
	fmt.Fprintf(stdout, "  signature verified: %v\n", verified)
	fmt.Fprintf(stdout, "  satisfies real provider selection gate: %v\n", satisfiesRealGate)
	fmt.Fprintf(stdout, "  api_entitlements: %d, operations: %d, faults: %d, stop_reselect_thresholds: %d\n",
		len(tp.APIEntitlements), len(tp.Operations), len(tp.Faults), len(tp.StopReselectThresholds))

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
