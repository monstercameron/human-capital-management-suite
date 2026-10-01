package agentdelegationstore

import (
	"context"
	"errors"
	"math"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestMain(m *testing.M) { pgtest.RunMain(m) }

var storeNow = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

type fixtureEnv struct {
	db     *pgtest.DB
	ids    map[values.TenantId]uuid.UUID
	mapper func(values.TenantId) uuid.UUID
}

func newEnv(t *testing.T, tenants ...values.TenantId) *fixtureEnv {
	t.Helper()
	db := pgtest.New(t)
	env := &fixtureEnv{db: db, ids: map[values.TenantId]uuid.UUID{}}
	for _, tenant := range tenants {
		id := uuid.New()
		env.ids[tenant] = id
		db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, id, string(tenant), string(tenant))
	}
	env.mapper = func(key values.TenantId) uuid.UUID { return env.ids[key] }
	return env
}

// appConn opens a fresh connection that has dropped to the application role so
// row level security is enforced.
func (e *fixtureEnv) appConn(t *testing.T) dbport.Beginner {
	t.Helper()
	conn := e.db.NewConn(t)
	if _, err := conn.Exec(context.Background(), "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatalf("assume app role: %v", err)
	}
	return conn
}

// grants returns a new store instance (own connection) scoped to tenant.
func (e *fixtureEnv) grants(t *testing.T, tenant values.TenantId) agentdelegation.GrantStore {
	t.Helper()
	store, err := New(e.appConn(t), e.mapper)
	if err != nil {
		t.Fatal(err)
	}
	gs, err := store.ForTenant(context.Background(), tenant)
	if err != nil {
		t.Fatal(err)
	}
	return gs
}

func userAuthority(tenant values.TenantId) trust.AuthorityScope {
	return trust.AuthorityScope{
		Tenant: tenant, OrganizationScopeID: "org-west", Capabilities: []string{"people.read", "people.write"},
		Resources: []string{"worker:42"}, Fields: []string{"display_name"}, Purposes: []string{"agent.read"},
		Assurance: trust.AssuranceSubstantial, NotBefore: storeNow.Add(-time.Hour), ExpiresAt: storeNow.Add(48 * time.Hour),
	}
}

