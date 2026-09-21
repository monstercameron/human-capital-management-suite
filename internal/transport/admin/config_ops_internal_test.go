package admin

import (
	"testing"
	"time"

	adminv1 "github.com/monstercameron/human-capital-management-suite/gen/go/hcmnext/admin/v1"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/config"
	"github.com/monstercameron/human-capital-management-suite/internal/platform/config/promotion"
)

func TestConfigWireVocabulary(t *testing.T) {
	dependencies := map[string]config.DependencyKind{
		"SCHEMA": config.DependencySchema, "RULE": config.DependencyRule,
		"WORKFLOW": config.DependencyWorkflow, "MAPPING": config.DependencyMapping,
		"CAPABILITY": config.DependencyCapability, "CONNECTOR": config.DependencyConnector,
		"AGENT": config.DependencyAgent, "REFERENCE": config.DependencyReference,
		"POLICY": config.DependencyPolicy,
	}
	for wire, want := range dependencies {
		if got, err := configDependencyKind(wire); err != nil || got != want {
			t.Errorf("dependency %s = %v, %v; want %v", wire, got, err, want)
		}
	}
	if _, err := configDependencyKind("UNKNOWN"); err == nil {
		t.Fatal("unknown dependency kind was accepted")
	}

	values := map[string]config.ValueKind{
		"STRING": config.KindString, "INT": config.KindInt, "BOOL": config.KindBool,
		"FLOAT": config.KindFloat, "DURATION": config.KindDuration, "LIST": config.KindList,
		"MAP": config.KindMap, "SECRET_REF": config.KindSecretRef,
	}
	for wire, want := range values {
		if got, err := configValueKind(wire); err != nil || got != want {
			t.Errorf("value %s = %v, %v; want %v", wire, got, err, want)
		}
	}
	if _, err := configValueKind("UNKNOWN"); err == nil {
		t.Fatal("unknown value kind was accepted")
	}

	semantics := map[string]config.SemanticClass{
		"": config.SemanticGeneric, "GENERIC": config.SemanticGeneric,
		"WORKFLOW": config.SemanticWorkflow, "SCHEMA": config.SemanticSchema,
		"MAPPING": config.SemanticMapping, "POLICY": config.SemanticPolicy,
	}
	for wire, want := range semantics {
		if got, err := configSemantic(wire); err != nil || got != want {
			t.Errorf("semantic %s = %v, %v; want %v", wire, got, err, want)
		}
	}
	if _, err := configSemantic("UNKNOWN"); err == nil {
		t.Fatal("unknown semantic class was accepted")
	}
}

func TestConfigWireConversion(t *testing.T) {
	if _, err := configPackageFromProto(nil); err == nil {
		t.Fatal("nil config package was accepted")
	}
	pb := &adminv1.ConfigPackage{
		Id: "release-1", Environment: "staging", PublicKey: []byte("key"),
		Bundle: &adminv1.ConfigSignedBundle{
			Digest: "sha256:digest", SignerKeyId: "key-1", Signature: "signature",
			Bundle: &adminv1.ConfigBundle{
				BundleId: "release-1", ManifestVersion: 2,
				Dependencies:       []*adminv1.ConfigDependency{{Kind: "SCHEMA", Name: "worker", Version: "v1", Digest: "sha256:schema"}},
				CompatibilityRange: ">=1", Signer: "release-bot", Provenance: "build-1",
				CredentialRefs: []string{"secretref://connector"}, TargetScope: "tenant:demo",
				MinimumRuntimeVersion: "1.2.0",
			},
		},
	}
	converted, err := configPackageFromProto(pb)
	if err != nil {
		t.Fatalf("package conversion: %v", err)
	}
	if converted.ID != pb.Id || converted.Bundle.Bundle.ManifestVersion != 2 || len(converted.Bundle.Bundle.Dependencies) != 1 || string(converted.PublicKey) != "key" {
		t.Fatalf("converted package = %+v", converted)
	}
	pb.Bundle.Bundle.Dependencies[0].Kind = "UNKNOWN"
	if _, err := configPackageFromProto(pb); err == nil {
		t.Fatal("package with unknown dependency kind was accepted")
	}

	if _, err := configSnapshotFromProto(nil); err == nil {
		t.Fatal("nil snapshot was accepted")
	}
	snapshot, err := configSnapshotFromProto(&adminv1.ConfigSnapshot{
		Name: "candidate", Version: "v2",
		Entries: []*adminv1.ConfigSnapshotEntry{
			{Key: "workflow.mode", Kind: "STRING", Value: "strict", Explicit: true, Semantic: "WORKFLOW", RefWorkflows: []string{"promotion"}},
			{Key: "connector.token", Kind: "SECRET_REF", SecretFingerprint: "sha256:secret", Semantic: "GENERIC", RefCapabilities: []string{"worker.read"}},
		},
	})
	if err != nil || snapshot.Name != "candidate" || len(snapshot.Entries()) != 2 {
		t.Fatalf("snapshot conversion = %+v, %v", snapshot, err)
	}
	if _, err := configSnapshotFromProto(&adminv1.ConfigSnapshot{Name: "candidate", Version: "v2", Entries: []*adminv1.ConfigSnapshotEntry{{Key: "secret", Kind: "SECRET_REF", Value: "plaintext", SecretFingerprint: "fingerprint"}}}); err == nil {
		t.Fatal("plaintext secret was accepted")
	}
	if _, err := configSnapshotFromProto(&adminv1.ConfigSnapshot{Name: "candidate", Version: "v2", Entries: []*adminv1.ConfigSnapshotEntry{{Key: "x", Kind: "UNKNOWN"}}}); err == nil {
		t.Fatal("unknown snapshot value kind was accepted")
	}
}

func TestConfigRecordAndClockProjection(t *testing.T) {
	record := promotion.Record{
		Package: promotion.Package{ID: "release-1", Environment: "production", Bundle: config.SignedBundle{Digest: "sha256:digest"}},
		Status:  promotion.StatusActive, Approval: promotion.Approval{Approver: "operator:two"},
		RollbackTo: "release-0", Evidence: promotion.ActivationEvidence{EvidenceDigest: "sha256:evidence"},
	}
	profile := configRecordProfile(record)
	if !profile.GetValidated() || !profile.GetSimulated() || !profile.GetApproved() || profile.GetApprover() != "operator:two" || profile.GetRollbackTo() != "release-0" {
		t.Fatalf("active profile = %+v", profile)
	}
	if got := configRecordProfile(promotion.Record{Status: promotion.StatusDraft}); got.GetValidated() || got.GetSimulated() || got.GetApproved() {
		t.Fatalf("draft profile gained lifecycle flags: %+v", got)
	}

	now := time.Date(2026, 9, 20, 12, 0, 0, 0, time.FixedZone("test", -4*60*60))
	if got := configNow(&server{deps: Dependencies{Now: func() time.Time { return now }}}); !got.Equal(now) || got.Location() != time.UTC {
		t.Fatalf("configNow = %v", got)
	}
	if configNow(&server{}).IsZero() {
		t.Fatal("default config clock returned zero")
	}
	for _, environment := range []string{"production", "PROD"} {
		if !isProductionEnv(environment) {
			t.Errorf("%q was not recognized as production", environment)
		}
	}
	if isProductionEnv("staging") {
		t.Fatal("staging was recognized as production")
	}
}
