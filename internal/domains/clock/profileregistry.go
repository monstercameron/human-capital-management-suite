// TCLOCK-001: the signed registry of accepted clock integration profiles.
//
// A profile names one accepted source class and the transport,
// authentication, trust ceiling, permitted identification methods, offline
// limits, clock-trust requirement and first named partner for it.
// Enrollment (TCLOCK-002) accepts only a device whose declared class
// resolves to a profile in this registry; an undeclared class is rejected,
// never defaulted, and a class's trust level is never inferred from a
// vendor name. The registry is pure: no database, no clock, no network.
package clock

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

// SourceClass is the closed vocabulary of accepted clock integration source
// classes. TCLOCK-001 names exactly these six; a class outside this set
// cannot be registered or accepted at enrollment.
type SourceClass string

const (
	SourceFirstPartyBrowser   SourceClass = "FIRST_PARTY_BROWSER"
	SourceManagedKiosk        SourceClass = "MANAGED_TABLET_KIOSK"
	SourceDevicePushHTTP      SourceClass = "DEVICE_PUSH_HTTP_HARDWARE"
	SourceServerPullVendorAPI SourceClass = "SERVER_PULL_VENDOR_API"
	SourceSFTPBatchFile       SourceClass = "SFTP_BATCH_FILE"
	SourceThirdPartyApp       SourceClass = "THIRD_PARTY_TIME_APP"
)

func (c SourceClass) Valid() bool {
	switch c {
	case SourceFirstPartyBrowser, SourceManagedKiosk, SourceDevicePushHTTP, SourceServerPullVendorAPI, SourceSFTPBatchFile, SourceThirdPartyApp:
		return true
	}
	return false
}

// SourceClasses lists every declared class in a stable order.
func SourceClasses() []SourceClass {
	return []SourceClass{SourceFirstPartyBrowser, SourceManagedKiosk, SourceDevicePushHTTP, SourceServerPullVendorAPI, SourceSFTPBatchFile, SourceThirdPartyApp}
}

// IdentificationMethod is the closed vocabulary of worker identification
// methods usable at a clock device. TCLOCK-005 authenticates them and
// TCLOCK-006 gates the biometric ones.
type IdentificationMethod string

const (
	MethodPIN                IdentificationMethod = "PIN"
	MethodBadge              IdentificationMethod = "BADGE"
	MethodQR                 IdentificationMethod = "QR"
	MethodFace               IdentificationMethod = "FACE"
	MethodFingerprint        IdentificationMethod = "FINGERPRINT"
	MethodSupervisorOverride IdentificationMethod = "SUPERVISOR_OVERRIDE"
)

func (m IdentificationMethod) Valid() bool {
	switch m {
	case MethodPIN, MethodBadge, MethodQR, MethodFace, MethodFingerprint, MethodSupervisorOverride:
		return true
	}
	return false
}

// Biometric reports whether the method requires TCLOCK-006 governance
// before it may be admitted.
func (m IdentificationMethod) Biometric() bool {
	return m == MethodFace || m == MethodFingerprint
}

// TrustCeiling is the maximum clock-trust grade a source class can produce,
// regardless of any individual device's behaviour. It never rises from
// observed behaviour; TCLOCK-008 heartbeat drift can only push a punch's
// confidence beneath it, never above it.
type TrustCeiling string

const (
	TrustCeilingLow    TrustCeiling = "LOW"
	TrustCeilingMedium TrustCeiling = "MEDIUM"
	TrustCeilingHigh   TrustCeiling = "HIGH"
)

func (c TrustCeiling) Valid() bool {
	switch c {
	case TrustCeilingLow, TrustCeilingMedium, TrustCeilingHigh:
		return true
	}
	return false
}

func (c TrustCeiling) rank() int {
	switch c {
	case TrustCeilingLow:
		return 1
	case TrustCeilingMedium:
		return 2
	case TrustCeilingHigh:
		return 3
	}
	return 0
}

