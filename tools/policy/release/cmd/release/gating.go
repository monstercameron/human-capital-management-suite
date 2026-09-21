// Command entry points for release admission (CICD-004), canary rollout
// (CICD-005) and the release decision manifest (CICD-006).
//
// Each subcommand loads its inputs from disk, calls exactly one library
// evaluation, prints the resulting record, and exits non-zero unless the
// outcome is ADMIT, a healthy stage advance, or PROCEED, so a CI or
// deploy-time step fails the job on any other verdict.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/tools/policy/release"
)

func loadJSONFile(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if err := json.Unmarshal(data, value); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	return nil
}

// parseNow resolves the evaluation clock. An empty flag means the current
// time; otherwise the value must be RFC3339, mirroring the pinned-clock
// convention the library evaluations use for deterministic tests.
func parseNow(raw string) (time.Time, error) {
	if strings.TrimSpace(raw) == "" {
		return time.Now().UTC(), nil
	}
	now, err := time.Parse(time.RFC3339, strings.TrimSpace(raw))
	if err != nil {
		return time.Time{}, fmt.Errorf("parse -now %q: want RFC3339: %w", raw, err)
	}
	return now.UTC(), nil
}

func printRecordJSON(stdout io.Writer, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, string(data))
	return nil
}

// runAdmit loads a deployment candidate and admission policy from disk and
// refuses (exit 1) anything Admit does not admit.
func runAdmit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("release admit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	candidatePath := fs.String("candidate", "", "JSON file with the deployment candidate")
	policyPath := fs.String("policy", "", "JSON file with the admission policy")
	nowRaw := fs.String("now", "", "evaluation time as RFC3339 (default: current time)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *candidatePath == "" || *policyPath == "" {
		fmt.Fprintln(stderr, "release admit: -candidate and -policy are required")
		return 2
	}
	var candidate release.DeploymentCandidate
	if err := loadJSONFile(*candidatePath, &candidate); err != nil {
		fmt.Fprintf(stderr, "release admit: %v\n", err)
		return 2
	}
	var policy release.AdmissionPolicy
	if err := loadJSONFile(*policyPath, &policy); err != nil {
		fmt.Fprintf(stderr, "release admit: %v\n", err)
		return 2
	}
	now, err := parseNow(*nowRaw)
	if err != nil {
		fmt.Fprintf(stderr, "release admit: %v\n", err)
		return 2
	}
	decision, err := release.Admit(candidate, policy, now)
	if printErr := printRecordJSON(stdout, decision); printErr != nil {
		fmt.Fprintf(stderr, "release admit: encode decision: %v\n", printErr)
		return 1
	}
	fmt.Fprintf(stderr, "%s\n", decision.Explain())
	if err != nil {
		fmt.Fprintf(stderr, "release admit: %v\n", err)
		return 1
	}
	return 0
}

// runRollout advances one rollout stage from disk-supplied health and
// policy, refusing (exit 1) any stage whose health does not advance.
func runRollout(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("release rollout", flag.ContinueOnError)
	fs.SetOutput(stderr)
	id := fs.String("id", "", "rollout identity")
	stages := fs.String("stages", "", "comma-separated stage names in order")
	healthPath := fs.String("health", "", "JSON file with the current stage health")
	policyPath := fs.String("policy", "", "JSON file with the rollout policy")
	nowRaw := fs.String("now", "", "evaluation time as RFC3339 (default: current time)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *id == "" || *stages == "" || *healthPath == "" || *policyPath == "" {
		fmt.Fprintln(stderr, "release rollout: -id, -stages, -health and -policy are required")
		return 2
	}
	names := make([]string, 0)
	for _, name := range strings.Split(*stages, ",") {
		if trimmed := strings.TrimSpace(name); trimmed != "" {
			names = append(names, trimmed)
		}
	}
	var health release.StageHealth
	if err := loadJSONFile(*healthPath, &health); err != nil {
		fmt.Fprintf(stderr, "release rollout: %v\n", err)
		return 2
	}
	var policy release.RolloutPolicy
	if err := loadJSONFile(*policyPath, &policy); err != nil {
		fmt.Fprintf(stderr, "release rollout: %v\n", err)
		return 2
	}
	now, err := parseNow(*nowRaw)
	if err != nil {
		fmt.Fprintf(stderr, "release rollout: %v\n", err)
		return 2
	}
	rollout, err := release.NewRollout(*id, names)
	if err != nil {
		fmt.Fprintf(stderr, "release rollout: %v\n", err)
		return 2
	}
	record, err := rollout.Advance(health, policy, now)
	if printErr := printRecordJSON(stdout, record); printErr != nil {
		fmt.Fprintf(stderr, "release rollout: encode record: %v\n", printErr)
		return 1
	}
	fmt.Fprintf(stderr, "%s\n", record.Explain())
	if err != nil {
		fmt.Fprintf(stderr, "release rollout: %v\n", err)
		return 1
	}
	return 0
}

// runDecide evaluates the eleven-class release decision evidence from disk,
// signs the manifest, and refuses (exit 1) anything but PROCEED.
func runDecide(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("release decide", flag.ContinueOnError)
	fs.SetOutput(stderr)
	id := fs.String("id", "", "decision identity")
	evidencePath := fs.String("evidence", "", "JSON file with the decision evidence")
	policyPath := fs.String("policy", "", "JSON file with the decision policy")
	keyPath := fs.String("key", release.DefaultKeyPath, "signing key fixture, resolved against -root when relative")
	root := fs.String("root", ".", "repository root used to resolve a relative -key")
	nowRaw := fs.String("now", "", "evaluation time as RFC3339 (default: current time)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *id == "" || *evidencePath == "" || *policyPath == "" {
		fmt.Fprintln(stderr, "release decide: -id, -evidence and -policy are required")
		return 2
	}
	var evidence release.DecisionEvidence
	if err := loadJSONFile(*evidencePath, &evidence); err != nil {
		fmt.Fprintf(stderr, "release decide: %v\n", err)
		return 2
	}
	var policy release.DecisionPolicy
	if err := loadJSONFile(*policyPath, &policy); err != nil {
		fmt.Fprintf(stderr, "release decide: %v\n", err)
		return 2
	}
	now, err := parseNow(*nowRaw)
	if err != nil {
		fmt.Fprintf(stderr, "release decide: %v\n", err)
		return 2
	}
	key := *keyPath
	if !filepath.IsAbs(key) {
		key = filepath.Join(*root, key)
	}
	source, err := release.NewFixtureKeySource(key)
	if err != nil {
		fmt.Fprintf(stderr, "release decide: load key source: %v\n", err)
		return 2
	}
	manifest, err := release.BuildDecisionManifest(*id, evidence, policy, now, source)
	if printErr := printRecordJSON(stdout, manifest); printErr != nil {
		fmt.Fprintf(stderr, "release decide: encode manifest: %v\n", printErr)
		return 1
	}
	fmt.Fprintf(stderr, "%s\n", manifest.Explain())
	if err != nil {
		fmt.Fprintf(stderr, "release decide: %v\n", err)
		return 1
	}
	if manifest.Verdict != release.DecisionProceed {
		fmt.Fprintf(stderr, "release decide: verdict %s is not PROCEED\n", manifest.Verdict)
		return 1
	}
	return 0
}
