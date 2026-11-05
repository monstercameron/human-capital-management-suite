package transport

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// SignallingKindName is one message kind of call signalling.
type SignallingKindName string

const (
	SignallingOffer         SignallingKindName = "offer"
	SignallingAnswer        SignallingKindName = "answer"
	SignallingCandidates    SignallingKindName = "candidates"
	SignallingRenegotiation SignallingKindName = "renegotiation"
	SignallingRinging       SignallingKindName = "ringing"
	SignallingHangUp        SignallingKindName = "hang-up"
)

// SignallingKind is one message kind with its purpose. Every kind requires
// the conversation's tenant and active-membership check, the same check
// that admits a posted message: signalling is never a weaker authority
// than posting.
type SignallingKind struct {
	Name               SignallingKindName
	Purpose            string
	RequiresMembership bool
}

// CallSignallingKinds returns the six signalling kinds in call order.
func CallSignallingKinds() []SignallingKind {
	return []SignallingKind{
		{Name: SignallingOffer, Purpose: "The caller's media proposal: call identity, conversation, audio/video intent and the first session description.", RequiresMembership: true},
		{Name: SignallingAnswer, Purpose: "The callee's acceptance with their session description, or a decline that ends the attempt.", RequiresMembership: true},
		{Name: SignallingCandidates, Purpose: "Connectivity candidates exchanged during setup. Relay-only by default, so no candidate carries a peer's address.", RequiresMembership: true},
		{Name: SignallingRenegotiation, Purpose: "Mid-call media changes: adding or dropping video, starting or stopping screen sharing.", RequiresMembership: true},
		{Name: SignallingRinging, Purpose: "Ring, accept and decline for one-to-one calls. The server suppresses ringing for quiet hours and mutes before any client is told.", RequiresMembership: true},
		{Name: SignallingHangUp, Purpose: "Leave for anyone, end-for-all for a channel manager. Hang-up is honoured even when media already failed.", RequiresMembership: true},
	}
}

// TunnelFindings is what the existing authenticated tunnel needs to carry
// signalling. It describes the cell's gRPC-over-WebSocket bridge as found:
// TunnelPath, the closed tunnelAllowedServices set, the library keepalive
// defaults and the double admission (upgrade-time plus per-RPC).
type TunnelFindings struct {
	Path              string
	Service           string
	AllowlistAddition string
	KeepaliveFinding  string
	AuthFinding       string
}

// ToString names the tunnel route and the service it would bridge.
func (f TunnelFindings) ToString() string { return f.Path + " bridges " + f.Service }

// RelayCredentialScheme is how short-lived relay credentials are minted.
type RelayCredentialScheme struct {
	MaxLifetime time.Duration
	Scope       string
	Minting     string
}

// RelayFindings is the relay story: how often one is needed, how media
// crosses hostile firewalls, what the relay carries and costs, and where
// relays must run.
type RelayFindings struct {
	NeedRate         string
	FirewallStory    string
	AudioCostPerHour string
	VideoCostPerHour string
	Residency        string
	Credential       RelayCredentialScheme
}

// PrivacyDefault is the call-privacy posture.
type PrivacyDefault string

// PrivacyRelayOnly keeps every call on the relay so no client ever learns
// another person's network address.
const PrivacyRelayOnly PrivacyDefault = "relay-only"

// CallSignalling is the CHATCALL-003 record: signalling over the tunnel,
// relaying through real networks, and the privacy default.
type CallSignalling struct {
	Tunnel          TunnelFindings
	Relay           RelayFindings
	PrivacyDefault  PrivacyDefault
	PrivacyTradeoff string
}

