package agentaccess

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/GoWebComponents/v5/ui"
	"github.com/monstercameron/human-capital-management-suite/internal/agentconnect"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/connectivity"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/lease"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/secrets"
)

type fakeIssuer struct{ next int }

func (f *fakeIssuer) Mint(req lease.Request) (lease.CredentialLease, lease.Evidence, error) {
	f.next++
	got := lease.CredentialLease{ID: fmt.Sprintf("lease-%d", f.next), CustodyLeaseID: fmt.Sprintf("custody-%d", f.next), Handle: req.Handle, Workload: req.Workload, Tenant: req.Tenant, Purpose: req.Purpose, Destination: req.Destination, Operation: req.Operation, Nonce: fmt.Sprintf("nonce-%d", f.next), IssuedAt: time.Unix(100, 0), ExpiresAt: time.Unix(100, 0).Add(req.TTL)}
	return got, lease.Evidence{LeaseID: got.ID, Outcome: "granted"}, nil
}
func (f *fakeIssuer) Use(got lease.CredentialLease, destination string, operation custody.Operation) (lease.Evidence, error) {
	return lease.Evidence{LeaseID: got.ID, Destination: destination, Operation: operation, Outcome: "granted"}, nil
}
func (f *fakeIssuer) Revoke(id, _ string) (lease.Evidence, error) {
	return lease.Evidence{LeaseID: id, Outcome: "granted"}, nil
}

func fixtureConnection(t *testing.T, id, tenant string) *connectivity.ConnectorConnection {
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
	connection, err := connectivity.NewConnection(connectivity.Publication{Definition: definition}, connectivity.ConnectionSpec{ConnectionID: id, TenantID: tenant, OrgID: "org-a", SystemID: "system-a", Environment: connectivity.EnvironmentProduction, Residency: "us", ConnectorID: "hris", ConnectorVersion: version, AuthMode: connectivity.AuthOAuth2ClientCredentials, CredentialRef: credential, Scopes: []string{"worker.read"}, EndpointPolicy: connectivity.EndpointPolicy{AllowedHosts: []string{"hris.example"}, RequireTLS: true, EgressProfile: "egress/us"}, Capabilities: []connectivity.Capability{capability}, Bounds: definition.Bounds, CreatedAt: time.Unix(1, 0)})
	if err != nil {
		t.Fatal(err)
	}
	for _, state := range []connectivity.LifecycleState{connectivity.StateValidating, connectivity.StateReady, connectivity.StateActive} {
		if err := connection.Transition(state, connectivity.TransitionEvidence{Reason: "test", ActorRef: "admin:test", EvidenceRef: "evidence:test", OccurredAt: time.Unix(2, 0)}); err != nil {
			t.Fatal(err)
		}
	}
	return connection
}

func fixtureBinding(tenant, id string) agentconnect.CredentialBinding {
	return agentconnect.CredentialBinding{Reference: secrets.SecretReference{ID: id, Kind: secrets.OAuthGrant, Version: "v1", Provider: "vault", ProviderPath: "opaque/path", Tenant: tenant, Region: "us", State: secrets.Active}, Handle: custody.Handle{ID: id, Kind: custody.Secret, Version: "v1", Tenant: tenant, Region: "us"}}
}

func fixtureUser(tenant, id string) agentconnect.UserContext {
	return agentconnect.UserContext{TenantID: tenant, UserID: id, Roles: []string{"manager"}, Population: "employees", OrganizationScopes: []string{"org-a"}}
}

