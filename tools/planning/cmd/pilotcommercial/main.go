// Command pilotcommercial reports on COMMERCIAL-001's signed pilot
// commercial package
// (definitions/planning/gates/commercial-001-pilot-package.yaml): it loads
// the freeze, validates its structure, verifies its signature, cross-checks
// it against the live PHASE-001 ceiling, SELECT-001 jurisdiction profile,
// SELECT-002 provider topology and internal/commercial registry, and prints
// a summary. It exits non-zero on any structural violation, signature
// failure or registry mismatch, so it can gate a CI step exactly like
// `go run ./tools/planning/cmd/pilotprovider` does for SELECT-002's
// topology.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/pilotcommercial"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "pilotcommercial:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("pilotcommercial", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("freeze", "definitions/planning/gates/commercial-001-pilot-package.yaml", "path to the commercial freeze")
	ceilingPath := flags.String("ceiling", "definitions/planning/gates/phase1-scope-ceiling.yaml", "path to the Phase 1 scope ceiling")
	jurisdictionPath := flags.String("jurisdiction", "definitions/planning/gates/select-001-jurisdiction-profile.yaml", "path to the SELECT-001 jurisdiction profile")
	providerPath := flags.String("provider", "definitions/planning/gates/select-002-provider-topology.yaml", "path to the SELECT-002 provider topology")
	if err := flags.Parse(args); err != nil {
		return err
	}

	f, err := pilotcommercial.LoadFreeze(*path)
	if err != nil {
		return fmt.Errorf("loading %s: %w", *path, err)
	}

	violations := f.Validate()
	for _, v := range violations {
		fmt.Fprintln(stderr, "VIOLATION:", v.String())
	}

	verified, verr := pilotcommercial.VerifyFreezeSignature(*f)
	if verr != nil {
		fmt.Fprintln(stderr, "SIGNATURE ERROR:", verr)
	} else if !verified {
		fmt.Fprintln(stderr, "SIGNATURE INVALID")
	}

	digest, derr := f.CanonicalDigest()
	if derr != nil {
		return fmt.Errorf("computing canonical digest: %w", derr)
	}

	mismatches, mErr := pilotcommercial.ConformsToLiveRegistries(*f, *ceilingPath, *jurisdictionPath, *providerPath)
	if mErr != nil {
		fmt.Fprintln(stderr, "REGISTRY LOAD ERROR:", mErr)
	}
	for _, m := range mismatches {
		fmt.Fprintln(stderr, "REGISTRY MISMATCH:", m.String())
	}

	fmt.Fprintf(stdout, "COMMERCIAL-001 pilot commercial package %s (signed %s)\n", *path, f.SignedDate)
	fmt.Fprintf(stdout, "  jurisdiction: %s (status=%s, review_status=%s)\n", f.Jurisdiction.State, f.Jurisdiction.Status, f.Jurisdiction.ReviewStatus)
	fmt.Fprintf(stdout, "  provider: status=%s\n", f.Provider.Status)
	fmt.Fprintf(stdout, "  slo: status=%s\n", f.Evidence.SLOStatus)
	fmt.Fprintf(stdout, "  canonical_digest: %s\n", digest)
	fmt.Fprintf(stdout, "  signature verified: %v\n", verified)
	fmt.Fprintf(stdout, "  conforms to live registries: %v\n", mErr == nil && len(mismatches) == 0)
	fmt.Fprintf(stdout, "  entitlements: %d, exclusions: %d\n", len(f.Intent.Entitlements), len(f.Intent.Exclusions))

	if len(violations) != 0 {
		return fmt.Errorf("%d structural violation(s)", len(violations))
	}
	if verr != nil {
		return verr
	}
	if !verified {
		return fmt.Errorf("signature did not verify")
	}
	if mErr != nil {
		return mErr
	}
	if len(mismatches) != 0 {
		return fmt.Errorf("%d registry mismatch(es)", len(mismatches))
	}
	return nil
}
