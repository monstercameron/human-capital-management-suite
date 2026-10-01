// TCLOCK-006: govern biometric enrollment, consent and template custody
// for clock identification.
//
// Biometric identification is off for a tenant and jurisdiction until a
// policy names its legal basis, notice version, retention and destruction
// schedule and a required non-biometric alternative. AdmitBiometricEnrollment
// gates one worker's biometric enrollment: consent must be bound to the
// policy's exact notice version, and face matching is admitted only when
// the device has declared ISO/IEC 30107-3 presentation-attack detection and
// the policy requires it. DestructionDate is the earlier of purpose end and
// the statutory limit. The package never touches a raw template or image:
// it only reasons about the governance facts around one. It is pure: no
// clock, no storage, no network.
package clock

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	// ErrBiometricRejected is the TCLOCK-006 seeded-defect sentinel. A test
	// that probes an enrollment before consent, a consent bound to a stale
	// notice version, a missing retention schedule or a face method without
	// declared PAD must see this error.
	ErrBiometricRejected = errors.New("TCLOCK_006_REJECTED")
	// ErrBiometricEvidence identifies an invalid admission that cannot be
	// used as enrollment evidence.
	ErrBiometricEvidence = errors.New("clock: biometric admission evidence is invalid")
)

// BiometricRejection is the stable TCLOCK-006 failure shape.
type BiometricRejection struct {
	Field   string
	State   string
	Version string
	Reason  string
}

func (r *BiometricRejection) Error() string {
	return fmt.Sprintf("%s: field=%s state=%s version=%s: %s", ErrBiometricRejected, r.Field, r.State, r.Version, r.Reason)
}

// Unwrap exposes the TCLOCK_006_REJECTED sentinel to errors.Is.
func (r *BiometricRejection) Unwrap() error { return ErrBiometricRejected }

func bioReject(field, state, version, reason string) error {
	return &BiometricRejection{Field: field, State: state, Version: version, Reason: reason}
}

const biometricVersion = "tclock-biometric/v1"

// LegalBasis is the closed vocabulary of statutory bases a biometric policy
// may cite. An empty basis is never treated as "no biometric law applies":
// it is refused whenever the policy is enabled.
type LegalBasis string

const (
	LegalBasisIllinoisBIPA    LegalBasis = "ILLINOIS_BIPA"
	LegalBasisTexasCUBI       LegalBasis = "TEXAS_CUBI"
	LegalBasisWashington      LegalBasis = "WASHINGTON_BIOMETRIC"
	LegalBasisGDPRArticleNine LegalBasis = "GDPR_ARTICLE_9"
)

func (b LegalBasis) Valid() bool {
	switch b {
	case LegalBasisIllinoisBIPA, LegalBasisTexasCUBI, LegalBasisWashington, LegalBasisGDPRArticleNine:
		return true
	}
	return false
}

// BiometricPolicy gates whether face or fingerprint identification may run
// for a tenant and jurisdiction. Enabled=false is the only safe zero value;
// every other field is required only once a tenant turns the policy on.
type BiometricPolicy struct {
	Tenant                  values.TenantId
	Jurisdiction            string
	Enabled                 bool
	LegalBasis              LegalBasis
	NoticeVersion           string
	RetentionPeriod         time.Duration
	StatutoryLimit          time.Duration
	NonBiometricAlternative string
	RequiresPAD             bool
	Version                 string
}

func (p BiometricPolicy) Validate() error {
	if err := p.Tenant.Validate(); err != nil {
		return bioReject("tenant", "", p.Version, "tenant is invalid")
	}
	if strings.TrimSpace(p.Jurisdiction) == "" {
		return bioReject("jurisdiction", "", p.Version, "jurisdiction is required")
	}
	if !p.Enabled {
		return nil
	}
	if !p.LegalBasis.Valid() {
		return bioReject("legal_basis", "ENABLED", p.Version, "legal basis is required once biometric identification is enabled")
	}
	if strings.TrimSpace(p.NoticeVersion) == "" {
		return bioReject("notice_version", "ENABLED", p.Version, "notice version is required")
	}
	if p.RetentionPeriod <= 0 {
		return bioReject("retention_period", "ENABLED", p.Version, "retention period must be positive")
	}
	if strings.TrimSpace(p.NonBiometricAlternative) == "" {
		return bioReject("non_biometric_alternative", "ENABLED", p.Version, "a non-biometric alternative is required")
	}
	if strings.TrimSpace(p.Version) == "" {
		return bioReject("version", "ENABLED", p.Version, "policy version is required")
	}
	return nil
}

// DestructionDate returns the earlier of purpose end (enrolledAt plus
// RetentionPeriod) and the statutory limit (enrolledAt plus StatutoryLimit),
// when StatutoryLimit is declared; otherwise purpose end alone governs.
func (p BiometricPolicy) DestructionDate(enrolledAt time.Time) (time.Time, error) {
	if err := p.Validate(); err != nil {
		return time.Time{}, err
	}
	if !p.Enabled {
		return time.Time{}, bioReject("policy.enabled", "OFF", p.Version, "destruction schedule requires an enabled policy")
	}
	if enrolledAt.IsZero() {
		return time.Time{}, bioReject("enrolled_at", "ENABLED", p.Version, "enrollment instant is required")
	}
	purposeEnd := enrolledAt.Add(p.RetentionPeriod)
	if p.StatutoryLimit <= 0 {
		return purposeEnd, nil
	}
	statutory := enrolledAt.Add(p.StatutoryLimit)
	if statutory.Before(purposeEnd) {
		return statutory, nil
	}
	return purposeEnd, nil
}