// fixtureRegistry registers one user-delegated connection for tenant-a that
// grants managers a read skill and a governed submission skill, and a second
// connection that grants nobody in these tests anything.
func fixtureRegistry(t *testing.T) *agentconnect.Registry {
	t.Helper()
	registry, err := agentconnect.NewRegistry(&fakeIssuer{}, func() time.Time { return time.Unix(200, 0) })
	if err != nil {
		t.Fatal(err)
	}
	tool := func(name string, class agentsecurity.ToolClass) agentsecurity.ToolDescriptor {
		return agentsecurity.ToolDescriptor{Name: name, Capability: name, Version: 1, Class: class, DataScope: []string{"workers.basic"}, Cost: 1, Schema: name + ".v1"}
	}
	revision := agentconnect.ConnectionRevision{ID: "hris", TenantID: "tenant-a", Revision: 1, Endpoint: "hris.example", Connection: fixtureConnection(t, "hris", "tenant-a"), CredentialMode: agentconnect.UserDelegated,
		Skills: []agentconnect.SkillExposure{
			{ID: "workers.read", Version: "1", Tool: tool("workers.read", agentsecurity.ToolRead), Tier: agentconnect.TierT0, CredentialOperation: custody.LeaseOperation},
			{ID: "promotions.submit", Version: "1", Tool: tool("promotions.submit", agentsecurity.ToolRead), Tier: agentconnect.TierT3, CredentialOperation: custody.LeaseOperation},
		},
		Grants: []agentconnect.GrantScope{{ID: "managers", Roles: []string{"manager"}, Population: "employees", OrganizationScopes: []string{"org-a"}, Skills: []string{"workers.read", "promotions.submit"}}}}
	if err := registry.Register(revision); err != nil {
		t.Fatal(err)
	}
	other := agentconnect.ConnectionRevision{ID: "payroll", TenantID: "tenant-a", Revision: 1, Endpoint: "hris.example", Connection: fixtureConnection(t, "payroll", "tenant-a"), CredentialMode: agentconnect.UserDelegated,
		Skills: []agentconnect.SkillExposure{{ID: "pay.read", Version: "1", Tool: tool("pay.read", agentsecurity.ToolRead), Tier: agentconnect.TierT0, CredentialOperation: custody.LeaseOperation}},
		Grants: []agentconnect.GrantScope{{ID: "finance", Roles: []string{"finance"}, Population: "employees", OrganizationScopes: []string{"org-a"}, Skills: []string{"pay.read"}}}}
	if err := registry.Register(other); err != nil {
		t.Fatal(err)
	}
	return registry
}

type fixtureProviders struct{}

func (fixtureProviders) Provider(tenant, id string) (Provider, bool) {
	if tenant != "tenant-a" || id != "hris" {
		return Provider{}, false
	}
	return Provider{ConnectionID: "hris", Name: "HRIS", AuthorizeURL: "https://hris.example/oauth/authorize", ClientID: "hcm-next", RedirectURL: "https://cell.test/workspace/app/chat/agents/access/callback", Scopes: []string{"worker.read"}}, true
}

type fakeDelegations struct {
	mu      sync.Mutex
	grants  map[string][]DelegationGrant
	revoked []string
}

func (f *fakeDelegations) ActiveGrants(tenant, userID string, at time.Time) ([]DelegationGrant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []DelegationGrant
	for _, grant := range f.grants[tenant+"/"+userID] {
		if grant.ExpiresAt.After(at) {
			out = append(out, grant)
		}
	}
	return out, nil
}

func (f *fakeDelegations) RevokeTaskGrant(tenant, userID, taskID, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	list := f.grants[tenant+"/"+userID]
	for i, grant := range list {
		if grant.TaskID == taskID {
			f.grants[tenant+"/"+userID] = append(list[:i:i], list[i+1:]...)
			f.revoked = append(f.revoked, taskID)
			return nil
		}
	}
	return ErrInvalid
}

type linkFixture struct {
	registry    *agentconnect.Registry
	linker      *Linker
	delegations *fakeDelegations
	access      *UserAccess
	exchanged   []string
}

