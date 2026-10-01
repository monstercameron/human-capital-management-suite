package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/application/projectservice"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/project"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type agentProjectGrantStore struct{ grant agentdelegation.Grant }

func (s agentProjectGrantStore) Get(string) (agentdelegation.Grant, error)             { return s.grant, nil }
func (s agentProjectGrantStore) Revoke(string, string) error                           { return nil }
func (s agentProjectGrantStore) Save(agentdelegation.Grant) error                      { return nil }
func (s agentProjectGrantStore) CurrentRevocationEpoch(values.TenantId, string) uint64 { return 1 }
func (s agentProjectGrantStore) BumpRevocationEpoch(values.TenantId, string, string) (uint64, error) {
	return 2, nil
}

type agentProjectAuthorizer struct{}

func (agentProjectAuthorizer) Authorize(context.Context, *trust.Principal, string, projectaccess.Capability) error {
	return nil
}
func (agentProjectAuthorizer) AuthorizeCreate(context.Context, *trust.Principal) error { return nil }
func (agentProjectAuthorizer) AuthorizeListProjects(context.Context, *trust.Principal) error {
	return nil
}

func agentProjectPrincipal(t *testing.T) *trust.Principal {
	t.Helper()
	now := time.Unix(100, 0).UTC()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant-a"), Subject: "user-a", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session-a", IssuedAt: now, ExpiresAt: now.Add(time.Hour), CredentialDigest: "digest-a"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func agentProjectClaims() agentdelegation.Claims {
	return agentdelegation.Claims{TokenType: agentdelegation.DelegatedAccessTokenType, Tenant: "tenant-a", Subject: "user-a", GrantID: "grant-a", Purpose: "project-agent", Skill: AgentProjectSkillID, Scope: []string{"project:p1", "task:t1"}, Actor: agentdelegation.ActorClaim{AgentVersion: "agent-v1", InstallationID: "install-a", RunID: "run-a", StepID: "step-a"}}
}

func agentProjectSkillFixture(t *testing.T) (*AgentProjectSkill, context.Context, AgentProjectTaskRequest) {
	t.Helper()
	p := agentProjectPrincipal(t)
	claims := agentProjectClaims()
	grant := agentdelegation.Grant{GrantID: "grant-a", UserID: "user-a", Tenant: values.TenantId("tenant-a"), AgentVersion: "agent-v1", InstallationID: "install-a", TaskID: "agent-task-a", Purpose: "project-agent", RevocationEpoch: 1, SkillScopes: map[string][]string{AgentProjectSkillID: {"project:p1", "task:t1"}}, SkillAuthorities: map[string]trust.SkillAuthority{AgentProjectSkillID: {Resources: []string{"project:p1", "task:t1"}}}, Authority: trust.DelegationGrant{Resources: []string{"project:p1", "task:t1"}}}
	skill := &AgentProjectSkill{Projects: projectservice.Service{Auth: agentProjectAuthorizer{}}, Grants: agentProjectGrantStore{grant: grant}}
	ctx := trust.WithPrincipal(context.Background(), p)
	return skill, ctx, AgentProjectTaskRequest{Principal: p, Claims: claims, ProjectID: "p1", TaskID: "t1"}
}

func TestTodo_AGENT_049(t *testing.T) {
	s := NewAgentProjectMemoryProposalStore()
	p := project.AgentTaskProposal{ID: "proposal-1", TenantID: "tenant-a", ProjectID: "p1", BaseTaskRevision: 4}
	if err := s.Save(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(context.Background(), p.TenantID, string(p.ProjectID), p.ID)
	if err != nil || got.BaseTaskRevision != 4 {
		t.Fatalf("proposal read = %+v, err=%v", got, err)
	}
	if err := s.Save(context.Background(), p); !errors.Is(err, ErrAgentProjectProposal) {
		t.Fatalf("duplicate proposal err=%v", err)
	}
}

func TestTodo_AGENT_049_Security(t *testing.T) {
	s, ctx, req := agentProjectSkillFixture(t)
	if err := s.RejectBusinessIntent(ctx, req, "COMPLETE_HCM_WORK_ITEM"); !errors.Is(err, project.ErrAgentHCMAction) {
		t.Fatalf("HCM action err=%v", err)
	}
	bad := req
	bad.TaskID = "other"
	if err := resourceAllowed(agentdelegation.Grant{Authority: trust.DelegationGrant{Resources: []string{"project:p1", "task:t1"}}, SkillAuthorities: map[string]trust.SkillAuthority{AgentProjectSkillID: {Resources: []string{"project:p1", "task:t1"}}}}, bad.Claims, bad.ProjectID, bad.TaskID); !errors.Is(err, ErrAgentProjectGrant) {
		t.Fatalf("resource denial err=%v", err)
	}
}

func TestTodo_AGENT_049_Conformance(t *testing.T) {
	s, ctx, req := agentProjectSkillFixture(t)
	bad := req
	bad.Claims.Actor.AgentVersion = ""
	if err := s.RejectBusinessIntent(ctx, bad, ""); !errors.Is(err, ErrAgentProjectNotAgent) {
		t.Fatalf("missing agent actor err=%v", err)
	}
	if err := s.RejectBusinessIntent(context.Background(), req, ""); !errors.Is(err, ErrAgentProjectPrincipal) {
		t.Fatalf("missing trusted context err=%v", err)
	}
}
