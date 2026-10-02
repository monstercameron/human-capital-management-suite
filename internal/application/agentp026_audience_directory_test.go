package application

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

const (
	agentp026Host = "agentp026-host"
	agentp026Home = "agentp026-home"
)

func agentp026Context(t *testing.T, tenant, subject string, kind trust.SubjectKind) (context.Context, *trust.Principal) {
	t.Helper()
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId(tenant), Subject: subject, SubjectKind: kind,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial,
		SessionRef: "session", IssuedAt: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour), CredentialDigest: "credential",
	})
	if err != nil {
		t.Fatal(err)
	}
	return trust.WithPrincipal(context.Background(), principal), principal
}

// agentp026Room is one conversation in the host tenant with the viewer and a
// member whose home tenant is a different one.
func agentp026Room(host string) audienceChatFake {
	return audienceChatFake{
		rooms: []chat.Conversation{{ID: "room", TenantID: host}},
		members: map[string][]chat.Membership{"room": {
			{ConversationID: "room", TenantID: host, HomeTenantID: host, SubjectID: "viewer"},
			{ConversationID: "room", TenantID: host, HomeTenantID: agentp026Home, SubjectID: "guest"},
		}},
	}
}

// agentp026Installations places two personas in the room: one for the "staff"
// population and one for members who are also in the "managers" population.
func agentp026Installations(host string) audienceInstallStoreFake {
	tenant := values.TenantId(host)
	return audienceInstallStoreFake{store: audienceInstallFake{
		published: []agentpersonastore.PersonaVersion{{TenantID: tenant, PersonaID: "persona.policy", Version: 1}, {TenantID: tenant, PersonaID: "persona.comp", Version: 1}},
		active: []agentpersonastore.ActiveInstallation{
			{PersonaID: "persona.policy", PersonaVersion: 1, InstallationID: "install-staff", ConversationID: "room", Audience: agentpersonastore.InstallationAudience{Populations: []string{"staff"}}},
			{PersonaID: "persona.comp", PersonaVersion: 1, InstallationID: "install-managers", ConversationID: "room", Audience: agentpersonastore.InstallationAudience{Roles: []string{"member"}, Populations: []string{"managers"}, OrganizationScope: []string{"org:host:ops"}}},
		},
	}}
}

func agentp026InstallationIDs(installations []agentpersonastore.AvailableInstallation) []string {
	ids := make([]string, 0, len(installations))
	for _, installation := range installations {
		ids = append(ids, installation.InstallationID)
	}
	return ids
}

// agentp026Directory answers the adapter's role and population queries from a
// table keyed by home tenant and subject, and records which tenant each read
// was scoped to.
type agentp026Directory struct {
	mu          sync.Mutex
	roles       map[string][]string
	populations map[string][]string
	homes       map[string]string
	scoped      []string
}

func (d *agentp026Directory) adapter() *PersonaAudienceDirectoryDB {
	tenantUUID := func(tenant values.TenantId) uuid.UUID {
		return uuid.NewSHA1(uuid.NameSpaceDNS, []byte(tenant))
	}
	return NewPersonaAudienceDirectoryDB(NewAgentDirectoryDB(agentp026DB{directory: d}, tenantUUID), WithPersonaHomeOrganizationDirectory(d))
}

func (d *agentp026Directory) CurrentHomeOrganization(_ context.Context, tenant values.TenantId, subject string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.scoped = append(d.scoped, "home:"+string(tenant)+"/"+subject)
	organization, ok := d.homes[string(tenant)+"/"+subject]
	if !ok {
		return "", ErrAgentHomeOrganizationUnavailable
	}
	return organization, nil
}

type agentp026DB struct{ directory *agentp026Directory }

func (d agentp026DB) Begin(context.Context) (dbport.Tx, error) {
	return &agentp026Tx{directory: d.directory}, nil
}

// agentp026Tx resolves the tenant from the UUID the reader binds, so a read
// scoped to the wrong tenant returns that tenant's rows and the test sees it.
type agentp026Tx struct{ directory *agentp026Directory }

