package trust

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// SubjectKind is the kind of authenticated actor a Principal describes. Zero
// is reserved for "unspecified", which is never a valid authenticated result.
type SubjectKind uint8

// Authenticated actor kinds. Human Capital Management Suite authenticates humans, first-party
// services, autonomous agents and third-party integrations with the same
// machinery; the kind changes what governance may authorize, never how the
// principal is derived.
const (
	SubjectKindUnspecified SubjectKind = iota
	SubjectKindHuman
	SubjectKindService
	SubjectKindAgent
	SubjectKindIntegration
)

// String returns the canonical wire spelling of the subject kind.
func (k SubjectKind) String() string {
	switch k {
	case SubjectKindHuman:
		return "human"
	case SubjectKindService:
		return "service"
	case SubjectKindAgent:
		return "agent"
	case SubjectKindIntegration:
		return "integration"
	case SubjectKindUnspecified:
		return "unspecified"
	default:
		return "invalid(" + strconv.Itoa(int(k)) + ")"
	}
}

// valid reports whether k is a concrete authenticated actor kind.
func (k SubjectKind) valid() bool {
	return k >= SubjectKindHuman && k <= SubjectKindIntegration
}

// AuthenticationMethod records how the credential was proven. It is part of
// the trusted context because step-up and re-authentication policy read it.
type AuthenticationMethod uint8

// Supported authentication methods. P1A verifies signed bearer tokens; the
// mutual-TLS peer identity value exists so that a workload-identity verifier
// added under TRUST-011 does not have to widen this enumeration.
const (
	AuthenticationMethodUnspecified AuthenticationMethod = iota
	AuthenticationMethodBearerToken
	AuthenticationMethodMutualTLS
)

// String returns the canonical wire spelling of the authentication method.
func (m AuthenticationMethod) String() string {
	switch m {
	case AuthenticationMethodBearerToken:
		return "bearer_token"
	case AuthenticationMethodMutualTLS:
		return "mutual_tls"
	case AuthenticationMethodUnspecified:
		return "unspecified"
	default:
		return "invalid(" + strconv.Itoa(int(m)) + ")"
	}
}

// valid reports whether m is a concrete authentication method.
func (m AuthenticationMethod) valid() bool {
	return m == AuthenticationMethodBearerToken || m == AuthenticationMethodMutualTLS
}

// Assurance is the session assurance level the authentication event reached.
// A capability that requires step-up compares against this value; it is never
// asserted by the caller.
type Assurance uint8

// Session assurance levels, ordered. Higher values satisfy lower
// requirements; see [Assurance.AtLeast].
const (
	AssuranceUnspecified Assurance = iota
	AssuranceLow
	AssuranceSubstantial
	AssuranceHigh
)

// String returns the canonical wire spelling of the assurance level.
func (a Assurance) String() string {
	switch a {
	case AssuranceLow:
		return "low"
	case AssuranceSubstantial:
		return "substantial"
	case AssuranceHigh:
		return "high"
	case AssuranceUnspecified:
		return "unspecified"
	default:
		return "invalid(" + strconv.Itoa(int(a)) + ")"
	}
}

// AtLeast reports whether a satisfies the required assurance level. An
// unspecified requirement is never satisfied, so a missing policy value fails
// closed.
func (a Assurance) AtLeast(required Assurance) bool {
	if required == AssuranceUnspecified || a == AssuranceUnspecified {
		return false
	}
	return a >= required
}