// BiometricConsent binds a worker's consent to one exact notice version.
type BiometricConsent struct {
	WorkerRef     string
	NoticeVersion string
	ConsentedAt   time.Time
}

func (c BiometricConsent) Validate() error {
	if strings.TrimSpace(c.WorkerRef) == "" {
		return bioReject("consent.worker_ref", "", biometricVersion, "worker reference is required")
	}
	if strings.TrimSpace(c.NoticeVersion) == "" {
		return bioReject("consent.notice_version", "", biometricVersion, "notice version is required")
	}
	if c.ConsentedAt.IsZero() {
		return bioReject("consent.consented_at", "", biometricVersion, "consent instant is required")
	}
	return nil
}

// EnrollmentGateRequest is the complete side-effect-free biometric
// enrollment question.
type EnrollmentGateRequest struct {
	Policy            BiometricPolicy
	Consent           BiometricConsent
	Method            IdentificationMethod
	DevicePADDeclared bool
	Now               time.Time
}

// BiometricAdmission is the auditable outcome of an admitted biometric
// enrollment. It never carries a raw template or image reference: template
// custody belongs to the device or a separately keyed store, not here.
type BiometricAdmission struct {
	WorkerRef     string
	Method        IdentificationMethod
	NoticeVersion string
	EnrolledAt    time.Time
	DestructionAt time.Time
	Digest        string
}

func (a BiometricAdmission) digestBody() string {
	return strings.Join([]string{a.WorkerRef, string(a.Method), a.NoticeVersion, a.EnrolledAt.UTC().Format(time.RFC3339Nano), a.DestructionAt.UTC().Format(time.RFC3339Nano)}, "\x00")
}

// AdmitBiometricEnrollment gates one worker's biometric enrollment: the
// policy must be enabled for the tenant and jurisdiction, consent must be
// bound to the policy's exact notice version and dated no later than now,
// and a face method requires declared presentation-attack detection
// whenever the policy requires it.
func AdmitBiometricEnrollment(req EnrollmentGateRequest) (BiometricAdmission, error) {
	if err := req.Policy.Validate(); err != nil {
		return BiometricAdmission{}, err
	}
	if !req.Policy.Enabled {
		return BiometricAdmission{}, bioReject("policy.enabled", "OFF", biometricVersion, "biometric identification is off for this tenant and jurisdiction")
	}
	if !req.Method.Biometric() {
		return BiometricAdmission{}, bioReject("method", "NOT_BIOMETRIC", biometricVersion, "method is not a biometric method")
	}
	if err := req.Consent.Validate(); err != nil {
		return BiometricAdmission{}, err
	}
	if req.Consent.NoticeVersion != req.Policy.NoticeVersion {
		return BiometricAdmission{}, bioReject("consent.notice_version", "MISMATCH", biometricVersion, "consent is bound to a different notice version")
	}
	if req.Now.IsZero() {
		return BiometricAdmission{}, bioReject("now", "", biometricVersion, "server clock is required")
	}
	if req.Now.Before(req.Consent.ConsentedAt) {
		return BiometricAdmission{}, bioReject("now", "BEFORE_CONSENT", biometricVersion, "enrollment cannot precede consent")
	}
	if req.Method == MethodFace && req.Policy.RequiresPAD && !req.DevicePADDeclared {
		return BiometricAdmission{}, bioReject("device.pad", "MISSING", biometricVersion, "device has not declared ISO/IEC 30107-3 presentation-attack detection")
	}
	at := req.Now.UTC()
	destruction, err := req.Policy.DestructionDate(at)
	if err != nil {
		return BiometricAdmission{}, err
	}
	admission := BiometricAdmission{WorkerRef: req.Consent.WorkerRef, Method: req.Method, NoticeVersion: req.Policy.NoticeVersion, EnrolledAt: at, DestructionAt: destruction}
	sum := sha256.Sum256([]byte(admission.digestBody()))
	admission.Digest = "sha256:" + hex.EncodeToString(sum[:])
	return admission, nil
}

// ReplayDestructionSchedule recomputes destruction dates for an ordered
// list of enrollment instants under one policy. It must equal recomputing
// DestructionDate one at a time; TestTodo_TCLOCK_006_Recovery proves that
// equivalence.
func ReplayDestructionSchedule(policy BiometricPolicy, enrolledAts []time.Time) ([]time.Time, error) {
	out := make([]time.Time, 0, len(enrolledAts))
	for _, at := range enrolledAts {
		d, err := policy.DestructionDate(at)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, nil
}

// BiometricExplanation is the audit-safe summary of one biometric
// admission.
type BiometricExplanation struct {
	Method        IdentificationMethod
	NoticeVersion string
	DestructionAt time.Time
	Digest        string
}

// ExplainBiometricAdmission validates and summarizes an admission for
// operator display.
func ExplainBiometricAdmission(a BiometricAdmission) (BiometricExplanation, error) {
	if a.Digest == "" || a.WorkerRef == "" {
		return BiometricExplanation{}, ErrBiometricEvidence
	}
	sum := sha256.Sum256([]byte(a.digestBody()))
	if "sha256:"+hex.EncodeToString(sum[:]) != a.Digest {
		return BiometricExplanation{}, fmt.Errorf("%w: digest mismatch", ErrBiometricEvidence)
	}
	return BiometricExplanation{Method: a.Method, NoticeVersion: a.NoticeVersion, DestructionAt: a.DestructionAt, Digest: a.Digest}, nil
}
