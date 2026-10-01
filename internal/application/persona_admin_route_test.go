package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type personaAdminRouteClientFake struct {
	snapshot productui.PersonaAdminSnapshotRequest
	ctx      context.Context
}

func (f *personaAdminRouteClientFake) Snapshot(ctx context.Context, req productui.PersonaAdminSnapshotRequest) (productui.PersonaAdminSnapshot, error) {
	f.ctx, f.snapshot = ctx, req
	return productui.PersonaAdminSnapshot{Available: true}, nil
}
func (*personaAdminRouteClientFake) Preview(context.Context, productui.PersonaAdminPreviewRequest) (productui.PersonaAdminPreview, error) {
	return productui.PersonaAdminPreview{}, nil
}
func (*personaAdminRouteClientFake) RequestReview(string) error   { return nil }
func (*personaAdminRouteClientFake) PublishPersona(string) error  { return nil }
func (*personaAdminRouteClientFake) RollbackPersona(string) error { return nil }
func (*personaAdminRouteClientFake) SuspendPersona(string) error  { return nil }
func (*personaAdminRouteClientFake) RetirePersona(string) error   { return nil }

func TestTodo_AGENTP_018_PersonaAdminRouteFailsClosedWithoutClientOrTrust(t *testing.T) {
	if _, err := NewPersonaAdminRoute(nil); !errors.Is(err, ErrPersonaAdminRouteUnavailable) {
		t.Fatalf("nil client error = %v", err)
	}
	client := &personaAdminRouteClientFake{}
	route, err := NewPersonaAdminRoute(client)
	if err != nil {
		t.Fatal(err)
	}
	if got := route.ClientForRequest(context.Background()); got != nil {
		t.Fatal("detached context returned a client")
	}
}

func TestTodo_AGENTP_018_PersonaAdminRouteBindsVerifiedPrincipal(t *testing.T) {
	client := &personaAdminRouteClientFake{}
	route, err := NewPersonaAdminRoute(client)
	if err != nil {
		t.Fatal(err)
	}
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant-a"), Subject: "user-a", SubjectKind: trust.SubjectKindHuman, OrganizationScopeID: "org-a", Roles: []string{"hcm_admin"}, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "session-a", IssuedAt: time.Unix(100, 0), ExpiresAt: time.Unix(200, 0), CredentialDigest: "sha256:fixture"})
	if err != nil {
		t.Fatal(err)
	}
	bound := route.ClientForRequest(trust.WithPrincipal(context.Background(), principal))
	if bound == nil {
		t.Fatal("trusted context did not return a client")
	}
	if _, err := bound.Snapshot(context.Background(), productui.PersonaAdminSnapshotRequest{TenantID: "attacker-tenant", Principal: "attacker"}); err != nil {
		t.Fatal(err)
	}
	if client.snapshot.TenantID != "tenant-a" || client.snapshot.Principal != "user-a" {
		t.Fatalf("snapshot request = %+v, want trusted principal", client.snapshot)
	}
	if client.ctx == nil {
		t.Fatal("underlying client did not receive request context")
	}
	for name, action := range map[string]func(string) error{
		"request review": bound.RequestReview,
		"publish":        bound.PublishPersona,
		"rollback":       bound.RollbackPersona,
		"suspend":        bound.SuspendPersona,
		"retire":         bound.RetirePersona,
	} {
		t.Run(name, func(t *testing.T) {
			if err := action("persona"); !errors.Is(err, ErrPersonaCatalogLifecycleUnavailable) {
				t.Fatalf("lifecycle action error = %v, want unavailable", err)
			}
		})
	}
}
