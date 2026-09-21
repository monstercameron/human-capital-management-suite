package onboardingruns

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/fakeincumbent"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/observe"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity/onboarding"
)

// rev036Tenant is the tenant every REV-036-01 fixture observes on behalf of.
const rev036Tenant = "5e3f1c2b-0000-4000-8000-000000000001"

const rev036PinDigest = "ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12ab12"

type rev036Harness struct {
	runs      *OnboardingRuns
	priv      ed25519.PrivateKey
	incumbent *fakeincumbent.Incumbent
	conn      *connectivity.ConnectorConnection
}

func rev036KeyPair() (ed25519.PublicKey, ed25519.PrivateKey) {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	priv := ed25519.NewKeyFromSeed(seed)
	return priv.Public().(ed25519.PublicKey), priv
}

func newRev036Harness(t *testing.T) *rev036Harness {
	t.Helper()
	pub, priv := rev036KeyPair()
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
		TenantID:         rev036Tenant,
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
	runs := NewOnboardingRuns(func(_ context.Context, tenantID string) (OnboardingBundle, error) {
		return OnboardingBundle{
			Connector:       incumbent,
			Connection:      conn,
			Observations:    store,
			Checkpoints:     store,
			PublicKey:       pub,
			FreshnessBudget: 3650 * 24 * time.Hour,
		}, nil
	})
	return &rev036Harness{runs: runs, priv: priv, incumbent: incumbent, conn: conn}
}

