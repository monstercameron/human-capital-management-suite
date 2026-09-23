package partnerapp

import (
	"context"
	"errors"
	"testing"
	"time"
)

func machineTestClient() MachineClient {
	return MachineClient{
		Tenant: "tenant-a", ClientID: "client-a", Owner: "owner-a", Status: "active",
		Scopes: []string{"intents.read"}, Purpose: "sync",
		IPAllowlist: []string{"10.0.0.0/8", "192.168.1.10"},
		ExpiresAt:   time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	}
}

// TestGrantsWrite proves the write-capability predicate the token endpoint
// enforces: only an explicit write scope confers it.
func TestGrantsWrite(t *testing.T) {
	for _, scopes := range [][]string{nil, {}, {"intents.read"}, {"read_only"}, {"rewrite"}} {
		if GrantsWrite(scopes) {
			t.Fatalf("scopes %q read as write-capable", scopes)
		}
	}
	for _, scopes := range [][]string{{"write"}, {"intents.write"}, {"intents.read", "workers.write"}} {
		if !GrantsWrite(scopes) {
			t.Fatalf("scopes %q read as read-only", scopes)
		}
	}
}

// stubRegistry is an in-memory ClientRegistry for the resolution tests.
type stubRegistry struct {
	clients map[string]MachineClient
	err     error
}

func (s *stubRegistry) LoadClient(_ context.Context, tenant, clientID string) (MachineClient, error) {
	if s.err != nil {
		return MachineClient{}, s.err
	}
	c, ok := s.clients[tenant+"\x00"+clientID]
	if !ok {
		return MachineClient{}, ErrMachineClientUnknown
	}
	return c, nil
}

func (s *stubRegistry) LoadClientKeys(_ context.Context, _, _ string) ([]MachineClientKey, error) {
	return nil, nil
}

func (s *stubRegistry) RecordClientUse(_ context.Context, _, _ string, _ time.Time) error {
	return nil
}

// TestGrantedScopesFor proves grant resolution fails closed: an active
// client resolves its registered scopes, while a suspended client, an
// unknown client, or a storage failure resolves no scopes with an error.
func TestGrantedScopesFor(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)
	active := machineTestClient()
	suspended := machineTestClient()
	suspended.ClientID = "client-s"
	suspended.Status = MachineClientSuspended
	reg := &stubRegistry{clients: map[string]MachineClient{
		"tenant-a\x00client-a": active,
		"tenant-a\x00client-s": suspended,
	}}
	scopes, err := GrantedScopesFor(context.Background(), reg, "tenant-a", "client-a", now, "10.1.2.3")
	if err != nil {
		t.Fatalf("resolve active: %v", err)
	}
	if len(scopes) != 1 || scopes[0] != "intents.read" {
		t.Fatalf("scopes = %v", scopes)
	}
	if _, err := GrantedScopesFor(context.Background(), reg, "tenant-a", "client-s", now, "10.1.2.3"); !errors.Is(err, ErrMachineClientSuspended) {
		t.Fatalf("suspended = %v, want suspension", err)
	}
	if _, err := GrantedScopesFor(context.Background(), reg, "tenant-a", "ghost", now, "10.1.2.3"); !errors.Is(err, ErrMachineClientUnknown) {
		t.Fatalf("unknown = %v, want unknown", err)
	}
	if _, err := GrantedScopesFor(context.Background(), &stubRegistry{err: errors.New("db down")}, "tenant-a", "client-a", now, "10.1.2.3"); err == nil {
		t.Fatal("storage failure resolved scopes")
	}
}

