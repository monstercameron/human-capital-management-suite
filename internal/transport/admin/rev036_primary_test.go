package admin_test

import (
	"context"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	adminv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/admin/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/onboardingruns"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
)

func rev036PrimaryClient(t *testing.T) (adminv1.OnboardingServiceClient, func()) {
	t.Helper()
	conn, cleanup := startOnboardingServer(t, rev036OperatorDeps(t))
	return adminv1.NewOnboardingServiceClient(conn), cleanup
}

func rev036Publish(t *testing.T, client adminv1.OnboardingServiceClient, ctx context.Context) string {
	t.Helper()
	pb, sm := rev036ProtoManifest(t, fixtureTenant)
	pub, err := client.PublishOnboardingManifest(ctx, &adminv1.PublishOnboardingManifestRequest{
		Manifest:    pb,
		SignerKeyId: sm.SignerKeyID,
		Signature:   sm.Signature,
		Digest:      sm.Digest,
	})
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if pub.GetRunId() == "" || pub.GetManifestDigest() == "" {
		t.Fatalf("publish returned an empty run: %+v", pub)
	}
	return pub.GetRunId()
}

func rev036AdjudicationScopes() []*adminv1.OnboardingAdjudicationScope {
	return []*adminv1.OnboardingAdjudicationScope{
		{
			Object: string(connectivity.ObjectWorker),
			Pin:    &adminv1.OnboardingReferencePin{Name: "crosswalk.worker.workday->hcmnext", Version: "v1", Digest: rev036PinDigest},
			Snapshot: &adminv1.OnboardingCrosswalkSnapshot{
				Name: "crosswalk.worker.workday->hcmnext", Version: "v1", Digest: rev036PinDigest,
				Entries: []*adminv1.OnboardingCrosswalkEntry{
					{Object: string(connectivity.ObjectWorker), ExternalId: "W-001", CanonicalId: "cand-worker-1", Exact: true},
				},
			},
			ExternalIds: []string{"W-001"},
		},
	}
}

func rev036CutoverEvidence() *adminv1.OnboardingCutoverEvidence {
	frozenAt := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	return &adminv1.OnboardingCutoverEvidence{
		Tenant:                   fixtureTenant,
		Domain:                   "workforce",
		SourceAuthority:          "workday-prod",
		TargetAuthority:          "hcmnext",
		Freeze:                   &adminv1.OnboardingFreeze{Token: "freeze-1", SourceVersion: "snap-001", FrozenAt: timestamppb.New(frozenAt)},
		CurrentSourceVersion:     "snap-001",
		TargetWriters:            []string{"cutover:harborcare"},
		CutoverWriter:            "cutover:harborcare",
		PriorEpoch:               7,
		CurrentEpoch:             7,
		Delta:                    &adminv1.OnboardingDelta{BaseVersion: "snap-000", ThroughVersion: "snap-001", CommitDigest: "delta:abc123", Complete: true},
		SimulationDigest:         "sim:zero-effect-1",
		SimulationZeroEffect:     true,
		ApprovedSimulationDigest: "sim:zero-effect-1",
		ApprovedBy:               "user:approver@harborcare",
		RequestedBy:              "user:ops@harborcare",
		MaxReplicationLagSeconds: 300,
		DecidedAt:                timestamppb.New(frozenAt.Add(time.Hour)),
	}
}

func rev036ExtractToComplete(t *testing.T, client adminv1.OnboardingServiceClient, ctx context.Context, runID string) string {
	t.Helper()
	state := ""
	for calls := 0; calls < 40; calls++ {
		ext, err := client.StartOnboardingExtraction(ctx, &adminv1.StartOnboardingExtractionRequest{
			TenantId: fixtureTenant, RunId: runID,
			Mode: "FULL", MaxPagesPerRun: 1, MaxRunsPerObject: 1,
		})
		if err != nil {
			t.Fatalf("extract: %v", err)
		}
		state = ext.GetState()
		if state == onboardingruns.OnboardingRunExtracted {
			return state
		}
	}
	t.Fatalf("extraction never reached EXTRACTED, last state %q", state)
	return ""
}

func rev036Get(t *testing.T, client adminv1.OnboardingServiceClient, ctx context.Context, runID string) *adminv1.GetOnboardingRunResponse {
	t.Helper()
	view, err := client.GetOnboardingRun(ctx, &adminv1.GetOnboardingRunRequest{TenantId: fixtureTenant, RunId: runID})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	return view
}

