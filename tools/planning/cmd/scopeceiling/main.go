// Command scopeceiling reports on PHASE-001's signed Phase 1 scope-ceiling
// manifest (definitions/planning/gates/phase1-scope-ceiling.yaml): it loads
// the manifest, validates its structure, verifies its signature, and prints
// a category/disposition summary plus the four selection slots' status.
// It exits non-zero on any structural violation or signature failure, so it
// can gate a CI step exactly like `go run ./tools/quality` does for the
// other planning gates.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/scopeceiling"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "scopeceiling:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("scopeceiling", flag.ContinueOnError)
	flags.SetOutput(stderr)
	path := flags.String("manifest", "definitions/planning/gates/phase1-scope-ceiling.yaml", "path to the scope-ceiling manifest")
	if err := flags.Parse(args); err != nil {
		return err
	}

	m, err := scopeceiling.LoadManifest(*path)
	if err != nil {
		return fmt.Errorf("loading %s: %w", *path, err)
	}

	violations := m.Validate()
	for _, v := range violations {
		fmt.Fprintln(stderr, "VIOLATION:", v.String())
	}

	verified, verr := scopeceiling.VerifyManifestSignature(*m)
	if verr != nil {
		fmt.Fprintln(stderr, "SIGNATURE ERROR:", verr)
	} else if !verified {
		fmt.Fprintln(stderr, "SIGNATURE INVALID")
	}

	digest, derr := m.CanonicalDigest()
	if derr != nil {
		return fmt.Errorf("computing canonical digest: %w", derr)
	}

	fmt.Fprintf(stdout, "PHASE-001 scope ceiling %s (signed %s)\n", *path, m.SignedDate)
	fmt.Fprintf(stdout, "  canonical_digest: %s\n", digest)
	fmt.Fprintf(stdout, "  signature verified: %v\n", verified)
	fmt.Fprintf(stdout, "  intents: %d, capabilities: %d, workflows: %d, user_flows: %d\n",
		len(m.Intents), len(m.Capabilities), len(m.Workflows), len(m.UserFlows))
	fmt.Fprintf(stdout, "  endpoints: %d, models: %d, effects: %d, deferred_domains: %d\n",
		len(m.Endpoints), len(m.Models), len(m.Effects), len(m.DeferredDomains))
	fmt.Fprintln(stdout, "  selection slots:")
	for _, s := range m.SelectionSlots {
		fmt.Fprintf(stdout, "    - %s: filled=%v (fills via %s)\n", s.Name, s.Filled, s.FillingTodoID)
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
