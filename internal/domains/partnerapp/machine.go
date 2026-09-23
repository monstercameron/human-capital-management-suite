package partnerapp

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"
)

// Machine-client registry port for INTAPI-001. The durable registry lives
// behind this interface (internal/data/truststore); the token endpoint
// speaks only this port, never storage. Tenant is the tenant key, matching
// the string-tenant identities this package already carries.
//
// This is the REFACTOR the todo calls "promote WorkloadIdentityManager onto
// the durable registry": machine-client authentication policy now lives in
// this package and reads durable registrations through the port, instead of
// trusting whatever authority a Bearer [REDACTED] claims.

// Machine-client lifecycle states. The registry stores these verbatim.
const (
	MachineClientActive    = "active"
	MachineClientSuspended = "suspended"
	MachineClientRevoked   = "revoked"
)

var (
	ErrMachineClientUnknown   = errors.New("partnerapp: unknown machine client")
	ErrMachineClientSuspended = errors.New("partnerapp: machine client is suspended")
	ErrMachineClientRevoked   = errors.New("partnerapp: machine client is revoked")
	ErrMachineClientExpired   = errors.New("partnerapp: machine client registration has expired")
	ErrMachineClientNotActive = errors.New("partnerapp: machine client registration is not yet valid")
	ErrMachineClientIPDenied  = errors.New("partnerapp: source address is not on the client allowlist")
	ErrMachineKeyUnknown      = errors.New("partnerapp: unknown client key")
	ErrMachineKeyRevoked      = errors.New("partnerapp: client key is revoked")
	ErrMachineKeyNotActive    = errors.New("partnerapp: client key is outside its validity window")
)

// MachineClient is the durable registration the token endpoint joins a
// client assertion against.
type MachineClient struct {
	Tenant      string
	ClientID    string
	Owner       string
	Status      string
	Scopes      []string
	Purpose     string
	DataDomains []string
	FieldSubset []string
	IPAllowlist []string
	// CertFingerprint is the lowercase hex SHA-256 of the DER client
	// certificate the terminator verified. Empty means no mTLS binding.
	CertFingerprint string
	ExpiresAt       time.Time
	Revoked         bool
	LastUsedAt      time.Time
}

// MachineClientKey is one assertion-verification key of a client. JWK carries
// the public key only; private material never leaves the client.
type MachineClientKey struct {
	ClientID  string
	KID       string
	Alg       string
	JWK       []byte
	NotBefore time.Time
	ExpiresAt time.Time
	Revoked   bool
}

// ClientRegistry is the durable machine-client storefront.
type ClientRegistry interface {
	LoadClient(ctx context.Context, tenant, clientID string) (MachineClient, error)
	LoadClientKeys(ctx context.Context, tenant, clientID string) ([]MachineClientKey, error)
	RecordClientUse(ctx context.Context, tenant, clientID string, now time.Time) error
}

// ClientUseRequest is the authentication instant the endpoint resolved.
type ClientUseRequest struct {
	// SourceIP is the remote address the token request arrived from, in the
	// terminator-verified form the edge passes down. Empty means unknown,
	// which fails closed whenever the client names any allowlist.
	SourceIP string
	At       time.Time
}

// AuthorizeClientUse decides whether a registered client may authenticate
// now from sourceIP. Lifecycle state, expiry and the IP allowlist are all
// enforced here so every caller shares one decision.
func AuthorizeClientUse(client MachineClient, req ClientUseRequest) error {
	if strings.TrimSpace(client.ClientID) == "" {
		return fmt.Errorf("%w: missing client", ErrMachineClientUnknown)
	}
	if req.At.IsZero() {
		return fmt.Errorf("%w: timestamp is required", ErrMachineClientUnknown)
	}
	switch client.Status {
	case MachineClientActive:
	case MachineClientSuspended:
		return fmt.Errorf("%w: %s", ErrMachineClientSuspended, client.ClientID)
	case MachineClientRevoked:
		return fmt.Errorf("%w: %s", ErrMachineClientRevoked, client.ClientID)
	default:
		return fmt.Errorf("%w: status %q", ErrMachineClientUnknown, client.Status)
	}
	if client.Revoked {
		return fmt.Errorf("%w: %s", ErrMachineClientRevoked, client.ClientID)
	}
	if !client.ExpiresAt.IsZero() && !req.At.Before(client.ExpiresAt) {
		return fmt.Errorf("%w: %s", ErrMachineClientExpired, client.ClientID)
	}
	if err := checkAllowlist(client.IPAllowlist, req.SourceIP); err != nil {
		return fmt.Errorf("%w: %s", ErrMachineClientIPDenied, client.ClientID)
	}
	return nil
}