// TestMachineClientPolicy proves the registry-fronting policy the token
// endpoint shares: active clients pass, suspended/revoked/expired clients
// fail, an empty allowlist admits nothing, and key selection never falls
// back to a different key than the assertion names.
func TestMachineClientPolicy(t *testing.T) {
	now := time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)

	t.Run("active client inside the allowlist passes", func(t *testing.T) {
		if err := AuthorizeClientUse(machineTestClient(), ClientUseRequest{SourceIP: "10.1.2.3", At: now}); err != nil {
			t.Fatalf("authorize = %v", err)
		}
		if err := AuthorizeClientUse(machineTestClient(), ClientUseRequest{SourceIP: "192.168.1.10", At: now}); err != nil {
			t.Fatalf("single-IP authorize = %v", err)
		}
	})

	t.Run("lifecycle and expiry fail closed", func(t *testing.T) {
		suspended := machineTestClient()
		suspended.Status = "suspended"
		if err := AuthorizeClientUse(suspended, ClientUseRequest{SourceIP: "10.1.2.3", At: now}); !errors.Is(err, ErrMachineClientSuspended) {
			t.Fatalf("suspended = %v, want ErrMachineClientSuspended", err)
		}
		revoked := machineTestClient()
		revoked.Revoked = true
		if err := AuthorizeClientUse(revoked, ClientUseRequest{SourceIP: "10.1.2.3", At: now}); !errors.Is(err, ErrMachineClientRevoked) {
			t.Fatalf("revoked = %v, want ErrMachineClientRevoked", err)
		}
		expired := machineTestClient()
		expired.ExpiresAt = now.Add(-time.Minute)
		if err := AuthorizeClientUse(expired, ClientUseRequest{SourceIP: "10.1.2.3", At: now}); !errors.Is(err, ErrMachineClientExpired) {
			t.Fatalf("expired = %v, want ErrMachineClientExpired", err)
		}
	})

	t.Run("empty allowlist admits nothing", func(t *testing.T) {
		locked := machineTestClient()
		locked.IPAllowlist = nil
		if err := AuthorizeClientUse(locked, ClientUseRequest{SourceIP: "10.1.2.3", At: now}); !errors.Is(err, ErrMachineClientIPDenied) {
			t.Fatalf("empty allowlist = %v, want ErrMachineClientIPDenied", err)
		}
		other := machineTestClient()
		if err := AuthorizeClientUse(other, ClientUseRequest{SourceIP: "203.0.113.9", At: now}); !errors.Is(err, ErrMachineClientIPDenied) {
			t.Fatalf("outside allowlist = %v, want ErrMachineClientIPDenied", err)
		}
		if err := AuthorizeClientUse(other, ClientUseRequest{SourceIP: "not-an-ip", At: now}); !errors.Is(err, ErrMachineClientIPDenied) {
			t.Fatalf("unparsable source = %v, want ErrMachineClientIPDenied", err)
		}
	})

	t.Run("key selection never falls back", func(t *testing.T) {
		keys := []MachineClientKey{
			{KID: "k1", Alg: "EdDSA", JWK: []byte(`{}`), NotBefore: now.Add(-time.Hour)},
			{KID: "k2", Alg: "EdDSA", JWK: []byte(`{}`), NotBefore: now.Add(-time.Hour), Revoked: true},
		}
		if _, err := SelectClientKey(keys, "k1", now); err != nil {
			t.Fatalf("select k1 = %v", err)
		}
		if _, err := SelectClientKey(keys, "k2", now); !errors.Is(err, ErrMachineKeyRevoked) {
			t.Fatalf("select revoked k2 = %v, want ErrMachineKeyRevoked (not k1)", err)
		}
		if _, err := SelectClientKey(keys, "k9", now); !errors.Is(err, ErrMachineKeyUnknown) {
			t.Fatalf("select missing = %v, want ErrMachineKeyUnknown", err)
		}
		future := []MachineClientKey{{KID: "k3", JWK: []byte(`{}`), NotBefore: now.Add(time.Hour)}}
		if _, err := SelectClientKey(future, "k3", now); !errors.Is(err, ErrMachineKeyNotActive) {
			t.Fatalf("select premature = %v, want ErrMachineKeyNotActive", err)
		}
	})
}