func (h *rev036Harness) manifest() onboarding.OnboardingManifest {
	return onboarding.OnboardingManifest{
		ManifestID:         "manifest-harborcare-2026-09",
		TenantID:           rev036Tenant,
		OwnerRef:           "user:ops@harborcare",
		SourceAuthorityRef: h.incumbent.Descriptor().AuthorityRef,
		Classification:     "PII,COMPENSATION",
		Residency:          "us-east",
		RetentionPolicy:    "retention.onboarding.default",
		RollbackPolicy:     "rollback.onboarding.default",
		IdempotencyKey:     "idem-harborcare-2026-09",
		ConnectorID:        h.incumbent.Descriptor().ConnectorID,
		ConnectorVersion:   h.incumbent.Descriptor().Version,
		ConnectionID:       h.conn.ID(),
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
}

func (h *rev036Harness) publish(t *testing.T) string {
	t.Helper()
	sm, err := onboarding.SignManifest(h.manifest(), "key-harborcare-01", h.priv)
	if err != nil {
		t.Fatalf("sign manifest: %v", err)
	}
	runID, err := h.runs.Publish(context.Background(), rev036Tenant, sm)
	if err != nil {
		t.Fatalf("publish: %v", err)
	}
	if runID == "" {
		t.Fatal("publish returned an empty run id")
	}
	return runID
}

func rev036ExtractionDone(results []onboarding.ObjectResult) bool {
	if len(results) == 0 {
		return false
	}
	for _, r := range results {
		if r.SnapshotChanged {
			return false
		}
		if r.Run.Status != observe.RunCompleted && r.Run.Status != observe.RunAlreadyComplete {
			return false
		}
	}
	return true
}

// TestTodo_REV_036_01 pins the operator-visible onboarding lifecycle: publish
// a signed manifest, pass preflight, extract to completion, adjudicate one
// exact identity, execute cutover on clean evidence and read the
// reconciliation report, every step through the application run registry.
func TestTodo_REV_036_01(t *testing.T) {
	ctx := context.Background()
	h := newRev036Harness(t)
	runID := h.publish(t)

	pre, err := h.runs.RunPreflight(ctx, rev036Tenant, runID)
	if err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if !pre.Passed {
		t.Fatalf("preflight did not pass: %+v", pre.Reason)
	}

	results, err := h.runs.StartExtraction(ctx, rev036Tenant, runID, onboarding.ExtractRequest{
		RunID: runID, Mode: connectivity.ReadFull, MaxPagesPerRun: 1, MaxRunsPerObject: 1,
	})
	if err != nil {
		t.Fatalf("start extraction: %v", err)
	}
	for calls := 1; !rev036ExtractionDone(results); calls++ {
		if calls > 40 {
			t.Fatalf("extraction never completed: %+v", results)
		}
		results, err = h.runs.StartExtraction(ctx, rev036Tenant, runID, onboarding.ExtractRequest{
			RunID: runID, Mode: connectivity.ReadFull, MaxPagesPerRun: 1, MaxRunsPerObject: 1,
		})
		if err != nil {
			t.Fatalf("resume extraction: %v", err)
		}
	}
	if len(results) != 1 || results[0].Object != connectivity.ObjectWorker {
		t.Fatalf("unexpected extraction results: %+v", results)
	}

	adj, err := h.runs.ReviewAdjudication(ctx, rev036Tenant, runID, []OnboardingAdjudicationScope{
		{
			Object: connectivity.ObjectWorker,
			Pin:    onboarding.ReferencePin{Name: "crosswalk.worker.workday->hcmnext", Version: "v1", Digest: rev036PinDigest},
			Snapshot: onboarding.CrosswalkSnapshot{
				Name: "crosswalk.worker.workday->hcmnext", Version: "v1",
				Digest:  rev036PinDigest,
				Entries: []onboarding.CrosswalkEntry{{Object: connectivity.ObjectWorker, ExternalID: "W-001", CanonicalID: "cand-worker-1", Exact: true}},
			},
			ExternalIDs: []string{"W-001"},
		},
	})
	if err != nil {
		t.Fatalf("adjudicate: %v", err)
	}
	if len(adj) != 1 || adj[0].Outcome != onboarding.IdentityExact || adj[0].CanonicalID != "cand-worker-1" {
		t.Fatalf("unexpected adjudications: %+v", adj)
	}

	dec, err := h.runs.ExecuteCutover(ctx, rev036Tenant, runID, rev036CleanEvidence(), rev036TestSigner(t))
	if err != nil {
		t.Fatalf("execute cutover: %v", err)
	}
	if dec.Outcome != onboarding.CutoverEpochSigned || dec.Epoch == nil {
		t.Fatalf("cutover not signed: %+v", dec)
	}

	rep, err := h.runs.ReportReconciliation(ctx, rev036Tenant, runID, "import-harborcare-01",
		[]onboarding.SourceRow{{RowID: "row-1", SubjectID: "W-001", EffectiveDate: "2026-09-01", Fields: map[string]string{"name": "Jane"}}},
		[]onboarding.TargetRecord{{SubjectID: "W-001", EffectiveDate: "2026-09-01", Fields: map[string]string{"name": "Jane"}, LedgerEventRefs: []string{"evt-1"}}},
	)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if rep.Digest == "" {
		t.Fatal("reconciliation report carries no digest")
	}

	view, err := h.runs.Get(ctx, rev036Tenant, runID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if view.State != OnboardingRunCutoverExecuted {
		t.Fatalf("run state = %q, want cutover-executed", view.State)
	}
	if view.ManifestDigest == "" || view.Preflight == nil || len(view.Adjudications) != 1 || view.Cutover == nil || view.Reconciliation == nil {
		t.Fatalf("run view is missing lifecycle evidence: %+v", view)
	}

	digest, err := h.manifest().Digest()
	if err != nil {
		t.Fatalf("digest: %v", err)
	}
	const wantDigest = "sha256:2e3f47e907ae0c2c7daf0fea1dde342fa9dafb9f465a7f11ad43a31954671e6c"
	if digest != wantDigest {
		t.Fatalf("manifest digest = %q, want golden %q", digest, wantDigest)
	}
}

type rev036EdSigner struct {
	authority string
	priv      ed25519.PrivateKey
}

func rev036TestSigner(t *testing.T) onboarding.CutoverSigner {
	t.Helper()
	_, priv := rev036KeyPair()
	return &rev036EdSigner{authority: "authority:hcmnext-cutover", priv: priv}
}

func (s *rev036EdSigner) Authority() string { return s.authority }

func (s *rev036EdSigner) Sign(payload []byte) (string, error) {
	sig := ed25519.Sign(s.priv, payload)
	return hex.EncodeToString(sig), nil
}

func (s *rev036EdSigner) Verify(payload []byte, signature string) error {
	raw, err := hex.DecodeString(signature)
	if err != nil {
		return err
	}
	pub := s.priv.Public().(ed25519.PublicKey)
	if !ed25519.Verify(pub, payload, raw) {
		return errors.New("rev036: cutover signature does not verify")
	}
	return nil
}

func rev036CleanEvidence() onboarding.CutoverEvidence {
	frozenAt := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	return onboarding.CutoverEvidence{
		Tenant:                   rev036Tenant,
		Domain:                   "workforce",
		SourceAuthority:          "workday-prod",
		TargetAuthority:          "hcmnext",
		Freeze:                   onboarding.FreezeRecord{Token: "freeze-1", SourceVersion: "snap-001", FrozenAt: frozenAt},
		CurrentSourceVersion:     "snap-001",
		TargetWriters:            []string{"cutover:harborcare"},
		CutoverWriter:            "cutover:harborcare",
		PriorEpoch:               7,
		CurrentEpoch:             7,
		Delta:                    onboarding.DeltaRecord{BaseVersion: "snap-000", ThroughVersion: "snap-001", CommitDigest: "delta:abc123", Complete: true},
		SimulationDigest:         "sim:zero-effect-1",
		SimulationZeroEffect:     true,
		ApprovedSimulationDigest: "sim:zero-effect-1",
		ApprovedBy:               "user:approver@harborcare",
		RequestedBy:              "user:ops@harborcare",
		MaxReplicationLag:        5 * time.Minute,
		DecidedAt:                frozenAt.Add(time.Hour),
	}
}
func TestTodo_REV_036_01_Fault(t *testing.T) {
	ctx := context.Background()
	h := newRev036Harness(t)

	sm, err := onboarding.SignManifest(h.manifest(), "key-harborcare-01", h.priv)
	if err != nil {
		t.Fatalf("sign manifest: %v", err)
	}
	tampered := sm
	tampered.Signature = "00" + tampered.Signature[2:]
	if _, err := h.runs.Publish(ctx, rev036Tenant, tampered); err == nil {
		t.Fatal("publish accepted a tampered signature")
	}

	runID := h.publish(t)

	if _, err := h.runs.StartExtraction(ctx, rev036Tenant, runID, onboarding.ExtractRequest{RunID: runID}); err == nil {
		t.Fatal("extraction proceeded without a passing preflight")
	}

	if _, err := h.runs.Get(ctx, "other-tenant", runID); !errors.Is(err, ErrOnboardingRunNotFound) {
		t.Fatalf("cross-tenant read gives %v, want run-not-found", err)
	}
	if _, err := h.runs.Get(ctx, rev036Tenant, "ob-nope"); !errors.Is(err, ErrOnboardingRunNotFound) {
		t.Fatalf("unknown run read gives %v, want run-not-found", err)
	}

	bare := NewOnboardingRuns(nil)
	if _, err := bare.Publish(ctx, rev036Tenant, sm); !errors.Is(err, ErrOnboardingUnconfigured) {
		t.Fatalf("nil-resolver publish gives %v, want unconfigured", err)
	}

	if _, err := h.runs.RunPreflight(ctx, rev036Tenant, runID); err != nil {
		t.Fatalf("preflight: %v", err)
	}
	if _, err := h.runs.ExecuteCutover(ctx, rev036Tenant, runID, rev036CleanEvidence(), rev036TestSigner(t)); err == nil {
		t.Fatal("cutover executed before adjudication")
	}

	if err := h.runs.Abort(ctx, rev036Tenant, runID, "operator changed their mind"); err != nil {
		t.Fatalf("abort: %v", err)
	}
	view, err := h.runs.Get(ctx, rev036Tenant, runID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if view.State != OnboardingRunAborted || view.AbortedReason == "" {
		t.Fatalf("aborted view is %+v", view)
	}
	if err := h.runs.Abort(ctx, rev036Tenant, runID, "again"); err == nil {
		t.Fatal("second abort succeeded on an aborted run")
	}
}
func TestTodo_REV_036_01_Recovery(t *testing.T) {
	ctx := context.Background()
	h := newRev036Harness(t)
	runID := h.publish(t)
	if _, err := h.runs.RunPreflight(ctx, rev036Tenant, runID); err != nil {
		t.Fatalf("preflight: %v", err)
	}

	first, err := h.runs.StartExtraction(ctx, rev036Tenant, runID, onboarding.ExtractRequest{
		RunID: runID, Mode: connectivity.ReadFull, MaxPagesPerRun: 1, MaxRunsPerObject: 1,
	})
	if err != nil {
		t.Fatalf("start extraction: %v", err)
	}
	if len(first) != 1 || first[0].Run.Status != observe.RunBounded {
		t.Fatalf("first capped attempt is %+v, want one BOUNDED object result", first)
	}

	results := first
	for calls := 1; !rev036ExtractionDone(results); calls++ {
		if calls > 40 {
			t.Fatalf("resumed extraction never completed: %+v", results)
		}
		results, err = h.runs.StartExtraction(ctx, rev036Tenant, runID, onboarding.ExtractRequest{
			RunID: runID, Mode: connectivity.ReadFull, MaxPagesPerRun: 1, MaxRunsPerObject: 1,
		})
		if err != nil {
			t.Fatalf("resume extraction: %v", err)
		}
	}

	again, err := h.runs.StartExtraction(ctx, rev036Tenant, runID, onboarding.ExtractRequest{
		RunID: runID, Mode: connectivity.ReadFull, MaxPagesPerRun: 1, MaxRunsPerObject: 1,
	})
	if err != nil {
		t.Fatalf("repeat extraction: %v", err)
	}
	if len(again) != 1 || (again[0].Run.Status != observe.RunAlreadyComplete && again[0].Run.Status != observe.RunCompleted) {
		t.Fatalf("repeat extraction is %+v, want ALREADY_COMPLETE", again)
	}

	view, err := h.runs.Get(ctx, rev036Tenant, runID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if view.State != OnboardingRunExtracted {
		t.Fatalf("run state is %q, want extracted", view.State)
	}
}
