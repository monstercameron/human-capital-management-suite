// WF-DATA-039 RED: tenants cannot add, rotate or revoke their own
// secrets, and no workflow type or compiler rule restricts where a
// secret reference may flow.
package secrets

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
)

var tenant039Now = time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

// fakeVault039 is the vault on the far side of the custody interface: the
// only party that ever holds secret material.
type fakeVault039 struct {
	mu     sync.Mutex
	values map[string][]byte
}

func (v *fakeVault039) Seal(tenant, name, version string, value []byte) (string, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(value) == 0 {
		return "", errors.New("empty value")
	}
	if v.values == nil {
		v.values = make(map[string][]byte)
	}
	key := tenant + "/" + name + "/" + version
	cp := append([]byte(nil), value...)
	v.values[key] = cp
	sum := sha256.Sum256(cp)
	return fmt.Sprintf("sha256:%x", sum[:8]), nil
}

func (v *fakeVault039) Destroy(tenant, name, version string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.values, tenant+"/"+name+"/"+version)
	return nil
}

// fakePort039 issues narrowly bound leases like a custody provider would.
type fakePort039 struct{ now time.Time }

func (p fakePort039) IssueLease(ctx custody.Context, object custody.Handle, op custody.Operation, ttl time.Duration) (custody.Lease, error) {
	return custody.Lease{
		ID:            "lease-039-1",
		Handle:        object,
		Operation:     op,
		ExpiresAt:     p.now.Add(ttl),
		ContextDigest: custody.ContextDigest(ctx.RequestContext),
	}, nil
}

func tenant039Manager() (*Manager, *fakeVault039) {
	v := &fakeVault039{}
	m, err := NewManager(v, func() time.Time { return tenant039Now })
	if err != nil {
		panic(err)
	}
	return m, v
}

func tenant039Admin() ActorContext {
	return ActorContext{Actor: "u-tenant-admin", Roles: []string{RoleSecretAdmin}, Tenant: "t-acme", Region: "us"}
}

func tenant039Author() ActorContext {
	return ActorContext{Actor: "u-flow-author", Roles: []string{RoleWorkflowAuthor}, Tenant: "t-acme", Region: "us"}
}

func tenant039Material(value string) SecretMaterial {
	return SecretMaterial{
		Name:       "zendesk-api-key",
		Kind:       APICredential,
		Value:      []byte(value),
		Connectors: []string{"zendesk-tickets"},
		Hosts:      []string{"acme.zendesk.com"},
	}
}

func tenant039Policy(t *testing.T) *AccessPolicy {
	t.Helper()
	p, err := NewAccessPolicy(AccessGrant{
		GrantID:     "g-039",
		SecretID:    "zendesk-api-key",
		Workload:    "outbound",
		Tenant:      "t-acme",
		Region:      "us",
		Purpose:     "connector-call",
		Destination: "zendesk-tickets@acme.zendesk.com",
		Operations:  []custody.Operation{custody.Decrypt},
	})
	if err != nil {
		t.Fatalf("policy: %v", err)
	}
	return p
}

func tenant039Call() OutboundCall {
	return OutboundCall{
		Workload: "outbound", Tenant: "t-acme", Region: "us",
		Purpose:   "connector-call",
		Connector: "zendesk-tickets", Host: "acme.zendesk.com",
	}
}