func (t *agentp026Tx) Exec(context.Context, string, ...any) (int64, error) { return 1, nil }
func (t *agentp026Tx) QueryRow(context.Context, string, ...any) dbport.Row { return directoryRowFake{} }
func (t *agentp026Tx) Commit(context.Context) error                        { return nil }
func (t *agentp026Tx) Rollback(context.Context) error                      { return nil }
func (t *agentp026Tx) Query(_ context.Context, sql string, args ...any) (dbport.Rows, error) {
	tenantID, subject := args[0].(uuid.UUID), args[1].(string)
	tenant := ""
	for _, candidate := range []string{"tenant", agentp026Home, agentp026Host} {
		if uuid.NewSHA1(uuid.NameSpaceDNS, []byte(candidate)) == tenantID {
			tenant = candidate
		}
	}
	t.directory.mu.Lock()
	defer t.directory.mu.Unlock()
	facts := t.directory.roles
	kind := "roles:"
	if strings.Contains(sql, "agent_current_population") {
		facts, kind = t.directory.populations, "populations:"
	}
	t.directory.scoped = append(t.directory.scoped, kind+tenant+"/"+subject)
	rows := &directoryRowsFake{}
	for _, fact := range facts[tenant+"/"+subject] {
		rows.rows = append(rows.rows, []any{fact})
	}
	return rows, nil
}

// TestTodo_AGENTP_026 composes the production adapter and source over an
// in-memory directory: persona discovery uses the member's current roles,
// named populations and home organization, read again on every discovery.
func TestTodo_AGENTP_026(t *testing.T) {
	ctx, principal := agentp026Context(t, "tenant", "viewer", trust.SubjectKindHuman)
	directory := &agentp026Directory{
		roles:       map[string][]string{"tenant/viewer": {"member"}, agentp026Home + "/guest": {"member"}},
		populations: map[string][]string{"tenant/viewer": {"staff"}, agentp026Home + "/guest": {"home-staff"}},
		homes:       map[string]string{"tenant/viewer": "org:host:ops", agentp026Home + "/guest": "org:home:field"},
	}
	source := &DatabasePersonaAudienceSource{Chat: agentp026Room("tenant"), Installations: agentp026Installations("tenant"), Directory: directory.adapter()}
	resolver := &CurrentPersonaAudience{Source: source}

	available, err := resolver.ResolveAvailablePersonaInstallations(ctx, principal)
	if err != nil || !slices.Equal(agentp026InstallationIDs(available), []string{"install-staff"}) {
		t.Fatalf("staff member candidates = %v, %v; want only the staff placement", agentp026InstallationIDs(available), err)
	}

	// A second named population makes the manager placement invocable on the
	// next discovery: nothing is cached between calls.
	directory.mu.Lock()
	directory.populations["tenant/viewer"] = []string{"managers", "staff"}
	directory.mu.Unlock()
	available, err = resolver.ResolveAvailablePersonaInstallations(ctx, principal)
	if err != nil || !slices.Equal(agentp026InstallationIDs(available), []string{"install-managers", "install-staff"}) {
		t.Fatalf("after a population change candidates = %v, %v", agentp026InstallationIDs(available), err)
	}

	// With the population fact gone, the role and organization that still
	// match must not keep any placement invocable.
	directory.mu.Lock()
	delete(directory.populations, "tenant/viewer")
	directory.mu.Unlock()
	available, err = resolver.ResolveAvailablePersonaInstallations(ctx, principal)
	if !errors.Is(err, ErrPersonaAudienceDirectoryFactsMissing) || len(available) != 0 {
		t.Fatalf("without a population fact candidates = %v, %v; want none and a missing-facts refusal", agentp026InstallationIDs(available), err)
	}
}

