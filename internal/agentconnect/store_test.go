package agentconnect

import (
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
)

// memoryStore is an in-memory Store used to prove the Registry seam without a
// database. failOn names a method that must fail.
type memoryStore struct {
	mu        sync.Mutex
	revisions map[string]RevisionRecord
	states    map[string]UserLinkState
	failOn    string
}

func newMemoryStore() *memoryStore {
	return &memoryStore{revisions: map[string]RevisionRecord{}, states: map[string]UserLinkState{}}
}

func (m *memoryStore) fail(op string) error {
	if m.failOn == op {
		return errors.New("store unavailable: " + op)
	}
	return nil
}

func (m *memoryStore) PutRevision(r RevisionRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("PutRevision"); err != nil {
		return err
	}
	key := itemKey(r.Revision.TenantID, r.Revision.ID)
	if _, ok := m.revisions[key]; ok {
		return ErrInvalid
	}
	m.revisions[key] = r
	return nil
}

func (m *memoryStore) ListRevisions(tenant string) ([]RevisionRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("ListRevisions"); err != nil {
		return nil, err
	}
	var out []RevisionRecord
	for _, r := range m.revisions {
		if r.Revision.TenantID == tenant {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *memoryStore) PutUserState(s UserLinkState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("PutUserState"); err != nil {
		return err
	}
	m.states[userKey(s.TenantID, s.ConnectionID, s.UserID)] = s
	return nil
}

func (m *memoryStore) ListUserStates(tenant string) ([]UserLinkState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("ListUserStates"); err != nil {
		return nil, err
	}
	var out []UserLinkState
	for _, s := range m.states {
		if s.TenantID == tenant {
			out = append(out, s)
		}
	}
	return out, nil
}

func (m *memoryStore) PutRevocation(r RevocationRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.fail("PutRevocation"); err != nil {
		return err
	}
	key := itemKey(r.TenantID, r.ConnectionID)
	rec, ok := m.revisions[key]
	if !ok {
		return ErrNotFound
	}
	rec.Epoch = r.Epoch
	if rec.Revocation == nil {
		copied := r
		rec.Revocation = &copied
	}
	m.revisions[key] = rec
	return nil
}

func persistentRegistry(t *testing.T, store Store) *Registry {
	t.Helper()
	registry, err := NewPersistentRegistry(&fakeIssuer{}, func() time.Time { return time.Unix(20, 0) }, store)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

func freshResolver(t *testing.T) ConnectionResolver {
	return func(tenant, id string) (*connectivity.ConnectorConnection, error) {
		return testConnection(t, id, tenant), nil
	}
}

func TestTodo_AGENT2_007_RestoreRebuildsRegistry(t *testing.T) {
	store := newMemoryStore()
	first := persistentRegistry(t, store)
	user := testUser()
	if err := first.Register(testRevision(t, UserDelegated)); err != nil {
		t.Fatal(err)
	}
	if err := first.LinkAccount(user, "conn-a", testBinding("tenant-a", "user-account"), "external-user-a"); err != nil {
		t.Fatal(err)
	}
	if _, err := first.IssueLease(user, "conn-a", "workers.read", "agent-a", "run-a", "case-review", time.Minute); err != nil {
		t.Fatal(err)
	}
	wantRevision, _ := first.Revision("tenant-a", "conn-a")
	wantLink, _ := first.LinkedAccount(user, "conn-a")

	second := persistentRegistry(t, store)
	if err := second.Restore("tenant-a", freshResolver(t)); err != nil {
		t.Fatal(err)
	}
	gotRevision, err := second.Revision("tenant-a", "conn-a")
	if err != nil {
		t.Fatal(err)
	}
	if gotRevision.ID != wantRevision.ID || gotRevision.Endpoint != wantRevision.Endpoint || gotRevision.CredentialMode != wantRevision.CredentialMode ||
		!reflect.DeepEqual(gotRevision.Grants, wantRevision.Grants) || len(gotRevision.Skills) != 1 || gotRevision.Skills[0].ID != "workers.read" {
		t.Fatalf("restored revision = %+v, want %+v", gotRevision, wantRevision)
	}
	if gotRevision.Skills[0].Tool.Validate != nil {
		t.Fatal("persisted skill kept a behavioural Validate func")
	}
	gotLink, err := second.LinkedAccount(user, "conn-a")
	if err != nil || gotLink != wantLink {
		t.Fatalf("restored link = %+v, %v; want %+v", gotLink, err, wantLink)
	}
	// The restored Registry is fully usable, and leases were not carried over.
	if _, err := second.IssueLease(user, "conn-a", "workers.read", "agent-a", "run-b", "case-review", time.Minute); err != nil {
		t.Fatalf("IssueLease after restore: %v", err)
	}
	if got := second.userEp[userKey("tenant-a", "conn-a", "user-a")]; got != 1 {
		t.Fatalf("restored user epoch = %d, want 1", got)
	}
	if len(second.issued) != 1 {
		t.Fatalf("restored registry holds %d leases, want only the new one", len(second.issued))
	}
}

func TestTodo_AGENT2_007_RestoreKeepsUnlinkAndRevocation(t *testing.T) {
	store := newMemoryStore()
	first := persistentRegistry(t, store)
	user := testUser()
	if err := first.Register(testRevision(t, UserDelegated)); err != nil {
		t.Fatal(err)
	}
	if err := first.LinkAccount(user, "conn-a", testBinding("tenant-a", "user-account"), "external-user-a"); err != nil {
		t.Fatal(err)
	}
	if err := first.UnlinkAccount(user, "conn-a", "left company"); err != nil {
		t.Fatal(err)
	}
	if err := first.RevokeConnection("tenant-a", "conn-a", "admin:one", "compromised", "incident-9"); err != nil {
		t.Fatal(err)
	}

	second := persistentRegistry(t, store)
	// The resolver hands back a still-ACTIVE aggregate: a restart must not
	// reactivate a connection the store knows was revoked.
	if err := second.Restore("tenant-a", freshResolver(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := second.LinkedAccount(user, "conn-a"); !errors.Is(err, ErrAccountNotLinked) {
		t.Fatalf("unlinked account after restore = %v, want ErrAccountNotLinked", err)
	}
	if got := second.userEp[userKey("tenant-a", "conn-a", "user-a")]; got != 2 {
		t.Fatalf("restored user epoch = %d, want 2 (link then unlink)", got)
	}
	revision, _ := second.Revision("tenant-a", "conn-a")
	if revision.Connection.State() != connectivity.StateRevoked {
		t.Fatalf("restored connection state = %s, want REVOKED", revision.Connection.State())
	}
	if got := second.items[itemKey("tenant-a", "conn-a")].epoch; got != 2 {
		t.Fatalf("restored connection epoch = %d, want 2", got)
	}
	if _, err := second.EffectiveSkills(user, "conn-a"); !errors.Is(err, ErrConnectionRevoked) {
		t.Fatalf("EffectiveSkills on restored revoked connection = %v, want ErrConnectionRevoked", err)
	}
}

func TestTodo_AGENT2_007_RestoreBrokeredKeepsApproval(t *testing.T) {
	store := newMemoryStore()
	first := persistentRegistry(t, store)
	if err := first.Register(testRevision(t, Brokered)); err != nil {
		t.Fatal(err)
	}
	second := persistentRegistry(t, store)
	if err := second.Restore("tenant-a", freshResolver(t)); err != nil {
		t.Fatalf("restore of a brokered revision failed digest validation: %v", err)
	}
	got, _ := second.Revision("tenant-a", "conn-a")
	if got.Approval == nil || got.BrokeredCredential.Reference.ID != "brokered" {
		t.Fatalf("brokered state lost: %+v", got)
	}
	// Tampering with the persisted skill after approval is detected.
	rec := store.revisions[itemKey("tenant-a", "conn-a")]
	rec.Revision.Skills = append([]SkillExposure(nil), rec.Revision.Skills...)
	rec.Revision.Skills[0].Tool.Capability = "tampered"
	store.revisions[itemKey("tenant-a", "conn-a")] = rec
	if err := persistentRegistry(t, store).Restore("tenant-a", freshResolver(t)); !errors.Is(err, ErrSecondAdminRequired) {
		t.Fatalf("Restore of tampered revision = %v, want ErrSecondAdminRequired", err)
	}
}

func TestTodo_AGENT2_007_StoreFailuresAreSurfaced(t *testing.T) {
	user := testUser()
	t.Run("register", func(t *testing.T) {
		store := newMemoryStore()
		store.failOn = "PutRevision"
		registry := persistentRegistry(t, store)
		if err := registry.Register(testRevision(t, UserDelegated)); err == nil {
			t.Fatal("Register succeeded although the store failed")
		}
		if _, err := registry.Revision("tenant-a", "conn-a"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("a revision the store refused is visible in memory: %v", err)
		}
	})
	t.Run("link and unlink", func(t *testing.T) {
		store := newMemoryStore()
		registry := persistentRegistry(t, store)
		if err := registry.Register(testRevision(t, UserDelegated)); err != nil {
			t.Fatal(err)
		}
		store.failOn = "PutUserState"
		if err := registry.LinkAccount(user, "conn-a", testBinding("tenant-a", "acct"), "ext"); err == nil {
			t.Fatal("LinkAccount succeeded although the store failed")
		}
		if _, err := registry.LinkedAccount(user, "conn-a"); !errors.Is(err, ErrAccountNotLinked) {
			t.Fatalf("link the store refused is visible: %v", err)
		}
		store.failOn = ""
		if err := registry.LinkAccount(user, "conn-a", testBinding("tenant-a", "acct"), "ext"); err != nil {
			t.Fatal(err)
		}
		store.failOn = "PutUserState"
		if err := registry.UnlinkAccount(user, "conn-a", "reason"); err == nil {
			t.Fatal("UnlinkAccount succeeded although the store failed")
		}
		if _, err := registry.LinkedAccount(user, "conn-a"); err != nil {
			t.Fatalf("link vanished although unlink was not persisted: %v", err)
		}
	})
	t.Run("revoke stays fail closed", func(t *testing.T) {
		store := newMemoryStore()
		registry := persistentRegistry(t, store)
		if err := registry.Register(testRevision(t, UserDelegated)); err != nil {
			t.Fatal(err)
		}
		if err := registry.LinkAccount(user, "conn-a", testBinding("tenant-a", "acct"), "ext"); err != nil {
			t.Fatal(err)
		}
		store.failOn = "PutRevocation"
		if err := registry.RevokeConnection("tenant-a", "conn-a", "admin", "why", "ev"); err == nil {
			t.Fatal("RevokeConnection hid a store failure")
		}
		if _, err := registry.EffectiveSkills(user, "conn-a"); !errors.Is(err, ErrConnectionRevoked) {
			t.Fatalf("connection is usable after a failed revocation persist: %v", err)
		}
	})
}

func TestTodo_AGENT2_007_RestoreRefusals(t *testing.T) {
	if _, err := NewPersistentRegistry(&fakeIssuer{}, nil, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("NewPersistentRegistry(nil store) = %v, want ErrInvalid", err)
	}
	if _, err := NewPersistentRegistry(nil, nil, newMemoryStore()); !errors.Is(err, ErrInvalid) {
		t.Fatalf("NewPersistentRegistry(nil issuer) = %v, want ErrInvalid", err)
	}
	plain := newTestRegistry(t, &fakeIssuer{})
	if err := plain.Restore("tenant-a", freshResolver(t)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Restore without a store = %v, want ErrInvalid", err)
	}
	var nilRegistry *Registry
	if err := nilRegistry.Restore("tenant-a", freshResolver(t)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil Restore = %v, want ErrInvalid", err)
	}
	store := newMemoryStore()
	seed := persistentRegistry(t, store)
	if err := seed.Register(testRevision(t, UserDelegated)); err != nil {
		t.Fatal(err)
	}
	if err := seed.LinkAccount(testUser(), "conn-a", testBinding("tenant-a", "acct"), "ext"); err != nil {
		t.Fatal(err)
	}
	registry := persistentRegistry(t, store)
	if err := registry.Restore("tenant-a", nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Restore without resolver = %v, want ErrInvalid", err)
	}
	boom := errors.New("resolver down")
	if err := registry.Restore("tenant-a", func(string, string) (*connectivity.ConnectorConnection, error) { return nil, boom }); !errors.Is(err, boom) {
		t.Fatalf("resolver error = %v", err)
	}
	if err := registry.Restore("tenant-a", func(string, string) (*connectivity.ConnectorConnection, error) { return nil, nil }); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil connection = %v, want ErrInvalid", err)
	}
	if _, err := registry.Revision("tenant-a", "conn-a"); !errors.Is(err, ErrNotFound) {
		t.Fatal("a failed Restore left partial state behind")
	}
	if err := registry.Restore("other-tenant", freshResolver(t)); err != nil {
		t.Fatalf("Restore of a tenant with no rows = %v", err)
	}
	if err := registry.Restore("tenant-a", freshResolver(t)); err != nil {
		t.Fatal(err)
	}
	if err := registry.Restore("tenant-a", freshResolver(t)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("second Restore = %v, want ErrInvalid (already registered)", err)
	}
	for _, op := range []string{"ListRevisions", "ListUserStates"} {
		failing := newMemoryStore()
		failing.revisions, failing.states = store.revisions, store.states
		failing.failOn = op
		if err := persistentRegistry(t, failing).Restore("tenant-a", freshResolver(t)); err == nil {
			t.Fatalf("Restore hid a %s failure", op)
		}
	}
	// A user state for a connection that has no revision, and a link whose
	// binding is not an OAuth grant, are both refused.
	orphan := newMemoryStore()
	orphan.states[userKey("tenant-a", "ghost", "user-a")] = UserLinkState{TenantID: "tenant-a", ConnectionID: "ghost", UserID: "user-a", Epoch: 1}
	if err := persistentRegistry(t, orphan).Restore("tenant-a", freshResolver(t)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("orphan user state = %v, want ErrInvalid", err)
	}
	badBinding := newMemoryStore()
	badBinding.revisions, badBinding.states = store.revisions, map[string]UserLinkState{}
	link := AccountLink{UserID: "user-a", ConnectionID: "conn-a", ExternalAccountID: "ext", Binding: testBinding("tenant-a", "acct")}
	link.Binding.Handle.Kind = custody.Key
	badBinding.states[userKey("tenant-a", "conn-a", "user-a")] = UserLinkState{TenantID: "tenant-a", ConnectionID: "conn-a", UserID: "user-a", Epoch: 1, Link: &link}
	if err := persistentRegistry(t, badBinding).Restore("tenant-a", freshResolver(t)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("non-oauth binding = %v, want ErrInvalid", err)
	}
	wrongConnector := func(tenant, id string) (*connectivity.ConnectorConnection, error) {
		return testConnection(t, id, tenant), nil
	}
	mismatch := newMemoryStore()
	mismatch.revisions = map[string]RevisionRecord{}
	for k, v := range store.revisions {
		v.ConnectorID = "other-connector"
		mismatch.revisions[k] = v
	}
	if err := persistentRegistry(t, mismatch).Restore("tenant-a", wrongConnector); !errors.Is(err, ErrInvalid) {
		t.Fatalf("connector mismatch = %v, want ErrInvalid", err)
	}
	foreign := newMemoryStore()
	foreign.revisions = map[string]RevisionRecord{}
	for k, v := range store.revisions {
		foreign.revisions[k] = v
	}
	if err := persistentRegistry(t, foreign).Restore("tenant-b", freshResolver(t)); err != nil {
		t.Fatalf("tenant-b restore should see no tenant-a rows: %v", err)
	}
}

func TestTodo_AGENT2_007_RevisionRecordDropsBehaviour(t *testing.T) {
	revision := testRevision(t, UserDelegated)
	revision.Skills[0].Tool.Validate = nil
	record := revisionRecord(revision)
	if record.Revision.Connection != nil || record.Epoch != 1 || record.ConnectorID != "hris" || record.ConnectorVersion != "1.0.0" {
		t.Fatalf("revisionRecord = %+v", record)
	}
	// It must be a copy: mutating the record cannot reach the caller's revision.
	record.Revision.Skills[0].ID = "mutated"
	if revision.Skills[0].ID == "mutated" {
		t.Fatal("revisionRecord aliased the caller's skills")
	}
	if got := revisionRecord(ConnectionRevision{ID: "x"}); got.ConnectorID != "" {
		t.Fatalf("record for a revision without a connection = %+v", got)
	}
}
