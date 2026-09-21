package admin_test

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/types/known/timestamppb"

	adminv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/admin/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/fakeincumbent"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/onboarding"
	"github.com/monstercameron/human-capital-management-suite/internal/operations/onboardingruns"
	"github.com/monstercameron/human-capital-management-suite/internal/transport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/admin"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/envelope"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/grpcserver"
)

const rev036PinDigest = "ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12"

type rev036EdSigner struct {
	authority string
	priv      ed25519.PrivateKey
}

func (s *rev036EdSigner) Authority() string { return s.authority }

func (s *rev036EdSigner) Sign(payload []byte) (string, error) {
	return hex.EncodeToString(ed25519.Sign(s.priv, payload)), nil
}

func (s *rev036EdSigner) Verify(payload []byte, signature string) error {
	raw, err := hex.DecodeString(signature)
	if err != nil {
		return err
	}
	if !ed25519.Verify(s.priv.Public().(ed25519.PublicKey), payload, raw) {
		return errors.New("rev036: cutover signature does not verify")
	}
	return nil
}

func startOnboardingServer(t *testing.T, deps admin.Dependencies) (*grpc.ClientConn, func()) {
	t.Helper()
	cfg := transport.Config{Verifier: fakeVerifier{}}
	srv := grpc.NewServer(grpc.ChainUnaryInterceptor(grpcserver.UnaryInterceptor(cfg)))
	admin.Register(srv, deps)
	admin.RegisterOnboarding(srv, deps)

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.Serve(lis) }()

	conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		srv.Stop()
		t.Fatalf("dial: %v", err)
	}
	return conn, func() {
		_ = conn.Close()
		srv.Stop()
		_ = lis.Close()
	}
}