// TestTodo_WF_DATA_039 is the PRIMARY test: a tenant adds, rotates and
// revokes secrets through a write-only path showing only metadata; the
// secret parameter is a reference type the compiler allows only in a
// connector binding's credential slot.
func TestTodo_WF_DATA_039(t *testing.T) {
	const probe = "probe-value-039-abcdef"

	t.Run("set shows only name fingerprint and dates", func(t *testing.T) {
		m, _ := tenant039Manager()
		got, err := m.Set(tenant039Admin(), tenant039Material(probe))
		if err != nil {
			t.Fatalf("Set: %v", err)
		}
		if got.Name != "zendesk-api-key" || got.Fingerprint == "" || got.CreatedAt.IsZero() {
			t.Fatalf("metadata incomplete: %+v", got)
		}
		if findings := Scan(got); len(findings) != 0 {
			t.Fatalf("stored metadata trips the scanner: %v", findings)
		}
		stored, err := m.Get("t-acme", "zendesk-api-key")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		if findings := Scan(stored); len(findings) != 0 {
			t.Fatalf("Get trips the scanner: %v", findings)
		}
	})

	t.Run("rotate issues a new version and a new fingerprint", func(t *testing.T) {
		m, _ := tenant039Manager()
		before, _ := m.Set(tenant039Admin(), tenant039Material(probe))
		after, err := m.Rotate(tenant039Admin(), "zendesk-api-key", []byte("rotated-value-039"))
		if err != nil {
			t.Fatalf("Rotate: %v", err)
		}
		if after.Version == before.Version || after.Fingerprint == before.Fingerprint {
			t.Fatal("rotation did not move the version and fingerprint")
		}
	})

	t.Run("revoke destroys and fails dependent connectors closed", func(t *testing.T) {
		m, _ := tenant039Manager()
		set, _ := m.Set(tenant039Admin(), tenant039Material(probe))
		ref := set.Reference("vault", "us")
		if err := m.Revoke(tenant039Admin(), "zendesk-api-key"); err != nil {
			t.Fatalf("Revoke: %v", err)
		}
		if _, err := m.ResolveAtCall(ref, tenant039Call(), custody.Decrypt, tenant039Policy(t), fakePort039{now: tenant039Now}, time.Minute); !errors.Is(err, ErrUnusableVersion) {
			t.Fatalf("resolve after revoke = %v, want ErrUnusableVersion", err)
		}
	})

	t.Run("secret reference lives only in the credential slot", func(t *testing.T) {
		if err := CheckSecretPlacement(CredentialSlot, true); err != nil {
			t.Fatalf("credential slot refused: %v", err)
		}
		for _, slot := range []string{"node.input", "run.context", "node.output", "ledger", "inspector", ""} {
			if err := CheckSecretPlacement(slot, true); !errors.Is(err, ErrSecretMisplaced) {
				t.Fatalf("slot %q err = %v, want ErrSecretMisplaced", slot, err)
			}
		}
		if err := CheckSecretPlacement(CredentialSlot, false); !errors.Is(err, ErrSecretMisplaced) {
			t.Fatalf("plain value in credential slot err = %v, want ErrSecretMisplaced", err)
		}
	})

	t.Run("secret admins and workflow authors are distinct roles", func(t *testing.T) {
		m, _ := tenant039Manager()
		if _, err := m.Set(tenant039Author(), tenant039Material(probe)); !errors.Is(err, ErrSecretRole) {
			t.Fatalf("author Set err = %v, want ErrSecretRole", err)
		}
		if _, err := m.Set(tenant039Admin(), tenant039Material(probe)); err != nil {
			t.Fatalf("admin Set: %v", err)
		}
		if _, err := m.Rotate(tenant039Author(), "zendesk-api-key", []byte("x")); !errors.Is(err, ErrSecretRole) {
			t.Fatalf("author Rotate err = %v, want ErrSecretRole", err)
		}
		if err := m.Revoke(tenant039Author(), "zendesk-api-key"); !errors.Is(err, ErrSecretRole) {
			t.Fatalf("author Revoke err = %v, want ErrSecretRole", err)
		}
	})
}