// PrincipalSpec is the verified authentication result a [Verifier] hands to
// [NewPrincipal]. Every field is server-resolved: a verifier fills it from a
// validated credential and from server-side directory resolution, never from
// a value the caller chose to send alongside the credential.
type PrincipalSpec struct {
	// Tenant is the canonical tenant slug the credential was issued for.
	Tenant values.TenantId
	// Subject is the opaque, stable, tenant-scoped subject identifier.
	Subject string
	// ClientID is the stable client identity resolved from a verified
	// machine credential. It remains constant when access tokens rotate.
	ClientID string
	// SubjectKind classifies the authenticated actor.
	SubjectKind SubjectKind
	// OrganizationScopeID is the organization scope the subject acts within.
	OrganizationScopeID string
	// Roles are the resolved role identifiers. Order is not significant;
	// NewPrincipal sorts and de-duplicates them.
	Roles []string
	// AuthorityRefs are references to the authority grants (delegated
	// authority, position authority, entitlement grants) that governance
	// composes over. They are references, never inline policy.
	AuthorityRefs []string
	// Purposes are the purposes of processing the subject is authorized for.
	// The first entry, after sorting, is the default purpose.
	Purposes []string
	// AuthenticationMethod records how the credential was proven.
	AuthenticationMethod AuthenticationMethod
	// Assurance is the assurance level the authentication event reached.
	Assurance Assurance
	// SessionRef references the server-side session record.
	SessionRef string
	// DelegationRefs reference active delegation grants, if any.
	DelegationRefs []string
	// Confirmation carries the token's sender-constraint coordinates (DPoP
	// jkt, mTLS x5t#S256) when the credential is sender-constrained. Empty
	// for bearer credentials. Enforcement points match proof material
	// against it; it never authorizes by itself.
	Confirmation map[string]string
	// IssuedAt and ExpiresAt bound the credential's validity.
	IssuedAt  time.Time
	ExpiresAt time.Time
	// CredentialDigest is a digest of the verified credential. It binds the
	// evidence identifier to the exact credential that was presented; the raw
	// credential itself never enters a Principal.
	CredentialDigest string
}

// Principal validation errors. All are matchable with errors.Is.
var (
	ErrPrincipalTenant          = errors.New("trust: principal tenant is not a canonical tenant slug")
	ErrPrincipalSubject         = errors.New("trust: principal subject is empty or not printable ASCII")
	ErrPrincipalSubjectKind     = errors.New("trust: principal subject kind is unspecified")
	ErrPrincipalAuthnMethod     = errors.New("trust: principal authentication method is unspecified")
	ErrPrincipalAssurance       = errors.New("trust: principal assurance is unspecified")
	ErrPrincipalSession         = errors.New("trust: principal session reference is empty")
	ErrPrincipalValidity        = errors.New("trust: principal validity window is empty or inverted")
	ErrPrincipalCredentialBound = errors.New("trust: principal is not bound to a credential digest")
)

// Principal is one immutable, server-derived authenticated identity. It is
// created only by [NewPrincipal], only from a [PrincipalSpec] a [Verifier]
// produced, and it is never mutated afterwards.
type Principal struct {
	tenant               values.TenantId
	subject              string
	clientID             string
	subjectKind          SubjectKind
	organizationScopeID  string
	roles                []string
	authorityRefs        []string
	purposes             []string
	authenticationMethod AuthenticationMethod
	assurance            Assurance
	sessionRef           string
	delegationRefs       []string
	confirmation         map[string]string
	issuedAt             time.Time
	expiresAt            time.Time
	credentialDigest     string
	evidenceID           string
	fingerprint          string
}