func newService(t *testing.T, tenant values.TenantId, store agentdelegation.GrantStore) *agentdelegation.Service {
	t.Helper()
	resolver := agentdelegation.ResolverFunc(func(string, values.TenantId, string, time.Time) (agentdelegation.UserAuthority, error) {
		return agentdelegation.UserAuthority{UserID: "user-42", Active: true, Authority: userAuthority(tenant)}, nil
	})
	service, err := agentdelegation.NewService(agentdelegation.Config{Store: store, Authority: resolver, Secret: []byte("0123456789abcdef0123456789abcdef"), Now: func() time.Time { return storeNow }})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func grantRequest(tenant values.TenantId, id string) agentdelegation.GrantRequest {
	return agentdelegation.GrantRequest{
		GrantID: id, UserID: "user-42", Tenant: tenant, AgentVersion: "agent-v3", InstallationID: "install-7",
		TaskID: "task-9", PlanSkillSetDigest: "sha256:plan-42", Purpose: "agent.read", OrganizationScopeID: "org-west",
		Skills:      []string{"people.lookup", "people.update"},
		SkillScopes: map[string][]string{"people.lookup": {"people.read"}, "people.update": {"people.write"}},
		ExpiresAt:   storeNow.Add(24 * time.Hour), UserAuthority: userAuthority(tenant),
	}
}

func scopedAuthority() trust.SkillAuthorities {
	return trust.SkillAuthorities{
		"people.lookup": {Capabilities: []string{"people.read"}, Resources: []string{"worker:42"}, Fields: []string{"display_name"}, Purposes: []string{"agent.read"}},
		"people.update": {Capabilities: []string{"people.write"}, Resources: []string{"worker:42"}, Fields: []string{"status"}, Purposes: []string{"agent.read"}},
	}
}

func exchange(id string) agentdelegation.ExchangeRequest {
	return agentdelegation.ExchangeRequest{SubjectToken: id, SubjectTokenType: agentdelegation.DelegationGrantTokenType,
		RunID: "run-1", StepID: "step-1", Skill: "people.lookup", Scope: []string{"people.read"},
		Audience: "capability-gateway", Lifetime: 4 * time.Minute, Sender: "workload/worker-7"}
}

// The durable store must hand back exactly what the reference memory store
// hands back for the same grant, and the token service must work over it.
func TestTodo_AGENT2_003_Integration(t *testing.T) {
	env := newEnv(t, "acme-corp")
	pg := env.grants(t, "acme-corp")
	service := newService(t, "acme-corp", pg)
	request := grantRequest("acme-corp", "grant-42")
	request.TargetAgentID = "agent:comp-analyst"
	saved, err := service.CreateGrant(request)
	if err != nil {
		t.Fatal(err)
	}
	if saved.AgentVersion != "agent-v3" || saved.TargetAgentID != "agent:comp-analyst" || saved.Authority.Delegate != "agent:comp-analyst" {
		t.Fatalf("grant identity conflated version and target: version=%q target=%q delegate=%q", saved.AgentVersion, saved.TargetAgentID, saved.Authority.Delegate)
	}
	memory := agentdelegation.NewMemoryGrantStore()
	if err := memory.Save(saved); err != nil {
		t.Fatal(err)
	}
	want, _ := memory.Get("grant-42")
	got, err := pg.Get("grant-42")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("postgres grant differs from memory reference\n got: %+v\nwant: %+v", got, want)
	}
	if got.TargetAgentID != "agent:comp-analyst" || got.TaskID != "task-9" {
		t.Fatalf("restored grant target/run scope = %q/%q", got.TargetAgentID, got.TaskID)
	}
	if _, err := service.Exchange(exchange("grant-42")); err != nil {
		t.Fatalf("Exchange over postgres store: %v", err)
	}

	if err := service.RevokeGrant("grant-42", "task cancelled"); err != nil {
		t.Fatal(err)
	}
	// A brand new store instance on a brand new connection sees the revocation.
	fresh := env.grants(t, "acme-corp")
	revoked, err := fresh.Get("grant-42")
	if err != nil {
		t.Fatal(err)
	}
	if !revoked.Revoked || !revoked.Authority.Revoked {
		t.Fatalf("revocation not durable: Revoked=%v Authority.Revoked=%v", revoked.Revoked, revoked.Authority.Revoked)
	}
	if _, err := newService(t, "acme-corp", fresh).Exchange(exchange("grant-42")); !errors.Is(err, agentdelegation.ErrGrantRevoked) {
		t.Fatalf("Exchange after durable revoke = %v, want ErrGrantRevoked", err)
	}
	// Revoke is idempotent and does not rewrite the first reason.
	if err := fresh.Revoke("grant-42", "second reason"); err != nil {
		t.Fatalf("second Revoke: %v", err)
	}
	var reason string
	if err := env.db.QueryRow(context.Background(), `SELECT revoked_reason FROM agent_delegation_grant WHERE grant_id='grant-42'`).Scan(&reason); err != nil {
		t.Fatal(err)
	}
	if reason != "task cancelled" {
		t.Fatalf("revoked_reason = %q, want the first reason", reason)
	}
}