func rev036OperatorDeps(t *testing.T) admin.Dependencies {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	pub := priv.Public().(ed25519.PublicKey)

	incumbent, err := fakeincumbent.New(fakeincumbent.Options{})
	if err != nil {
		t.Fatalf("new incumbent: %v", err)
	}
	registry := connectivity.NewRegistry()
	pubDef, err := registry.Publish(fakeincumbent.DefaultDefinition(), connectivity.PublicationMeta{
		PublishedBy: "user:platform@hcmnext",
		PublishedAt: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("publish definition: %v", err)
	}
	credential, err := connectivity.ParseCredentialRef("secretref://harborcare/workday/client")
	if err != nil {
		t.Fatalf("credential: %v", err)
	}
	conn, err := connectivity.NewConnection(pubDef, connectivity.ConnectionSpec{
		ConnectionID:     fakeincumbent.DefaultDescriptor().ConnectionID,
		TenantID:         fixtureTenant,
		OrgID:            "org-harborcare-us",
		SystemID:         "sys-workday-prod",
		Environment:      connectivity.EnvironmentProduction,
		Residency:        "us-east",
		ConnectorID:      pubDef.Definition.ConnectorID,
		ConnectorVersion: pubDef.Definition.Version,
		AuthMode:         connectivity.AuthOAuth2ClientCredentials,
		CredentialRef:    credential,
		Scopes:           []string{"worker.read"},
		EndpointPolicy: connectivity.EndpointPolicy{
			AllowedHosts:  []string{"api.workday.example"},
			RequireTLS:    true,
			EgressProfile: "cell-egress/us-east",
		},
		Capabilities: connectivity.ReadCapabilities(connectivity.ObjectWorker),
		Bounds:       pubDef.Definition.Bounds,
		CreatedAt:    time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("new connection: %v", err)
	}
	for i, step := range []connectivity.LifecycleState{
		connectivity.StateValidating, connectivity.StateReady, connectivity.StateActive,
	} {
		err := conn.Transition(step, connectivity.TransitionEvidence{
			Reason:      "enable",
			ActorRef:    "user:ops@harborcare",
			EvidenceRef: "evd:enable",
			OccurredAt:  time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC).Add(time.Duration(i+1) * time.Minute),
		})
		if err != nil {
			t.Fatalf("transition to %s: %v", step, err)
		}
	}
	store := observe.NewMemoryStore()
	runs := onboardingruns.NewOnboardingRuns(func(_ context.Context, _ string) (onboardingruns.OnboardingBundle, error) {
		return onboardingruns.OnboardingBundle{
			Connector:       incumbent,
			Connection:      conn,
			Observations:    store,
			Checkpoints:     store,
			PublicKey:       pub,
			FreshnessBudget: 3650 * 24 * time.Hour,
		}, nil
	})
	return admin.Dependencies{
		Onboarding:              runs,
		OnboardingCutoverSigner: &rev036EdSigner{authority: "authority:hcmnext-cutover", priv: priv},
	}
}

func rev036ProtoManifest(t *testing.T, tenant string) (*adminv1.OnboardingManifest, onboarding.SignedManifest) {
	t.Helper()
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	priv := ed25519.NewKeyFromSeed(seed)

	incumbent, err := fakeincumbent.New(fakeincumbent.Options{})
	if err != nil {
		t.Fatalf("new incumbent: %v", err)
	}
	m := onboarding.OnboardingManifest{
		ManifestID:         "manifest-harborcare-2026-09",
		TenantID:           tenant,
		OwnerRef:           "user:ops@harborcare",
		SourceAuthorityRef: incumbent.Descriptor().AuthorityRef,
		Classification:     "PII,COMPENSATION",
		Residency:          "us-east",
		RetentionPolicy:    "retention.onboarding.default",
		RollbackPolicy:     "rollback.onboarding.default",
		IdempotencyKey:     "idem-harborcare-2026-09",
		ConnectorID:        incumbent.Descriptor().ConnectorID,
		ConnectorVersion:   incumbent.Descriptor().Version,
		ConnectionID:       fakeincumbent.DefaultDescriptor().ConnectionID,
		Objects:            []connectivity.ObjectKind{connectivity.ObjectWorker},
		SchemaVersionPins: map[connectivity.ObjectKind]string{
			connectivity.ObjectWorker: fakeincumbent.SchemaWorkerV1,
		},
		ReferencePins: []onboarding.ReferencePin{
			{Name: "crosswalk.worker.workday->hcmnext", Version: "v1", Digest: rev036PinDigest},
		},
		Budget: onboarding.Budget{
			MaxPages: 1024, MaxRecords: 65536, MaxBytes: 16 << 20, MaxWallTime: time.Hour,
		},
		CreatedAt: time.Date(2026, 9, 1, 7, 0, 0, 0, time.UTC),
		CreatedBy: "user:ops@harborcare",
	}
	sm, err := onboarding.SignManifest(m, "key-harborcare-01", priv)
	if err != nil {
		t.Fatalf("sign manifest: %v", err)
	}
	pb := &adminv1.OnboardingManifest{
		ManifestId:         m.ManifestID,
		TenantId:           m.TenantID,
		OwnerRef:           m.OwnerRef,
		SourceAuthorityRef: m.SourceAuthorityRef,
		Classification:     m.Classification,
		Residency:          m.Residency,
		RetentionPolicy:    m.RetentionPolicy,
		RollbackPolicy:     m.RollbackPolicy,
		IdempotencyKey:     m.IdempotencyKey,
		ConnectorId:        m.ConnectorID,
		ConnectorVersion:   m.ConnectorVersion.String(),
		ConnectionId:       m.ConnectionID,
		Objects:            []string{string(connectivity.ObjectWorker)},
		SchemaVersionPins:  map[string]string{string(connectivity.ObjectWorker): fakeincumbent.SchemaWorkerV1},
		ReferencePins: []*adminv1.OnboardingReferencePin{
			{Name: "crosswalk.worker.workday->hcmnext", Version: "v1", Digest: rev036PinDigest},
		},
		Budget: &adminv1.OnboardingBudget{
			MaxPages: 1024, MaxRecords: 65536, MaxBytes: 16 << 20, MaxWallTimeSeconds: 3600,
		},
		CreatedAt: timestamppb.New(m.CreatedAt),
		CreatedBy: m.CreatedBy,
	}
	return pb, sm
}
func TestTodo_REV_036_01_Integration(t *testing.T) {
	conn, cleanup := startOnboardingServer(t, rev036OperatorDeps(t))
	defer cleanup()
	client := adminv1.NewOnboardingServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	opCtx := withToken(ctx, fixtureOperatorToken)

	pb, sm := rev036ProtoManifest(t, fixtureTenant)
	pub, err := client.PublishOnboardingManifest(opCtx, &adminv1.PublishOnboardingManifestRequest{
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
	runID := pub.GetRunId()

	pre, err := client.RunOnboardingPreflight(opCtx, &adminv1.RunOnboardingPreflightRequest{TenantId: fixtureTenant, RunId: runID})
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if !pre.GetPassed() {
		t.Fatalf("preflight did not pass: %s", pre.GetReason())
	}

	var state string
	for calls := 0; calls < 40; calls++ {
		ext, err := client.StartOnboardingExtraction(opCtx, &adminv1.StartOnboardingExtractionRequest{
			TenantId: fixtureTenant, RunId: runID,
			Mode: "FULL", MaxPagesPerRun: 1, MaxRunsPerObject: 1,
		})
		if err != nil {
			t.Fatalf("extract: %v", err)
		}
		state = ext.GetState()
		if state == onboardingruns.OnboardingRunExtracted {
			break
		}
	}
	if state != onboardingruns.OnboardingRunExtracted {
		t.Fatalf("extraction never reached EXTRACTED, last state %q", state)
	}

	adj, err := client.ReviewOnboardingAdjudication(opCtx, &adminv1.ReviewOnboardingAdjudicationRequest{
		TenantId: fixtureTenant,
		RunId:    runID,
		Scopes: []*adminv1.OnboardingAdjudicationScope{
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
		},
	})
	if err != nil {
		t.Fatalf("adjudicate: %v", err)
	}
	if len(adj.GetAdjudications()) != 1 || adj.GetAdjudications()[0].GetOutcome() != "EXACT" {
		t.Fatalf("unexpected adjudications: %+v", adj.GetAdjudications())
	}

	frozenAt := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	cut, err := client.ExecuteOnboardingCutover(opCtx, &adminv1.ExecuteOnboardingCutoverRequest{
		TenantId: fixtureTenant,
		RunId:    runID,
		Evidence: &adminv1.OnboardingCutoverEvidence{
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
		},
	})
	if err != nil {
		t.Fatalf("cutover: %v", err)
	}
	if cut.GetOutcome() != "EPOCH_SIGNED" || cut.GetEpochDigest() == "" {
		t.Fatalf("cutover not signed: %+v", cut)
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

	view, err := client.GetOnboardingRun(opCtx, &adminv1.GetOnboardingRunRequest{TenantId: fixtureTenant, RunId: runID})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if view.GetState() != onboardingruns.OnboardingRunCutoverExecuted {
		t.Fatalf("run state is %q, want CUTOVER_EXECUTED", view.GetState())
	}
	if !view.GetPreflightPassed() || view.GetObjectsExtracted() != 1 || len(view.GetAdjudications()) != 1 {
		t.Fatalf("run view is missing lifecycle evidence: %+v", view)
	}
	if view.GetCutoverOutcome() != "EPOCH_SIGNED" || view.GetReconciliationDigest() == "" {
		t.Fatalf("run view is missing cutover evidence: %+v", view)
	}
}

func TestTodo_REV_036_01_Security(t *testing.T) {
	conn, cleanup := startOnboardingServer(t, rev036OperatorDeps(t))
	defer cleanup()
	client := adminv1.NewOnboardingServiceClient(conn)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pb, sm := rev036ProtoManifest(t, fixtureTenant)
	ordinary := withToken(ctx, fixtureOrdinaryToken)
	_, err := client.PublishOnboardingManifest(ordinary, &adminv1.PublishOnboardingManifestRequest{
		Manifest:    pb,
		SignerKeyId: sm.SignerKeyID,
		Signature:   sm.Signature,
		Digest:      sm.Digest,
	})
	assertOwnedCode(t, err, envelope.CodePermissionDenied)

	opCtx := withToken(ctx, fixtureOperatorToken)
	_, err = client.GetOnboardingRun(opCtx, &adminv1.GetOnboardingRunRequest{TenantId: "other-tenant", RunId: "ob-nope"})
	assertOwnedCode(t, err, envelope.CodePermissionDenied)
}