// AtMost reports whether c is no more trusted than ceiling.
func (c TrustCeiling) AtMost(ceiling TrustCeiling) bool { return c.rank() <= ceiling.rank() }

var (
	// ErrProfileRejected is the TCLOCK-001 seeded-defect sentinel. A test
	// that probes an unregistered class, a duplicate class or a malformed
	// profile must see this error with the offending field and reason.
	ErrProfileRejected = errors.New("TCLOCK_001_REJECTED")
	// ErrProfileNotRegistered reports a source class with no profile in the
	// registry: enrollment must reject it rather than infer a default.
	ErrProfileNotRegistered = errors.New("clock: source class is not in the profile registry")
)

// ProfileRejection is the stable TCLOCK-001 failure shape.
type ProfileRejection struct {
	Field   string
	Class   string
	Version string
	Reason  string
}

func (r *ProfileRejection) Error() string {
	return fmt.Sprintf("%s: field=%s class=%s version=%s: %s", ErrProfileRejected, r.Field, r.Class, r.Version, r.Reason)
}

// Unwrap exposes the TCLOCK_001_REJECTED sentinel to errors.Is.
func (r *ProfileRejection) Unwrap() error { return ErrProfileRejected }

func profileReject(field, class, version, reason string) error {
	return &ProfileRejection{Field: field, Class: class, Version: version, Reason: reason}
}

// IntegrationProfile is one signed, accepted source class and its governing
// facts. FirstPartner is the named partner or device the 2026-09-28
// hardware review chose for this class; it is descriptive evidence, never
// inferred from a vendor string at enrollment time.
type IntegrationProfile struct {
	Class              SourceClass
	Transport          string
	Authentication     string
	TrustCeiling       TrustCeiling
	PermittedMethods   []IdentificationMethod
	OfflineLimit       time.Duration
	ClockTrustRequired bool
	FirstPartner       string
	Version            string
}