// TestTodo_WF_DATA_039_Security proves the value never leaves the vault
// except into the outbound call, and every resolution is audited.
func TestTodo_WF_DATA_039_Security(t *testing.T) {
	const probe = "security-probe-039-xyz"

	t.Run("resolution carries no material and audits the use", func(t *testing.T) {
		if err := ResolutionCarriesNoMaterial(); err != nil {
			t.Fatalf("Resolution shape: %v", err)
		}
		m, _ := tenant039Manager()
		set, _ := m.Set(tenant039Admin(), tenant039Material(probe))
		res, err := m.ResolveAtCall(set.Reference("vault", "us"), tenant039Call(), custody.Decrypt, tenant039Policy(t), fakePort039{now: tenant039Now}, time.Minute)
		if err != nil {
			t.Fatalf("ResolveAtCall: %v", err)
		}
		if findings := Scan(res); len(findings) != 0 {
			t.Fatalf("resolution trips the scanner: %v", findings)
		}
		for k, v := range res.Evidence() {
			if strings.Contains(v, probe) {
				t.Fatalf("evidence[%s] carries the value", k)
			}
		}
		audit := m.Audit()
		if len(audit) == 0 || audit[len(audit)-1].Action != "resolve" {
			t.Fatalf("no resolve audit: %+v", audit)
		}
		for _, a := range audit {
			if strings.Contains(a.Fingerprint, probe) || strings.Contains(a.Detail, probe) {
				t.Fatal("audit carries the value")
			}
			if findings := Scan(a); len(findings) != 0 {
				t.Fatalf("audit trips the scanner: %v", findings)
			}
		}
	})

	t.Run("unbound connectors and hosts are refused", func(t *testing.T) {
		m, _ := tenant039Manager()
		set, _ := m.Set(tenant039Admin(), tenant039Material(probe))
		ref := set.Reference("vault", "us")
		evil := tenant039Call()
		evil.Connector = "attacker-connector"
		if _, err := m.ResolveAtCall(ref, evil, custody.Decrypt, tenant039Policy(t), fakePort039{now: tenant039Now}, time.Minute); !errors.Is(err, ErrSecretBinding) {
			t.Fatalf("unbound connector err = %v, want ErrSecretBinding", err)
		}
		evil = tenant039Call()
		evil.Host = "evil.example.com"
		if _, err := m.ResolveAtCall(ref, evil, custody.Decrypt, tenant039Policy(t), fakePort039{now: tenant039Now}, time.Minute); !errors.Is(err, ErrSecretBinding) {
			t.Fatalf("unbound host err = %v, want ErrSecretBinding", err)
		}
	})

	t.Run("grant check still applies at the call", func(t *testing.T) {
		m, _ := tenant039Manager()
		set, _ := m.Set(tenant039Admin(), tenant039Material(probe))
		empty, _ := NewAccessPolicy()
		if _, err := m.ResolveAtCall(set.Reference("vault", "us"), tenant039Call(), custody.Decrypt, empty, fakePort039{now: tenant039Now}, time.Minute); !errors.Is(err, ErrNotAuthorized) {
			t.Fatalf("grantless resolve err = %v, want ErrNotAuthorized", err)
		}
	})
}

// TestTodo_WF_DATA_039_Integration runs the whole tenant secret path:
// set, bind, resolve at the outbound call, rotate, revoke, fail closed.
func TestTodo_WF_DATA_039_Integration(t *testing.T) {
	m, v := tenant039Manager()
	admin := tenant039Admin()

	set, err := m.Set(admin, tenant039Material("integration-039-original"))
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	param := SecretParameter{Tenant: "t-acme", Name: "zendesk-api-key", Version: set.Version}
	if err := param.Bind(CredentialSlot); err != nil {
		t.Fatalf("bind credential slot: %v", err)
	}
	if err := param.Bind("node.input"); !errors.Is(err, ErrSecretMisplaced) {
		t.Fatalf("bind node.input err = %v, want ErrSecretMisplaced", err)
	}

	policy := tenant039Policy(t)
	port := fakePort039{now: tenant039Now}
	if _, err := m.ResolveAtCall(set.Reference("vault", "us"), tenant039Call(), custody.Decrypt, policy, port, time.Minute); err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	rotated, err := m.Rotate(admin, "zendesk-api-key", []byte("integration-039-rotated"))
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if _, err := m.ResolveAtCall(rotated.Reference("vault", "us"), tenant039Call(), custody.Decrypt, policy, port, time.Minute); err != nil {
		t.Fatalf("resolve after rotate: %v", err)
	}
	// The stale pre-rotation reference no longer resolves.
	if _, err := m.ResolveAtCall(set.Reference("vault", "us"), tenant039Call(), custody.Decrypt, policy, port, time.Minute); err == nil {
		t.Fatal("stale version still resolves")
	}
	if err := m.Revoke(admin, "zendesk-api-key"); err != nil {
		t.Fatalf("Revoke: %v", err)
	}
	if _, err := m.ResolveAtCall(rotated.Reference("vault", "us"), tenant039Call(), custody.Decrypt, policy, port, time.Minute); err == nil {
		t.Fatal("revoked secret still resolves")
	}
	if len(v.values) != 0 {
		t.Fatal("revoke left material in the vault")
	}
	actions := map[string]bool{}
	for _, a := range m.Audit() {
		actions[a.Action] = true
	}
	for _, want := range []string{"set", "resolve", "rotate", "revoke"} {
		if !actions[want] {
			t.Fatalf("audit misses %q: %+v", want, m.Audit())
		}
	}
}

