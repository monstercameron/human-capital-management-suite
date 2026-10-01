package agentdelegation_test

import (
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

var delegationNow = time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)

func authority(active bool, capabilities ...string) agentdelegation.UserAuthority {
	return agentdelegation.UserAuthority{UserID: "user-42", Active: active, Authority: trust.AuthorityScope{
		Tenant: "acme-corp", OrganizationScopeID: "org-west", Capabilities: capabilities,
		Resources: []string{"worker:42", "worker:43"}, Fields: []string{"display_name", "status"},
		Purposes: []string{"agent.read"}, Assurance: trust.AssuranceSubstantial,
		NotBefore: delegationNow.Add(-time.Hour), ExpiresAt: delegationNow.Add(48 * time.Hour),
	}}
}

func fixture(t *testing.T, resolver agentdelegation.AuthorityResolver) (*agentdelegation.Service, *agentdelegation.MemoryGrantStore, agentdelegation.Grant) {
	t.Helper()
	store := agentdelegation.NewMemoryGrantStore()
	if resolver == nil {
		resolver = agentdelegation.ResolverFunc(func(string, values.TenantId, string, time.Time) (agentdelegation.UserAuthority, error) {
			return authority(true, "people.read", "people.write"), nil
		})
	}
	service, err := agentdelegation.NewService(agentdelegation.Config{Store: store, Authority: resolver, Secret: []byte("0123456789abcdef0123456789abcdef"), Now: func() time.Time { return delegationNow }})
	if err != nil {
		t.Fatal(err)
	}
	grant, err := service.CreateGrant(agentdelegation.GrantRequest{
		GrantID: "grant-42", UserID: "user-42", Tenant: "acme-corp", AgentVersion: "agent-v3",
		InstallationID: "install-7", TaskID: "task-9", PlanSkillSetDigest: "sha256:plan-42",
		Purpose: "agent.read", OrganizationScopeID: "org-west", Skills: []string{"people.lookup", "people.update"},
		SkillScopes: map[string][]string{"people.lookup": {"people.read"}, "people.update": {"people.write"}},
		ExpiresAt:   delegationNow.Add(24 * time.Hour), UserAuthority: authority(true, "people.read", "people.write").Authority,
	})
	if err != nil {
		t.Fatal(err)
	}
	return service, store, grant
}

func exchangeRequest(g agentdelegation.Grant) agentdelegation.ExchangeRequest {
	return agentdelegation.ExchangeRequest{SubjectToken: g.GrantID, SubjectTokenType: agentdelegation.DelegationGrantTokenType,
		RunID: "run-1", StepID: "step-1", Skill: "people.lookup", Scope: []string{"people.read"},
		Audience: "capability-gateway", Lifetime: 4 * time.Minute, Sender: "workload/worker-7"}
}

// TestTodo_AGENT2_003 proves the primary user-started exchange contract:
// the subject remains the signed-in user, while the run and step are actors.
func TestTodo_AGENT2_003(t *testing.T) {
	service, _, grant := fixture(t, nil)
	credential, err := service.Exchange(exchangeRequest(grant))
	if err != nil {
		t.Fatal(err)
	}
	if credential.Raw == "" || strings.Contains(credential.Raw, "browser-session") {
		t.Fatalf("credential raw = %q, want a signed delegated credential without a browser token", credential.Raw)
	}
	claims, err := service.Verify(credential.Raw, agentdelegation.VerifyRequest{Audience: "capability-gateway", Sender: "workload/worker-7", Skill: "people.lookup", Scope: []string{"people.read"}})
	if err != nil {
		t.Fatal(err)
	}
	if claims.Subject != "user-42" || claims.Subject == grant.AgentVersion {
		t.Fatalf("subject = %q, want signed-in user user-42 and not agent identity", claims.Subject)
	}
	if claims.Actor.AgentVersion != "agent-v3" || claims.Actor.InstallationID != "install-7" || claims.Actor.RunID != "run-1" || claims.Actor.StepID != "step-1" {
		t.Fatalf("actor = %+v", claims.Actor)
	}
	if claims.Audience != "capability-gateway" || claims.SenderConstraint != "workload/worker-7" || claims.Scope[0] != "people.read" {
		t.Fatalf("binding claims = %+v", claims)
	}
	if time.Unix(claims.ExpiresAtUnix, 0).Sub(time.Unix(claims.IssuedAtUnix, 0)) > agentdelegation.MaxTokenLifetime {
		t.Fatal("token lifetime exceeds five minutes")
	}
}

