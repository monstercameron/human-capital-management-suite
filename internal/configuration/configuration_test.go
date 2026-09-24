package configuration

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

var configurationTestNow = time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)

func configurationApprovalKey() ed25519.PrivateKey {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i + 1)
	}
	return ed25519.NewKeyFromSeed(seed)
}

func approvalBytes(approval Approval) []byte {
	return []byte(fmt.Sprintf("%s\x00%s\x00%s\x00%s\x00%s", approval.Bundle, approval.Action, approval.Approver, approval.Authority, approval.Expiry))
}

func signedApproval(bundle, action, approver, expiry string) Approval {
	approval := Approval{Bundle: bundle, Action: action, Approver: approver, Authority: "release-board", Expiry: expiry}
	approval.Signature = ed25519.Sign(configurationApprovalKey(), approvalBytes(approval))
	return approval
}

func testApprovalVerifier() ApprovalVerifier {
	public := configurationApprovalKey().Public().(ed25519.PublicKey)
	return ApprovalVerifierFunc(func(approval Approval) error {
		if !ed25519.Verify(public, approvalBytes(approval), approval.Signature) {
			return errors.New("signature invalid")
		}
		return nil
	})
}

func recordTestApproval(t *testing.T, reg *Registry, bundle, action, approver, expiry string) {
	t.Helper()
	if err := reg.RecordApproval(signedApproval(bundle, action, approver, expiry), testApprovalVerifier(), configurationTestNow); err != nil {
		t.Fatal(err)
	}
}