// TestTodo_AGENTP_026_Security covers the ways a persona could be shown to
// someone outside its audience through the directory binding.
func TestTodo_AGENTP_026_Security(t *testing.T) {
	facts := func() *agentp026Directory {
		return &agentp026Directory{
			roles:       map[string][]string{"tenant/viewer": {"member"}, agentp026Home + "/guest": {"member"}, "tenant/guest": {"hcm_admin"}},
			populations: map[string][]string{"tenant/viewer": {"contractors"}, agentp026Home + "/guest": {"home-staff"}, "tenant/guest": {"managers"}},
			homes:       map[string]string{"tenant/viewer": "org:host:ops", agentp026Home + "/guest": "org:home:field", "tenant/guest": "org:host:ops"},
		}
	}

	t.Run("role and organization match without the population is not eligibility", func(t *testing.T) {
		ctx, principal := agentp026Context(t, "tenant", "viewer", trust.SubjectKindHuman)
		directory := facts()
		source := &DatabasePersonaAudienceSource{Chat: agentp026Room("tenant"), Installations: agentp026Installations("tenant"), Directory: directory.adapter()}
		available, err := (&CurrentPersonaAudience{Source: source}).ResolveAvailablePersonaInstallations(ctx, principal)
		if err != nil || len(available) != 0 {
			t.Fatalf("contractor candidates = %v, %v; want none", agentp026InstallationIDs(available), err)
		}
	})

	t.Run("a foreign home-tenant member is read from its own tenant", func(t *testing.T) {
		ctx, _ := agentp026Context(t, "tenant", "viewer", trust.SubjectKindHuman)
		directory := facts()
		source := &DatabasePersonaAudienceSource{Chat: agentp026Room("tenant"), Installations: agentp026Installations("tenant"), Directory: directory.adapter()}
		audience, err := source.ListCurrentPersonaAudience(ctx, "tenant", "viewer")
		if err != nil || len(audience) != 1 || len(audience[0].Members) != 2 {
			t.Fatalf("audience = %+v, %v", audience, err)
		}
		guest := memberFor(audience[0].Members, "guest")
		// The host tenant holds a different, wider set of facts under the same
		// subject reference; using them would resolve the guest as a host admin.
		if !slices.Equal(guest.Roles, []string{"member"}) || !slices.Equal(guest.Populations, []string{"home-staff"}) || guest.OrganizationScope != "org:home:field" {
			t.Fatalf("guest facts = %+v; want the home tenant's facts", guest)
		}
		for _, read := range directory.scoped {
			if read == "roles:tenant/guest" || read == "populations:tenant/guest" || read == "home:tenant/guest" {
				t.Fatalf("guest was resolved under the host tenant: %v", directory.scoped)
			}
		}
	})

	t.Run("a member with missing or ambiguous facts is left out, never given empty facts", func(t *testing.T) {
		ctx, _ := agentp026Context(t, "tenant", "viewer", trust.SubjectKindHuman)
		for name, mutate := range map[string]func(*agentp026Directory){
			"missing population": func(d *agentp026Directory) { delete(d.populations, agentp026Home+"/guest") },
			"duplicate population": func(d *agentp026Directory) {
				d.populations[agentp026Home+"/guest"] = []string{"home-staff", "home-staff"}
			},
			"missing home": func(d *agentp026Directory) { delete(d.homes, agentp026Home+"/guest") },
			"missing role": func(d *agentp026Directory) { delete(d.roles, agentp026Home+"/guest") },
		} {
			directory := facts()
			mutate(directory)
			source := &DatabasePersonaAudienceSource{Chat: agentp026Room("tenant"), Installations: agentp026Installations("tenant"), Directory: directory.adapter()}
			audience, err := source.ListCurrentPersonaAudience(ctx, "tenant", "viewer")
			if err != nil || len(audience) != 1 || len(audience[0].Members) != 1 || audience[0].Members[0].SubjectID != "viewer" {
				t.Fatalf("%s: audience = %+v, %v; want only the viewer", name, audience, err)
			}
		}
	})

	t.Run("only the signed-in human of this tenant may discover", func(t *testing.T) {
		directory := facts()
		source := &DatabasePersonaAudienceSource{Chat: agentp026Room("tenant"), Installations: agentp026Installations("tenant"), Directory: directory.adapter()}
		human, _ := agentp026Context(t, "tenant", "viewer", trust.SubjectKindHuman)
		if _, err := source.ListCurrentPersonaAudience(human, "other-tenant", "viewer"); !errors.Is(err, errPersonaAudienceSourceUnavailable) {
			t.Fatalf("forged tenant error = %v", err)
		}
		if _, err := source.ListCurrentPersonaAudience(human, "tenant", "guest"); !errors.Is(err, errPersonaAudienceSourceUnavailable) {
			t.Fatalf("forged subject error = %v", err)
		}
		service, servicePrincipal := agentp026Context(t, "tenant", "viewer", trust.SubjectKindService)
		if _, err := source.ListCurrentPersonaAudience(service, "tenant", "viewer"); !errors.Is(err, errPersonaAudienceSourceUnavailable) {
			t.Fatalf("service principal error = %v", err)
		}
		if available, err := (&CurrentPersonaAudience{Source: source}).ResolveAvailablePersonaInstallations(service, servicePrincipal); err == nil || len(available) != 0 {
			t.Fatalf("service principal candidates = %v, %v", agentp026InstallationIDs(available), err)
		}
		if len(directory.scoped) != 0 {
			t.Fatalf("refused discovery still read the directory: %v", directory.scoped)
		}
	})
}

