// Command threatregister reports on THREAT-001's signed Phase 1
// trust-boundary threat register
// (definitions/planning/gates/threat-001-register.yaml): it loads the
// register, validates its structure, verifies its signature, evaluates
// whether it currently blocks release, and prints a summary. It exits
// non-zero on any structural violation, signature failure or release
// block, so it can gate a CI step exactly like
// `go run ./tools/planning/cmd/pilotprovider` does for SELECT-002's
// topology.
//
// The checked-in register carries signed, edge-specific mitigations and
// reports release unblocked when no critical threat remains without a
// mitigation or current residual-risk acceptance.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/threatregister"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "threatregister:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("threatregister", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("register", "definitions/planning/gates/threat-001-register.yaml", "path to the threat register")
	if err := flags.Parse(args); err != nil {
		return err
	}

	r, err := threatregister.LoadRegister(*path)
	if err != nil {
		return fmt.Errorf("loading %s: %w", *path, err)
	}

	violations := r.Validate()
	for _, v := range violations {
		fmt.Fprintln(stderr, "VIOLATION:", v.String())
	}

	verified, verr := threatregister.VerifyRegisterSignature(*r)
	if verr != nil {
		fmt.Fprintln(stderr, "SIGNATURE ERROR:", verr)
	} else if !verified {
		fmt.Fprintln(stderr, "SIGNATURE INVALID")
	}

	digest, derr := r.CanonicalDigest()
	if derr != nil {
		return fmt.Errorf("computing canonical digest: %w", derr)
	}

	blocked, blockers := r.ReleaseDecision(time.Now().UTC())
	for _, b := range blockers {
		fmt.Fprintln(stderr, "RELEASE BLOCKER:", b.String())
	}

	sliceCount, threatCount, edgeCount := 0, 0, 0
	sliceCount = len(r.Slices)
	for _, s := range r.Slices {
		threatCount += len(s.Threats)
		edgeCount += len(s.Edges)
	}

	fmt.Fprintf(stdout, "THREAT-001 threat register %s (signed %s)\n", *path, r.SignedDate)
	fmt.Fprintf(stdout, "  slices: %d, threats: %d, edges: %d\n", sliceCount, threatCount, edgeCount)
	fmt.Fprintf(stdout, "  canonical_digest: %s\n", digest)
	fmt.Fprintf(stdout, "  signature verified: %v\n", verified)
	fmt.Fprintf(stdout, "  release blocked: %v\n", blocked)

	if len(violations) != 0 {
		return fmt.Errorf("%d structural violation(s)", len(violations))
	}
	if verr != nil {
		return verr
	}
	if !verified {
		return fmt.Errorf("signature did not verify")
	}
	if blocked {
		return fmt.Errorf("release blocked by %d unmitigated critical finding(s)", len(blockers))
	}
	return nil
}
