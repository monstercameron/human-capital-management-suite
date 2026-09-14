// Package authority binds exactly one P1B authority topology per tenant.
//
// A P1B authority amendment is the immutable, digest-backed decision
// next-steps.md requires before any P1B write path is built: either the
// incumbent stays authoritative for the bound fields (external authority,
// observed state is never emitted as a local domain fact) or the partner
// explicitly transferred those fields (transferred authority, local writes
// only under the signed grant, inside the effective window).
//
// The package is deliberately kernel-pure: it consumes tenant, field and
// grant identifiers supplied by governance, transaction and application
// callers, but performs no storage, clock reads or network calls. Fact truth
// vocabulary is owned by internal/domains/evidence; this package only
// selects which truth an amendment permits.
//
// NEXT-006: Select and bind exactly one P1B authority topology.
package authority

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/domains/evidence"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/canonicalbytes"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

const (
	amendmentSchema    = "hcmnext.governance.authority.Amendment"
	amendmentSchemaVer = 1
)

// Topology is the single P1B write model an amendment selects. The zero
// value is never legal: an amendment that does not say which world it
// lives in authorizes nothing.
type Topology string

const (
	TopologyExternalAuthority    Topology = "EXTERNAL_AUTHORITY"
	TopologyTransferredAuthority Topology = "TRANSFERRED_AUTHORITY"
)

// Valid reports whether t is one of the two selectable topologies.
func (t Topology) Valid() bool {
	return t == TopologyExternalAuthority || t == TopologyTransferredAuthority
}

// Authority truth vocabulary. The contract lives in
// internal/domains/evidence; these aliases keep call sites readable
// without duplicating the wire tokens.
const (
	KindLocalAuthoritative  = evidence.AuthorityLocal
	KindExternalObservation = evidence.AuthorityExternalObservation
)

// Amendment errors. All are matchable with errors.Is; denial text never
// enumerates the amendment's bound fields, so a denial against one tenant
// leaks nothing about what another tenant may do.
var (
	ErrInvalidAmendment  = errors.New("authority: invalid amendment")
	ErrAmendmentInactive = errors.New("authority: amendment is not active")
	ErrTopologyMismatch  = errors.New("authority: field or operation is outside the bound topology")
	ErrLocalWriteDenied  = errors.New("authority: local write denied")
	ErrDigestMismatch    = errors.New("authority: digest does not match amendment content")
)

// Amendment is one immutable P1B authority decision: which tenant, which
// single topology, which exact fields and operations, over which effective
// window, under which partner grant. Digest pins the binding; VerifyDigest
// re-derives it rather than trusting the carrier.
type Amendment struct {
	AmendmentID string          `json:"amendment_id"`
	Tenant      values.TenantId `json:"tenant"`
	Topology    Topology        `json:"topology"`
	Fields      []string        `json:"fields"`
	Operations  []string        `json:"operations"`
	From        time.Time       `json:"from"`
	Until       time.Time       `json:"until"`
	GrantRef    string          `json:"grant_ref"`
	Revoked     bool            `json:"revoked"`
	Digest      string          `json:"digest"`
}

// Bind validates a proposed amendment and returns it with its canonical
// digest. Exactly one topology must be selected; tenant, fields,
// operations and an explicit expiry window are required; a transferred
// amendment requires the partner's signed grant while an external
// amendment must not carry one (a grant on an external amendment is an
// ambiguity, not belt-and-braces).
func Bind(a Amendment) (Amendment, error) {
	if err := checkProposal(a); err != nil {
		return Amendment{}, err
	}
	out := clone(a)
	out.Digest = digestAmendment(out)
	return out, nil
}

func checkProposal(a Amendment) error {
	switch {
	case strings.TrimSpace(a.AmendmentID) == "":
		return fmt.Errorf("%w: amendment id is required", ErrInvalidAmendment)
	case a.Tenant.Validate() != nil:
		return fmt.Errorf("%w: %v", ErrInvalidAmendment, a.Tenant.Validate())
	case !a.Topology.Valid():
		return fmt.Errorf("%w: topology must be exactly EXTERNAL_AUTHORITY or TRANSFERRED_AUTHORITY", ErrInvalidAmendment)
	case len(a.Fields) == 0:
		return fmt.Errorf("%w: at least one bound field is required", ErrInvalidAmendment)
	case len(a.Operations) == 0:
		return fmt.Errorf("%w: at least one bound operation is required", ErrInvalidAmendment)
	case a.From.IsZero() || a.Until.IsZero():
		return fmt.Errorf("%w: explicit effective window with expiry is required", ErrInvalidAmendment)
	case !a.Until.After(a.From):
		return fmt.Errorf("%w: expiry must be after the effective start", ErrInvalidAmendment)
	}
	if err := checkNames("field", a.Fields); err != nil {
		return err
	}
	if err := checkNames("operation", a.Operations); err != nil {
		return err
	}
	if a.Topology == TopologyTransferredAuthority && strings.TrimSpace(a.GrantRef) == "" {
		return fmt.Errorf("%w: transferred authority requires the explicit partner grant", ErrInvalidAmendment)
	}
	if a.Topology == TopologyExternalAuthority && strings.TrimSpace(a.GrantRef) != "" {
		return fmt.Errorf("%w: external authority must not carry a transfer grant", ErrInvalidAmendment)
	}
	return nil
}