func (p IntegrationProfile) normalizedMethods() []IdentificationMethod {
	out := append([]IdentificationMethod(nil), p.PermittedMethods...)
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func (p IntegrationProfile) Validate() error {
	if !p.Class.Valid() {
		return profileReject("class", string(p.Class), p.Version, "source class is not declared")
	}
	if strings.TrimSpace(p.Transport) == "" {
		return profileReject("transport", string(p.Class), p.Version, "transport is required")
	}
	if strings.TrimSpace(p.Authentication) == "" {
		return profileReject("authentication", string(p.Class), p.Version, "authentication is required")
	}
	if !p.TrustCeiling.Valid() {
		return profileReject("trust_ceiling", string(p.Class), p.Version, "trust ceiling is not declared")
	}
	if len(p.PermittedMethods) == 0 {
		return profileReject("permitted_methods", string(p.Class), p.Version, "at least one identification method is required")
	}
	seen := make(map[IdentificationMethod]bool, len(p.PermittedMethods))
	for _, m := range p.PermittedMethods {
		if !m.Valid() {
			return profileReject("permitted_methods", string(p.Class), p.Version, "identification method is not declared")
		}
		if seen[m] {
			return profileReject("permitted_methods", string(p.Class), p.Version, "identification method is duplicated")
		}
		seen[m] = true
	}
	if p.OfflineLimit < 0 {
		return profileReject("offline_limit", string(p.Class), p.Version, "offline limit cannot be negative")
	}
	if strings.TrimSpace(p.FirstPartner) == "" {
		return profileReject("first_partner", string(p.Class), p.Version, "first named partner or device is required")
	}
	if strings.TrimSpace(p.Version) == "" {
		return profileReject("version", string(p.Class), p.Version, "profile version is required")
	}
	return nil
}

// PermitsMethod reports whether method is admitted for this profile.
func (p IntegrationProfile) PermitsMethod(method IdentificationMethod) bool {
	for _, m := range p.PermittedMethods {
		if m == method {
			return true
		}
	}
	return false
}

func (p IntegrationProfile) digestBody() string {
	methods := p.normalizedMethods()
	parts := make([]string, 0, len(methods))
	for _, m := range methods {
		parts = append(parts, string(m))
	}
	return strings.Join([]string{
		string(p.Class), p.Transport, p.Authentication, string(p.TrustCeiling),
		strings.Join(parts, ","), p.OfflineLimit.String(), fmt.Sprintf("%v", p.ClockTrustRequired),
		p.FirstPartner, p.Version,
	}, "\x00")
}

// ProfileRegistryVersion is the immutable registry contract version.
const ProfileRegistryVersion = "tclock-profile-registry/v1"

// ProfileRegistry is an immutable, validated set of integration profiles,
// at most one per source class.
type ProfileRegistry struct {
	Version  string
	Profiles []IntegrationProfile
	Digest   string
}

// NewProfileRegistry copies, validates and digests the supplied profiles. A
// class named more than once, or a profile that fails validation, rejects
// the whole registry: a signed registry has no partial state.
func NewProfileRegistry(profiles []IntegrationProfile) (ProfileRegistry, error) {
	if len(profiles) == 0 {
		return ProfileRegistry{}, profileReject("profiles", "", ProfileRegistryVersion, "registry requires at least one profile")
	}
	copied := append([]IntegrationProfile(nil), profiles...)
	seen := make(map[SourceClass]bool, len(copied))
	for i := range copied {
		copied[i].PermittedMethods = copied[i].normalizedMethods()
		if err := copied[i].Validate(); err != nil {
			return ProfileRegistry{}, err
		}
		if seen[copied[i].Class] {
			return ProfileRegistry{}, profileReject("class", string(copied[i].Class), copied[i].Version, "source class is registered more than once")
		}
		seen[copied[i].Class] = true
	}
	sort.Slice(copied, func(i, j int) bool { return copied[i].Class < copied[j].Class })
	var body strings.Builder
	for _, p := range copied {
		body.WriteString(p.digestBody())
		body.WriteString("\x1e")
	}
	sum := sha256.Sum256([]byte(body.String()))
	return ProfileRegistry{Version: ProfileRegistryVersion, Profiles: copied, Digest: "sha256:" + hex.EncodeToString(sum[:])}, nil
}

// Profile returns the registered profile for class, if any.
func (r ProfileRegistry) Profile(class SourceClass) (IntegrationProfile, bool) {
	for _, p := range r.Profiles {
		if p.Class == class {
			return p, true
		}
	}
	return IntegrationProfile{}, false
}

// Accepts is the TCLOCK-002 enrollment gate: it rejects an undeclared class
// and, when method is non-empty, one this profile does not permit.
func (r ProfileRegistry) Accepts(class SourceClass, method IdentificationMethod) error {
	profile, ok := r.Profile(class)
	if !ok {
		return fmt.Errorf("%w: class=%s", ErrProfileNotRegistered, class)
	}
	if method != "" && !profile.PermitsMethod(method) {
		return profileReject("permitted_methods", string(class), r.Version, "identification method is not permitted by the registered profile")
	}
	return nil
}

// ProfileExplanation is the audit-safe summary of a registered profile.
type ProfileExplanation struct {
	Class              SourceClass
	TrustCeiling       TrustCeiling
	MethodCount        int
	ClockTrustRequired bool
	FirstPartner       string
	Digest             string
}

// ExplainProfile validates and summarizes one profile for operator display.
func ExplainProfile(p IntegrationProfile) (ProfileExplanation, error) {
	if err := p.Validate(); err != nil {
		return ProfileExplanation{}, err
	}
	sum := sha256.Sum256([]byte(p.digestBody()))
	return ProfileExplanation{
		Class: p.Class, TrustCeiling: p.TrustCeiling, MethodCount: len(p.normalizedMethods()),
		ClockTrustRequired: p.ClockTrustRequired, FirstPartner: p.FirstPartner,
		Digest: "sha256:" + hex.EncodeToString(sum[:]),
	}, nil
}