func newLinkFixture(t *testing.T) *linkFixture {
	t.Helper()
	f := &linkFixture{registry: fixtureRegistry(t), delegations: &fakeDelegations{grants: map[string][]DelegationGrant{
		"tenant-a/ana":   {{ID: "g1", TaskID: "task-1", TaskLabel: "Prepare the promotion request", Scope: "Read worker profile", ExpiresAt: time.Unix(200, 0).Add(time.Hour)}},
		"tenant-a/other": {{ID: "g2", TaskID: "task-2", TaskLabel: "Someone else's task", Scope: "Read worker profile", ExpiresAt: time.Unix(200, 0).Add(time.Hour)}},
	}}}
	exchange := func(_ context.Context, provider Provider, code, verifier string) (agentconnect.CredentialBinding, string, error) {
		f.exchanged = append(f.exchanged, provider.ConnectionID+"|"+code+"|"+verifier)
		return fixtureBinding("tenant-a", "ana-grant"), "ana@hris.example", nil
	}
	linker, err := NewLinker(fixtureProviders{}, f.registry, exchange, func() time.Time { return time.Unix(200, 0) }, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.linker = linker
	f.access, err = NewUserAccess(f.registry, linker, f.delegations, func() time.Time { return time.Unix(200, 0) })
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func linkParams(t *testing.T, start productui.AgentAuthorizationStart) url.Values {
	t.Helper()
	target, err := url.Parse(start.AuthorizationURL)
	if err != nil {
		t.Fatal(err)
	}
	return target.Query()
}

// The page's data: only the connections that grant this person something,
// each with link state, skills and tiers, and the person's own task grants.
func TestTodo_AGENT2_018(t *testing.T) {
	f := newLinkFixture(t)
	snapshot, err := f.access.For(fixtureUser("tenant-a", "ana")).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Connections) != 1 || snapshot.Connections[0].ID != "hris" {
		t.Fatalf("connections = %+v, want only the one that grants this person something", snapshot.Connections)
	}
	hris := snapshot.Connections[0]
	if hris.LinkState != "not_linked" || !hris.CanLink || hris.CanUnlink {
		t.Fatalf("an unlinked connection offers link = %v unlink = %v state %q", hris.CanLink, hris.CanUnlink, hris.LinkState)
	}
	tiers := map[string]string{}
	for _, skill := range hris.Skills {
		tiers[skill.ID] = skill.Tier
	}
	if tiers["workers.read"] != "T0_READ" || tiers["promotions.submit"] != "T3_SUBMIT_GOVERNED" || len(tiers) != 2 {
		t.Fatalf("skills and tiers = %v", tiers)
	}
	if len(snapshot.Delegations) != 1 || snapshot.Delegations[0].TaskID != "task-1" || snapshot.Delegations[0].ExpiresAt == "" || !snapshot.Delegations[0].Revocable {
		t.Fatalf("task grants = %+v, want only this person's, with an expiry", snapshot.Delegations)
	}

	session := f.access.For(fixtureUser("tenant-a", "ana"))
	start, err := session.StartProviderAuthorization("hris")
	if err != nil || !start.ValidPKCE() {
		t.Fatalf("start = %+v, %v", start, err)
	}
	params := linkParams(t, start)
	if params.Get("code_challenge_method") != "S256" || params.Get("state") != start.State || params.Get("response_type") != "code" {
		t.Fatalf("authorization URL = %v", params)
	}
	connection, err := f.linker.Complete(context.Background(), fixtureUser("tenant-a", "ana"), start.State, "the-code")
	if err != nil || connection != "hris" {
		t.Fatalf("complete = %q, %v", connection, err)
	}
	if len(f.exchanged) != 1 || CodeChallenge(strings.Split(f.exchanged[0], "|")[2]) != params.Get("code_challenge") {
		t.Fatalf("the verifier sent to the provider does not match the challenge in the URL: %v", f.exchanged)
	}
	snapshot, err = session.Snapshot()
	if err != nil || snapshot.Connections[0].LinkState != "linked" || !snapshot.Connections[0].CanUnlink || snapshot.Connections[0].AccountLabel != "ana@hris.example" {
		t.Fatalf("after linking = %+v, %v", snapshot.Connections, err)
	}
}

// Unlinking and revoking take effect before the next step: the next lease the
// agent asks for is refused.
func TestTodo_AGENT2_018_Integration(t *testing.T) {
	f := newLinkFixture(t)
	user := fixtureUser("tenant-a", "ana")
	session := f.access.For(user)
	start, err := session.StartProviderAuthorization("hris")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.linker.Complete(context.Background(), user, start.State, "code"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.registry.IssueLease(user, "hris", "workers.read", "agent-a", "run-1", "answer", time.Minute); err != nil {
		t.Fatalf("a linked account could not be used: %v", err)
	}
	if err := session.UnlinkConnection("hris"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.registry.IssueLease(user, "hris", "workers.read", "agent-a", "run-1", "answer", time.Minute); !errors.Is(err, agentconnect.ErrAccountNotLinked) {
		t.Fatalf("the step after unlinking = %v, want ErrAccountNotLinked", err)
	}
	if err := session.RevokeDelegation("task-1"); err != nil {
		t.Fatal(err)
	}
	snapshot, err := session.Snapshot()
	if err != nil || len(snapshot.Delegations) != 0 || len(f.delegations.revoked) != 1 {
		t.Fatalf("after revoking: grants %+v revoked %v err %v", snapshot.Delegations, f.delegations.revoked, err)
	}
}

// Linking starts only from the page and only for the person who started it.
func TestTodo_AGENT2_018_Security(t *testing.T) {
	f := newLinkFixture(t)
	ana, mallory := fixtureUser("tenant-a", "ana"), fixtureUser("tenant-a", "mallory")
	session := f.access.For(ana)
	start, err := session.StartProviderAuthorization("hris")
	if err != nil {
		t.Fatal(err)
	}
	// A callback link posted in chat carries a state this server never issued.
	if _, err := f.linker.Complete(context.Background(), ana, "forged-state", "code"); !errors.Is(err, ErrStateRejected) {
		t.Fatalf("forged state = %v", err)
	}
	// Someone who opens Ana's link cannot finish it for themselves, and the
	// attempt does not spend Ana's state.
	if _, err := f.linker.Complete(context.Background(), mallory, start.State, "code"); !errors.Is(err, ErrStateRejected) {
		t.Fatalf("another person's state = %v", err)
	}
	if len(f.exchanged) != 0 {
		t.Fatalf("the provider was called for a refused callback: %v", f.exchanged)
	}
	if _, err := f.linker.Complete(context.Background(), ana, start.State, ""); !errors.Is(err, ErrStateRejected) {
		t.Fatalf("empty code = %v", err)
	}
	if _, err := f.linker.Complete(context.Background(), ana, start.State, "code"); err != nil {
		t.Fatalf("the refused attempts spent the real person's state: %v", err)
	}
	// A state is single use.
	if _, err := f.linker.Complete(context.Background(), ana, start.State, "code"); !errors.Is(err, ErrStateRejected) {
		t.Fatalf("replayed state = %v", err)
	}
	// A state expires.
	clock := time.Unix(200, 0)
	linker, err := NewLinker(fixtureProviders{}, f.registry, func(context.Context, Provider, string, string) (agentconnect.CredentialBinding, string, error) {
		return fixtureBinding("tenant-a", "x"), "x", nil
	}, func() time.Time { return clock }, nil)
	if err != nil {
		t.Fatal(err)
	}
	expiring, err := linker.Start(ana, "hris")
	if err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(11 * time.Minute)
	if _, err := linker.Complete(context.Background(), ana, expiring.State, "code"); !errors.Is(err, ErrStateRejected) {
		t.Fatalf("expired state = %v", err)
	}
	// A connection that is not published for this tenant, or has no provider,
	// cannot be started; another tenant's user sees nothing.
	if _, err := session.StartProviderAuthorization("payroll"); !errors.Is(err, ErrNotLinkable) {
		t.Fatalf("an unpublished provider started = %v", err)
	}
	foreign, err := f.access.For(fixtureUser("tenant-b", "ana")).Snapshot()
	if err != nil || len(foreign.Connections) != 0 {
		t.Fatalf("another tenant's snapshot = %+v, %v", foreign, err)
	}
	// Another person's grant cannot be revoked by task id.
	if err := f.access.For(ana).RevokeDelegation("task-2"); err == nil {
		t.Fatal("revoked someone else's grant")
	}
	// Nothing in the page data is a credential.
	snapshot, _ := f.access.For(ana).Snapshot()
	if text := fmt.Sprintf("%+v", snapshot); strings.Contains(text, "ana-grant") || strings.Contains(text, "opaque/path") {
		t.Fatalf("the page data carries credential material: %s", text)
	}
}

// The page renders the service's data in each language, with the tier in
// words and a Link button for the connection that can be linked.
func TestTodo_AGENT2_018_Browser(t *testing.T) {
	f := newLinkFixture(t)
	snapshot, err := f.access.For(fixtureUser("tenant-a", "ana")).Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		locale string
		want   []string
	}{
		{"en-US", []string{"My agent access", "Connected accounts", "Link account", "Not linked", "Read information", "Submit for approval", "Active task grants", "Prepare the promotion request", "Revoke"}},
		{"de-DE", []string{"Mein Agentenzugriff", "Konto verknüpfen", "Nicht verknüpft", "Aktive Aufgabenberechtigungen"}},
		{"ar", []string{"الوصول الخاص بوكلائي", "ربط الحساب", "غير مرتبط"}},
	} {
		markup, err := ui.RenderToString(productui.AgentAccessPage(productui.AgentAccessPageProps{I18nProps: productui.I18nProps{Locale: productui.ResolveProductLocale(tc.locale)}, State: productui.AgentAccessStateReady, Snapshot: snapshot, Client: f.access.For(fixtureUser("tenant-a", "ana"))}))
		if err != nil {
			t.Fatal(err)
		}
		markup = strings.NewReplacer("&#39;", "'", "&amp;", "&").Replace(markup)
		for _, want := range tc.want {
			if !strings.Contains(markup, want) {
				t.Errorf("%s page missing %q:\n%s", tc.locale, want, markup)
			}
		}
		for _, raw := range []string{">T0_READ<", ">T3_SUBMIT_GOVERNED<", "Account actions are unavailable"} {
			if strings.Contains(markup, raw) {
				t.Errorf("%s page shows %q", tc.locale, raw)
			}
		}
	}
}
