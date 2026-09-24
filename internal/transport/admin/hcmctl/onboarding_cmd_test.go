package hcmctl

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"

	adminv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/admin/v1"
)

type fakeOnboardingClient struct {
	adminv1.OnboardingServiceClient
	published   *adminv1.PublishOnboardingManifestRequest
	preflight   *adminv1.RunOnboardingPreflightRequest
	extracted   *adminv1.StartOnboardingExtractionRequest
	fetched     *adminv1.GetOnboardingRunRequest
	adjudicated *adminv1.ReviewOnboardingAdjudicationRequest
	cutover     *adminv1.ExecuteOnboardingCutoverRequest
	reconciled  *adminv1.ReportOnboardingReconciliationRequest
	aborted     *adminv1.AbortOnboardingRunRequest
}

func (f *fakeOnboardingClient) PublishOnboardingManifest(_ context.Context, in *adminv1.PublishOnboardingManifestRequest, _ ...grpc.CallOption) (*adminv1.PublishOnboardingManifestResponse, error) {
	f.published = in
	return &adminv1.PublishOnboardingManifestResponse{RunId: "ob-abc123", ManifestDigest: "sha256:abc"}, nil
}

func (f *fakeOnboardingClient) RunOnboardingPreflight(_ context.Context, in *adminv1.RunOnboardingPreflightRequest, _ ...grpc.CallOption) (*adminv1.RunOnboardingPreflightResponse, error) {
	f.preflight = in
	return &adminv1.RunOnboardingPreflightResponse{Passed: true}, nil
}

func (f *fakeOnboardingClient) ReviewOnboardingAdjudication(_ context.Context, in *adminv1.ReviewOnboardingAdjudicationRequest, _ ...grpc.CallOption) (*adminv1.ReviewOnboardingAdjudicationResponse, error) {
	f.adjudicated = in
	return &adminv1.ReviewOnboardingAdjudicationResponse{Adjudications: []*adminv1.OnboardingAdjudicationView{{Object: "WORKER", ExternalId: "W-001", Outcome: "EXACT", CanonicalId: "worker-001"}}}, nil
}

func (f *fakeOnboardingClient) ExecuteOnboardingCutover(_ context.Context, in *adminv1.ExecuteOnboardingCutoverRequest, _ ...grpc.CallOption) (*adminv1.ExecuteOnboardingCutoverResponse, error) {
	f.cutover = in
	return &adminv1.ExecuteOnboardingCutoverResponse{Outcome: "EPOCH_SIGNED", EpochDigest: "sha256:epoch", Authority: "hcmnext"}, nil
}

func (f *fakeOnboardingClient) ReportOnboardingReconciliation(_ context.Context, in *adminv1.ReportOnboardingReconciliationRequest, _ ...grpc.CallOption) (*adminv1.ReportOnboardingReconciliationResponse, error) {
	f.reconciled = in
	return &adminv1.ReportOnboardingReconciliationResponse{Digest: "sha256:reconcile", Counts: map[string]int32{"matched": 1}}, nil
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

func onboardingRequestFile(t *testing.T, req proto.Message) string {
	t.Helper()
	raw, err := protojson.Marshal(req)
	if err != nil {
		t.Fatalf("marshal onboarding request: %v", err)
	}
	path := filepath.Join(t.TempDir(), "request.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write onboarding request: %v", err)
	}
	return path
}