func TestTodo_AGENT2_003_SaveRules(t *testing.T) {
	env := newEnv(t, "acme-corp", "globex")
	pg := env.grants(t, "acme-corp")
	service := newService(t, "acme-corp", pg)
	g, err := service.CreateGrant(grantRequest("acme-corp", "grant-a"))
	if err != nil {
		t.Fatal(err)
	}
	if err := pg.Save(g); !errors.Is(err, agentdelegation.ErrInvalidGrant) {
		t.Fatalf("duplicate Save = %v, want ErrInvalidGrant", err)
	}
	// A duplicate must not have mutated the stored grant.
	stored, _ := pg.Get("grant-a")
	if stored.TaskID != g.TaskID {
		t.Fatal("duplicate save overwrote the stored grant")
	}
	invalid := g
	invalid.GrantID, invalid.Skills = "grant-b", nil
	if err := pg.Save(invalid); !errors.Is(err, agentdelegation.ErrInvalidGrant) {
		t.Fatalf("invalid Save = %v, want ErrInvalidGrant", err)
	}
	stale := g
	stale.GrantID, stale.RevocationEpoch = "grant-c", 7
	if err := pg.Save(stale); !errors.Is(err, agentdelegation.ErrGrantRevoked) {
		t.Fatalf("stale-epoch Save = %v, want ErrGrantRevoked", err)
	}
	if _, err := pg.Get("grant-c"); !errors.Is(err, agentdelegation.ErrGrantNotFound) {
		t.Fatalf("rejected grant was persisted: %v", err)
	}
	foreign := g
	foreign.GrantID, foreign.Tenant = "grant-d", "globex"
	if err := pg.Save(foreign); !errors.Is(err, agentdelegation.ErrInvalidGrant) {
		t.Fatalf("cross-tenant Save = %v, want ErrInvalidGrant", err)
	}
	if err := pg.Revoke("grant-a", "  "); !errors.Is(err, agentdelegation.ErrInvalidRequest) {
		t.Fatalf("blank-reason Revoke = %v, want ErrInvalidRequest", err)
	}
	if err := pg.Revoke("missing", "why"); !errors.Is(err, agentdelegation.ErrGrantNotFound) {
		t.Fatalf("Revoke(missing) = %v, want ErrGrantNotFound", err)
	}
	// A grant saved already-revoked stays revoked, as in the memory store.
	pre := g
	pre.GrantID, pre.Revoked = "grant-e", true
	pre.Authority.Revoked = true
	if err := pg.Save(pre); err != nil {
		t.Fatal(err)
	}
	if got, _ := pg.Get("grant-e"); !got.Revoked {
		t.Fatal("pre-revoked grant lost its revoked flag")
	}
}

