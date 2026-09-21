package hcmctl

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc"

	adminv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/admin/v1"
)

type fakeOnboardingClient struct {
	adminv1.OnboardingServiceClient
	published *adminv1.PublishOnboardingManifestRequest
	preflight *adminv1.RunOnboardingPreflightRequest
	extracted *adminv1.StartOnboardingExtractionRequest
	fetched   *adminv1.GetOnboardingRunRequest
	aborted   *adminv1.AbortOnboardingRunRequest
}

func (f *fakeOnboardingClient) PublishOnboardingManifest(_ context.Context, in *adminv1.PublishOnboardingManifestRequest, _ ...grpc.CallOption) (*adminv1.PublishOnboardingManifestResponse, error) {
	f.published = in
	return &adminv1.PublishOnboardingManifestResponse{RunId: "ob-abc123", ManifestDigest: "sha256:abc"}, nil
}

func (f *fakeOnboardingClient) RunOnboardingPreflight(_ context.Context, in *adminv1.RunOnboardingPreflightRequest, _ ...grpc.CallOption) (*adminv1.RunOnboardingPreflightResponse, error) {
	f.preflight = in
	return &adminv1.RunOnboardingPreflightResponse{Passed: true}, nil
}

func (f *fakeOnboardingClient) StartOnboardingExtraction(_ context.Context, in *adminv1.StartOnboardingExtractionRequest, _ ...grpc.CallOption) (*adminv1.StartOnboardingExtractionResponse, error) {
	f.extracted = in
	return &adminv1.StartOnboardingExtractionResponse{
		RunId: in.GetRunId(), State: "EXTRACTED",
		Outcomes: []*adminv1.OnboardingObjectOutcome{{Object: "WORKER", Status: "COMPLETED", Pages: 2, Records: 4}},
	}, nil
}

func (f *fakeOnboardingClient) GetOnboardingRun(_ context.Context, in *adminv1.GetOnboardingRunRequest, _ ...grpc.CallOption) (*adminv1.GetOnboardingRunResponse, error) {
	f.fetched = in
	return &adminv1.GetOnboardingRunResponse{RunId: in.GetRunId(), State: "EXTRACTED", ManifestDigest: "sha256:abc", PreflightPassed: true, ObjectsExtracted: 1}, nil
}

func (f *fakeOnboardingClient) AbortOnboardingRun(_ context.Context, in *adminv1.AbortOnboardingRunRequest, _ ...grpc.CallOption) (*adminv1.AbortOnboardingRunResponse, error) {
	f.aborted = in
	return &adminv1.AbortOnboardingRunResponse{State: "ABORTED"}, nil
}

func TestTodo_REV_036_01_Hcmctl(t *testing.T) {
	fake := &fakeOnboardingClient{}
	ctx := context.Background()

	cmd, err := parseArgs([]string{"onboarding", "-action", "get", "-tenant", "harborcare-demo", "-run", "ob-abc123"})
	if err != nil {
		t.Fatalf("parse get: %v", err)
	}
	if cmd.runOnboarding == nil {
		t.Fatal("onboarding get did not select the onboarding client path")
	}
	out, err := cmd.runOnboarding(ctx, fake)
	if err != nil {
		t.Fatalf("run get: %v", err)
	}
	if !strings.Contains(out, "ob-abc123") || !strings.Contains(out, "EXTRACTED") {
		t.Fatalf("get output missing run evidence: %q", out)
	}
	if fake.fetched.GetTenantId() != "harborcare-demo" || fake.fetched.GetRunId() != "ob-abc123" {
		t.Fatalf("get request = %+v", fake.fetched)
	}

	cmd, err = parseArgs([]string{"onboarding", "-action", "extract", "-tenant", "harborcare-demo", "-run", "ob-abc123", "-mode", "FULL", "-max-pages", "1"})
	if err != nil {
		t.Fatalf("parse extract: %v", err)
	}
	out, err = cmd.runOnboarding(ctx, fake)
	if err != nil {
		t.Fatalf("run extract: %v", err)
	}
	if !strings.Contains(out, "COMPLETED") || !strings.Contains(out, "pages=2") {
		t.Fatalf("extract output missing outcome evidence: %q", out)
	}
	if fake.extracted.GetMode() != "FULL" || fake.extracted.GetMaxPagesPerRun() != 1 {
		t.Fatalf("extract request = %+v", fake.extracted)
	}

	cmd, err = parseArgs([]string{"onboarding", "-action", "abort", "-tenant", "harborcare-demo", "-run", "ob-abc123"})
	if err != nil {
		t.Fatalf("parse abort: %v", err)
	}
	out, err = cmd.runOnboarding(ctx, fake)
	if err != nil {
		t.Fatalf("run abort: %v", err)
	}
	if !strings.Contains(out, "ABORTED") {
		t.Fatalf("abort output missing state: %q", out)
	}

	if _, err := parseArgs([]string{"onboarding", "-action", "bogus", "-tenant", "t", "-run", "r"}); err == nil {
		t.Fatal("unknown onboarding action parsed without an error")
	}
	if _, err := parseArgs([]string{"onboarding", "-action", "get", "-tenant", "t"}); err == nil {
		t.Fatal("onboarding get without -run parsed without an error")
	}
	if _, err := parseArgs([]string{"onboarding", "-action", "publish"}); err == nil {
		t.Fatal("onboarding publish without -file parsed without an error")
	}
	if _, err := parseArgs([]string{"other-command"}); err == nil {
		t.Fatal("unknown subcommand parsed without an error")
	}
}
