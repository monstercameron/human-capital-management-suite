// Command release builds and verifies an offline immutable release bundle.
//
// Examples:
//
//	go run ./tools/policy/release/cmd/release bundle -root . -out out/release \
//	  -version-file VERSION -binary hcmnext=bin/hcmnext \
//	  -policy driftgate=out/driftgate.json -policy apigate=out/apigate.json \
//	  -policy substratecoverage=out/substratecoverage.json \
//	  -policy cleancheckout=out/cleancheckout.json
//	go run ./tools/policy/release/cmd/release verify -bundle out/release
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/release"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "release: command is required: bundle, verify, admit, rollout or decide")
		return 2
	}
	switch args[0] {
	case "bundle", "build":
		return runBundle(args[1:], stdout, stderr)
	case "verify":
		return runVerify(args[1:], stdout, stderr)
	case "admit":
		return runAdmit(args[1:], stdout, stderr)
	case "rollout":
		return runRollout(args[1:], stdout, stderr)
	case "decide":
		return runDecide(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "release: unknown command %q (want bundle, verify, admit, rollout or decide)\n", args[0])
		return 2
	}
}

func runBundle(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("release bundle", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "path to the Go module root")
	out := fs.String("out", "", "output directory for the immutable bundle")
	version := fs.String("version", "", "release version; overrides -version-file")
	versionFile := fs.String("version-file", release.DefaultVersionFile, "declared version file, relative to -root")
	sbom := fs.String("sbom", "", "SBOM path, relative to -root")
	provenance := fs.String("provenance", "", "provenance statement path, relative to -root")
	p1a := fs.String("p1a", "", "P1A evidence report path, relative to -root")
	key := fs.String("key", release.DefaultKeyPath, "Ed25519 signing key fixture")
	binaries := stringListFlag{}
	policies := stringListFlag{}
	gates := stringListFlag{}
	fs.Var(&binaries, "binary", "built binary as name=path (repeatable)")
	fs.Var(&policies, "policy", "policy report as name=path (repeatable)")
	fs.Var(&gates, "gate", "product-slice gate evidence as gate=digest (repeatable, all eight gates required)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *out == "" {
		fmt.Fprintln(stderr, "release: -out is required")
		return 2
	}
	inputs, err := parseBinaries(binaries)
	if err != nil {
		fmt.Fprintf(stderr, "release: %v\n", err)
		return 2
	}
	policyReports, err := parsePolicies(policies)
	if err != nil {
		fmt.Fprintf(stderr, "release: %v\n", err)
		return 2
	}
	gateEvidence, err := parseGates(gates)
	if err != nil {
		fmt.Fprintf(stderr, "release: %v\n", err)
		return 2
	}
	manifest, err := release.Build(*root, release.Options{
		Out:                 *out,
		Version:             *version,
		VersionFile:         *versionFile,
		Binaries:            inputs,
		SBOMPath:            *sbom,
		ProvenancePath:      *provenance,
		P1AEvidencePath:     *p1a,
		PolicyReports:       policyReports,
		ProductGateEvidence: gateEvidence,
		KeyPath:             *key,
	})
	if err != nil {
		fmt.Fprintf(stderr, "release: %v\n", err)
		return 1
	}
	digest, err := manifest.CanonicalDigest()
	if err != nil {
		fmt.Fprintf(stderr, "release: compute manifest digest: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "release: wrote %s (manifest sha256:%s)\n", *out, digest)
	return 0
}

func runVerify(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("release verify", flag.ContinueOnError)
	fs.SetOutput(stderr)
	bundle := fs.String("bundle", "", "bundle directory to verify")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *bundle == "" {
		fmt.Fprintln(stderr, "release verify: -bundle is required")
		return 2
	}
	receipt, err := release.VerifyBundle(*bundle, release.VerifyOptions{})
	if err != nil {
		fmt.Fprintf(stderr, "release verify: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "%s\n", receipt.Explain())
	return 0
}

type stringListFlag []string

func (f *stringListFlag) String() string { return strings.Join(*f, ",") }
func (f *stringListFlag) Set(value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("repeated flag value cannot be empty")
	}
	*f = append(*f, value)
	return nil
}

func parseBinaries(values []string) ([]release.BinaryInput, error) {
	result := make([]release.BinaryInput, 0, len(values))
	for _, value := range values {
		name, path, ok := strings.Cut(value, "=")
		if !ok || strings.TrimSpace(name) == "" || strings.TrimSpace(path) == "" {
			return nil, fmt.Errorf("binary %q must use name=path", value)
		}
		result = append(result, release.BinaryInput{Name: strings.TrimSpace(name), Path: strings.TrimSpace(path)})
	}
	return result, nil
}

func parsePolicies(values []string) (map[string]string, error) {
	result := make(map[string]string, len(values))
	for _, value := range values {
		name, path, ok := strings.Cut(value, "=")
		name = strings.TrimSpace(name)
		path = strings.TrimSpace(path)
		if !ok || name == "" || path == "" {
			return nil, fmt.Errorf("policy %q must use name=path", value)
		}
		name = strings.TrimSuffix(name, ".json")
		if result[name] != "" {
			return nil, fmt.Errorf("duplicate policy report %q", name)
		}
		result[name] = path
	}
	return result, nil
}

func parseGates(values []string) (map[string]string, error) {
	result := make(map[string]string, len(values))
	for _, value := range values {
		name, digest, ok := strings.Cut(value, "=")
		name = strings.TrimSpace(name)
		digest = strings.TrimSpace(digest)
		if !ok || name == "" || digest == "" {
			return nil, fmt.Errorf("gate %q must use gate=digest", value)
		}
		if _, dup := result[name]; dup {
			return nil, fmt.Errorf("duplicate product-gate evidence %q", name)
		}
		result[name] = digest
	}
	return result, nil
}
