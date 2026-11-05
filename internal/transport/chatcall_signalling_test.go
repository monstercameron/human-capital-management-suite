package transport

import (
	"strings"
	"testing"
	"time"
)

// TestTodo_CHATCALL_003 pins the signalling and network-traversal record:
// signalling over the authenticated tunnel, the relay story, relay cost and
// residency, and the credential scheme the SECURITY test holds to its
// binding promises.
func TestTodo_CHATCALL_003(t *testing.T) {
	record := CallSignallingRecord()
	if strings.TrimSpace(record.Tunnel.ToString()) == "" {
		t.Fatal("the record names no tunnel to carry signalling")
	}
	for _, kind := range CallSignallingKinds() {
		if strings.TrimSpace(string(kind.Name)) == "" || strings.TrimSpace(kind.Purpose) == "" {
			t.Fatalf("signalling kind leaves its name or purpose unanswered: %+v", kind)
		}
		if !kind.RequiresMembership {
			t.Fatalf("signalling kind %s escapes the membership check", kind.Name)
		}
	}
	wantKinds := []SignallingKindName{
		SignallingOffer, SignallingAnswer, SignallingCandidates,
		SignallingRenegotiation, SignallingRinging, SignallingHangUp,
	}
	if len(CallSignallingKinds()) != len(wantKinds) {
		t.Fatalf("signalling kinds = %d, want %d", len(CallSignallingKinds()), len(wantKinds))
	}
	for i, want := range wantKinds {
		if CallSignallingKinds()[i].Name != want {
			t.Fatalf("signalling kind %d = %s, want %s", i, CallSignallingKinds()[i].Name, want)
		}
	}
	if strings.TrimSpace(record.Tunnel.AllowlistAddition) == "" ||
		strings.TrimSpace(record.Tunnel.KeepaliveFinding) == "" ||
		strings.TrimSpace(record.Tunnel.AuthFinding) == "" {
		t.Fatalf("the tunnel section leaves the allowlist, keepalive or auth finding unanswered: %+v", record.Tunnel)
	}
	if strings.TrimSpace(record.Relay.NeedRate) == "" ||
		strings.TrimSpace(record.Relay.FirewallStory) == "" ||
		strings.TrimSpace(record.Relay.AudioCostPerHour) == "" ||
		strings.TrimSpace(record.Relay.VideoCostPerHour) == "" ||
		strings.TrimSpace(record.Relay.Residency) == "" {
		t.Fatalf("the relay section leaves an answer open: %+v", record.Relay)
	}
	rendered := RenderCallSignalling()
	for _, phrase := range []string{
		"/workspace/grpc", "tunnelAllowedServices",
		"offer", "answer", "candidates", "renegotiation", "ringing", "hang-up",
		"TCP", "443", "short-lived",
		"relay-only",
	} {
		if !strings.Contains(rendered, phrase) {
			t.Fatalf("record does not pin %q", phrase)
		}
	}
	if !strings.Contains(rendered, record.Relay.Credential.MaxLifetime.String()) {
		t.Fatal("the rendered record hides the relay credential lifetime")
	}
}

// TestTodo_CHATCALL_003_Security holds the relay credential scheme to the
// three promises of CHATCALL-003: bound to one person, one tenant and one
// call; signalling that cannot leak network addresses; and removal that
// disconnects.
func TestTodo_CHATCALL_003_Security(t *testing.T) {
	at := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	good := RelayCredential{CallID: "call-1", PersonID: "ana", TenantID: "tenant-a", MembershipRevision: 7, ExpiresAt: at.Add(time.Hour)}
	if err := good.Allows("call-1", "ana", "tenant-a", 7, at); err != nil {
		t.Fatalf("a current credential for its own call is refused: %v", err)
	}
	for _, tc := range []struct {
		name         string
		call, person string
		tenant       string
		revision     uint64
		at           time.Time
	}{
		{"another person is refused", "call-1", "bob", "tenant-a", 7, at},
		{"another tenant is refused", "call-1", "ana", "tenant-b", 7, at},
		{"another call is refused", "call-2", "ana", "tenant-a", 7, at},
		{"a stale membership revision is refused", "call-1", "ana", "tenant-a", 6, at},
		{"an expired credential is refused", "call-1", "ana", "tenant-a", 7, at.Add(2 * time.Hour)},
	} {
		if err := good.Allows(tc.call, tc.person, tc.tenant, tc.revision, tc.at); err == nil {
			t.Fatalf("%s: credential accepted", tc.name)
		}
	}
	// A person removed from the conversation mid-call holds a credential
	// whose membership revision can never be current again.
	removed := RelayCredential{CallID: "call-1", PersonID: "ana", TenantID: "tenant-a", MembershipRevision: 7, ExpiresAt: at.Add(time.Hour)}
	if err := removed.Allows("call-1", "ana", "tenant-a", 9, at); err == nil {
		t.Fatal("a credential minted before removal still admits after removal")
	}
	// Relay-only is the privacy default: a client that never learns a
	// peer's address cannot leak it, at the cost of relaying every call.
	if CallSignallingRecord().PrivacyDefault != PrivacyRelayOnly {
		t.Fatalf("privacy default = %q, want relay-only", CallSignallingRecord().PrivacyDefault)
	}
	if strings.TrimSpace(CallSignallingRecord().PrivacyTradeoff) == "" {
		t.Fatal("the relay-only default states no trade-off")
	}
}

func TestTodo_CHATCALL_003_Golden(t *testing.T) {
	const digest = "16a4f2292eb42b5297a60a34b2054b42aa4d7875b2aa9dae75f3feae6213a56b"
	if got := strings.Count(RenderCallSignalling(), "\n"); got < 15 {
		t.Fatalf("record has %d lines", got)
	}
	if CallSignallingDigest() != digest {
		t.Fatalf("the record changed; digest is now %s", CallSignallingDigest())
	}
}
