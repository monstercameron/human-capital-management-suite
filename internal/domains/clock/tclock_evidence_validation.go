package clock

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// ValidateDeviceIdentity verifies the canonical digest and required
// lifecycle fields of a device identity before it is used as transition
// evidence. The legacy transition entrypoint does not perform this check.
// The existing canonical digest covers identity scope, key fingerprint,
// revision and state; it does not cover EnrolledAt, TransitionedAt or Reason,
// so this helper cannot detect tampering limited to those omitted fields.
func ValidateDeviceIdentity(d DeviceIdentity) error {
	if d.Tenant.Validate() != nil {
		return fmt.Errorf("%w: tenant is invalid", ErrEnrollmentEvidence)
	}
	if strings.TrimSpace(d.DeviceRef) == "" || strings.TrimSpace(d.SiteRef) == "" {
		return fmt.Errorf("%w: device and site references are required", ErrEnrollmentEvidence)
	}
	if !d.Profile.Valid() || !validTimezone(d.Timezone) {
		return fmt.Errorf("%w: profile or timezone is invalid", ErrEnrollmentEvidence)
	}
	if !validSHA256Fingerprint(d.PublicKeyFingerprint) || d.Revision == 0 || !d.State.Valid() {
		return fmt.Errorf("%w: identity evidence fields are invalid", ErrEnrollmentEvidence)
	}
	if d.EnrolledAt.IsZero() || d.TransitionedAt.IsZero() || d.TransitionedAt.Before(d.EnrolledAt) {
		return fmt.Errorf("%w: identity timestamps are invalid", ErrEnrollmentEvidence)
	}
	if !validSHA256Fingerprint(d.Digest) {
		return fmt.Errorf("%w: identity digest is invalid", ErrEnrollmentEvidence)
	}
	sum := sha256.Sum256([]byte(d.digestBody()))
	want := "sha256:" + hex.EncodeToString(sum[:])
	if d.Digest != want {
		return fmt.Errorf("%w: identity digest mismatch", ErrEnrollmentEvidence)
	}
	return nil
}

// ValidateIdentificationEvidence verifies the canonical digest and the
// method-to-assurance relationship of identification evidence before it is
// persisted or used for authorization.
func ValidateIdentificationEvidence(e IdentificationEvidence) error {
	if !e.Method.Valid() || e.AssuranceLevel != e.Method.AssuranceLevel() {
		return fmt.Errorf("%w: method or assurance is invalid", ErrIdentificationRejected)
	}
	if strings.TrimSpace(e.WorkerRef) == "" || strings.TrimSpace(e.DeviceRef) == "" || e.At.IsZero() {
		return fmt.Errorf("%w: identification evidence is incomplete", ErrIdentificationRejected)
	}
	if e.Method == MethodSupervisorOverride && strings.TrimSpace(e.SupervisorRef) == "" {
		return fmt.Errorf("%w: supervisor evidence is required", ErrIdentificationRejected)
	}
	if e.Method != MethodSupervisorOverride && strings.TrimSpace(e.SupervisorRef) != "" {
		return fmt.Errorf("%w: unexpected supervisor evidence", ErrIdentificationRejected)
	}
	if !validSHA256Fingerprint(e.Digest) {
		return fmt.Errorf("%w: identification digest is invalid", ErrIdentificationRejected)
	}
	body := strings.Join([]string{string(e.Method), e.DeviceRef, e.WorkerRef, e.SupervisorRef, e.At.UTC().Format(time.RFC3339Nano)}, "\x00")
	sum := sha256.Sum256([]byte(body))
	if e.Digest != "sha256:"+hex.EncodeToString(sum[:]) {
		return fmt.Errorf("%w: identification digest mismatch", ErrIdentificationRejected)
	}
	return nil
}

// ValidateProfileRegistry verifies every profile, the registry version and
// the current canonical registry digest. This detects changes made through
// the exported profile slices after construction; the digest is integrity
// evidence, not an independently signed registry authority.
func ValidateProfileRegistry(r ProfileRegistry) error {
	if r.Version != ProfileRegistryVersion || len(r.Profiles) == 0 || !validSHA256Fingerprint(r.Digest) {
		return fmt.Errorf("%w: registry evidence is incomplete", ErrProfileRejected)
	}
	seen := make(map[SourceClass]struct{}, len(r.Profiles))
	profiles := make([]IntegrationProfile, len(r.Profiles))
	for i, p := range r.Profiles {
		if err := p.Validate(); err != nil {
			return err
		}
		if _, ok := seen[p.Class]; ok {
			return fmt.Errorf("%w: duplicate profile class", ErrProfileRejected)
		}
		seen[p.Class] = struct{}{}
		profiles[i] = p
		profiles[i].PermittedMethods = p.normalizedMethods()
	}
	// NewProfileRegistry is the canonical sorter and digest builder, but this
	// validator must preserve the caller's registry value and return no copy.
	canonical, err := NewProfileRegistry(profiles)
	if err != nil {
		return err
	}
	if canonical.Digest != r.Digest {
		return fmt.Errorf("%w: registry digest mismatch", ErrProfileRejected)
	}
	return nil
}

// ValidateBiometricAdmission verifies a biometric admission's canonical
// digest and active lifetime at now. Tenant and jurisdiction binding cannot
// be checked because BiometricAdmission does not currently carry those fields.
func ValidateBiometricAdmission(a BiometricAdmission, now time.Time) error {
	if strings.TrimSpace(a.WorkerRef) == "" || !a.Method.Biometric() || strings.TrimSpace(a.NoticeVersion) == "" {
		return fmt.Errorf("%w: biometric admission fields are invalid", ErrBiometricEvidence)
	}
	if a.EnrolledAt.IsZero() || a.DestructionAt.IsZero() || a.DestructionAt.Before(a.EnrolledAt) {
		return fmt.Errorf("%w: biometric admission timestamps are invalid", ErrBiometricEvidence)
	}
	if now.IsZero() || now.Before(a.EnrolledAt) || !now.Before(a.DestructionAt) {
		return fmt.Errorf("%w: biometric admission is expired", ErrBiometricEvidence)
	}
	if !validSHA256Fingerprint(a.Digest) {
		return fmt.Errorf("%w: biometric admission digest is invalid", ErrBiometricEvidence)
	}
	sum := sha256.Sum256([]byte(a.digestBody()))
	if a.Digest != "sha256:"+hex.EncodeToString(sum[:]) {
		return fmt.Errorf("%w: biometric admission digest mismatch", ErrBiometricEvidence)
	}
	return nil
}

// TransitionValidatedDevice validates prior evidence before applying the
// existing pure transition operation, and validates the resulting revision.
func TransitionValidatedDevice(req DeviceTransitionRequest) (DeviceIdentity, error) {
	if err := ValidateDeviceIdentity(req.Previous); err != nil {
		return DeviceIdentity{}, err
	}
	next, err := TransitionDevice(req)
	if err != nil {
		return DeviceIdentity{}, err
	}
	if err := ValidateDeviceIdentity(next); err != nil {
		return DeviceIdentity{}, err
	}
	return next, nil
}

// RequireValidatedFaceComparisonAdmission validates an active admission
// before invoking the existing worker-match gate. A nil admission fails
// closed.
func RequireValidatedFaceComparisonAdmission(worker string, admission *BiometricAdmission, now time.Time) error {
	if admission == nil {
		return fmt.Errorf("%w: biometric admission is required", ErrPhotoRejected)
	}
	if err := ValidateBiometricAdmission(*admission, now); err != nil {
		return err
	}
	return RequireFaceComparisonAdmission(*admission, worker)
}

func validSHA256Fingerprint(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}