// NewPrincipal validates a verified authentication result and returns the
// immutable principal for it, deriving the authentication evidence identifier
// and the trusted-context fingerprint server-side.
//
// Callers outside a [Verifier] implementation have no business calling this:
// the transport layer never does, and a handler that wants the current
// principal reads it from the context with [FromContext].
func NewPrincipal(spec PrincipalSpec) (*Principal, error) {
	if err := spec.Tenant.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPrincipalTenant, err)
	}
	if !printableASCII(spec.Subject, 1, 200) {
		return nil, ErrPrincipalSubject
	}
	if spec.ClientID != "" && !printableASCII(spec.ClientID, 1, 200) {
		return nil, ErrPrincipalSubject
	}
	if !spec.SubjectKind.valid() {
		return nil, ErrPrincipalSubjectKind
	}
	if !spec.AuthenticationMethod.valid() {
		return nil, ErrPrincipalAuthnMethod
	}
	if spec.Assurance == AssuranceUnspecified || spec.Assurance > AssuranceHigh {
		return nil, ErrPrincipalAssurance
	}
	if !printableASCII(spec.SessionRef, 1, 200) {
		return nil, ErrPrincipalSession
	}
	if spec.IssuedAt.IsZero() || spec.ExpiresAt.IsZero() || !spec.ExpiresAt.After(spec.IssuedAt) {
		return nil, ErrPrincipalValidity
	}
	if !printableASCII(spec.CredentialDigest, 1, 200) {
		return nil, ErrPrincipalCredentialBound
	}

	p := &Principal{
		tenant:               spec.Tenant,
		subject:              spec.Subject,
		clientID:             spec.ClientID,
		subjectKind:          spec.SubjectKind,
		organizationScopeID:  spec.OrganizationScopeID,
		roles:                normalizeSet(spec.Roles),
		authorityRefs:        normalizeSet(spec.AuthorityRefs),
		purposes:             normalizeSet(spec.Purposes),
		authenticationMethod: spec.AuthenticationMethod,
		assurance:            spec.Assurance,
		sessionRef:           spec.SessionRef,
		delegationRefs:       normalizeSet(spec.DelegationRefs),
		confirmation:         cloneConfirmation(spec.Confirmation),
		issuedAt:             spec.IssuedAt.UTC(),
		expiresAt:            spec.ExpiresAt.UTC(),
		credentialDigest:     spec.CredentialDigest,
	}
	p.fingerprint = p.computeFingerprint()
	p.evidenceID = "ev:authn:" + p.fingerprint[:32]
	return p, nil
}

// Tenant returns the canonical tenant the principal is scoped to.
func (p *Principal) Tenant() values.TenantId { return p.tenant }

// Subject returns the opaque stable subject identifier.
func (p *Principal) Subject() string { return p.subject }

// ClientID returns the stable, verifier-derived machine client identity, or
// the empty string when the verified credential is not a machine credential.
func (p *Principal) ClientID() string { return p.clientID }

// SubjectKind returns the authenticated actor kind.
func (p *Principal) SubjectKind() SubjectKind { return p.subjectKind }

// OrganizationScopeID returns the organization scope the subject acts within.
func (p *Principal) OrganizationScopeID() string { return p.organizationScopeID }

// Roles returns a copy of the resolved role identifiers.
func (p *Principal) Roles() []string { return slices.Clone(p.roles) }

// HasRole reports whether the principal holds role.
func (p *Principal) HasRole(role string) bool { return slices.Contains(p.roles, role) }

// AuthorityRefs returns a copy of the authority grant references.
func (p *Principal) AuthorityRefs() []string { return slices.Clone(p.authorityRefs) }

// Purposes returns a copy of the authorized purposes of processing.
func (p *Principal) Purposes() []string { return slices.Clone(p.purposes) }

// DefaultPurpose returns the purpose used when a caller does not name one, or
// the empty string when the principal has no authorized purpose.
func (p *Principal) DefaultPurpose() string {
	if len(p.purposes) == 0 {
		return ""
	}
	return p.purposes[0]
}

// AuthorizesPurpose reports whether purpose is one of the principal's
// authorized purposes of processing.
func (p *Principal) AuthorizesPurpose(purpose string) bool {
	return slices.Contains(p.purposes, purpose)
}

// AuthenticationMethod returns how the credential was proven.
func (p *Principal) AuthenticationMethod() AuthenticationMethod {
	return p.authenticationMethod
}

// Assurance returns the assurance level the authentication event reached.
func (p *Principal) Assurance() Assurance { return p.assurance }

// SessionRef returns the server-side session reference.
func (p *Principal) SessionRef() string { return p.sessionRef }

// DelegationRefs returns a copy of the active delegation references.
func (p *Principal) DelegationRefs() []string { return slices.Clone(p.delegationRefs) }