// TestTodo_REV_036_01 is the REV-036-01 primary: an authorized operator
// drives the full onboarding lifecycle for a real tenant through the admin
// RPC alone — publish, preflight, resumable extraction, adjudication,
// cutover and reconciliation — and every step's state and evidence is
// visible through GetOnboardingRun. The test holds only the gRPC client:
// nothing reaches into the onboarding operator except through the RPC.
func TestTodo_REV_036_01(t *testing.T) {
	client, cleanup := rev036PrimaryClient(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	opCtx := withToken(ctx, fixtureOperatorToken)

	runID := rev036Publish(t, client, opCtx)
	if got := rev036Get(t, client, opCtx, runID); got.GetState() != onboardingruns.OnboardingRunPublished || got.GetManifestDigest() == "" {
		t.Fatalf("published view = %+v", got)
	}

	pre, err := client.RunOnboardingPreflight(opCtx, &adminv1.RunOnboardingPreflightRequest{TenantId: fixtureTenant, RunId: runID})
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if !pre.GetPassed() {
		t.Fatalf("preflight did not pass: %s", pre.GetReason())
	}
	if got := rev036Get(t, client, opCtx, runID); got.GetState() != onboardingruns.OnboardingRunPreflightPassed || !got.GetPreflightPassed() {
		t.Fatalf("post-preflight view = %+v", got)
	}

	if state := rev036ExtractToComplete(t, client, opCtx, runID); state != onboardingruns.OnboardingRunExtracted {
		t.Fatalf("extraction state = %q", state)
	}
	if got := rev036Get(t, client, opCtx, runID); got.GetState() != onboardingruns.OnboardingRunExtracted || got.GetObjectsExtracted() != 1 {
		t.Fatalf("post-extraction view = %+v", got)
	}

	adj, err := client.ReviewOnboardingAdjudication(opCtx, &adminv1.ReviewOnboardingAdjudicationRequest{
		TenantId: fixtureTenant, RunId: runID, Scopes: rev036AdjudicationScopes(),
	})
	if err != nil {
		t.Fatalf("adjudicate: %v", err)
	}
	if len(adj.GetAdjudications()) != 1 {
		t.Fatalf("adjudications = %+v", adj.GetAdjudications())
	}
	if got := rev036Get(t, client, opCtx, runID); got.GetState() != onboardingruns.OnboardingRunAdjudicated || len(got.GetAdjudications()) != 1 {
		t.Fatalf("post-adjudication view = %+v", got)
	}

	cut, err := client.ExecuteOnboardingCutover(opCtx, &adminv1.ExecuteOnboardingCutoverRequest{
		TenantId: fixtureTenant, RunId: runID, Evidence: rev036CutoverEvidence(),
	})
	if err != nil {
		t.Fatalf("cutover: %v", err)
	}
	if cut.GetOutcome() != "EPOCH_SIGNED" {
		t.Fatalf("cutover = %+v", cut)
	}
	if got := rev036Get(t, client, opCtx, runID); got.GetState() != onboardingruns.OnboardingRunCutoverExecuted || got.GetCutoverOutcome() != "EPOCH_SIGNED" {
		t.Fatalf("post-cutover view = %+v", got)
	}

	rep, err := client.ReportOnboardingReconciliation(opCtx, &adminv1.ReportOnboardingReconciliationRequest{
		TenantId: fixtureTenant,
		RunId:    runID,
		ImportId: "import-harborcare-01",
		Source: []*adminv1.OnboardingSourceRow{
			{RowId: "row-1", SubjectId: "W-001", EffectiveDate: "2026-09-01", Fields: map[string]string{"name": "Jane"}},
		},
		Target: []*adminv1.OnboardingTargetRecord{
			{SubjectId: "W-001", EffectiveDate: "2026-09-01", Fields: map[string]string{"name": "Jane"}, LedgerEventRefs: []string{"evt-1"}},
		},
	})
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if rep.GetDigest() == "" {
		t.Fatal("reconciliation report carries no digest")
	}
	if got := rev036Get(t, client, opCtx, runID); got.GetReconciliationDigest() != rep.GetDigest() {
		t.Fatalf("reconciliation digest not visible: %+v", got)
	}
}

// TestTodo_REV_036_01_Fault proves malformed, out-of-order and unknown-run
// calls fail with owned codes and zero effect on the stored run.
func TestTodo_REV_036_01_Fault(t *testing.T) {
	client, cleanup := rev036PrimaryClient(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	opCtx := withToken(ctx, fixtureOperatorToken)

	runID := rev036Publish(t, client, opCtx)

	_, err := client.GetOnboardingRun(opCtx, &adminv1.GetOnboardingRunRequest{TenantId: fixtureTenant, RunId: "ob-nope"})
	assertOwnedCode(t, err, envelope.CodeNotFound)
	_, err = client.RunOnboardingPreflight(opCtx, &adminv1.RunOnboardingPreflightRequest{TenantId: fixtureTenant, RunId: "ob-nope"})
	assertOwnedCode(t, err, envelope.CodeNotFound)
	_, err = client.StartOnboardingExtraction(opCtx, &adminv1.StartOnboardingExtractionRequest{TenantId: fixtureTenant, RunId: "ob-nope", Mode: "FULL"})
	assertOwnedCode(t, err, envelope.CodeNotFound)
	_, err = client.AbortOnboardingRun(opCtx, &adminv1.AbortOnboardingRunRequest{TenantId: fixtureTenant, RunId: "ob-nope", Reason: "nope"})
	assertOwnedCode(t, err, envelope.CodeNotFound)

	// Out-of-order transitions refuse and leave the run untouched.
	_, err = client.StartOnboardingExtraction(opCtx, &adminv1.StartOnboardingExtractionRequest{TenantId: fixtureTenant, RunId: runID, Mode: "FULL"})
	assertOwnedCode(t, err, envelope.CodeFailedPrecondition)
	_, err = client.ReviewOnboardingAdjudication(opCtx, &adminv1.ReviewOnboardingAdjudicationRequest{TenantId: fixtureTenant, RunId: runID, Scopes: rev036AdjudicationScopes()})
	assertOwnedCode(t, err, envelope.CodeFailedPrecondition)
	_, err = client.ExecuteOnboardingCutover(opCtx, &adminv1.ExecuteOnboardingCutoverRequest{TenantId: fixtureTenant, RunId: runID, Evidence: rev036CutoverEvidence()})
	assertOwnedCode(t, err, envelope.CodeFailedPrecondition)
	_, err = client.ReportOnboardingReconciliation(opCtx, &adminv1.ReportOnboardingReconciliationRequest{TenantId: fixtureTenant, RunId: runID, ImportId: "import-x"})
	assertOwnedCode(t, err, envelope.CodeFailedPrecondition)
	if got := rev036Get(t, client, opCtx, runID); got.GetState() != onboardingruns.OnboardingRunPublished {
		t.Fatalf("faulted run moved to %q, want PUBLISHED", got.GetState())
	}

	// A tampered manifest signature never publishes.
	pb, sm := rev036ProtoManifest(t, fixtureTenant)
	_, err = client.PublishOnboardingManifest(opCtx, &adminv1.PublishOnboardingManifestRequest{
		Manifest:    pb,
		SignerKeyId: sm.SignerKeyID,
		Signature:   "00",
		Digest:      sm.Digest,
	})
	if err == nil {
		t.Fatal("tampered manifest signature published")
	}

	// A manifest naming another tenant is denied at the transport edge.
	foreign, fsm := rev036ProtoManifest(t, "other-tenant")
	_, err = client.PublishOnboardingManifest(opCtx, &adminv1.PublishOnboardingManifestRequest{
		Manifest:    foreign,
		SignerKeyId: fsm.SignerKeyID,
		Signature:   fsm.Signature,
		Digest:      fsm.Digest,
	})
	assertOwnedCode(t, err, envelope.CodePermissionDenied)

	// The healthy run still works after every fault above.
	pre, err := client.RunOnboardingPreflight(opCtx, &adminv1.RunOnboardingPreflightRequest{TenantId: fixtureTenant, RunId: runID})
	if err != nil || !pre.GetPassed() {
		t.Fatalf("healthy run failed after faults: %+v %v", pre, err)
	}
}

// TestTodo_REV_036_01_Recovery proves abort is terminal and observable, and
// that extraction is resumable: repeated Start calls advance or safely
// re-complete without duplicating work.
func TestTodo_REV_036_01_Recovery(t *testing.T) {
	client, cleanup := rev036PrimaryClient(t)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	opCtx := withToken(ctx, fixtureOperatorToken)

	runID := rev036Publish(t, client, opCtx)
	if _, err := client.RunOnboardingPreflight(opCtx, &adminv1.RunOnboardingPreflightRequest{TenantId: fixtureTenant, RunId: runID}); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if _, err := client.StartOnboardingExtraction(opCtx, &adminv1.StartOnboardingExtractionRequest{
		TenantId: fixtureTenant, RunId: runID, Mode: "FULL", MaxPagesPerRun: 1, MaxRunsPerObject: 1,
	}); err != nil {
		t.Fatalf("first extraction page: %v", err)
	}

	if _, err := client.AbortOnboardingRun(opCtx, &adminv1.AbortOnboardingRunRequest{TenantId: fixtureTenant, RunId: runID, Reason: "source freeze slipped"}); err != nil {
		t.Fatalf("abort: %v", err)
	}
	aborted := rev036Get(t, client, opCtx, runID)
	if aborted.GetState() != onboardingruns.OnboardingRunAborted || aborted.GetAbortedReason() != "source freeze slipped" {
		t.Fatalf("aborted view = %+v", aborted)
	}
	if !aborted.GetPreflightPassed() || aborted.GetManifestDigest() == "" {
		t.Fatalf("abort erased lifecycle evidence: %+v", aborted)
	}

	// Every post-abort mutation refuses; the run stays observable.
	_, err := client.StartOnboardingExtraction(opCtx, &adminv1.StartOnboardingExtractionRequest{TenantId: fixtureTenant, RunId: runID, Mode: "FULL"})
	assertOwnedCode(t, err, envelope.CodeFailedPrecondition)
	_, err = client.ReviewOnboardingAdjudication(opCtx, &adminv1.ReviewOnboardingAdjudicationRequest{TenantId: fixtureTenant, RunId: runID, Scopes: rev036AdjudicationScopes()})
	assertOwnedCode(t, err, envelope.CodeFailedPrecondition)
	_, err = client.ExecuteOnboardingCutover(opCtx, &adminv1.ExecuteOnboardingCutoverRequest{TenantId: fixtureTenant, RunId: runID, Evidence: rev036CutoverEvidence()})
	assertOwnedCode(t, err, envelope.CodeFailedPrecondition)
	_, err = client.ReportOnboardingReconciliation(opCtx, &adminv1.ReportOnboardingReconciliationRequest{TenantId: fixtureTenant, RunId: runID, ImportId: "import-x"})
	assertOwnedCode(t, err, envelope.CodeFailedPrecondition)
	_, err = client.AbortOnboardingRun(opCtx, &adminv1.AbortOnboardingRunRequest{TenantId: fixtureTenant, RunId: runID, Reason: "again"})
	assertOwnedCode(t, err, envelope.CodeFailedPrecondition)
	if got := rev036Get(t, client, opCtx, runID); got.GetState() != onboardingruns.OnboardingRunAborted {
		t.Fatalf("aborted run moved to %q", got.GetState())
	}

	// Resumable extraction: repeated Start calls drive a fresh run to
	// EXTRACTED, and re-completing afterwards is safe and stable. A
	// fresh server isolates the run: publish is idempotent by manifest
	// digest, so reusing this server would return the aborted run.
	resumeClient, resumeCleanup := rev036PrimaryClient(t)
	defer resumeCleanup()
	resumeID := rev036Publish(t, resumeClient, opCtx)
	if _, err := resumeClient.RunOnboardingPreflight(opCtx, &adminv1.RunOnboardingPreflightRequest{TenantId: fixtureTenant, RunId: resumeID}); err != nil {
		t.Fatalf("resume preflight: %v", err)
	}
	if state := rev036ExtractToComplete(t, resumeClient, opCtx, resumeID); state != onboardingruns.OnboardingRunExtracted {
		t.Fatalf("resumed extraction state = %q", state)
	}
	before := rev036Get(t, resumeClient, opCtx, resumeID)
	again, err := resumeClient.StartOnboardingExtraction(opCtx, &adminv1.StartOnboardingExtractionRequest{
		TenantId: fixtureTenant, RunId: resumeID, Mode: "FULL", MaxPagesPerRun: 1, MaxRunsPerObject: 1,
	})
	if err != nil {
		t.Fatalf("re-complete extraction: %v", err)
	}
	if again.GetState() != onboardingruns.OnboardingRunExtracted {
		t.Fatalf("re-complete state = %q", again.GetState())
	}
	after := rev036Get(t, resumeClient, opCtx, resumeID)
	if after.GetObjectsExtracted() != before.GetObjectsExtracted() {
		t.Fatalf("re-complete changed objects %d -> %d", before.GetObjectsExtracted(), after.GetObjectsExtracted())
	}
}
