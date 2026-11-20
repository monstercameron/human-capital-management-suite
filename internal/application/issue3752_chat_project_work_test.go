package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/application/projectservice"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/domains/projectaccess"
	kernelvalues "github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// The project directory fails closed: no verified caller, a caller who is not
// the chat principal, or a project service that is not composed all refuse.
func TestIssue3752_ChatProjectWorkDirectoryFailsClosed_Security(t *testing.T) {
	d := chatProjectWorkDirectory{service: projectservice.Service{}}
	p := chat.Principal{TenantID: "t1", SubjectID: "u1"}
	if _, err := d.ProjectWorkVisible(context.Background(), p, "p1", "k1"); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("no verified caller: err = %v, want permission denied", err)
	}
	other := trust.WithPrincipal(context.Background(), mustChat3752Principal(t, "t1", "someone-else"))
	if _, err := d.ProjectWorkVisible(other, p, "p1", "k1"); !errors.Is(err, chat.ErrPermissionDenied) {
		t.Fatalf("mismatched caller: err = %v, want permission denied", err)
	}
	self := trust.WithPrincipal(context.Background(), mustChat3752Principal(t, "t1", "u1"))
	if _, err := d.ProjectWorkVisible(self, p, "p1", "k1"); !errors.Is(err, chat.ErrUnavailable) {
		t.Fatalf("project service not composed: err = %v, want unavailable", err)
	}
}

func mustChat3752Principal(t *testing.T, tenant, subject string) *trust.Principal {
	t.Helper()
	now := time.Now()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: kernelvalues.TenantId(tenant), Subject: subject, SubjectKind: trust.SubjectKindHuman, Roles: []string{"hcm_admin"}, OrganizationScopeID: "org", AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "issue3752", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "sha256:issue3752"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

type chat3752Auth struct{ allowed string }

func (a chat3752Auth) Authorize(_ context.Context, _ *trust.Principal, project string, _ projectaccess.Capability) error {
	if project != a.allowed {
		return errors.New("denied")
	}
	return nil
}
func (chat3752Auth) AuthorizeCreate(context.Context, *trust.Principal) error       { return nil }
func (chat3752Auth) AuthorizeListProjects(context.Context, *trust.Principal) error { return nil }

type chat3752Reads struct {
	projectservice.ReadRepository
	tenant string
}

func (r chat3752Reads) GetProject(_ context.Context, tenant, id string) (projectservice.ProjectRecord, error) {
	return projectservice.ProjectRecord{ID: id, TenantID: r.tenant, Name: "Launch"}, nil
}
func (r chat3752Reads) GetTask(_ context.Context, tenant, project, id string) (projectservice.TaskRecord, error) {
	return projectservice.TaskRecord{ID: id, ProjectID: project, TenantID: r.tenant, Title: "Ship it"}, nil
}

type chat3752Workflows struct{ projectservice.WorkflowPort }

func (chat3752Workflows) GetPublished(context.Context, string, string) (projectservice.WorkflowConfiguration, error) {
	return projectservice.WorkflowConfiguration{}, nil
}

// The adapter answers with the project service's own read rule: a board the
// caller may read is named, one the caller may not read and a row from another
// tenant look exactly alike (permission denied), so a reference never confirms
// that something exists.
func TestIssue3752_ChatProjectWorkDirectoryUsesTheProjectReadRule_Security(t *testing.T) {
	p := chat.Principal{TenantID: "t1", SubjectID: "u1"}
	ctx := trust.WithPrincipal(context.Background(), mustChat3752Principal(t, "t1", "u1"))
	d := chatProjectWorkDirectory{service: projectservice.Service{Auth: chat3752Auth{allowed: "p1"}, Reads: chat3752Reads{tenant: "t1"}, Workflows: chat3752Workflows{}}}
	if title, err := d.ProjectWorkVisible(ctx, p, "p1", "k1"); err != nil || title != "Ship it" {
		t.Fatalf("task = %q, %v", title, err)
	}
	if title, err := d.ProjectWorkVisible(ctx, p, "p1", ""); err != nil || title != "Launch" {
		t.Fatalf("board = %q, %v", title, err)
	}
	if title, err := d.ProjectWorkVisible(ctx, p, "p2", "k1"); !errors.Is(err, chat.ErrPermissionDenied) || title != "" {
		t.Fatalf("project the caller may not read = %q, %v", title, err)
	}
	crossTenant := chatProjectWorkDirectory{service: projectservice.Service{Auth: chat3752Auth{allowed: "p1"}, Reads: chat3752Reads{tenant: "t2"}, Workflows: chat3752Workflows{}}}
	if title, err := crossTenant.ProjectWorkVisible(ctx, p, "p1", "k1"); !errors.Is(err, chat.ErrPermissionDenied) || title != "" {
		t.Fatalf("another tenant's row = %q, %v", title, err)
	}
}
