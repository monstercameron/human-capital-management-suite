package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type agentuxDemoProjectGrantFixture struct {
	tenant           uuid.UUID
	subject, project string
	active           bool
	calls            int
}

func (f *agentuxDemoProjectGrantFixture) SupportProjectCreateAllowed(_ context.Context, tenant uuid.UUID, subject, project string) (bool, error) {
	f.calls++
	return tenant == f.tenant && subject == f.subject && project == f.project && f.active, nil
}

type agentuxDemoHumanProjectAuthority struct{ calls int }

func (f *agentuxDemoHumanProjectAuthority) Authorize(context.Context, *trust.Principal, string, projectaccess.Capability) error {
	f.calls++
	return nil
}
func (f *agentuxDemoHumanProjectAuthority) AuthorizeCreate(context.Context, *trust.Principal) error {
	f.calls++
	return nil
}
func (f *agentuxDemoHumanProjectAuthority) AuthorizeListProjects(context.Context, *trust.Principal) error {
	f.calls++
	return nil
}

func TestAgentUXDemo_ProjectAuthority_Security(t *testing.T) {
	_, fixture, _, _ := agentuxDemoSupportSetup(t)
	principal := fixture.grant.Principal
	grant := &agentuxDemoProjectGrantFixture{tenant: fixture.grant.TenantID, subject: principal.Subject(), project: "customer-support", active: true}
	human := &agentuxDemoHumanProjectAuthority{}
	authorizer := AgentUXDemoSupportProjectAuthorizer{Human: human, Grants: grant, TenantUUID: func(tenant values.TenantId) uuid.UUID {
		if tenant.String() == "ironridge-demo" {
			return grant.tenant
		}
		return uuid.NewSHA1(uuid.NameSpaceOID, []byte(tenant))
	}}
	ctx := context.Background()
	if err := authorizer.Authorize(ctx, principal, grant.project, projectaccess.CreateTask); err != nil {
		t.Fatal(err)
	}
	for _, action := range []projectaccess.Capability{projectaccess.ReadProject, projectaccess.ReadTask, projectaccess.EditTask, projectaccess.MoveTask, projectaccess.Comment, projectaccess.ManageMembers, projectaccess.ManageProject, projectaccess.ConfigureWorkflow, projectaccess.Archive, projectaccess.Export} {
		if err := authorizer.Authorize(ctx, principal, grant.project, action); !errors.Is(err, projectaccess.ErrUnauthorized) {
			t.Fatalf("overbroad service permission %s: %v", action, err)
		}
	}
	if err := authorizer.Authorize(ctx, principal, "another-project", projectaccess.CreateTask); !errors.Is(err, projectaccess.ErrUnauthorized) {
		t.Fatal("grant escaped project")
	}
	grant.active = false
	if err := authorizer.Authorize(ctx, principal, grant.project, projectaccess.CreateTask); !errors.Is(err, projectaccess.ErrUnauthorized) {
		t.Fatal("revoked grant remained live")
	}
	if err := authorizer.AuthorizeCreate(ctx, principal); !errors.Is(err, projectaccess.ErrUnauthorized) {
		t.Fatal("service could create project")
	}
	if err := authorizer.AuthorizeListProjects(ctx, principal); !errors.Is(err, projectaccess.ErrUnauthorized) {
		t.Fatal("service could list projects")
	}
	if human.calls != 0 {
		t.Fatal("service borrowed human authorizer")
	}
	p, err := localAgentDemoPrincipal(time.Date(2026, 10, 1, 13, 0, 0, 0, time.UTC), "ironridge-demo", localAgentDemoAdmin, "org:ironridge-demo:people-ops")
	if err != nil {
		t.Fatal(err)
	}
	if err := authorizer.Authorize(ctx, p, grant.project, projectaccess.ReadProject); err != nil {
		t.Fatal(err)
	}
	if err := authorizer.AuthorizeCreate(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := authorizer.AuthorizeListProjects(ctx, p); err != nil {
		t.Fatal(err)
	}
	if human.calls != 3 {
		t.Fatal("human authority not preserved")
	}
	if err := authorizer.Authorize(ctx, nil, grant.project, projectaccess.CreateTask); !errors.Is(err, projectaccess.ErrUnauthorized) {
		t.Fatal("nil actor admitted")
	}
}
