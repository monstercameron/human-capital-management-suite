package agentconnectionstore

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentconnect"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/secrets"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

type env struct {
	db  *pgtest.DB
	ids map[string]uuid.UUID
}

func (e *env) mapper(key values.TenantId) uuid.UUID { return e.ids[string(key)] }

func newEnv(t *testing.T, tenants ...string) *env {
	t.Helper()
	e := &env{db: pgtest.New(t), ids: map[string]uuid.UUID{}}
	for _, tenant := range tenants {
		id := uuid.New()
		e.ids[tenant] = id
		e.db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, id, tenant, tenant)
	}
	return e
}

type rawConn interface {
	dbport.Beginner
	Exec(context.Context, string, ...any) (int64, error)
	QueryRow(context.Context, string, ...any) dbport.Row
}

// appConn is a new connection on the application role, so RLS is enforced.
func (e *env) appConn(t *testing.T) rawConn {
	t.Helper()
	conn := e.db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume app role: %v", err)
	}
	return conn
}

// store returns a new Store instance on its own connection.
func (e *env) store(t *testing.T) *Store {
	t.Helper()
	s, err := New(e.appConn(t), e.mapper)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

type stubIssuer struct{ n int }

func (s *stubIssuer) Mint(req lease.Request) (lease.CredentialLease, lease.Evidence, error) {
	s.n++
	l := lease.CredentialLease{ID: "lease-" + string(rune('a'+s.n)), CustodyLeaseID: "c", Handle: req.Handle, Workload: req.Workload, Tenant: req.Tenant, Purpose: req.Purpose, Destination: req.Destination, Operation: req.Operation, Nonce: "n", IssuedAt: time.Unix(100, 0), ExpiresAt: time.Unix(100, 0).Add(req.TTL)}
	return l, lease.Evidence{LeaseID: l.ID, Outcome: "granted"}, nil
}
func (s *stubIssuer) Use(l lease.CredentialLease, d string, o custody.Operation) (lease.Evidence, error) {
	return lease.Evidence{LeaseID: l.ID, Destination: d, Operation: o, Outcome: "granted"}, nil
}
func (s *stubIssuer) Revoke(id, _ string) (lease.Evidence, error) {
	return lease.Evidence{LeaseID: id, Outcome: "granted"}, nil
}

func connection(t *testing.T, id, tenant string) *connectivity.ConnectorConnection {
	t.Helper()
	version, err := connectivity.ParseVersion("1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	credential, err := connectivity.ParseCredentialRef("vault://tenant/connector")
	if err != nil {
		t.Fatal(err)
	}
	capability := connectivity.Capability{Object: connectivity.ObjectWorker, Operation: connectivity.OperationRead}
	definition := connectivity.ConnectorDefinition{ConnectorID: "hris", Version: version, AuthModes: []connectivity.AuthMode{connectivity.AuthOAuth2ClientCredentials}, Capabilities: []connectivity.Capability{capability}, Bounds: connectivity.Bounds{MaxPageSize: 100, MaxPagesPerRun: 10, MaxRecordsPerRun: 1000, MaxRecordBytes: 4096}}
	c, err := connectivity.NewConnection(connectivity.Publication{Definition: definition}, connectivity.ConnectionSpec{ConnectionID: id, TenantID: tenant, OrgID: "org-a", SystemID: "system-a", Environment: connectivity.EnvironmentProduction, Residency: "us", ConnectorID: "hris", ConnectorVersion: version, AuthMode: connectivity.AuthOAuth2ClientCredentials, CredentialRef: credential, Scopes: []string{"worker.read"}, EndpointPolicy: connectivity.EndpointPolicy{AllowedHosts: []string{"hris.example"}, RequireTLS: true, EgressProfile: "egress/us"}, Capabilities: []connectivity.Capability{capability}, Bounds: definition.Bounds, CreatedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []connectivity.LifecycleState{connectivity.StateValidating, connectivity.StateReady, connectivity.StateActive} {
		if err := c.Transition(state, connectivity.TransitionEvidence{Reason: "test", ActorRef: "admin:test", EvidenceRef: "evidence:test", OccurredAt: time.Unix(2, 0)}); err != nil {
			t.Fatal(err)
		}
	}
	return c
}

func binding(tenant, id string) agentconnect.CredentialBinding {
	return agentconnect.CredentialBinding{Reference: secrets.SecretReference{ID: id, Kind: secrets.OAuthGrant, Version: "v1", Provider: "vault", ProviderPath: "opaque/path", Tenant: tenant, Region: "us", State: secrets.Active}, Handle: custody.Handle{ID: id, Kind: custody.Secret, Version: "v1", Tenant: tenant, Region: "us"}}
}

func user(tenant, id string) agentconnect.UserContext {
	return agentconnect.UserContext{TenantID: tenant, UserID: id, Roles: []string{"manager"}, Population: "employees", OrganizationScopes: []string{"org-a"}}
}

func revision(t *testing.T, tenant, id string, mode agentconnect.CredentialMode) agentconnect.ConnectionRevision {
	t.Helper()
	rev := agentconnect.ConnectionRevision{ID: id, TenantID: tenant, Revision: 1, Endpoint: "hris.example", Connection: connection(t, id, tenant), CredentialMode: mode,
		Skills: []agentconnect.SkillExposure{{ID: "workers.read", Version: "1", Tool: agentsecurity.ToolDescriptor{Name: "workers.read", Capability: "workers.read", Version: 1, Class: agentsecurity.ToolRead, DataScope: []string{"workers.basic"}, Cost: 1, Schema: "workers.v1"}, Tier: agentconnect.TierT0, CredentialOperation: custody.LeaseOperation, SharedRead: mode == agentconnect.Brokered}},
		Grants: []agentconnect.GrantScope{{ID: "grant-a", Roles: []string{"manager"}, Population: "employees", OrganizationScopes: []string{"org-a"}, Skills: []string{"workers.read"}}}}
	if mode == agentconnect.Brokered {
		rev.Skills[0].RecordFilter, rev.Skills[0].SharedRead = "user.organization_scope", false
		rev.BrokeredCredential = binding(tenant, "brokered")
		digest, err := rev.ApprovalDigest()
		if err != nil {
			t.Fatal(err)
		}
		rev.Approval = &agentconnect.SecondAdminApproval{RequestedBy: "admin-one", ApprovedBy: "admin-two", RequestHash: digest, ApprovedAt: time.Unix(10, 0).UTC(), StepUp: true}
	}
	return rev
}

func registry(t *testing.T, store agentconnect.Store) *agentconnect.Registry {
	t.Helper()
	r, err := agentconnect.NewPersistentRegistry(&stubIssuer{}, func() time.Time { return time.Unix(20, 0) }, store)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func resolver(t *testing.T) agentconnect.ConnectionResolver {
	return func(tenant, id string) (*connectivity.ConnectorConnection, error) {
		return connection(t, id, tenant), nil
	}
}

// A Registry rebuilt from a NEW store instance returns the same revisions,
// links and revoked state as the one that wrote them.
func TestTodo_AGENT2_007_Integration(t *testing.T) {
	e := newEnv(t, "tenant-a")
	first := registry(t, e.store(t))
	u1, u2 := user("tenant-a", "user-1"), user("tenant-a", "user-2")
	if err := first.Register(revision(t, "tenant-a", "conn-a", agentconnect.UserDelegated)); err != nil {
		t.Fatal(err)
	}
	if err := first.Register(revision(t, "tenant-a", "conn-b", agentconnect.Brokered)); err != nil {
		t.Fatal(err)
	}
	if err := first.LinkAccount(u1, "conn-a", binding("tenant-a", "acct-1"), "ext-1"); err != nil {
		t.Fatal(err)
	}
	if err := first.LinkAccount(u2, "conn-a", binding("tenant-a", "acct-2"), "ext-2"); err != nil {
		t.Fatal(err)
	}
	if err := first.UnlinkAccount(u2, "conn-a", "offboarded"); err != nil {
		t.Fatal(err)
	}
	if err := first.RevokeConnection("tenant-a", "conn-b", "admin:one", "compromised", "incident-9"); err != nil {
		t.Fatal(err)
	}
	// Revoke again: idempotent in the store, first actor and reason retained.
	if err := first.Disconnect("tenant-a", "conn-b", "admin:two", "second thoughts", "incident-10"); err != nil {
		t.Fatal(err)
	}

	second := registry(t, e.store(t))
	if err := second.Restore("tenant-a", resolver(t)); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"conn-a", "conn-b"} {
		want, _ := first.Revision("tenant-a", id)
		got, err := second.Revision("tenant-a", id)
		if err != nil {
			t.Fatal(err)
		}
		if got.ID != want.ID || got.Revision != want.Revision || got.Endpoint != want.Endpoint || got.CredentialMode != want.CredentialMode ||
			!reflect.DeepEqual(got.Grants, want.Grants) || !reflect.DeepEqual(got.Approval, want.Approval) || got.BrokeredCredential != want.BrokeredCredential {
			t.Fatalf("%s: restored revision differs\n got: %+v\nwant: %+v", id, got, want)
		}
		if len(got.Skills) != 1 || !reflect.DeepEqual(got.Skills[0].Tool.DataScope, want.Skills[0].Tool.DataScope) ||
			got.Skills[0].Tier != want.Skills[0].Tier || got.Skills[0].RecordFilter != want.Skills[0].RecordFilter || got.Skills[0].Tool.Class != want.Skills[0].Tool.Class {
			t.Fatalf("%s: restored skills differ: %+v vs %+v", id, got.Skills, want.Skills)
		}
	}
	link, err := second.LinkedAccount(u1, "conn-a")
	if err != nil || link.ExternalAccountID != "ext-1" || link.Binding != binding("tenant-a", "acct-1") || !link.LinkedAt.Equal(time.Unix(20, 0)) {
		t.Fatalf("restored link = %+v, %v", link, err)
	}
	if _, err := second.LinkedAccount(u2, "conn-a"); !errors.Is(err, agentconnect.ErrAccountNotLinked) {
		t.Fatalf("unlinked user after restart = %v, want ErrAccountNotLinked", err)
	}
	brokered, _ := second.Revision("tenant-a", "conn-b")
	if brokered.Connection.State() != connectivity.StateRevoked {
		t.Fatalf("revoked connection came back as %s", brokered.Connection.State())
	}
	if _, err := second.EffectiveSkills(u1, "conn-b"); !errors.Is(err, agentconnect.ErrConnectionRevoked) {
		t.Fatalf("EffectiveSkills on revoked connection after restart = %v", err)
	}
	if _, err := second.IssueLease(u1, "conn-a", "workers.read", "agent", "run", "purpose", time.Minute); err != nil {
		t.Fatalf("restored registry cannot issue a lease: %v", err)
	}

	states, err := e.store(t).ListUserStates("tenant-a")
	if err != nil || len(states) != 2 {
		t.Fatalf("ListUserStates = %+v, %v", states, err)
	}
	for _, s := range states {
		wantEpoch := map[string]uint64{"user-1": 1, "user-2": 2}[s.UserID]
		if s.Epoch != wantEpoch || (s.UserID == "user-2") != (s.Link == nil) {
			t.Fatalf("user state %+v, want epoch %d and link only for user-1", s, wantEpoch)
		}
	}
	revisions, _ := e.store(t).ListRevisions("tenant-a")
	for _, r := range revisions {
		if r.Revision.ID == "conn-b" && (r.Revocation == nil || r.Revocation.Actor != "admin:one" || r.Revocation.Reason != "compromised" || r.Epoch != 3) {
			t.Fatalf("conn-b revocation = %+v epoch=%d, want first actor kept and epoch 3", r.Revocation, r.Epoch)
		}
		if r.Revision.ID == "conn-a" && (r.Revocation != nil || r.Epoch != 1) {
			t.Fatalf("conn-a unexpectedly revoked or bumped: %+v", r)
		}
	}
}

func TestTodo_AGENT2_007_StoresNoCredentialMaterial(t *testing.T) {
	e := newEnv(t, "tenant-a")
	r := registry(t, e.store(t))
	if err := r.Register(revision(t, "tenant-a", "conn-a", agentconnect.UserDelegated)); err != nil {
		t.Fatal(err)
	}
	if err := r.LinkAccount(user("tenant-a", "user-1"), "conn-a", binding("tenant-a", "acct-1"), "ext-1"); err != nil {
		t.Fatal(err)
	}
	var text string
	if err := e.db.QueryRow(context.Background(), `SELECT binding::text FROM agent_connection_user_state`).Scan(&text); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Reference map[string]any
		Handle    map[string]any
	}
	if err := json.Unmarshal([]byte(text), &doc); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{"id": true, "kind": true, "version": true, "provider": true, "provider_path": true, "tenant": true, "region": true, "state": true}
	for key := range doc.Reference {
		if !allowed[key] {
			t.Errorf("credential reference persisted unexpected field %q", key)
		}
	}
	for key := range doc.Handle {
		if !allowed[key] {
			t.Errorf("custody handle persisted unexpected field %q", key)
		}
	}
}

func TestTodo_AGENT2_007_CrossTenantIsolation(t *testing.T) {
	e := newEnv(t, "tenant-a", "tenant-b")
	a, b := e.store(t), e.store(t)
	ra := registry(t, a)
	if err := ra.Register(revision(t, "tenant-a", "conn-a", agentconnect.UserDelegated)); err != nil {
		t.Fatal(err)
	}
	if err := ra.LinkAccount(user("tenant-a", "user-1"), "conn-a", binding("tenant-a", "acct-1"), "ext-1"); err != nil {
		t.Fatal(err)
	}
	if got, err := b.ListRevisions("tenant-b"); err != nil || len(got) != 0 {
		t.Fatalf("tenant B sees revisions %+v (err=%v)", got, err)
	}
	if got, err := b.ListUserStates("tenant-b"); err != nil || len(got) != 0 {
		t.Fatalf("tenant B sees user states %+v (err=%v)", got, err)
	}
	// Tenant B cannot revoke tenant A's connection through the store.
	err := b.PutRevocation(agentconnect.RevocationRecord{TenantID: "tenant-b", ConnectionID: "conn-a", Actor: "x", Reason: "y", EvidenceRef: "z", RevokedAt: time.Unix(1, 0), Epoch: 2})
	if !errors.Is(err, agentconnect.ErrNotFound) {
		t.Fatalf("cross-tenant PutRevocation = %v, want ErrNotFound", err)
	}
	// Nor attach a user state to it.
	if err := b.PutUserState(agentconnect.UserLinkState{TenantID: "tenant-b", ConnectionID: "conn-a", UserID: "user-9", Epoch: 1}); err == nil {
		t.Fatal("tenant B attached a user state to tenant A's connection")
	}
	revs, _ := a.ListRevisions("tenant-a")
	if len(revs) != 1 || revs[0].Revocation != nil || revs[0].Epoch != 1 {
		t.Fatalf("tenant A connection was affected by tenant B: %+v", revs)
	}
	// The same connection id is independent per tenant.
	rb := registry(t, b)
	if err := rb.Register(revision(t, "tenant-b", "conn-a", agentconnect.UserDelegated)); err != nil {
		t.Fatalf("tenant B could not reuse the connection id: %v", err)
	}
	// A restore for tenant B never yields tenant A's link.
	restored := registry(t, e.store(t))
	if err := restored.Restore("tenant-b", resolver(t)); err != nil {
		t.Fatal(err)
	}
	if _, err := restored.LinkedAccount(user("tenant-b", "user-1"), "conn-a"); !errors.Is(err, agentconnect.ErrAccountNotLinked) {
		t.Fatalf("tenant B restored tenant A's link: %v", err)
	}
	// The database itself refuses, independent of the adapter.
	raw := e.appConn(t)
	ctx := context.Background()
	if _, err := raw.Exec(ctx, `SELECT set_config('app.tenant_id',$1,false)`, e.ids["tenant-b"].String()); err != nil {
		t.Fatal(err)
	}
	var visible int
	if err := raw.QueryRow(ctx, `SELECT count(*) FROM agent_connection_revision WHERE tenant_id=$1`, e.ids["tenant-a"]).Scan(&visible); err != nil || visible != 0 {
		t.Fatalf("RLS leaked %d tenant A rows (err=%v)", visible, err)
	}
	if _, err := raw.Exec(ctx, `UPDATE agent_connection_revision SET connection_epoch=99 WHERE tenant_id=$1`, e.ids["tenant-a"]); err != nil {
		t.Fatalf("cross-tenant update errored instead of matching zero rows: %v", err)
	}
	if got, _ := a.ListRevisions("tenant-a"); got[0].Epoch != 1 {
		t.Fatal("RLS let tenant B update tenant A's epoch")
	}
}

func TestTodo_AGENT2_007_ImmutableRevisionAndDuplicates(t *testing.T) {
	e := newEnv(t, "tenant-a")
	s := e.store(t)
	r := registry(t, s)
	rev := revision(t, "tenant-a", "conn-a", agentconnect.UserDelegated)
	if err := r.Register(rev); err != nil {
		t.Fatal(err)
	}
	dup := agentconnect.RevisionRecord{Revision: rev, ConnectorID: "hris", ConnectorVersion: "1.0.0"}
	dup.Revision.Connection = nil
	dup.Revision.Endpoint = "evil.example"
	if err := s.PutRevision(dup); !errors.Is(err, agentconnect.ErrInvalid) {
		t.Fatalf("duplicate PutRevision = %v, want ErrInvalid", err)
	}
	got, _ := s.ListRevisions("tenant-a")
	if got[0].Revision.Endpoint != "hris.example" {
		t.Fatal("duplicate PutRevision overwrote the immutable revision")
	}
	raw := e.appConn(t)
	ctx := context.Background()
	if _, err := raw.Exec(ctx, `SELECT set_config('app.tenant_id',$1,false)`, e.ids["tenant-a"].String()); err != nil {
		t.Fatal(err)
	}
	if err := r.LinkAccount(user("tenant-a", "user-1"), "conn-a", binding("tenant-a", "acct-1"), "ext-1"); err != nil {
		t.Fatal(err)
	}
	if err := r.RevokeConnection("tenant-a", "conn-a", "admin", "why", "ev"); err != nil {
		t.Fatal(err)
	}
	for name, stmt := range map[string]string{
		"change endpoint":        `UPDATE agent_connection_revision SET endpoint='evil.example'`,
		"widen skills":           `UPDATE agent_connection_revision SET skills='[{"id":"x"}]'::jsonb`,
		"delete revision":        `DELETE FROM agent_connection_revision`,
		"decrease epoch":         `UPDATE agent_connection_revision SET connection_epoch=1`,
		"reinstate":              `UPDATE agent_connection_revision SET revoked_at=NULL, revoked_by=NULL, revoked_reason=NULL, revoked_evidence_ref=NULL`,
		"rewrite revoker":        `UPDATE agent_connection_revision SET revoked_by='someone-else'`,
		"partial revocation":     `INSERT INTO agent_connection_revision (tenant_id,connection_id,revision,endpoint,connector_id,connector_version,credential_mode,skills,grants,revoked_by) SELECT tenant_id,'c2',1,'e','c','1','USER_DELEGATED',skills,grants,'me' FROM agent_connection_revision`,
		"brokered without cred":  `INSERT INTO agent_connection_revision (tenant_id,connection_id,revision,endpoint,connector_id,connector_version,credential_mode,skills,grants) SELECT tenant_id,'c3',1,'e','c','1','BROKERED',skills,grants FROM agent_connection_revision`,
		"delete user state":      `DELETE FROM agent_connection_user_state`,
		"decrease user epoch":    `UPDATE agent_connection_user_state SET user_epoch=0`,
		"linked without binding": `INSERT INTO agent_connection_user_state (tenant_id,connection_id,user_id,user_epoch,linked) SELECT tenant_id,connection_id,'u',1,true FROM agent_connection_revision`,
	} {
		if _, err := raw.Exec(ctx, stmt); err == nil {
			t.Errorf("%s: database accepted a forbidden write", name)
		}
	}
}

func TestTodo_AGENT2_007_EpochConcurrency(t *testing.T) {
	e := newEnv(t, "tenant-a")
	seed := e.store(t)
	r := registry(t, seed)
	if err := r.Register(revision(t, "tenant-a", "conn-a", agentconnect.UserDelegated)); err != nil {
		t.Fatal(err)
	}
	const workers = 6
	var wg sync.WaitGroup
	errs := make(chan error, workers*2)
	for w := 0; w < workers; w++ {
		s := e.store(t)
		wg.Add(1)
		go func(epoch uint64) {
			defer wg.Done()
			errs <- s.PutUserState(agentconnect.UserLinkState{TenantID: "tenant-a", ConnectionID: "conn-a", UserID: "user-1", Epoch: epoch})
			errs <- s.PutRevocation(agentconnect.RevocationRecord{TenantID: "tenant-a", ConnectionID: "conn-a", Actor: "a", Reason: "r", EvidenceRef: "e", RevokedAt: time.Unix(int64(epoch), 0), Epoch: epoch})
		}(uint64(w + 1))
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	states, _ := seed.ListUserStates("tenant-a")
	if len(states) != 1 || states[0].Epoch != workers {
		t.Fatalf("user epoch after concurrent writes = %+v, want the maximum %d", states, workers)
	}
	revs, _ := seed.ListRevisions("tenant-a")
	if revs[0].Epoch != workers || revs[0].Revocation == nil {
		t.Fatalf("connection epoch/revocation after concurrent writes = %+v", revs[0])
	}
	// Late, lower epochs never move it back.
	if err := seed.PutUserState(agentconnect.UserLinkState{TenantID: "tenant-a", ConnectionID: "conn-a", UserID: "user-1", Epoch: 2}); err != nil {
		t.Fatal(err)
	}
	if states, _ = seed.ListUserStates("tenant-a"); states[0].Epoch != workers {
		t.Fatalf("a stale PutUserState lowered the epoch to %d", states[0].Epoch)
	}
}

type failingDB struct{}

func (failingDB) Begin(context.Context) (dbport.Tx, error) { return nil, errors.New("db down") }

func TestTodo_AGENT2_007_ValidationAndFailures(t *testing.T) {
	if _, err := New(nil, nil); !errors.Is(err, agentconnect.ErrInvalid) {
		t.Fatalf("New(nil) = %v, want ErrInvalid", err)
	}
	s, err := New(failingDB{}, func(values.TenantId) uuid.UUID { return uuid.New() })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListRevisions("tenant-a"); err == nil || errors.Is(err, agentconnect.ErrInvalid) {
		t.Fatalf("ListRevisions on database failure = %v", err)
	}
	if _, err := s.ListUserStates("tenant-a"); err == nil {
		t.Fatal("ListUserStates hid a database failure")
	}
	rev := agentconnect.RevisionRecord{Revision: agentconnect.ConnectionRevision{ID: "c", TenantID: "tenant-a", Revision: 1, Endpoint: "e", CredentialMode: agentconnect.UserDelegated}, ConnectorID: "hris", ConnectorVersion: "1.0.0"}
	if err := s.PutRevision(rev); err == nil || errors.Is(err, agentconnect.ErrInvalid) {
		t.Fatalf("PutRevision on database failure = %v", err)
	}
	if err := s.PutUserState(agentconnect.UserLinkState{TenantID: "tenant-a", ConnectionID: "c", UserID: "u", Epoch: 1}); err == nil {
		t.Fatal("PutUserState hid a database failure")
	}
	if err := s.PutRevocation(agentconnect.RevocationRecord{TenantID: "tenant-a", ConnectionID: "c", Actor: "a", Reason: "r", EvidenceRef: "e", RevokedAt: time.Unix(1, 0), Epoch: 2}); err == nil {
		t.Fatal("PutRevocation hid a database failure")
	}
	for name, err := range map[string]error{
		"incomplete revision":   s.PutRevision(agentconnect.RevisionRecord{}),
		"incomplete user state": s.PutUserState(agentconnect.UserLinkState{TenantID: "tenant-a"}),
		"incomplete revocation": s.PutRevocation(agentconnect.RevocationRecord{TenantID: "tenant-a", ConnectionID: "c"}),
		"blank tenant":          s.PutUserState(agentconnect.UserLinkState{ConnectionID: "c", UserID: "u", Epoch: 1}),
	} {
		if !errors.Is(err, agentconnect.ErrInvalid) {
			t.Errorf("%s = %v, want ErrInvalid", name, err)
		}
	}
	unknown, _ := New(failingDB{}, func(values.TenantId) uuid.UUID { return uuid.Nil })
	if _, err := unknown.ListRevisions("nobody"); !errors.Is(err, agentconnect.ErrInvalid) {
		t.Fatalf("unknown tenant = %v, want ErrInvalid", err)
	}
	var nilStore *Store
	if _, err := nilStore.ListRevisions("tenant-a"); !errors.Is(err, agentconnect.ErrInvalid) {
		t.Fatalf("nil store = %v, want ErrInvalid", err)
	}
	if got := deref(nil); got != "" {
		t.Fatalf("deref(nil) = %q", got)
	}
	if got := nonNil(nil); got == nil {
		t.Fatal("nonNil(nil) returned nil")
	}
}
