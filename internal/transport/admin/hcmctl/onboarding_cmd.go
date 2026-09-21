package hcmctl

import (
	"context"
	"fmt"
	"os"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"

	adminv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/admin/v1"
)

func parseOnboarding(g globalFlags, args []string) (parsedCommand, error) {
	sub := newSubFlagSet("onboarding")
	action := sub.String("action", "", "run lifecycle action: publish|preflight|extract|get|adjudicate|cutover|abort|reconcile")
	tenant := sub.String("tenant", "", "caller tenant id (must match the credential tenant)")
	run := sub.String("run", "", "onboarding run id")
	file := sub.String("file", "", "protojson request payload file for publish|adjudicate|cutover|reconcile")
	mode := sub.String("mode", "FULL", "extraction mode for extract: FULL|INCREMENTAL|DELTA")
	maxPages := sub.Int("max-pages", 0, "extraction page cap per attempt (0 means the connection bound)")
	maxRuns := sub.Int("max-runs", 0, "extraction resume cap per object (0 means the package default)")
	if err := sub.Parse(args); err != nil {
		return parsedCommand{}, err
	}
	switch *action {
	case "publish", "adjudicate", "cutover", "reconcile":
		if strings.TrimSpace(*file) == "" {
			return parsedCommand{}, fmt.Errorf("hcmctl: onboarding -action=%s requires -file with the protojson request payload", *action)
		}
		return parseOnboardingFile(g, *action, *file)
	case "preflight", "extract", "get", "abort":
		if strings.TrimSpace(*tenant) == "" || strings.TrimSpace(*run) == "" {
			return parsedCommand{}, fmt.Errorf("hcmctl: onboarding -action=%s requires -tenant and -run", *action)
		}
		return parseOnboardingFlags(g, *action, *tenant, *run, *mode, *maxPages, *maxRuns)
	default:
		return parsedCommand{}, fmt.Errorf("hcmctl: unknown onboarding action %q (want publish|preflight|extract|get|adjudicate|cutover|abort|reconcile)", *action)
	}
}

func parseOnboardingFile(g globalFlags, action, path string) (parsedCommand, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return parsedCommand{}, err
	}
	switch action {
	case "publish":
		req := &adminv1.PublishOnboardingManifestRequest{}
		if err := protojson.Unmarshal(raw, req); err != nil {
			return parsedCommand{}, err
		}
		return parsedCommand{global: g, runOnboarding: func(ctx context.Context, client adminv1.OnboardingServiceClient) (string, error) {
			resp, err := client.PublishOnboardingManifest(ctx, req)
			if err != nil {
				return "", err
			}
			var b strings.Builder
			fmt.Fprintf(&b, "run_id: %s\nmanifest_digest: %s\n", resp.GetRunId(), resp.GetManifestDigest())
			return b.String(), nil
		}}, nil
	case "adjudicate":
		req := &adminv1.ReviewOnboardingAdjudicationRequest{}
		if err := protojson.Unmarshal(raw, req); err != nil {
			return parsedCommand{}, err
		}
		return parsedCommand{global: g, runOnboarding: func(ctx context.Context, client adminv1.OnboardingServiceClient) (string, error) {
			resp, err := client.ReviewOnboardingAdjudication(ctx, req)
			if err != nil {
				return "", err
			}
			var b strings.Builder
			for _, adj := range resp.GetAdjudications() {
				fmt.Fprintf(&b, "%-8s %-16s %-10s %s\n", adj.GetObject(), adj.GetExternalId(), adj.GetOutcome(), adj.GetCanonicalId())
			}
			return b.String(), nil
		}}, nil
	case "cutover":
		req := &adminv1.ExecuteOnboardingCutoverRequest{}
		if err := protojson.Unmarshal(raw, req); err != nil {
			return parsedCommand{}, err
		}
		return parsedCommand{global: g, runOnboarding: func(ctx context.Context, client adminv1.OnboardingServiceClient) (string, error) {
			resp, err := client.ExecuteOnboardingCutover(ctx, req)
			if err != nil {
				return "", err
			}
			var b strings.Builder
			fmt.Fprintf(&b, "outcome: %s\nfailed_gate: %s\nreason: %s\nepoch_digest: %s\nauthority: %s\n",
				resp.GetOutcome(), resp.GetFailedGate(), resp.GetReason(), resp.GetEpochDigest(), resp.GetAuthority())
			return b.String(), nil
		}}, nil
	case "reconcile":
		req := &adminv1.ReportOnboardingReconciliationRequest{}
		if err := protojson.Unmarshal(raw, req); err != nil {
			return parsedCommand{}, err
		}
		return parsedCommand{global: g, runOnboarding: func(ctx context.Context, client adminv1.OnboardingServiceClient) (string, error) {
			resp, err := client.ReportOnboardingReconciliation(ctx, req)
			if err != nil {
				return "", err
			}
			var b strings.Builder
			fmt.Fprintf(&b, "digest: %s\n", resp.GetDigest())
			for name, count := range resp.GetCounts() {
				fmt.Fprintf(&b, "  %-20s %d\n", name, count)
			}
			return b.String(), nil
		}}, nil
	default:
		return parsedCommand{}, fmt.Errorf("hcmctl: unknown onboarding file action %q", action)
	}
}