// TestTodo_AGENT2_003_Golden pins the signed identity shape and nested actor
// representation while leaving the MAC itself an implementation detail.
func TestTodo_AGENT2_003_Golden(t *testing.T) {
	service, _, grant := fixture(t, nil)
	credential, err := service.Exchange(exchangeRequest(grant))
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"token_type":"urn:ietf:params:oauth:token-type:access_token","iss":"hcmnext.agentdelegation","jti":"adt-","sub":"user-42","tenant":"acme-corp","grant_id":"grant-42","purpose":"agent.read","skill":"people.lookup","scope":["people.read"],"aud":"capability-gateway","cnf_workload":"workload/worker-7","revocation_epoch":1,"iat":1790596800,"exp":1790597040,"act":{"agent_version":"agent-v3","installation_id":"install-7","run_id":"run-1","step_id":"step-1"}}`
	// JTI is deliberately variable in the golden projection; all other claims
	// are exact and are the contract the capability gateway consumes.
	got := credential.Claims
	got.JTI = "adt-"
	encoded, err := jsonMarshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != want {
		t.Fatalf("claims golden = %s\nwant             = %s", encoded, want)
	}
}

// TestTodo_AGENT2_003_Security proves revocation, current-user activity,
// audience/sender binding and nested child-scope narrowing.
func TestTodo_AGENT2_003_Security(t *testing.T) {
	service, store, grant := fixture(t, nil)
	missingType := exchangeRequest(grant)
	missingType.SubjectTokenType = ""
	if _, err := service.Exchange(missingType); !errors.Is(err, agentdelegation.ErrInvalidRequest) {
		t.Fatalf("missing subject token type error = %v", err)
	}
	credential, err := service.Exchange(exchangeRequest(grant))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Verify(credential.Raw, agentdelegation.VerifyRequest{Audience: "other-gateway", Sender: "workload/worker-7"}); !errors.Is(err, agentdelegation.ErrTokenAudienceMismatch) {
		t.Fatalf("wrong audience error = %v", err)
	}
	if _, err := service.Verify(credential.Raw, agentdelegation.VerifyRequest{Audience: "capability-gateway", Sender: "workload/other"}); !errors.Is(err, agentdelegation.ErrTokenSenderMismatch) {
		t.Fatalf("wrong sender error = %v", err)
	}
	childReq := exchangeRequest(grant)
	childReq.StepID, childReq.AgentVersion, childReq.InstallationID = "child-step", "specialist-v1", "child-install"
	childReq.Parent = &credential
	child, err := service.Exchange(childReq)
	if err != nil {
		t.Fatal(err)
	}
	childClaims, err := service.Verify(child.Raw, agentdelegation.VerifyRequest{Audience: "capability-gateway", Sender: "workload/worker-7"})
	if err != nil || childClaims.Actor.Act == nil || childClaims.Actor.Act.AgentVersion != "agent-v3" {
		t.Fatalf("child actor = %+v, err = %v", childClaims.Actor, err)
	}
	if err := store.Revoke(grant.GrantID, "user sign-out"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Verify(credential.Raw, agentdelegation.VerifyRequest{Audience: "capability-gateway", Sender: "workload/worker-7"}); !errors.Is(err, agentdelegation.ErrTokenRevoked) {
		t.Fatalf("revoked token error = %v", err)
	}
	if _, err := service.Exchange(exchangeRequest(grant)); !errors.Is(err, agentdelegation.ErrGrantRevoked) {
		t.Fatalf("revoked exchange error = %v", err)
	}

	inactive, _, inactiveGrant := fixture(t, agentdelegation.ResolverFunc(func(string, values.TenantId, string, time.Time) (agentdelegation.UserAuthority, error) {
		return authority(false, "people.read", "people.write"), nil
	}))
	if _, err := inactive.Exchange(exchangeRequest(inactiveGrant)); !errors.Is(err, agentdelegation.ErrUserInactive) {
		t.Fatalf("inactive user exchange error = %v", err)
	}
}

// TestTodo_AGENT2_003_Property proves every exchanged scope is inside both
// the durable skill ceiling and the current user's authority.
func TestTodo_AGENT2_003_Property(t *testing.T) {
	service, _, grant := fixture(t, nil)
	for _, scope := range []string{"people.read"} {
		req := exchangeRequest(grant)
		req.Scope = []string{scope}
		credential, err := service.Exchange(req)
		if err != nil {
			t.Fatal(err)
		}
		claims, err := service.Verify(credential.Raw, agentdelegation.VerifyRequest{Audience: req.Audience, Sender: req.Sender})
		if err != nil || len(claims.Scope) != 1 || claims.Scope[0] != scope {
			t.Fatalf("scope = %v, err = %v", claims.Scope, err)
		}
	}
	tooWide := exchangeRequest(grant)
	tooWide.Scope = []string{"people.admin"}
	if _, err := service.Exchange(tooWide); !errors.Is(err, agentdelegation.ErrScopeExpanded) {
		t.Fatalf("widened scope error = %v", err)
	}
	narrowed, err := service.CreateGrant(agentdelegation.GrantRequest{
		GrantID: "grant-narrowed", UserID: "user-42", Tenant: "acme-corp", AgentVersion: "agent-v3",
		InstallationID: "install-7", TaskID: "task-narrowed", PlanSkillSetDigest: "sha256:narrowed",
		Purpose: "agent.read", OrganizationScopeID: "org-west", Skills: []string{"people.lookup"},
		SkillScopes: map[string][]string{"people.lookup": {"people.read", "people.admin"}},
		ExpiresAt:   delegationNow.Add(24 * time.Hour), UserAuthority: authority(true, "people.read").Authority,
	})
	if err != nil || len(narrowed.SkillScopes["people.lookup"]) != 1 || narrowed.SkillScopes["people.lookup"][0] != "people.read" {
		t.Fatalf("durable grant scope = %v, err = %v; want current-user intersection", narrowed.SkillScopes, err)
	}
}

// TestTodo_AGENT2_003_Race holds exchange at the live authority lookup and
// bumps the user's epoch. The exchange must return no usable credential.
func TestTodo_AGENT2_003_Race(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	resolver := agentdelegation.ResolverFunc(func(string, values.TenantId, string, time.Time) (agentdelegation.UserAuthority, error) {
		once.Do(func() { close(entered) })
		<-release
		return authority(true, "people.read", "people.write"), nil
	})
	service, store, grant := fixture(t, resolver)
	result := make(chan error, 1)
	go func() { _, err := service.Exchange(exchangeRequest(grant)); result <- err }()
	<-entered
	if _, err := store.BumpRevocationEpoch("acme-corp", "user-42", "session revoke"); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-result; !errors.Is(err, agentdelegation.ErrGrantRevoked) {
		t.Fatalf("exchange during revocation error = %v", err)
	}
}

func TestCreateScopedGrant_DisjointSkillResourcesStayPaired(t *testing.T) {
	current := authority(true, "worker.read", "worker.write")
	current.SkillAuthorities = agentdelegation.SkillAuthorities{
		"read":  {Capabilities: []string{"worker.read"}, Resources: []string{"worker:1"}, Fields: []string{"name"}, Purposes: []string{"agent.read"}},
		"write": {Capabilities: []string{"worker.write"}, Resources: []string{"worker:2"}, Fields: []string{"status"}, Purposes: []string{"agent.read"}},
	}
	current.Authority.Resources = []string{"worker:1", "worker:2"}
	current.Authority.SkillAuthorities = current.SkillAuthorities
	service, _, _ := fixture(t, agentdelegation.ResolverFunc(func(string, values.TenantId, string, time.Time) (agentdelegation.UserAuthority, error) {
		return current, nil
	}))
	grant, err := service.CreateScopedGrant(agentdelegation.GrantRequest{GrantID: "scoped", UserID: "user-42", Tenant: "acme-corp", AgentVersion: "agent-v3", InstallationID: "install-7", TaskID: "task-scoped", PlanSkillSetDigest: "sha256:scoped", Purpose: "agent.read", OrganizationScopeID: "org-west", Skills: []string{"read", "write"}, SkillScopes: map[string][]string{"read": {"worker.read"}, "write": {"worker.write"}}, SkillAuthorities: current.SkillAuthorities, ExpiresAt: delegationNow.Add(24 * time.Hour), UserAuthority: current.Authority})
	if err != nil {
		t.Fatal(err)
	}
	if got := grant.SkillAuthorities["read"].Resources; len(got) != 1 || got[0] != "worker:1" {
		t.Fatalf("read resources = %v", got)
	}
	if got := grant.SkillAuthorities["write"].Resources; len(got) != 1 || got[0] != "worker:2" {
		t.Fatalf("write resources = %v", got)
	}
	if _, err := service.CreateScopedGrant(agentdelegation.GrantRequest{GrantID: "forged", UserID: "user-42", Tenant: "acme-corp", AgentVersion: "agent-v3", InstallationID: "install-7", TaskID: "task-forged", PlanSkillSetDigest: "sha256:forged", Purpose: "agent.read", OrganizationScopeID: "org-west", Skills: []string{"read"}, SkillScopes: map[string][]string{"read": {"worker.read"}}, SkillAuthorities: agentdelegation.SkillAuthorities{"read": {Capabilities: []string{"worker.read"}, Resources: []string{"worker:2"}, Purposes: []string{"agent.read"}}}, ExpiresAt: delegationNow.Add(24 * time.Hour), UserAuthority: current.Authority}); !errors.Is(err, agentdelegation.ErrScopeExpanded) {
		t.Fatalf("forged authority error = %v", err)
	}
}

func TestCreateScopedGrant_MissingMapFailsClosed(t *testing.T) {
	service, _, _ := fixture(t, nil)
	_, err := service.CreateScopedGrant(agentdelegation.GrantRequest{Skills: []string{"read"}})
	if !errors.Is(err, agentdelegation.ErrScopeExpanded) {
		t.Fatalf("missing map error = %v", err)
	}
}

func TestTodo_AGENTP_008_ExactTargetIdentityIsSeparateFromVersion(t *testing.T) {
	service, store, _ := fixture(t, nil)
	request := scopedGrantRequestForIdentityTest()
	request.TargetAgentID = "agent:comp-analyst"
	grant, err := service.CreateScopedGrant(request)
	if err != nil {
		t.Fatal(err)
	}
	if grant.AgentVersion != "persona-v3" || grant.TargetAgentID != "agent:comp-analyst" || grant.Authority.Delegate != "agent:comp-analyst" {
		t.Fatalf("version and delegate identity were conflated: version=%q target=%q delegate=%q", grant.AgentVersion, grant.TargetAgentID, grant.Authority.Delegate)
	}
	grant.GrantID = "identity-target-forged"
	grant.TargetAgentID = "agent:other"
	if err := store.Save(grant); !errors.Is(err, agentdelegation.ErrInvalidGrant) {
		t.Fatalf("mismatched exact target Save = %v, want ErrInvalidGrant", err)
	}
}

func scopedGrantRequestForIdentityTest() agentdelegation.GrantRequest {
	current := trust.AuthorityScope{
		Tenant: "acme-corp", OrganizationScopeID: "org-west", Capabilities: []string{"people.read"},
		Resources: []string{"worker:42"}, Purposes: []string{"agent.read"}, Assurance: trust.AssuranceSubstantial,
		NotBefore: delegationNow.Add(-time.Hour), ExpiresAt: delegationNow.Add(48 * time.Hour),
		SkillAuthorities: trust.SkillAuthorities{"read": {Capabilities: []string{"people.read"}, Resources: []string{"worker:42"}, Purposes: []string{"agent.read"}}},
	}
	return agentdelegation.GrantRequest{GrantID: "identity-target", UserID: "user-42", Tenant: "acme-corp", AgentVersion: "persona-v3", InstallationID: "install-7", TaskID: "task-9", PlanSkillSetDigest: "sha256:identity-target", Purpose: "agent.read", OrganizationScopeID: "org-west", Skills: []string{"read"}, SkillScopes: map[string][]string{"read": {"people.read"}}, SkillAuthorities: trust.CloneSkillAuthorities(current.SkillAuthorities), ExpiresAt: delegationNow.Add(24 * time.Hour), UserAuthority: current}
}

func TestScopedExchange_CurrentSkillResourceShrinkFailsClosed(t *testing.T) {
	current := authority(true, "worker.read", "worker.write")
	current.SkillAuthorities = agentdelegation.SkillAuthorities{
		"read":  {Capabilities: []string{"worker.read"}, Resources: []string{"worker:1"}, Purposes: []string{"agent.read"}},
		"write": {Capabilities: []string{"worker.write"}, Resources: []string{"worker:2"}, Purposes: []string{"agent.read"}},
	}
	current.Authority.SkillAuthorities = current.SkillAuthorities
	current.Authority.Resources = []string{"worker:1", "worker:2"}
	shrunk := false
	service, _, _ := fixture(t, agentdelegation.ResolverFunc(func(string, values.TenantId, string, time.Time) (agentdelegation.UserAuthority, error) {
		if shrunk {
			current.SkillAuthorities = agentdelegation.SkillAuthorities{"read": {Capabilities: []string{"worker.read"}, Resources: []string{"worker:9"}, Purposes: []string{"agent.read"}}, "write": current.SkillAuthorities["write"]}
		}
		return current, nil
	}))
	grant, err := service.CreateScopedGrant(agentdelegation.GrantRequest{GrantID: "shrink", UserID: "user-42", Tenant: "acme-corp", AgentVersion: "agent-v3", InstallationID: "install-7", TaskID: "task-shrink", PlanSkillSetDigest: "sha256:shrink", Purpose: "agent.read", OrganizationScopeID: "org-west", Skills: []string{"read", "write"}, SkillScopes: map[string][]string{"read": {"worker.read"}, "write": {"worker.write"}}, SkillAuthorities: current.SkillAuthorities, ExpiresAt: delegationNow.Add(24 * time.Hour), UserAuthority: current.Authority})
	if err != nil {
		t.Fatal(err)
	}
	shrunk = true
	read := exchangeRequest(grant)
	read.Skill = "read"
	read.Scope = []string{"worker.read"}
	if _, err := service.Exchange(read); !errors.Is(err, agentdelegation.ErrScopeExpanded) {
		t.Fatalf("shrunk read exchange error = %v", err)
	}
	write := read
	write.Skill = "write"
	write.Scope = []string{"worker.write"}
	if _, err := service.Exchange(write); err != nil {
		t.Fatalf("unaffected write exchange error = %v", err)
	}
}

func jsonMarshal(v any) ([]byte, error) {
	// Keep the test's dependency surface tiny while using encoding/json's
	// stable struct field order for the golden projection.
	return json.Marshal(v)
}