// agentp026Core seeds real directory tables for two tenants in one schema.
type agentp026Core struct {
	db      *pgtest.DB
	tenants map[values.TenantId]uuid.UUID
}

func newAgentp026Core(t *testing.T) *agentp026Core {
	t.Helper()
	core := &agentp026Core{db: pgtest.New(t), tenants: map[values.TenantId]uuid.UUID{agentp026Host: uuid.New(), agentp026Home: uuid.New()}}
	for key, id := range core.tenants {
		core.db.Exec(t, `INSERT INTO tenant (tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES ($1,$2,'cell-local',$3,'ACTIVE',timestamptz '2026-01-01T00:00:00Z')`, id, string(key), string(key))
	}
	return core
}

func (c *agentp026Core) tenantUUID(tenant values.TenantId) uuid.UUID { return c.tenants[tenant] }

func (c *agentp026Core) seedMember(t *testing.T, tenant values.TenantId, subject, role, population, organization string) {
	t.Helper()
	id := c.tenants[tenant]
	c.db.Exec(t, `INSERT INTO access_role (tenant_id,role_id,version,name,updated_by) VALUES ($1,$2,1,$2,'test') ON CONFLICT DO NOTHING`, id, role)
	c.db.Exec(t, `INSERT INTO worker_access_role_set (tenant_id,worker_ref,version,updated_by) VALUES ($1,$2,1,'test')`, id, subject)
	c.db.Exec(t, `INSERT INTO worker_access_role_assignment (tenant_id,worker_ref,role_id) VALUES ($1,$2,$3)`, id, subject, role)
	c.db.Exec(t, `INSERT INTO agent_current_population (tenant_id,membership_id,subject_ref,population_id,source_identity,revision,effective_from) VALUES ($1,$2,$3,$4,'directory',1,timestamptz '2026-01-01T00:00:00Z')`, id, uuid.New(), subject, population)
	c.db.Exec(t, `INSERT INTO agent_current_home_organization (tenant_id,fact_id,subject_ref,organization_scope_id,source_identity,revision,effective_from) VALUES ($1,$2,$3,$4,'directory',1,timestamptz '2026-01-01T00:00:00Z')`, id, uuid.New(), subject, organization)
}

func (c *agentp026Core) adapter(t *testing.T) *PersonaAudienceDirectoryDB {
	t.Helper()
	return NewPersonaAudienceDirectoryDB(NewAgentDirectoryDB(appRoleConnForPopulation(t, c.db), c.tenantUUID))
}