func parseOnboardingFlags(g globalFlags, action, tenant, run, mode string, maxPages, maxRuns int) (parsedCommand, error) {
	switch action {
	case "preflight":
		return parsedCommand{global: g, runOnboarding: func(ctx context.Context, client adminv1.OnboardingServiceClient) (string, error) {
			resp, err := client.RunOnboardingPreflight(ctx, &adminv1.RunOnboardingPreflightRequest{TenantId: tenant, RunId: run})
			if err != nil {
				return "", err
			}
			var b strings.Builder
			fmt.Fprintf(&b, "passed: %v\nreason: %s\n", resp.GetPassed(), resp.GetReason())
			return b.String(), nil
		}}, nil
	case "extract":
		return parsedCommand{global: g, runOnboarding: func(ctx context.Context, client adminv1.OnboardingServiceClient) (string, error) {
			resp, err := client.StartOnboardingExtraction(ctx, &adminv1.StartOnboardingExtractionRequest{
				TenantId: tenant, RunId: run,
				Mode:             mode,
				MaxPagesPerRun:   int32(maxPages),
				MaxRunsPerObject: int32(maxRuns),
			})
			if err != nil {
				return "", err
			}
			var b strings.Builder
			fmt.Fprintf(&b, "run_id: %s\nstate: %s\n", resp.GetRunId(), resp.GetState())
			for _, outcome := range resp.GetOutcomes() {
				fmt.Fprintf(&b, "  %-8s %-16s pages=%d records=%d snapshot_changed=%v\n",
					outcome.GetObject(), outcome.GetStatus(), outcome.GetPages(), outcome.GetRecords(), outcome.GetSnapshotChanged())
			}
			return b.String(), nil
		}}, nil
	case "get":
		return parsedCommand{global: g, runOnboarding: func(ctx context.Context, client adminv1.OnboardingServiceClient) (string, error) {
			resp, err := client.GetOnboardingRun(ctx, &adminv1.GetOnboardingRunRequest{TenantId: tenant, RunId: run})
			if err != nil {
				return "", err
			}
			var b strings.Builder
			fmt.Fprintf(&b, "run_id: %s\nstate: %s\nmanifest_digest: %s\npreflight_passed: %v\nobjects_extracted: %d\n",
				resp.GetRunId(), resp.GetState(), resp.GetManifestDigest(), resp.GetPreflightPassed(), resp.GetObjectsExtracted())
			fmt.Fprintf(&b, "cutover_outcome: %s\ncutover_failed_gate: %s\nreconciliation_digest: %s\naborted_reason: %s\n",
				resp.GetCutoverOutcome(), resp.GetCutoverFailedGate(), resp.GetReconciliationDigest(), resp.GetAbortedReason())
			for _, adj := range resp.GetAdjudications() {
				fmt.Fprintf(&b, "  %-8s %-16s %-10s %s\n", adj.GetObject(), adj.GetExternalId(), adj.GetOutcome(), adj.GetCanonicalId())
			}
			return b.String(), nil
		}}, nil
	case "abort":
		return parsedCommand{global: g, runOnboarding: func(ctx context.Context, client adminv1.OnboardingServiceClient) (string, error) {
			resp, err := client.AbortOnboardingRun(ctx, &adminv1.AbortOnboardingRunRequest{TenantId: tenant, RunId: run})
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("state: %s\n", resp.GetState()), nil
		}}, nil
	default:
		return parsedCommand{}, fmt.Errorf("hcmctl: unknown onboarding flag action %q", action)
	}
}
