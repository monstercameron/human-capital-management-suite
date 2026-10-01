package application

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

type discoveryRoles func(context.Context, *trust.Principal) ([]string, error)

func (f discoveryRoles) ResolveRoles(ctx context.Context, p *trust.Principal) ([]string, error) {
	return f(ctx, p)
}

type discoveryPopulation func(context.Context, *trust.Principal) (string, error)

func (f discoveryPopulation) ResolvePopulation(ctx context.Context, p *trust.Principal) (string, error) {
	return f(ctx, p)
}

type discoveryOrganizations func(context.Context, *trust.Principal, []string) ([]string, error)

func (f discoveryOrganizations) ResolveOrganizationScopes(ctx context.Context, p *trust.Principal, roles []string) ([]string, error) {
	return f(ctx, p, roles)
}

type discoverySubjects func(context.Context, *trust.Principal, string, []string, []string) ([]agentgate.Subject, error)

func (f discoverySubjects) ResolveSubjects(ctx context.Context, p *trust.Principal, purpose string, roles, orgs []string) ([]agentgate.Subject, error) {
	return f(ctx, p, purpose, roles, orgs)
}

type discoveryFields func(context.Context, *trust.Principal, string, []string, []string, []agentgate.Subject) ([]authz.FieldID, error)

func (f discoveryFields) ResolveFields(ctx context.Context, p *trust.Principal, purpose string, roles, orgs []string, subjects []agentgate.Subject) ([]authz.FieldID, error) {
	return f(ctx, p, purpose, roles, orgs, subjects)
}

func discoveryPrincipal(t *testing.T) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "user-a", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "session-a", IssuedAt: time.Unix(100, 0), ExpiresAt: time.Unix(200, 0), CredentialDigest: "cred:sha256:abc9634869d43eda6d04f5b95613a32aa439651327c67ba871cf47fa3d0a3530"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func discoveryContext() *ServerAgentDiscoveryContext {
	return &ServerAgentDiscoveryContext{
		Roles:      discoveryRoles(func(context.Context, *trust.Principal) ([]string, error) { return []string{"employee"}, nil }),
		Population: discoveryPopulation(func(context.Context, *trust.Principal) (string, error) { return "employees", nil }),
		Organizations: discoveryOrganizations(func(_ context.Context, _ *trust.Principal, roles []string) ([]string, error) {
			return append([]string(nil), roles...), nil
		}),
		Subjects: discoverySubjects(func(_ context.Context, _ *trust.Principal, _ string, _, _ []string) ([]agentgate.Subject, error) {
			return []agentgate.Subject{{Ref: values.EntityRef{Tenant: "tenant-a", Kind: "worker", Id: "00000000-0000-0000-0000-000000000001"}, Organization: authz.OrgUnitRef{Tenant: "tenant-a", ID: "org-a"}}}, nil
		}),
		Fields: discoveryFields(func(_ context.Context, _ *trust.Principal, _ string, _, _ []string, _ []agentgate.Subject) ([]authz.FieldID, error) {
			return []authz.FieldID{authz.FieldWorkerNumber}, nil
		}),
	}
}

// TestTodo_AGENT2_005_Resolve uses only source-owned facts and returns all
// four current authority dimensions needed by the gate.
func TestTodo_AGENT2_005_Resolve(t *testing.T) {
	p := discoveryPrincipal(t)
	got, subjects, fields, err := discoveryContext().Resolve(trust.WithPrincipal(context.Background(), p), p, "agent.self_service")
	if err != nil {
		t.Fatal(err)
	}
	if got.Principal != p || got.Population != "employees" || !reflect.DeepEqual(got.Roles, []string{"employee"}) || !reflect.DeepEqual(got.OrganizationScopes, []string{"employee"}) {
		t.Fatalf("user = %+v", got)
	}
	if len(subjects) != 1 || len(fields) != 1 || fields[0] != authz.FieldWorkerNumber {
		t.Fatalf("scope = subjects %v fields %v", subjects, fields)
	}
}

// TestTodo_AGENT2_005_ResolveRejectsForgedOrMissingPopulation proves context
// identity and population are fail-closed before discovery can proceed.
func TestTodo_AGENT2_005_ResolveRejectsForgedOrMissingPopulation(t *testing.T) {
	p := discoveryPrincipal(t)
	cases := []struct {
		name       string
		ctx        context.Context
		population string
	}{
		{"missing verified context", context.Background(), "employees"},
		{"missing population", trust.WithPrincipal(context.Background(), p), ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := discoveryContext()
			c.Population = discoveryPopulation(func(context.Context, *trust.Principal) (string, error) { return tc.population, nil })
			if _, _, _, err := c.Resolve(tc.ctx, p, "agent.self_service"); err == nil || !errors.Is(err, errAgentDiscoveryContext) && tc.population == "" {
				t.Fatalf("Resolve error = %v", err)
			}
		})
	}
	other, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: "tenant-b", Subject: "user-b", SubjectKind: trust.SubjectKindHuman,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial,
		SessionRef: "session-b", IssuedAt: time.Unix(100, 0), ExpiresAt: time.Unix(200, 0),
		CredentialDigest: "cred:sha256:3b2f8a7c2e1d4f6a9b8c7d6e5f4a3b2c1d0e9f8a7b6c5d4e3f2a1b0c9d8e7f6a5",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := discoveryContext().Resolve(trust.WithPrincipal(context.Background(), other), p, "agent.self_service"); err == nil {
		t.Fatal("mismatched context principal accepted")
	}
}