func checkNames(kind string, names []string) error {
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		if strings.TrimSpace(name) == "" || name != strings.TrimSpace(name) {
			return fmt.Errorf("%w: bound %s names must be non-blank and unpadded", ErrInvalidAmendment, kind)
		}
		if seen[name] {
			return fmt.Errorf("%w: bound %s %q is named twice", ErrInvalidAmendment, kind, name)
		}
		seen[name] = true
	}
	return nil
}

// VerifyDigest recomputes the amendment digest from its canonical content.
// A binding is never trusted on the word of the digest it carries.
func (a Amendment) VerifyDigest() error {
	if a.Digest == "" {
		return fmt.Errorf("%w: amendment carries no digest", ErrDigestMismatch)
	}
	if got := digestAmendment(a); got != a.Digest {
		return fmt.Errorf("%w: amendment %q records %q but its content hashes to %q",
			ErrDigestMismatch, a.AmendmentID, a.Digest, got)
	}
	return nil
}

// Active fails closed outside the effective window or once revoked. The
// window end is exclusive: at the expiry instant the amendment is already
// dead, never dying.
func (a Amendment) Active(at time.Time) error {
	if a.Revoked {
		return fmt.Errorf("%w: amendment %q is revoked", ErrAmendmentInactive, a.AmendmentID)
	}
	if at.Before(a.From) || !at.Before(a.Until) {
		return fmt.Errorf("%w: amendment %q is not effective at the attempted instant", ErrAmendmentInactive, a.AmendmentID)
	}
	return nil
}

// Classify returns the truth an amendment permits for one field: locally
// authoritative facts only under a bound transfer, external observation
// otherwise. An unbound field is a topology mismatch, never a guess.
func (a Amendment) Classify(field string) (evidence.AuthorityKind, error) {
	if err := a.VerifyDigest(); err != nil {
		return evidence.AuthorityUnspecified, err
	}
	if !boundName(a.Fields, field) {
		return evidence.AuthorityUnspecified, fmt.Errorf("%w: field is outside the bound topology", ErrTopologyMismatch)
	}
	if a.Topology == TopologyTransferredAuthority {
		return evidence.AuthorityLocal, nil
	}
	return evidence.AuthorityExternalObservation, nil
}

// AdmitLocalWrite gates one local write the way the P1B cell must: digest,
// liveness, requesting-tenant match, single-topology admission, exact
// field/operation binding and, for transfers, presentation of the bound
// grant. The tenant check is defense in depth against a confused deputy
// presenting another tenant's amendment. Every denial is a typed error a
// caller can match; none narrates the amendment's contents.
func (a Amendment) AdmitLocalWrite(field, operation string, at time.Time, tenant values.TenantId, grant string) error {
	if err := a.VerifyDigest(); err != nil {
		return err
	}
	if err := a.Active(at); err != nil {
		return err
	}
	if tenant.Validate() != nil || tenant != a.Tenant {
		return fmt.Errorf("%w: amendment does not cover the requesting tenant", ErrLocalWriteDenied)
	}
	if a.Topology != TopologyTransferredAuthority {
		return fmt.Errorf("%w: external authority keeps incumbent state as observation only", ErrLocalWriteDenied)
	}
	if !boundName(a.Fields, field) || !boundName(a.Operations, operation) {
		return fmt.Errorf("%w: field or operation is outside the bound topology", ErrTopologyMismatch)
	}
	if strings.TrimSpace(grant) == "" || grant != a.GrantRef {
		return fmt.Errorf("%w: transfer requires presentation of the bound partner grant", ErrLocalWriteDenied)
	}
	return nil
}

// Canonical returns the canonical byte encoding, or nil when invalid.
func (a Amendment) Canonical() []byte {
	if checkProposal(canonicalView(a)) != nil {
		return nil
	}
	raw, err := canonicalbytes.New(amendmentSchema, amendmentSchemaVer).
		String("amendment_id", a.AmendmentID).
		String("tenant", string(a.Tenant)).
		String("topology", string(a.Topology)).
		SortedStrings("fields", a.Fields).
		SortedStrings("operations", a.Operations).
		String("from", a.From.UTC().Format(time.RFC3339)).
		String("until", a.Until.UTC().Format(time.RFC3339)).
		String("grant_ref", a.GrantRef).
		Bool("revoked", a.Revoked).
		Bytes()
	if err != nil {
		return nil
	}
	return raw
}

// Explain returns a bounded summary suitable for an operator log. It names
// the decision and its envelope, never the grant.
func (a Amendment) Explain() string {
	return fmt.Sprintf("authority amendment v%d id=%s tenant=%s topology=%s fields=%d operations=%d revoked=%t digest=%s",
		amendmentSchemaVer, a.AmendmentID, a.Tenant, a.Topology, len(a.Fields), len(a.Operations), a.Revoked, a.Digest)
}

func canonicalView(a Amendment) Amendment { a.Digest = ""; return a }

func digestAmendment(a Amendment) string {
	raw := canonicalView(a).Canonical()
	if raw == nil {
		return ""
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func boundName(names []string, want string) bool {
	for _, name := range names {
		if name == want {
			return true
		}
	}
	return false
}

func clone(a Amendment) Amendment {
	a.Fields = append([]string(nil), a.Fields...)
	a.Operations = append([]string(nil), a.Operations...)
	return a
}