func TestTodo_REV_036_01_Hcmctl(t *testing.T) {
	fake := &fakeOnboardingClient{}
	ctx := context.Background()

	manifestFile := onboardingRequestFile(t, &adminv1.PublishOnboardingManifestRequest{
		Manifest:    &adminv1.OnboardingManifest{TenantId: "harborcare-demo", ManifestId: "manifest-1"},
		SignerKeyId: "key-1", Signature: "signature", Digest: "sha256:manifest",
	})
	cmd, err := parseArgs([]string{"onboarding", "-action", "publish", "-file", manifestFile})
	if err != nil {
		t.Fatalf("parse publish: %v", err)
	}
	out, err := cmd.runOnboarding(ctx, fake)
	if err != nil {
		t.Fatalf("run publish: %v", err)
	}
	if !strings.Contains(out, "ob-abc123") || !strings.Contains(out, "sha256:abc") || fake.published.GetManifest().GetTenantId() != "harborcare-demo" {
		t.Fatalf("publish output/request missing evidence: %q %+v", out, fake.published)
	}

	cmd, err = parseArgs([]string{"onboarding", "-action", "preflight", "-tenant", "harborcare-demo", "-run", "ob-abc123"})
	if err != nil {
		t.Fatalf("parse preflight: %v", err)
	}
	out, err = cmd.runOnboarding(ctx, fake)
	if err != nil {
		t.Fatalf("run preflight: %v", err)
	}
	if !strings.Contains(out, "passed: true") || fake.preflight.GetRunId() != "ob-abc123" {
		t.Fatalf("preflight output/request missing evidence: %q %+v", out, fake.preflight)
	}

	cmd, err = parseArgs([]string{"onboarding", "-action", "get", "-tenant", "harborcare-demo", "-run", "ob-abc123"})
	if err != nil {
		t.Fatalf("parse get: %v", err)
	}
	if cmd.runOnboarding == nil {
		t.Fatal("onboarding get did not select the onboarding client path")
	}
	out, err = cmd.runOnboarding(ctx, fake)
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

	adjudicateFile := onboardingRequestFile(t, &adminv1.ReviewOnboardingAdjudicationRequest{
		TenantId: "harborcare-demo", RunId: "ob-abc123",
	})
	cmd, err = parseArgs([]string{"onboarding", "-action", "adjudicate", "-file", adjudicateFile})
	if err != nil {
		t.Fatalf("parse adjudicate: %v", err)
	}
	out, err = cmd.runOnboarding(ctx, fake)
	if err != nil {
		t.Fatalf("run adjudicate: %v", err)
	}
	if !strings.Contains(out, "EXACT") || !strings.Contains(out, "worker-001") || fake.adjudicated.GetRunId() != "ob-abc123" {
		t.Fatalf("adjudication output/request missing evidence: %q %+v", out, fake.adjudicated)
	}

	cutoverFile := onboardingRequestFile(t, &adminv1.ExecuteOnboardingCutoverRequest{
		TenantId: "harborcare-demo", RunId: "ob-abc123",
	})
	cmd, err = parseArgs([]string{"onboarding", "-action", "cutover", "-file", cutoverFile})
	if err != nil {
		t.Fatalf("parse cutover: %v", err)
	}
	out, err = cmd.runOnboarding(ctx, fake)
	if err != nil {
		t.Fatalf("run cutover: %v", err)
	}
	if !strings.Contains(out, "EPOCH_SIGNED") || !strings.Contains(out, "sha256:epoch") || fake.cutover.GetTenantId() != "harborcare-demo" {
		t.Fatalf("cutover output/request missing evidence: %q %+v", out, fake.cutover)
	}

	reconcileFile := onboardingRequestFile(t, &adminv1.ReportOnboardingReconciliationRequest{
		TenantId: "harborcare-demo", RunId: "ob-abc123", ImportId: "import-1",
	})
	cmd, err = parseArgs([]string{"onboarding", "-action", "reconcile", "-file", reconcileFile})
	if err != nil {
		t.Fatalf("parse reconcile: %v", err)
	}
	out, err = cmd.runOnboarding(ctx, fake)
	if err != nil {
		t.Fatalf("run reconcile: %v", err)
	}
	if !strings.Contains(out, "sha256:reconcile") || !strings.Contains(out, "matched") || fake.reconciled.GetImportId() != "import-1" {
		t.Fatalf("reconciliation output/request missing evidence: %q %+v", out, fake.reconciled)
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