// CallSignallingRecord returns the record.
func CallSignallingRecord() CallSignalling {
	return CallSignalling{
		Tunnel: TunnelFindings{
			Path:              "/workspace/grpc",
			Service:           "hcmnext.chat.v1.CallSignallingService",
			AllowlistAddition: "Signalling is a new workspace service on the tunnel, added to tunnelAllowedServices by an explicit edit plus a registration in NewTunnelGRPCServer, exactly the way the chat services joined. TestTunnelServesOnlyWorkspaceServices keeps the map and the server in agreement, and unknown services keep failing closed.",
			KeepaliveFinding:  "The library keepalive defaults (30s ping, 120s idle) stand unchanged. Signalling is short RPCs — offer, answer, candidates — not held streams: a call that needs no signalling for minutes needs no keepalive change, and media never flows through the tunnel at all.",
			AuthFinding:       "Signalling keeps the tunnel's double admission: upgrade-time PreAdmit plus a per-RPC bearer credential on every call. Every signalling message additionally passes the conversation's tenant and active-membership check, the same authority that admits SendPost, so a caller can never signal into a conversation they cannot post in.",
		},
		Relay: RelayFindings{
			NeedRate:         "Industry experience: most calls connect directly, but on corporate networks with symmetric NAT or UDP-blocking firewalls roughly one call in five needs a relay. The prototype of CHATCALL-002 measures the product's own rate instead of trusting this figure.",
			FirewallStory:    "TURN over UDP first, falling back to TURN over TCP and then to TURN over TLS on port 443, which survives every common firewall short of a TLS-intercepting proxy. Tenants on private networks get an in-region relay rather than an exception (CHATCALL-006).",
			AudioCostPerHour: "About 11 MB per person per hour at 24 kbps each way: tens of cents per thousand call-hours at a stated $0.09/GB egress assumption, re-quoted at build time.",
			VideoCostPerHour: "About 450 MB per person per hour at 1 Mbps each way: the video mix, not the relay, dominates cost, which is why the drop-in is audio-first with capped tiles.",
			Residency:        "Relays and forwarding servers run in the tenant's region, inside the residency intersection the egress rules already enforce for outside services. Media never crosses a region boundary it was not admitted to, and a tenant that admits no outside region gets no outside relay.",
			Credential: RelayCredentialScheme{
				MaxLifetime: time.Hour,
				Scope:       "One call, one person, one tenant, one membership revision. A credential minted before a removal or a rejoin never admits again.",
				Minting:     "Minted per call and per person at ring or accept time by the chat service, after the membership check, in the short-lived shared-secret form with an expiring username. Never a long-lived account, never minted for a conversation the person cannot post in.",
			},
		},
		PrivacyDefault:  PrivacyRelayOnly,
		PrivacyTradeoff: "Relay-only costs relay bandwidth and a latency hop on every call, even where a direct path would connect. In exchange no client ever learns another person's network address, signalling cannot be used to probe for one, and a caller cannot tell whether the callee is behind a VPN, at home or in the office.",
	}
}

// ErrRelayCredential means a relay credential is not valid for this call.
var ErrRelayCredential = errors.New("transport: relay credential is not valid for this call")

// RelayCredential is one minted relay credential. It binds the call, the
// person, the tenant and the membership revision it was minted under: any
// change of membership — removal, rejoin, tenant switch — invalidates it
// without a revocation list.
type RelayCredential struct {
	CallID             string
	PersonID           string
	TenantID           string
	MembershipRevision uint64
	ExpiresAt          time.Time
}

// Allows reports whether the credential admits its holder to the call now.
// The revision must equal the conversation's current membership revision,
// and the credential must not have expired; the expiry instant itself is
// already expired.
func (c RelayCredential) Allows(callID, personID, tenantID string, membershipRevision uint64, at time.Time) error {
	switch {
	case c.CallID == "" || c.PersonID == "" || c.TenantID == "" || c.MembershipRevision == 0 || c.ExpiresAt.IsZero():
		return fmt.Errorf("%w: credential is incomplete", ErrRelayCredential)
	case c.CallID != callID || c.PersonID != personID || c.TenantID != tenantID:
		return fmt.Errorf("%w: credential belongs to another call, person or tenant", ErrRelayCredential)
	case c.MembershipRevision != membershipRevision:
		return fmt.Errorf("%w: membership changed since minting", ErrRelayCredential)
	case !at.Before(c.ExpiresAt):
		return fmt.Errorf("%w: credential expired", ErrRelayCredential)
	default:
		return nil
	}
}

// RenderCallSignalling is the stable text form used by the golden test.
func RenderCallSignalling() string {
	r := CallSignallingRecord()
	var b strings.Builder
	b.WriteString("CHATCALL-003 signalling and network traversal recorded 2026-10-22\n")
	fmt.Fprintf(&b, "\ntunnel: %s\nallowlist: %s\nkeepalive: %s\nauth: %s\n",
		r.Tunnel.ToString(), r.Tunnel.AllowlistAddition, r.Tunnel.KeepaliveFinding, r.Tunnel.AuthFinding)
	b.WriteString("\nkinds:\n")
	for _, k := range CallSignallingKinds() {
		fmt.Fprintf(&b, "%s (membership=%v): %s\n", k.Name, k.RequiresMembership, k.Purpose)
	}
	fmt.Fprintf(&b, "\nrelay need: %s\nfirewalls: %s\naudio cost: %s\nvideo cost: %s\nresidency: %s\ncredential lifetime: %s\ncredential scope: %s\ncredential minting: %s\n",
		r.Relay.NeedRate, r.Relay.FirewallStory, r.Relay.AudioCostPerHour, r.Relay.VideoCostPerHour, r.Relay.Residency,
		r.Relay.Credential.MaxLifetime.String(), r.Relay.Credential.Scope, r.Relay.Credential.Minting)
	fmt.Fprintf(&b, "\nprivacy default: %s\ntrade-off: %s\n", r.PrivacyDefault, r.PrivacyTradeoff)
	return b.String()
}

// CallSignallingDigest is the digest of the rendered record.
func CallSignallingDigest() string {
	sum := sha256.Sum256([]byte(RenderCallSignalling()))
	return hex.EncodeToString(sum[:])
}
