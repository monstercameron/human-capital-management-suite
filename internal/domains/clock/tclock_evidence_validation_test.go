package clock

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestValidateDeviceIdentity_RejectsTampering(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	pub, priv := testKeyPair(t)
	id, err := Enroll(EnrollmentRequest{Code: baseCode(now), Proof: testProof(t, pub, priv, []byte("challenge")), DeviceRef: "device-1", Registry: testRegistry(t), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateDeviceIdentity(id); err != nil {
		t.Fatalf("valid identity rejected: %v", err)
	}
	id.SiteRef = "attacker-site"
	if !errors.Is(ValidateDeviceIdentity(id), ErrEnrollmentEvidence) {
		t.Fatal("tampered identity was accepted")
	}
}

func TestTransitionValidatedDevice_RejectsForgedPrevious(t *testing.T) {
	now := time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
	forged := DeviceIdentity{DeviceRef: "d", Tenant: "tenant", SiteRef: "s", Profile: SourceManagedKiosk, Timezone: "UTC", PublicKeyFingerprint: "sha256:" + strings.Repeat("00", 32), Revision: 1, State: DeviceActive, EnrolledAt: now, TransitionedAt: now, Digest: "sha256:" + strings.Repeat("11", 32)}
	_, err := TransitionValidatedDevice(DeviceTransitionRequest{Previous: forged, Kind: TransitionSuspend, Reason: "maintenance", Now: now.Add(time.Minute)})
	if !errors.Is(err, ErrEnrollmentEvidence) {
		t.Fatalf("expected forged previous rejection, got %v", err)
	}
}

func TestValidateIdentificationEvidence_RejectsForgedDigest(t *testing.T) {
	now := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	cred, err := HashPIN("worker-1", "4821", mustSalt(t))
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := IdentifyWorker(IdentificationRequest{Method: MethodPIN, DeviceRef: "device-1", WorkerRef: "worker-1", Now: now, PINCredential: cred, PINEntered: "4821"})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateIdentificationEvidence(evidence); err != nil {
		t.Fatalf("valid evidence rejected: %v", err)
	}
	evidence.Digest = "sha256:" + strings.Repeat("00", 32)
	if !errors.Is(ValidateIdentificationEvidence(evidence), ErrIdentificationRejected) {
		t.Fatal("forged evidence was accepted")
	}
}

func TestValidateBiometricAdmission_RejectsForgeryAndExpiry(t *testing.T) {
	now := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	policy := enabledBiometricPolicy()
	a, err := AdmitBiometricEnrollment(EnrollmentGateRequest{Policy: policy, Consent: BiometricConsent{WorkerRef: "worker-1", NoticeVersion: policy.NoticeVersion, ConsentedAt: now.Add(-time.Hour)}, Method: MethodFace, DevicePADDeclared: true, Now: now})
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateBiometricAdmission(a, now.Add(time.Hour)); err != nil {
		t.Fatalf("valid admission rejected: %v", err)
	}
	if !errors.Is(ValidateBiometricAdmission(a, a.DestructionAt), ErrBiometricEvidence) {
		t.Fatal("expired admission was accepted")
	}
	if !errors.Is(ValidateBiometricAdmission(a, now.Add(-time.Minute)), ErrBiometricEvidence) {
		t.Fatal("admission was accepted before enrollment")
	}
	a.Digest = "sha256:" + strings.Repeat("00", 32)
	if !errors.Is(ValidateBiometricAdmission(a, now), ErrBiometricEvidence) {
		t.Fatal("forged admission was accepted")
	}
}

func TestValidateProfileRegistry_RejectsMutatedNestedProfile(t *testing.T) {
	r := testRegistry(t)
	if err := ValidateProfileRegistry(r); err != nil {
		t.Fatalf("valid registry rejected: %v", err)
	}
	r.Profiles[0].PermittedMethods[0] = MethodFace
	if !errors.Is(ValidateProfileRegistry(r), ErrProfileRejected) {
		t.Fatal("mutated nested profile was accepted")
	}
}

func TestRequireValidatedFaceComparisonAdmission_RejectsDeadbeef(t *testing.T) {
	now := time.Date(2026, 9, 28, 10, 0, 0, 0, time.UTC)
	a := BiometricAdmission{WorkerRef: "worker-1", Method: MethodFace, NoticeVersion: "notice", EnrolledAt: now, DestructionAt: now.Add(time.Hour), Digest: "sha256:deadbeef"}
	if !errors.Is(RequireValidatedFaceComparisonAdmission("worker-1", &a, now), ErrBiometricEvidence) {
		t.Fatal("forged face admission was accepted")
	}
	if !errors.Is(RequireValidatedFaceComparisonAdmission("worker-1", nil, now), ErrPhotoRejected) {
		t.Fatal("nil face admission was accepted")
	}
}

func TestValidateIdentificationEvidence_RequiresSupervisorEvidence(t *testing.T) {
	now := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	e := IdentificationEvidence{Method: MethodSupervisorOverride, AssuranceLevel: AssuranceHigh, WorkerRef: "worker", DeviceRef: "device", At: now, Digest: "sha256:" + strings.Repeat("00", 32)}
	if !errors.Is(ValidateIdentificationEvidence(e), ErrIdentificationRejected) {
		t.Fatal("override without supervisor evidence was accepted")
	}
}
