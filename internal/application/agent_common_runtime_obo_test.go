package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentdelegationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func TestTodo_AGENT_034_CommonOBO_Integration(t *testing.T) {
	ctx := context.Background()
	core, isolated := pgtest.New(t), pgtest.NewEmpty(t)
	if err := agentstore.Migrate(ctx, isolated.SQL); err != nil {
		t.Fatal(err)
	}
	tenant := uuid.New()
	core.Exec(t, `INSERT INTO tenant(tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES($1,'common-tenant','cell-local','Common','ACTIVE',CURRENT_TIMESTAMP)`, tenant)
	core.Exec(t, `INSERT INTO tenant_agent_setting(tenant_id,enabled,revision,updated_by) VALUES($1,true,1,'admin')`, tenant)
	isolated.Exec(t, `INSERT INTO tenant(tenant_id) VALUES($1)`, tenant)
	agents := commonAgentOpenIntegrationStore(t, isolated)
	now := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	request := commonAgentTestRequest(now)
	request.Purpose = "workflow-start"
	request.Source.Kind = agentrun.SourceAPI
	request.Source.Ref = "actual-source-owner-fixture"
	request.Principal = agentrun.PrincipalChain{Mode: agentrun.ModeOnBehalfOf, AgentPrincipalID: uuid.NewString(), InvokerID: "worker-common", DelegatedCredentialRef: "grant-common"}
	manifest := commonAgentIntegrationManifest(t, agents, tenant, request)
	request.Agent.Digest, _ = manifest.Digest()
	request.Source, _ = (agentrun.CanonicalSourceConverter{}).ConvertSource(ctx, request)
	mapper := func(ref values.TenantId) uuid.UUID {
		if ref == "common-tenant" {
			return tenant
		}
		return uuid.Nil
	}
	current := trust.AuthorityScope{Tenant: "common-tenant", OrganizationScopeID: "org-common", Capabilities: []string{"workflows.request_start"},
		Resources: []string{"workflow:one"}, Fields: []string{"status"}, Purposes: []string{request.Purpose}, Assurance: trust.AssuranceLow,
		NotBefore: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour), SkillAuthorities: trust.SkillAuthorities{
			"start-workflow": {Capabilities: []string{"workflows.request_start"}, Resources: []string{"workflow:one"}, Fields: []string{"status"}, Purposes: []string{request.Purpose}},
		}}
	scope := CommonAgentBindingScope{SchemaVersion: 1, TenantID: request.Source.TenantID, Agent: request.Agent, InstallationID: request.InstallationID,
		AgentPrincipal: request.Principal.AgentPrincipalID, InvokerID: request.Principal.InvokerID, DelegationRef: request.Principal.DelegatedCredentialRef,
		LegalEntity: request.LegalEntity, Purpose: request.Purpose, Audience: request.Audience, Context: request.Context, Sources: []agentrun.SourceKind{agentrun.SourceAPI},
		BudgetCeiling: request.Budget, Deadline: request.Deadline}
	source, user, userBinding := uuid.New(), uuid.New(), uuid.New()
	core.Exec(t, `INSERT INTO authority_source(tenant_id,authority_source_id,kind,display_name,uri,valid_interval,content_digest)
		VALUES($1,$2,'POLICY_BUNDLE','Current OBO','policy:common-obo',tstzrange($3,$4,'[)'),$5)`, tenant, source, now.Add(-time.Hour), now.Add(time.Hour), strings.TrimPrefix(request.Agent.Digest, "sha256:"))
	core.Exec(t, `INSERT INTO principal(tenant_id,principal_id,kind,subject,assurance,authn_method) VALUES($1,$2,'SERVICE','common-agent','AAL1','workload'),($1,$3,'USER',$4,'AAL1','session')`, tenant, request.Principal.AgentPrincipalID, user, request.Principal.InvokerID)
	for i, principal := range []any{request.Principal.AgentPrincipalID, user} {
		binding := uuid.New()
		if i == 1 {
			binding, scope.UserAuthority = userBinding, &current
		}
		raw, _ := json.Marshal(scope)
		core.Exec(t, `INSERT INTO authority_binding(tenant_id,binding_id,principal_id,authority_source_id,scope,valid_from,valid_to) VALUES($1,$2,$3,$4,$5::jsonb,$6,$7)`, tenant, binding, principal, source, string(raw), now.Add(-time.Hour), now.Add(time.Hour))
	}
	connection := core.NewConn(t)
	if _, err := connection.Exec(ctx, "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	grants, err := agentdelegationstore.New(connection, mapper)
	if err != nil {
		t.Fatal(err)
	}
	store, err := grants.ForTenant(ctx, "common-tenant")
	if err != nil {
		t.Fatal(err)
	}
	delegation, err := agentdelegation.NewService(agentdelegation.Config{Store: store, Now: func() time.Time { return now }, Authority: agentdelegation.ResolverFunc(func(string, values.TenantId, string, time.Time) (agentdelegation.UserAuthority, error) {
		return agentdelegation.UserAuthority{UserID: request.Principal.InvokerID, Active: true, Authority: current, SkillAuthorities: current.SkillAuthorities}, nil
	})})
	if err != nil {
		t.Fatal(err)
	}
	_, err = delegation.CreateScopedGrant(agentdelegation.GrantRequest{GrantID: request.Principal.DelegatedCredentialRef, UserID: request.Principal.InvokerID, Tenant: "common-tenant",
		AgentVersion: request.Agent.Version, TargetAgentID: request.Agent.AgentID, InstallationID: request.InstallationID, TaskID: request.Source.Key, PlanSkillSetDigest: "bounded-plan",
		Purpose: request.Purpose, OrganizationScopeID: current.OrganizationScopeID, Skills: []string{"start-workflow"}, SkillScopes: map[string][]string{"start-workflow": {"workflows.request_start"}},
		NotBefore: now, ExpiresAt: now.Add(time.Hour), UserAuthority: current, SkillAuthorities: current.SkillAuthorities})
	if err != nil {
		t.Fatal(err)
	}
	authority, err := NewCommonAgentAuthority(CommonAgentAuthorityConfig{CoreDB: connection, Agents: agents, TenantUUID: mapper, Models: commonAgentTestModelPolicy{}, Now: func() time.Time { return now },
		Sources: map[agentrun.SourceKind]CommonAgentSourceAuthority{agentrun.SourceAPI: commonAgentExactSource{request}}})
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewCommonAgentRuntime(CommonAgentRuntimeConfig{Stores: DatabaseCommonAgentStores{Agents: agents, TenantUUID: mapper}, Authority: authority, Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	record, _, err := runtime.Admit(ctx, request)
	if err != nil || record.Decision != agentrun.DecisionAccepted || record.Authority.Principal != request.Principal || record.Authority.GrantRef != request.Principal.DelegatedCredentialRef {
		t.Fatalf("actual OBO admission = %+v %v", record, err)
	}
	run, err := runtime.GetRun(ctx, "common-tenant", record.ID)
	if err != nil || run.State != runstate.StateReady || run.ActorID != request.Principal.InvokerID || run.PrincipalMode != agentrun.ModeOnBehalfOf {
		t.Fatalf("persisted actual OBO identity = %+v %v", run, err)
	}
	if err := runtime.Recheck(ctx, "common-tenant", record.ID); err != nil {
		t.Fatal(err)
	}
	// Revoke the exact current user skill ceiling without revoking the grant.
	current.SkillAuthorities["start-workflow"] = trust.SkillAuthority{Capabilities: []string{"workflows.read_status"}, Resources: []string{"workflow:one"}, Purposes: []string{request.Purpose}}
	raw, _ := json.Marshal(scope)
	core.Exec(t, `UPDATE authority_binding SET scope=$3::jsonb WHERE tenant_id=$1 AND binding_id=$2`, tenant, userBinding, string(raw))
	if err := runtime.Recheck(ctx, "common-tenant", record.ID); !errors.Is(err, agentrun.ErrAuthorityRefusal) {
		t.Fatalf("removed current skill retained authority = %v", err)
	}
	current.SkillAuthorities["start-workflow"] = trust.SkillAuthority{Capabilities: []string{"workflows.request_start"}, Resources: []string{"workflow:one"}, Fields: []string{"status"}, Purposes: []string{request.Purpose}}
	raw, _ = json.Marshal(scope)
	core.Exec(t, `UPDATE authority_binding SET scope=$3::jsonb WHERE tenant_id=$1 AND binding_id=$2`, tenant, userBinding, string(raw))
	core.Exec(t, `UPDATE principal SET lifecycle='REVOKED',revocation_epoch=revocation_epoch+1 WHERE tenant_id=$1 AND principal_id=$2`, tenant, user)
	if err := runtime.Recheck(ctx, "common-tenant", record.ID); !errors.Is(err, agentrun.ErrAuthorityRefusal) {
		t.Fatalf("revoked current user retained authority = %v", err)
	}
	core.Exec(t, `UPDATE principal SET lifecycle='ACTIVE' WHERE tenant_id=$1 AND principal_id=$2`, tenant, user)
	if err := store.Revoke(request.Principal.DelegatedCredentialRef, "user revoked"); err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Claim(ctx, "common-tenant", record.ID, "worker", time.Minute); !errors.Is(err, agentrun.ErrAuthorityRefusal) {
		t.Fatalf("revoked durable grant executed = %v", err)
	}
}

func TestTodo_AGENT_034_CommonOBO_Security(t *testing.T) {
	now := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	request := commonAgentTestRequest(now)
	request.Purpose = "workflow-start"
	request.Principal = agentrun.PrincipalChain{Mode: agentrun.ModeOnBehalfOf, AgentPrincipalID: "agent-principal", InvokerID: "worker-common", DelegatedCredentialRef: "grant-common"}
	scopes := trust.SkillAuthorities{"start": {Capabilities: []string{"workflows.request_start"}, Resources: []string{"workflow:one"}, Fields: []string{"status"}, Purposes: []string{request.Purpose}}}
	grant := agentdelegation.Grant{GrantID: "grant-common", UserID: request.Principal.InvokerID, Tenant: "common-tenant", TargetAgentID: request.Agent.AgentID,
		AgentVersion: request.Agent.Version, InstallationID: request.InstallationID, TaskID: "actual-task", Purpose: request.Purpose, OrganizationScopeID: "org",
		PlanSkillSetDigest: "actual-plan", Skills: []string{"start"}, SkillScopes: map[string][]string{"start": {"workflows.request_start"}}, SkillAuthorities: scopes,
		NotBefore: now.Add(-time.Minute), ExpiresAt: request.Deadline.Add(time.Minute), RevocationEpoch: 1,
		Authority: trust.DelegationGrant{GrantID: "grant-common", Delegator: request.Principal.InvokerID, Delegate: request.Agent.AgentID, Tenant: "common-tenant",
			OrganizationScopeID: "org", NotBefore: now.Add(-time.Minute), ExpiresAt: request.Deadline.Add(time.Minute), RevocationEpoch: 1, SkillAuthorities: scopes}}
	if !commonAgentOBOGrantMatches(grant, request, now, 1) {
		t.Fatal("valid exact delegation envelope refused")
	}
	canonical := grant
	canonical.AgentVersion = request.Agent.AgentID + "@" + request.Agent.Version
	if !commonAgentOBOGrantMatches(canonical, request, now, 1) {
		t.Fatal("exact canonical delegation version refused")
	}
	for name, mutate := range map[string]func(*agentdelegation.Grant){
		"different user":            func(g *agentdelegation.Grant) { g.UserID = "other-user" },
		"different tenant":          func(g *agentdelegation.Grant) { g.Tenant = "other-tenant" },
		"different target":          func(g *agentdelegation.Grant) { g.TargetAgentID = "other-agent" },
		"different version":         func(g *agentdelegation.Grant) { g.AgentVersion = "other@" + request.Agent.Version },
		"different installation":    func(g *agentdelegation.Grant) { g.InstallationID = "other-installation" },
		"different credential":      func(g *agentdelegation.Grant) { g.GrantID = "other-grant" },
		"revoked":                   func(g *agentdelegation.Grant) { g.Revoked = true },
		"expired":                   func(g *agentdelegation.Grant) { g.ExpiresAt = now },
		"authority actor mismatch":  func(g *agentdelegation.Grant) { g.Authority.Delegator = "other-user" },
		"authority epoch mismatch":  func(g *agentdelegation.Grant) { g.Authority.RevocationEpoch = 2 },
		"authority window mismatch": func(g *agentdelegation.Grant) { g.Authority.ExpiresAt = g.ExpiresAt.Add(time.Hour) },
		"legacy flat authority":     func(g *agentdelegation.Grant) { g.SkillAuthorities = nil },
	} {
		t.Run(name, func(t *testing.T) {
			changed := grant
			mutate(&changed)
			if commonAgentOBOGrantMatches(changed, request, now, 1) {
				t.Fatal("changed delegation envelope retained authority")
			}
		})
	}
	if commonAgentOBOGrantMatches(grant, request, now, 2) || commonAgentOBOGrantMatches(grant, request, now, 0) {
		t.Fatal("changed or unresolved revocation epoch retained authority")
	}
	current := trust.EffectiveAuthority{SkillAuthorities: trust.CloneSkillAuthorities(scopes)}
	if !commonAgentSkillsCurrent(grant, current) {
		t.Fatal("exact current skill scope denied")
	}
	current.SkillAuthorities["start"] = trust.SkillAuthority{Capabilities: []string{"workflows.request_start"}, Resources: []string{"workflow:one"}, Purposes: []string{request.Purpose}}
	if commonAgentSkillsCurrent(grant, current) {
		t.Fatal("removed disclosure field retained authority")
	}
	principal := request.Principal
	principal.SponsorID = "sponsor"
	if commonAgentPrincipalMode(principal) {
		t.Fatal("mixed sponsored and human principal chain accepted")
	}
}