func TestTodo_AGENTP_008_ScopedAuthoritiesRoundTrip(t *testing.T) {
	env := newEnv(t, "acme-corp")
	pg := env.grants(t, "acme-corp")
	current := userAuthority("acme-corp")
	current.SkillAuthorities = scopedAuthority()
	current.SkillAuthorities = trust.CloneSkillAuthorities(current.SkillAuthorities)
	resolver := agentdelegation.ResolverFunc(func(string, values.TenantId, string, time.Time) (agentdelegation.UserAuthority, error) {
		return agentdelegation.UserAuthority{UserID: "user-42", Active: true, Authority: current, SkillAuthorities: current.SkillAuthorities}, nil
	})
	service, err := agentdelegation.NewService(agentdelegation.Config{Store: pg, Authority: resolver, Secret: []byte("0123456789abcdef0123456789abcdef"), Now: func() time.Time { return storeNow }})
	if err != nil {
		t.Fatal(err)
	}
	req := grantRequest("acme-corp", "scoped-round-trip")
	req.UserAuthority = current
	req.SkillAuthorities = trust.CloneSkillAuthorities(current.SkillAuthorities)
	g, err := service.CreateScopedGrant(req)
	if err != nil {
		t.Fatal(err)
	}
	got, err := pg.Get(g.GrantID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(g.SkillAuthorities, got.SkillAuthorities) {
		t.Fatalf("top-level scoped authority changed after restore: got=%v want=%v", got.SkillAuthorities, g.SkillAuthorities)
	}
	if !reflect.DeepEqual(got.SkillAuthorities, got.Authority.SkillAuthorities) {
		t.Fatalf("restored top-level and embedded scoped authorities differ: top=%v embedded=%v", got.SkillAuthorities, got.Authority.SkillAuthorities)
	}
	embeddedOnly := g
	embeddedOnly.GrantID = "scoped-embedded-only"
	embeddedOnly.SkillAuthorities = nil
	if err := pg.Save(embeddedOnly); err != nil {
		t.Fatalf("Save embedded-only scoped authority: %v", err)
	}
	restored, err := pg.Get(embeddedOnly.GrantID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(restored.SkillAuthorities, g.SkillAuthorities) {
		t.Fatalf("embedded-only scoped authority was not restored: got=%v want=%v", restored.SkillAuthorities, g.SkillAuthorities)
	}
}

func TestTodo_AGENTP_008_ScopedAuthorityMismatchRejected(t *testing.T) {
	env := newEnv(t, "acme-corp")
	pg := env.grants(t, "acme-corp")
	current := userAuthority("acme-corp")
	current.SkillAuthorities = scopedAuthority()
	current.SkillAuthorities = trust.CloneSkillAuthorities(current.SkillAuthorities)
	resolver := agentdelegation.ResolverFunc(func(string, values.TenantId, string, time.Time) (agentdelegation.UserAuthority, error) {
		return agentdelegation.UserAuthority{UserID: "user-42", Active: true, Authority: current, SkillAuthorities: current.SkillAuthorities}, nil
	})
	service, err := agentdelegation.NewService(agentdelegation.Config{Store: pg, Authority: resolver, Secret: []byte("0123456789abcdef0123456789abcdef"), Now: func() time.Time { return storeNow }})
	if err != nil {
		t.Fatal(err)
	}
	req := grantRequest("acme-corp", "scoped-mismatch")
	req.UserAuthority = current
	req.SkillAuthorities = trust.CloneSkillAuthorities(current.SkillAuthorities)
	g, err := service.CreateScopedGrant(req)
	if err != nil {
		t.Fatal(err)
	}
	g.GrantID = "scoped-forged"
	g.SkillAuthorities = trust.CloneSkillAuthorities(g.SkillAuthorities)
	g.SkillAuthorities["people.lookup"] = trust.SkillAuthority{Capabilities: []string{"people.read"}, Resources: []string{"worker:99"}, Fields: []string{"display_name"}, Purposes: []string{"agent.read"}}
	if err := pg.Save(g); !errors.Is(err, agentdelegation.ErrInvalidGrant) {
		t.Fatalf("mismatched scoped authority Save = %v, want ErrInvalidGrant", err)
	}
	if _, err := pg.Get("scoped-forged"); !errors.Is(err, agentdelegation.ErrGrantNotFound) {
		t.Fatalf("mismatched grant persisted: %v", err)
	}
}

func TestTodo_AGENT2_003_CrossTenantIsolation(t *testing.T) {
	env := newEnv(t, "acme-corp", "globex")
	a := env.grants(t, "acme-corp")
	b := env.grants(t, "globex")
	grant := grantRequest("acme-corp", "grant-a")
	grant.TargetAgentID = "agent:comp-analyst"
	if _, err := newService(t, "acme-corp", a).CreateGrant(grant); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Get("grant-a"); !errors.Is(err, agentdelegation.ErrGrantNotFound) {
		t.Fatalf("tenant B Get(A's grant) = %v, want ErrGrantNotFound", err)
	}
	if err := b.Revoke("grant-a", "hostile"); !errors.Is(err, agentdelegation.ErrGrantNotFound) {
		t.Fatalf("tenant B Revoke(A's grant) = %v, want ErrGrantNotFound", err)
	}
	if got, err := a.Get("grant-a"); err != nil || got.Revoked {
		t.Fatalf("tenant A grant after B's attempts: revoked=%v err=%v", got.Revoked, err)
	}
	// The same grant id is independent per tenant.
	if _, err := newService(t, "globex", b).CreateGrant(grantRequest("globex", "grant-a")); err != nil {
		t.Fatalf("tenant B could not reuse the id: %v", err)
	}
	// Epochs are per tenant and per adapter.
	if _, err := a.BumpRevocationEpoch("acme-corp", "user-42", "kill switch"); err != nil {
		t.Fatal(err)
	}
	if got := b.CurrentRevocationEpoch("globex", "user-42"); got != 1 {
		t.Fatalf("tenant B epoch moved to %d after tenant A bump", got)
	}
	if _, err := b.BumpRevocationEpoch("acme-corp", "user-42", "hostile"); !errors.Is(err, agentdelegation.ErrInvalidRequest) {
		t.Fatalf("cross-tenant bump = %v, want ErrInvalidRequest", err)
	}
	if got := b.CurrentRevocationEpoch("acme-corp", "user-42"); got != math.MaxUint64 {
		t.Fatalf("cross-tenant epoch read = %d, want fail-closed maximum", got)
	}

	// Independent of the adapter: with the app role and tenant B's context the
	// database itself refuses to show or write tenant A's rows.
	raw := env.appConn(t).(interface {
		Exec(context.Context, string, ...any) (int64, error)
		QueryRow(context.Context, string, ...any) dbport.Row
	})
	ctx := context.Background()
	if _, err := raw.Exec(ctx, `SELECT set_config('app.tenant_id',$1,false)`, env.ids["globex"].String()); err != nil {
		t.Fatal(err)
	}
	var visible int
	if err := raw.QueryRow(ctx, `SELECT count(*) FROM agent_delegation_grant WHERE tenant_id=$1`, env.ids["acme-corp"]).Scan(&visible); err != nil || visible != 0 {
		t.Fatalf("RLS leaked %d tenant A rows to tenant B (err=%v)", visible, err)
	}
	if _, err := raw.Exec(ctx, `INSERT INTO agent_delegation_epoch (tenant_id,user_id,epoch) VALUES ($1,'x',1)`, env.ids["acme-corp"]); err == nil {
		t.Fatal("RLS WITH CHECK allowed tenant B to write a tenant A epoch row")
	}
}

func TestTodo_AGENT2_003_EpochMonotonicUnderConcurrency(t *testing.T) {
	env := newEnv(t, "acme-corp")
	seed := env.grants(t, "acme-corp")
	if got := seed.CurrentRevocationEpoch("acme-corp", "user-42"); got != 1 {
		t.Fatalf("initial epoch = %d, want 1", got)
	}
	const workers, each = 6, 5
	stores := make([]agentdelegation.GrantStore, workers)
	for i := range stores {
		stores[i] = env.grants(t, "acme-corp")
	}
	var (
		mu   sync.Mutex
		seen []int
		wg   sync.WaitGroup
	)
	errs := make(chan error, workers*each)
	for _, gs := range stores {
		wg.Add(1)
		go func(gs agentdelegation.GrantStore) {
			defer wg.Done()
			last := uint64(0)
			for i := 0; i < each; i++ {
				epoch, err := gs.BumpRevocationEpoch("acme-corp", "user-42", "concurrent")
				if err != nil {
					errs <- err
					return
				}
				if epoch <= last {
					errs <- errors.New("a single caller observed a non-increasing epoch")
					return
				}
				last = epoch
				mu.Lock()
				seen = append(seen, int(epoch))
				mu.Unlock()
			}
		}(gs)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	sort.Ints(seen)
	for i, epoch := range seen {
		if epoch != i+2 {
			t.Fatalf("bump results are not the contiguous run 2..%d (lost or duplicated update): %v", workers*each+1, seen)
		}
	}
	if got := seed.CurrentRevocationEpoch("acme-corp", "user-42"); got != uint64(workers*each+1) {
		t.Fatalf("final epoch = %d, want %d", got, workers*each+1)
	}
}

func TestTodo_AGENT2_003_EpochFencesGrants(t *testing.T) {
	env := newEnv(t, "acme-corp")
	pg := env.grants(t, "acme-corp")
	service := newService(t, "acme-corp", pg)
	g, err := service.CreateGrant(grantRequest("acme-corp", "grant-a"))
	if err != nil {
		t.Fatal(err)
	}
	if epoch, err := service.BumpUserRevocationEpoch("acme-corp", "user-42", "deactivation"); err != nil || epoch != 2 {
		t.Fatalf("bump = %d, %v; want 2", epoch, err)
	}
	if _, err := service.Exchange(exchange("grant-a")); !errors.Is(err, agentdelegation.ErrGrantRevoked) {
		t.Fatalf("Exchange after epoch bump = %v, want ErrGrantRevoked", err)
	}
	g.GrantID = "grant-old"
	if err := pg.Save(g); !errors.Is(err, agentdelegation.ErrGrantRevoked) {
		t.Fatalf("Save with pre-bump epoch = %v, want ErrGrantRevoked", err)
	}
	// A grant minted after the bump carries the new epoch and works.
	if fresh, err := service.CreateGrant(grantRequest("acme-corp", "grant-new")); err != nil || fresh.RevocationEpoch != 2 {
		t.Fatalf("post-bump CreateGrant epoch=%d err=%v", fresh.RevocationEpoch, err)
	}
	if _, err := service.Exchange(exchange("grant-new")); err != nil {
		t.Fatalf("Exchange on post-bump grant: %v", err)
	}
	for _, bad := range []struct{ user, reason string }{{"", "r"}, {"user-42", " "}} {
		if _, err := pg.BumpRevocationEpoch("acme-corp", bad.user, bad.reason); !errors.Is(err, agentdelegation.ErrInvalidRequest) {
			t.Fatalf("Bump(%q,%q) = %v, want ErrInvalidRequest", bad.user, bad.reason, err)
		}
	}
	if _, err := pg.BumpRevocationEpoch("Bad Tenant", "user-42", "r"); !errors.Is(err, agentdelegation.ErrInvalidRequest) {
		t.Fatalf("Bump(invalid tenant) = %v, want ErrInvalidRequest", err)
	}
	if got := pg.CurrentRevocationEpoch("acme-corp", " "); got != math.MaxUint64 {
		t.Fatalf("blank-user epoch = %d, want fail-closed maximum", got)
	}
}

// The append-only guard is enforced by the database, not only by the adapter.
func TestTodo_AGENT2_003_DatabaseGuards(t *testing.T) {
	env := newEnv(t, "acme-corp")
	pg := env.grants(t, "acme-corp")
	if _, err := newService(t, "acme-corp", pg).CreateGrant(grantRequest("acme-corp", "grant-a")); err != nil {
		t.Fatal(err)
	}
	if _, err := pg.BumpRevocationEpoch("acme-corp", "user-42", "bump"); err != nil {
		t.Fatal(err)
	}
	raw := env.appConn(t).(interface {
		Exec(context.Context, string, ...any) (int64, error)
	})
	ctx := context.Background()
	if _, err := raw.Exec(ctx, `SELECT set_config('app.tenant_id',$1,false)`, env.ids["acme-corp"].String()); err != nil {
		t.Fatal(err)
	}
	for name, stmt := range map[string]string{
		"widen purpose":       `UPDATE agent_delegation_grant SET purpose='agent.write' WHERE grant_id='grant-a'`,
		"extend expiry":       `UPDATE agent_delegation_grant SET expires_at=expires_at+interval '1 hour' WHERE grant_id='grant-a'`,
		"alter authority":     `UPDATE agent_delegation_grant SET authority=jsonb_set(authority,'{Purposes}','["x"]') WHERE grant_id='grant-a'`,
		"delete grant":        `DELETE FROM agent_delegation_grant WHERE grant_id='grant-a'`,
		"decrease epoch":      `UPDATE agent_delegation_epoch SET epoch=1 WHERE user_id='user-42'`,
		"delete epoch":        `DELETE FROM agent_delegation_epoch WHERE user_id='user-42'`,
		"zero epoch":          `INSERT INTO agent_delegation_epoch (tenant_id,user_id,epoch) SELECT tenant_id,'other',0 FROM agent_delegation_epoch LIMIT 1`,
		"revoked without why": `UPDATE agent_delegation_grant SET revoked_reason='x' WHERE grant_id='grant-a' AND NOT revoked`,
	} {
		if _, err := raw.Exec(ctx, stmt); err == nil {
			t.Errorf("%s: database accepted a forbidden mutation", name)
		}
	}
	if err := pg.Revoke("grant-a", "done"); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(ctx, `UPDATE agent_delegation_grant SET revoked=false, revoked_reason=NULL, revoked_at=NULL WHERE grant_id='grant-a'`); err == nil {
		t.Error("database allowed a revoked grant to be reinstated")
	}
	if _, err := raw.Exec(ctx, `UPDATE agent_delegation_grant SET revoked_reason='rewritten' WHERE grant_id='grant-a'`); err == nil {
		t.Error("database allowed the revocation record to be rewritten")
	}
}

type failingDB struct{ err error }

func (f failingDB) Begin(context.Context) (dbport.Tx, error) { return nil, f.err }

func TestTodo_AGENT2_003_ConstructionAndFailClosed(t *testing.T) {
	if _, err := New(nil, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("New(nil) = %v, want ErrInvalid", err)
	}
	mapper := func(values.TenantId) uuid.UUID { return uuid.New() }
	store, err := New(failingDB{err: errors.New("db down")}, mapper)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ForTenant(nil, "acme-corp"); !errors.Is(err, ErrInvalid) { //nolint:staticcheck // nil context is the case under test
		t.Fatalf("ForTenant(nil ctx) = %v, want ErrInvalid", err)
	}
	if _, err := store.ForTenant(context.Background(), "Not Valid"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("ForTenant(invalid) = %v, want ErrInvalid", err)
	}
	var nilStore *Store
	if _, err := nilStore.ForTenant(context.Background(), "acme-corp"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("nil store ForTenant = %v, want ErrInvalid", err)
	}
	unknown, _ := New(failingDB{}, func(values.TenantId) uuid.UUID { return uuid.Nil })
	if _, err := unknown.ForTenant(context.Background(), "acme-corp"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("ForTenant(unknown tenant) = %v, want ErrInvalid", err)
	}

	gs, err := store.ForTenant(context.Background(), "acme-corp")
	if err != nil {
		t.Fatal(err)
	}
	if got := gs.CurrentRevocationEpoch("acme-corp", "user-42"); got != math.MaxUint64 {
		t.Fatalf("epoch on database failure = %d, want fail-closed maximum", got)
	}
	if _, err := gs.Get("g"); err == nil || errors.Is(err, agentdelegation.ErrGrantNotFound) {
		t.Fatalf("Get on database failure = %v, want a non-NotFound error", err)
	}
	if err := gs.Revoke("g", "r"); err == nil {
		t.Fatal("Revoke on database failure succeeded")
	}
	if _, err := gs.BumpRevocationEpoch("acme-corp", "user-42", "r"); err == nil {
		t.Fatal("Bump on database failure succeeded")
	}
	env := agentdelegation.Grant{}
	if err := gs.Save(env); !errors.Is(err, agentdelegation.ErrInvalidGrant) {
		t.Fatalf("Save(zero grant) = %v, want ErrInvalidGrant", err)
	}
	valid := grantForFailure()
	if err := gs.Save(valid); err == nil || errors.Is(err, agentdelegation.ErrInvalidGrant) {
		t.Fatalf("Save on database failure = %v, want an infrastructure error", err)
	}
}

func grantForFailure() agentdelegation.Grant {
	return agentdelegation.Grant{GrantID: "g", UserID: "user-42", Tenant: "acme-corp", AgentVersion: "v", InstallationID: "i",
		TaskID: "t", PlanSkillSetDigest: "d", Purpose: "p", OrganizationScopeID: "o", Skills: []string{"s"},
		SkillScopes: map[string][]string{"s": {"c"}}, NotBefore: storeNow, ExpiresAt: storeNow.Add(time.Hour), RevocationEpoch: 1, Authority: trust.DelegationGrant{Delegate: "v"}}
}