// Confirmation returns a copy of the token's sender-constraint coordinates,
// or nil for a Bearer [REDACTED] Enforcement points match DPoP or mutual-TLS
// proof material against it.
func (p *Principal) Confirmation() map[string]string { return maps.Clone(p.confirmation) }

// cloneConfirmation copies confirmation coordinates so the immutable
// principal never aliases caller-owned maps.
func cloneConfirmation(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	return maps.Clone(in)
}

// IssuedAt returns the credential issuance instant in UTC.
func (p *Principal) IssuedAt() time.Time { return p.issuedAt }

// ExpiresAt returns the credential expiry instant in UTC.
func (p *Principal) ExpiresAt() time.Time { return p.expiresAt }

// CredentialDigest returns the digest of the verified credential. The raw
// credential is never retained.
func (p *Principal) CredentialDigest() string { return p.credentialDigest }

// EvidenceID returns the durable authentication-evidence identifier derived
// from the trusted fields. Two transports that verified the same credential
// derive the same identifier, which is what makes cross-transport evidence
// parity checkable.
func (p *Principal) EvidenceID() string { return p.evidenceID }

// Fingerprint returns the canonical hex digest over every trusted field. It is
// the equality test used to prove that gRPC and the HTTP edge constructed
// identical trusted context.
func (p *Principal) Fingerprint() string { return p.fingerprint }

// String returns a redacted, log-safe description. It never contains the
// credential, the credential digest or role/authority detail.
func (p *Principal) String() string {
	return fmt.Sprintf("principal(tenant=%s subject=%s kind=%s authn=%s assurance=%s evidence=%s)",
		p.tenant, p.subject, p.subjectKind, p.authenticationMethod, p.assurance, p.evidenceID)
}

// computeFingerprint hashes every trusted field with length-prefixed framing so
// that no two distinct principals can collide by field-boundary ambiguity.
func (p *Principal) computeFingerprint() string {
	h := sha256.New()
	write := func(label, v string) {
		fmt.Fprintf(h, "%s=%d:%s;", label, len(v), v)
	}
	writeSet := func(label string, vs []string) {
		fmt.Fprintf(h, "%s[%d]:", label, len(vs))
		for _, v := range vs {
			write("", v)
		}
		h.Write([]byte(";"))
	}
	writeMap := func(label string, vs map[string]string) {
		keys := make([]string, 0, len(vs))
		for k := range vs {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		fmt.Fprintf(h, "%s{%d}:", label, len(keys))
		for _, k := range keys {
			write(k, vs[k])
		}
		h.Write([]byte(";"))
	}
	write("tenant", p.tenant.String())
	write("subject", p.subject)
	write("client", p.clientID)
	write("kind", p.subjectKind.String())
	write("orgscope", p.organizationScopeID)
	writeSet("roles", p.roles)
	writeSet("authority", p.authorityRefs)
	writeSet("purposes", p.purposes)
	write("authn", p.authenticationMethod.String())
	write("assurance", p.assurance.String())
	write("session", p.sessionRef)
	writeSet("delegation", p.delegationRefs)
	writeMap("confirmation", p.confirmation)
	write("iat", strconv.FormatInt(p.issuedAt.Unix(), 10))
	write("exp", strconv.FormatInt(p.expiresAt.Unix(), 10))
	write("credential", p.credentialDigest)
	return hex.EncodeToString(h.Sum(nil))
}

// normalizeSet sorts, de-duplicates and drops empty entries so that a
// principal's set-valued fields have exactly one canonical form.
func normalizeSet(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v != "" {
			out = append(out, v)
		}
	}
	if len(out) == 0 {
		return nil
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// printableASCII reports whether s is between min and max bytes long and
// contains only printable ASCII. Trusted identifiers are opaque canonical IDs;
// rejecting control characters and non-ASCII here keeps them safe to place in
// logs, headers and evidence records without escaping.
func printableASCII(s string, minLen, maxLen int) bool {
	if len(s) < minLen || len(s) > maxLen {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return !strings.ContainsAny(s, " \t")
}