func seedBundle(t *testing.T) (Registry, Bundle) {
	t.Helper()
	reg := NewRegistry()
	def := Definition{Kind: "rule", Name: "overtime-cap", Source: "definitions/operations/overtime.yaml"}
	snap, err := SnapshotDefinition(def, []byte("max_hours: 12"))
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := reg.Assemble("overtime-rollout", []Snapshot{snap}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return reg, bundle
}

func TestConfigurationActivationRequiresApproval(t *testing.T) {
	reg, bundle := seedBundle(t)
	if _, err := reg.Activate(bundle.Digest, "release-operator", configurationTestNow); !errors.Is(err, ErrInvalidApproval) {
		t.Fatal("activation without approval was accepted")
	}
	recordTestApproval(t, &reg, bundle.Digest, ApprovalActionActivate, "release-captains", "2026-09-24T13:00:00Z")
	record, err := reg.Activate(bundle.Digest, "release-operator", configurationTestNow)
	if err != nil {
		t.Fatalf("approved activation rejected: %v", err)
	}
	if record.Bundle != bundle.Digest || record.ActivatedBy != "release-operator" || record.Approval.Approver != "release-captains" {
		t.Fatalf("activation did not preserve distinct approved actors and exact bundle: %+v", record)
	}
}

func TestConfigurationActivationBindsImmutableDigest(t *testing.T) {
	reg, bundle := seedBundle(t)
	recordTestApproval(t, &reg, bundle.Digest, ApprovalActionActivate, "release-captains", "2026-09-24T13:00:00Z")
	if _, err := reg.Activate("sha256:tampered", "release-operator", configurationTestNow); err == nil {
		t.Fatal("activation with tampered digest was accepted")
	}
	foreignSnap, err := SnapshotDefinition(Definition{Kind: "rule", Name: "meal-break", Source: "definitions/operations/meals.yaml"}, []byte("minutes: 30"))
	if err != nil {
		t.Fatal(err)
	}
	otherRegistry, otherBundle := seedBundle(t)
	otherForeign, err := otherRegistry.Assemble("meal-rollout", []Snapshot{foreignSnap}, nil)
	if err != nil {
		t.Fatal(err)
	}
	recordTestApproval(t, &otherRegistry, otherForeign.Digest, ApprovalActionActivate, "release-captains", "2026-09-24T13:00:00Z")
	if _, err := otherRegistry.Activate(otherBundle.Digest, "release-operator", configurationTestNow); !errors.Is(err, ErrInvalidApproval) {
		t.Fatal("activation with foreign approval was accepted")
	}
}

func TestConfigurationRollbackTargetsPriorDigest(t *testing.T) {
	reg, bundle := seedBundle(t)
	recordTestApproval(t, &reg, bundle.Digest, ApprovalActionActivate, "release-captains", "2026-09-24T13:00:00Z")
	if _, err := reg.Activate(bundle.Digest, "release-operator", configurationTestNow); err != nil {
		t.Fatal(err)
	}
	def := Definition{Kind: "rule", Name: "overtime-cap", Source: "definitions/operations/overtime.yaml"}
	next, err := SnapshotDefinition(def, []byte("max_hours: 10"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := reg.Assemble("overtime-rollout", []Snapshot{next}, []Dependency{{Bundle: bundle.Digest}})
	if err != nil {
		t.Fatal(err)
	}
	recordTestApproval(t, &reg, second.Digest, ApprovalActionActivate, "release-captains", "2026-09-24T13:00:00Z")
	if _, err := reg.Activate(second.Digest, "release-operator", configurationTestNow); err != nil {
		t.Fatal(err)
	}
	recordTestApproval(t, &reg, bundle.Digest, ApprovalActionRollback, "rollback-reviewer", "2026-09-24T13:00:00Z")
	record, err := reg.Rollback(second.Digest, bundle.Digest, "rollback-operator", configurationTestNow)
	if err != nil {
		t.Fatalf("approved rollback rejected: %v", err)
	}
	if record.Bundle != bundle.Digest || record.ActivatedBy != "rollback-operator" || !record.Rollback {
		t.Fatalf("rollback bound %q, want prior %q", record.Bundle, bundle.Digest)
	}
	if _, err := reg.Rollback(second.Digest, bundle.Digest, "release-operator", configurationTestNow); err == nil {
		t.Fatal("rollback without approval was accepted")
	}
}

func TestConfigurationApprovalRejectsForgeryExpiryAndSelfApproval(t *testing.T) {
	t.Run("forged signature", func(t *testing.T) {
		reg, bundle := seedBundle(t)
		approval := signedApproval(bundle.Digest, ApprovalActionActivate, "release-captains", "2026-09-24T13:00:00Z")
		approval.Approver = "attacker"
		if err := reg.RecordApproval(approval, testApprovalVerifier(), configurationTestNow); !errors.Is(err, ErrInvalidApproval) {
			t.Fatalf("forged approval error = %v, want ErrInvalidApproval", err)
		}
		if _, err := reg.Activate(bundle.Digest, "operator", configurationTestNow); !errors.Is(err, ErrInvalidApproval) || len(reg.log) != 0 {
			t.Fatalf("forgery changed activation state: err=%v log=%+v", err, reg.log)
		}
	})
	t.Run("expired on receipt", func(t *testing.T) {
		reg, bundle := seedBundle(t)
		err := reg.RecordApproval(signedApproval(bundle.Digest, ApprovalActionActivate, "reviewer", "2026-09-24T11:59:59Z"), testApprovalVerifier(), configurationTestNow)
		if !errors.Is(err, ErrExpiredApproval) {
			t.Fatalf("expired approval error = %v, want ErrExpiredApproval", err)
		}
	})
	t.Run("expires before activation", func(t *testing.T) {
		reg, bundle := seedBundle(t)
		recordTestApproval(t, &reg, bundle.Digest, ApprovalActionActivate, "reviewer", "2026-09-24T12:01:00Z")
		_, err := reg.Activate(bundle.Digest, "operator", configurationTestNow.Add(2*time.Minute))
		if !errors.Is(err, ErrExpiredApproval) || len(reg.log) != 0 {
			t.Fatalf("expired activation: err=%v log=%+v", err, reg.log)
		}
	})
	t.Run("self approval", func(t *testing.T) {
		reg, bundle := seedBundle(t)
		recordTestApproval(t, &reg, bundle.Digest, ApprovalActionActivate, "operator", "2026-09-24T13:00:00Z")
		_, err := reg.Activate(bundle.Digest, "operator", configurationTestNow)
		if !errors.Is(err, ErrSelfApproval) || len(reg.log) != 0 {
			t.Fatalf("self approval: err=%v log=%+v", err, reg.log)
		}
	})
}

func TestConfigurationValidationRejectsUnknownBundle(t *testing.T) {
	reg, _ := seedBundle(t)
	if findings := reg.Validate("sha256:missing"); len(findings) == 0 {
		t.Fatal("unknown bundle validated clean")
	}
}

func TestConfigurationDiffIsDeterministic(t *testing.T) {
	reg, bundle := seedBundle(t)
	a := reg.Diff(bundle.Digest, bundle.Digest)
	b := reg.Diff(bundle.Digest, bundle.Digest)
	if len(a) != 0 || len(a) != len(b) {
		t.Fatalf("identical digests differ: %+v vs %+v", a, b)
	}
	def := Definition{Kind: "rule", Name: "overtime-cap", Source: "definitions/operations/overtime.yaml"}
	next, err := SnapshotDefinition(def, []byte("max_hours: 10"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := reg.Assemble("overtime-rollout", []Snapshot{next}, nil)
	if err != nil {
		t.Fatal(err)
	}
	changes := reg.Diff(bundle.Digest, second.Digest)
	if len(changes) == 0 {
		t.Fatal("changed bundle produced no diff")
	}
	for _, c := range changes {
		if strings.TrimSpace(string(c)) == "" {
			t.Fatal("empty diff entry")
		}
	}
}