// checkAllowlist enforces the client's source-address policy. An empty
// allowlist admits nothing: fail closed, never open.
func checkAllowlist(allowlist []string, source string) error {
	addr, err := netip.ParseAddr(strings.TrimSpace(source))
	if err != nil || !addr.IsValid() {
		return errors.New("unknown or unparsable source")
	}
	addr = addr.Unmap()
	for _, entry := range allowlist {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if strings.Contains(entry, "/") {
			prefix, err := netip.ParsePrefix(entry)
			if err != nil {
				continue
			}
			if prefix.Contains(addr) {
				return nil
			}
			continue
		}
		if other, err := netip.ParseAddr(entry); err == nil && other.Unmap() == addr {
			return nil
		}
	}
	return errors.New("no allowlist entry covers the source")
}

// GrantedScopesFor resolves the scope grant a capability call is
// intersected under: the registered client's scopes, re-checked for
// lifecycle at the instant of the call so a suspension or revocation
// between issuance and use fails closed. Unknown clients resolve to no
// scopes (fail closed, never open); callers distinguish "no grant" from
// storage failure by the error.
func GrantedScopesFor(ctx context.Context, reg ClientRegistry, tenant, clientID string, at time.Time, sourceIP string) ([]string, error) {
	client, err := reg.LoadClient(ctx, tenant, clientID)
	if err != nil {
		return nil, err
	}
	if err := AuthorizeClientUse(client, ClientUseRequest{SourceIP: sourceIP, At: at}); err != nil {
		return nil, err
	}
	return append([]string(nil), client.Scopes...), nil
}

// GrantsWrite reports whether a scope grant makes its client write-capable.
// Scopes read `<resource>.read` or `<resource>.write`; anything else is
// read-only by default, because an unknown scope must never silently
// confer write capability.
func GrantsWrite(scopes []string) bool {
	for _, s := range scopes {
		if s == "write" || strings.HasSuffix(s, ".write") {
			return true
		}
	}
	return false
}

// SelectClientKey picks the registry key an assertion names. The key must
// be unrevoked and inside its validity window at now; selection never falls
// back to another key when the named one is unusable, because silent
// fallback would authenticate under a different key than the client proved.
func SelectClientKey(keys []MachineClientKey, kid string, now time.Time) (MachineClientKey, error) {
	for _, k := range keys {
		if k.KID != kid {
			continue
		}
		if k.Revoked {
			return MachineClientKey{}, fmt.Errorf("%w: %s", ErrMachineKeyRevoked, kid)
		}
		if now.Before(k.NotBefore) {
			return MachineClientKey{}, fmt.Errorf("%w: %s", ErrMachineKeyNotActive, kid)
		}
		if !k.ExpiresAt.IsZero() && !now.Before(k.ExpiresAt) {
			return MachineClientKey{}, fmt.Errorf("%w: %s", ErrMachineKeyNotActive, kid)
		}
		if len(k.JWK) == 0 {
			return MachineClientKey{}, fmt.Errorf("%w: %s", ErrMachineKeyUnknown, kid)
		}
		return k, nil
	}
	return MachineClientKey{}, fmt.Errorf("%w: %s", ErrMachineKeyUnknown, kid)
}
