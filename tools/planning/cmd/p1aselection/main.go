// Command p1aselection reports on NEXT-002's selection-bound release
// manifests: it loads definitions/planning/gates/p1a-manifest.yaml and
// p1b-template.yaml, verifies both under the trusted development key,
// recomputes every selection binding's live digest, and prints the
// selection-completeness verdict selectionbind.Evaluate computes from the
// bound artifacts' own gates.
//
// It exits non-zero when either document is structurally invalid, untrusted,
// tampered or stale. An honest INCOMPLETE verdict is reported but is not a
// failure unless -require-complete is set, because incompleteness is the
// true current state of the pilot selections, not a defect in the manifest.
package main

import (
	"crypto/ed25519"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/planning/gateevidence"
	"github.com/monstercameron/human-capital-management-suite/tools/planning/gateevidence/selectionbind"
	"github.com/monstercameron/human-capital-management-suite/tools/policy/provenance"
)

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr, time.Now()); err != nil {
		fmt.Fprintln(os.Stderr, "p1aselection:", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer, now time.Time) error {
	flags := flag.NewFlagSet("p1aselection", flag.ContinueOnError)
	flags.SetOutput(stderr)
	root := flags.String("root", ".", "repository root binding paths resolve against")
	keyPath := flags.String("key", "tools/planning/gateevidence/testdata/dev-signing-key.yaml", "signing-key fixture whose public key is the only trusted key")
	asJSON := flags.Bool("json", false, "print the completeness report as JSON")
	requireComplete := flags.Bool("require-complete", false, "exit non-zero unless selection completeness is COMPLETE")
	if err := flags.Parse(args); err != nil {
		return err
	}

	priv, err := provenance.LoadSigningKeyFixture(filepath.Join(*root, filepath.FromSlash(*keyPath)))
	if err != nil {
		return err
	}
	trusted := hex.EncodeToString(priv.Public().(ed25519.PublicKey))

	p1a, err := gateevidence.LoadP1AManifest(filepath.Join(*root, filepath.FromSlash(gateevidence.P1AManifestPath)))
	if err != nil {
		return err
	}
	tpl, err := gateevidence.LoadP1BTemplate(filepath.Join(*root, "definitions", "planning", "gates", "p1b-template.yaml"))
	if err != nil {
		return err
	}

	report, err := selectionbind.Evaluate(*p1a, selectionbind.Options{Root: *root, Now: now, TrustedPublicKey: trusted})
	if err != nil {
		return err
	}
	p1bReasons := selectionbind.EvaluateP1B(*tpl, *root, trusted)
	stale := selectionbind.VerifyBindings(*root, p1a.SelectionBindings)

	if *asJSON {
		out, err := selectionbind.RenderJSON(report)
		if err != nil {
			return err
		}
		if _, err := stdout.Write(out); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(stdout, "P1A manifest %s digest %s\n", gateevidence.P1AManifestPath, report.ManifestDigest)
		fmt.Fprintf(stdout, "selection completeness: %s (as of %s)\n", report.Status, report.AsOf)
		for _, b := range report.Bindings {
			fmt.Fprintf(stdout, "  binding %-14s ready=%-5v %s\n", b.TodoID, b.Ready, b.Path)
		}
		for _, s := range report.Slots {
			fmt.Fprintf(stdout, "  slot %-12s filled=%v\n", s.Slot, s.Filled)
		}
		for _, reason := range report.Reasons() {
			fmt.Fprintln(stdout, "  REASON:", reason)
		}
		fmt.Fprintf(stdout, "P1B template activatable=%v\n", tpl.CanActivate())
	}
	for _, reason := range p1bReasons {
		fmt.Fprintln(stderr, "P1B:", reason)
	}

	switch {
	case len(report.ManifestReasons) != 0:
		return fmt.Errorf("P1A manifest is invalid or untrusted (%d reason(s))", len(report.ManifestReasons))
	case len(stale) != 0:
		return fmt.Errorf("%d stale selection binding(s)", len(stale))
	case len(p1bReasons) != 0:
		return fmt.Errorf("P1B template is invalid, untrusted or stale (%d reason(s))", len(p1bReasons))
	case *requireComplete && report.Status != selectionbind.StatusComplete:
		return fmt.Errorf("selection completeness is %s", report.Status)
	}
	return nil
}