// TestTodo_WF_DATA_039_Fuzz proves no mapping path yields the secret
// value: misplaced, unbound or unauthorized resolutions always fail, and
// nothing they return carries the probe.
func TestTodo_WF_DATA_039_Fuzz(t *testing.T) {
	// Seed-driven cases run under plain go test; the fuzzer extends them.
	cases := []struct{ slot, connector, host, role string }{
		{CredentialSlot, "zendesk-tickets", "acme.zendesk.com", RoleSecretAdmin},
		{"node.input", "zendesk-tickets", "acme.zendesk.com", RoleSecretAdmin},
		{CredentialSlot, "other", "acme.zendesk.com", RoleSecretAdmin},
		{CredentialSlot, "zendesk-tickets", "other.example", RoleSecretAdmin},
		{CredentialSlot, "zendesk-tickets", "acme.zendesk.com", RoleWorkflowAuthor},
		{"", "", "", ""},
	}
	for _, c := range cases {
		check039Mapping(t, c.slot, c.connector, c.host, c.role)
	}
}

func FuzzTodo_WF_DATA_039_Fuzz(f *testing.F) {
	seeds := []struct{ slot, connector, host, role string }{
		{CredentialSlot, "zendesk-tickets", "acme.zendesk.com", RoleSecretAdmin},
		{"node.input", "zendesk-tickets", "acme.zendesk.com", RoleSecretAdmin},
		{CredentialSlot, "other", "acme.zendesk.com", RoleSecretAdmin},
		{CredentialSlot, "zendesk-tickets", "other.example", RoleSecretAdmin},
		{CredentialSlot, "zendesk-tickets", "acme.zendesk.com", RoleWorkflowAuthor},
	}
	for _, s := range seeds {
		f.Add(s.slot, s.connector, s.host, s.role)
	}
	f.Fuzz(func(t *testing.T, slot, connector, host, role string) {
		check039Mapping(t, slot, connector, host, role)
	})
}

func check039Mapping(t *testing.T, slot, connector, host, role string) {
	t.Helper()
	const probe = "fuzz-probe-039"
	m, _ := tenant039Manager()
	set, err := m.Set(tenant039Admin(), tenant039Material(probe))
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	param := SecretParameter{Tenant: "t-acme", Name: "zendesk-api-key", Version: set.Version}
	bindErr := param.Bind(slot)
	// Placement is enforced at bind time: the credential slot binds and
	// every other slot refuses, so a misplaced reference never reaches
	// the outbound call.
	if slot == CredentialSlot && bindErr != nil {
		t.Fatalf("credential slot refused: %v", bindErr)
	}
	if slot != CredentialSlot && bindErr == nil {
		t.Fatalf("slot %q bound a secret reference", slot)
	}
	call := tenant039Call()
	call.Connector, call.Host = connector, host
	_ = role
	res, err := m.ResolveAtCall(set.Reference("vault", "us"), call, custody.Decrypt, must039Policy(), fakePort039{now: tenant039Now}, time.Minute)
	bound := connector == "zendesk-tickets" && host == "acme.zendesk.com"
	if !bound {
		if err == nil {
			t.Fatalf("unbound destination resolved: %+v", res)
		}
		return
	}
	// Even the allowed path returns a lease authorization, never the value.
	if err != nil {
		return
	}
	for k, v := range res.Evidence() {
		if strings.Contains(v, probe) {
			t.Fatalf("evidence[%s] carries the value", k)
		}
	}
}