// TestTodo_AGENTP_026_Integration runs discovery over the real directory
// relations as the runtime role: facts come from each member's home tenant, a
// population revision changes the candidates on the next read, and a
// revocation leaves no candidate.
func TestTodo_AGENTP_026_Integration(t *testing.T) {
	core := newAgentp026Core(t)
	core.seedMember(t, agentp026Host, "viewer", "member", "staff", "org:host:ops")
	core.seedMember(t, agentp026Home, "guest", "member", "home-staff", "org:home:field")
	// The same subject reference in the host tenant carries wider facts that
	// must never be used for the guest.
	core.seedMember(t, agentp026Host, "guest", "hcm_admin", "managers", "org:host:ops")

	ctx, principal := agentp026Context(t, agentp026Host, "viewer", trust.SubjectKindHuman)
	source := &DatabasePersonaAudienceSource{Chat: agentp026Room(agentp026Host), Installations: agentp026Installations(agentp026Host), Directory: core.adapter(t)}
	resolver := &CurrentPersonaAudience{Source: source}

	audience, err := source.ListCurrentPersonaAudience(ctx, agentp026Host, "viewer")
	if err != nil || len(audience) != 1 || len(audience[0].Members) != 2 {
		t.Fatalf("audience = %+v, %v", audience, err)
	}
	viewer, guest := memberFor(audience[0].Members, "viewer"), memberFor(audience[0].Members, "guest")
	if !slices.Equal(viewer.Roles, []string{"member"}) || !slices.Equal(viewer.Populations, []string{"staff"}) || viewer.OrganizationScope != "org:host:ops" {
		t.Fatalf("viewer facts = %+v", viewer)
	}
	if !slices.Equal(guest.Roles, []string{"member"}) || !slices.Equal(guest.Populations, []string{"home-staff"}) || guest.OrganizationScope != "org:home:field" {
		t.Fatalf("guest facts = %+v; want the home tenant's facts, not the host tenant's", guest)
	}
	available, err := resolver.ResolveAvailablePersonaInstallations(ctx, principal)
	if err != nil || !slices.Equal(agentp026InstallationIDs(available), []string{"install-staff"}) {
		t.Fatalf("candidates = %v, %v; want only the staff placement", agentp026InstallationIDs(available), err)
	}

	// The directory supersedes the viewer's population with a new revision.
	host := core.tenants[agentp026Host]
	next := uuid.New()
	core.db.Exec(t, `UPDATE agent_current_population SET superseded_at=CURRENT_TIMESTAMP,superseded_by=$3 WHERE tenant_id=$1 AND subject_ref=$2 AND revoked_at IS NULL AND superseded_at IS NULL`, host, "viewer", next)
	core.db.Exec(t, `INSERT INTO agent_current_population (tenant_id,membership_id,subject_ref,population_id,source_identity,revision,effective_from) VALUES ($1,$2,'viewer','managers','directory',2,timestamptz '2026-02-01T00:00:00Z')`, host, next)
	available, err = resolver.ResolveAvailablePersonaInstallations(ctx, principal)
	if err != nil || !slices.Equal(agentp026InstallationIDs(available), []string{"install-managers"}) {
		t.Fatalf("after the revision candidates = %v, %v; want only the manager placement", agentp026InstallationIDs(available), err)
	}

	// A revocation leaves the viewer's role and home organization in place.
	core.db.Exec(t, `UPDATE agent_current_population SET revoked_at=CURRENT_TIMESTAMP,revoked_by='admin',revocation_reason='left the population' WHERE tenant_id=$1 AND membership_id=$2`, host, next)
	available, err = resolver.ResolveAvailablePersonaInstallations(ctx, principal)
	if !errors.Is(err, ErrPersonaAudienceDirectoryFactsMissing) || len(available) != 0 {
		t.Fatalf("after the revocation candidates = %v, %v; want none", agentp026InstallationIDs(available), err)
	}
}

// TestTodo_AGENTP_026_Race revokes a member's population while several
// discoveries read it. A read returns the complete facts or a refusal, and
// every read started after the revocation committed is a refusal.
func TestTodo_AGENTP_026_Race(t *testing.T) {
	core := newAgentp026Core(t)
	core.seedMember(t, agentp026Host, "viewer", "member", "staff", "org:host:ops")
	ctx, _ := agentp026Context(t, agentp026Host, "viewer", trust.SubjectKindHuman)

	const readers = 4
	adapters := make([]*PersonaAudienceDirectoryDB, readers)
	for i := range adapters {
		adapters[i] = core.adapter(t)
	}
	revoked := make(chan struct{})
	failures := make(chan error, readers)
	var wg sync.WaitGroup
	for _, adapter := range adapters {
		wg.Add(1)
		go func() {
			defer wg.Done()
			after := 0
			for after < 5 {
				committed := false
				select {
				case <-revoked:
					committed = true
				default:
				}
				facts, err := adapter.ResolvePersonaAudienceMember(ctx, agentp026Host, "viewer")
				switch {
				case err == nil:
					if committed {
						failures <- errors.New("a read started after the revocation still resolved the member")
						return
					}
					if !slices.Equal(facts.Roles, []string{"member"}) || !slices.Equal(facts.Populations, []string{"staff"}) || facts.OrganizationScope != "org:host:ops" {
						failures <- errors.New("a concurrent read returned partial facts")
						return
					}
				case !errors.Is(err, ErrPersonaAudienceDirectoryUnavailable):
					failures <- err
					return
				}
				if committed {
					after++
				}
			}
		}()
	}
	core.db.Exec(t, `UPDATE agent_current_population SET revoked_at=CURRENT_TIMESTAMP,revoked_by='admin',revocation_reason='left the population' WHERE tenant_id=$1 AND subject_ref='viewer'`, core.tenants[agentp026Host])
	close(revoked)
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
}