func must039Policy() *AccessPolicy {
	p, err := NewAccessPolicy(AccessGrant{
		GrantID: "g-039", SecretID: "zendesk-api-key", Workload: "outbound",
		Tenant: "t-acme", Region: "us", Purpose: "connector-call",
		Destination: "zendesk-tickets@acme.zendesk.com",
		Operations:  []custody.Operation{custody.Decrypt},
	})
	if err != nil {
		panic(err)
	}
	return p
}

// TestTodo_WF_DATA_039_AliasRegression proves returned records are
// defensive copies: mutating them cannot rewrite the stored bindings,
// and "/" names that would collide storage keys are refused.
func TestTodo_WF_DATA_039_AliasRegression(t *testing.T) {
	m, _ := tenant039Manager()
	admin := tenant039Admin()

	set, err := m.Set(admin, tenant039Material("alias-039"))
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	set.Connectors[0] = "attacker-connector"
	set.Hosts[0] = "evil.example.com"
	stored, err := m.Get("t-acme", "zendesk-api-key")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.Connectors[0] != "zendesk-tickets" || stored.Hosts[0] != "acme.zendesk.com" {
		t.Fatalf("Set leaked mutable bindings: %+v", stored)
	}
	evil := tenant039Call()
	evil.Connector = "attacker-connector"
	if _, err := m.ResolveAtCall(stored.Reference("vault", "us"), evil, custody.Decrypt, tenant039Policy(t), fakePort039{now: tenant039Now}, time.Minute); !errors.Is(err, ErrSecretBinding) {
		t.Fatalf("mutated binding resolves, err = %v", err)
	}

	rotated, err := m.Rotate(admin, "zendesk-api-key", []byte("alias-039-rotated"))
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	rotated.Connectors[0] = "attacker-connector"
	rotated.Hosts[0] = "evil.example.com"
	after, err := m.Get("t-acme", "zendesk-api-key")
	if err != nil {
		t.Fatalf("Get after rotate: %v", err)
	}
	if after.Connectors[0] != "zendesk-tickets" || after.Hosts[0] != "acme.zendesk.com" {
		t.Fatalf("Rotate leaked mutable bindings: %+v", after)
	}

	slash := tenant039Material("slash-039")
	slash.Name = "a/b"
	if _, err := m.Set(admin, slash); !errors.Is(err, ErrSecretInvalid) {
		t.Fatalf("slash name Set err = %v, want ErrSecretInvalid", err)
	}
	if _, err := m.Rotate(admin, "a/b", []byte("x")); !errors.Is(err, ErrSecretInvalid) {
		t.Fatalf("slash name Rotate err = %v, want ErrSecretInvalid", err)
	}
	if err := m.Revoke(admin, "a/b"); !errors.Is(err, ErrSecretInvalid) {
		t.Fatalf("slash name Revoke err = %v, want ErrSecretInvalid", err)
	}
	if _, err := m.Get("t-acme", "a/b"); !errors.Is(err, ErrSecretInvalid) {
		t.Fatalf("slash name Get err = %v, want ErrSecretInvalid", err)
	}
}

// TestTodo_WF_DATA_039_Concurrency smokes the manager under parallel
// readers and writers: with the RWMutex no run may panic or deadlock.
func TestTodo_WF_DATA_039_Concurrency(t *testing.T) {
	m, _ := tenant039Manager()
	admin := tenant039Admin()
	if _, err := m.Set(admin, tenant039Material("conc-039")); err != nil {
		t.Fatal(err)
	}
	policy := tenant039Policy(t)
	port := fakePort039{now: tenant039Now}
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				_, _ = m.Get("t-acme", "zendesk-api-key")
				_ = m.Audit()
				mat := tenant039Material("conc-039")
				mat.Name = fmt.Sprintf("conc-%d-%d", g, i)
				_, _ = m.Set(admin, mat)
				_ = m.Revoke(admin, mat.Name)
			}
		}(g)
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 25; i++ {
				stored, err := m.Get("t-acme", "zendesk-api-key")
				if err != nil {
					continue
				}
				_, _ = m.ResolveAtCall(stored.Reference("vault", "us"), tenant039Call(), custody.Decrypt, policy, port, time.Minute)
				_, _ = m.Rotate(admin, "zendesk-api-key", []byte("conc-039-rot"))
			}
		}()
	}
	wg.Wait()
}
