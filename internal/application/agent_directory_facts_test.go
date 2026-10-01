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

type roleDirectoryFunc func(context.Context, values.TenantId, string) ([]string, error)

func (f roleDirectoryFunc) CurrentRoles(c context.Context, t values.TenantId, s string) ([]string, error) {
	return f(c, t, s)
}

type populationDirectoryFunc func(context.Context, values.TenantId, string) (string, error)

func (f populationDirectoryFunc) CurrentPopulation(c context.Context, t values.TenantId, s string) (string, error) {
	return f(c, t, s)
}

type organizationDirectoryFunc func(context.Context, values.TenantId, string, []string) ([]string, error)

func (f organizationDirectoryFunc) CurrentOrganizationScopes(c context.Context, t values.TenantId, s string, r []string) ([]string, error) {
	return f(c, t, s, r)
}

type subjectDirectoryFunc func(context.Context, values.TenantId, string, string, []string, []string) ([]agentgate.Subject, error)

func (f subjectDirectoryFunc) CurrentSubjects(c context.Context, t values.TenantId, s, p string, r, o []string) ([]agentgate.Subject, error) {
	return f(c, t, s, p, r, o)
}

type fieldPolicyFunc func(context.Context, *trust.Principal, string, []string, []string, []agentgate.Subject) ([]authz.FieldID, error)

func (f fieldPolicyFunc) CurrentFields(c context.Context, p *trust.Principal, purpose string, r, o []string, s []agentgate.Subject) ([]authz.FieldID, error) {
	return f(c, p, purpose, r, o, s)
}

func directoryFactsPrincipal(t *testing.T) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "user-a", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "session", IssuedAt: time.Unix(1, 0), ExpiresAt: time.Unix(1000, 0), CredentialDigest: "cred:sha256:f3313ba3acd7c3d657bb5f325eea437efc73e915eeeb52bd7538aa8d67b6bfb4"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTodo_AGENT2_005_DirectoryFactsUseTrustedTenant(t *testing.T) {
	p := directoryFactsPrincipal(t)
	ctx := trust.WithPrincipal(context.Background(), p)
	var gotTenant values.TenantId
	roles, err := NewTenantAgentRoleSource(roleDirectoryFunc(func(_ context.Context, tenant values.TenantId, subject string) ([]string, error) {
		gotTenant = tenant
		if subject != "user-a" {
			t.Fatalf("subject = %q", subject)
		}
		return []string{" manager ", "manager"}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	got, err := roles.ResolveRoles(ctx, p)
	if err != nil || gotTenant != "tenant-a" || !reflect.DeepEqual(got, []string{"manager"}) {
		t.Fatalf("roles = %v tenant=%q err=%v", got, gotTenant, err)
	}
	pop, err := NewTenantAgentPopulationSource(populationDirectoryFunc(func(_ context.Context, tenant values.TenantId, _ string) (string, error) {
		return string(tenant) + "/employees", nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := pop.ResolvePopulation(ctx, p); err != nil || got != "tenant-a/employees" {
		t.Fatalf("population = %q err=%v", got, err)
	}
}

func TestTodo_AGENT2_005_DirectoryFactsRejectForgedAndUnprovedFacts(t *testing.T) {
	p := directoryFactsPrincipal(t)
	other, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-b", Subject: "user-b", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "session-b", IssuedAt: time.Unix(1, 0), ExpiresAt: time.Unix(1000, 0), CredentialDigest: "cred:sha256:1b8e5b84f2595ab41b3c88aaf913c195e9329d51d1a2903a15745fd219277d08"})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name      string
		ctx       context.Context
		principal *trust.Principal
	}{
		{"missing context", context.Background(), p},
		{"nil source principal", trust.WithPrincipal(context.Background(), p), nil},
		{"mismatched context", trust.WithPrincipal(context.Background(), other), p},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source, err := NewTenantAgentPopulationSource(populationDirectoryFunc(func(context.Context, values.TenantId, string) (string, error) { return "invented", nil }))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := source.ResolvePopulation(tc.ctx, tc.principal); !errors.Is(err, errAgentDirectoryFacts) {
				t.Fatalf("err=%v", err)
			}
		})
	}
	fields, err := NewTenantAgentFieldSource(fieldPolicyFunc(func(context.Context, *trust.Principal, string, []string, []string, []agentgate.Subject) ([]authz.FieldID, error) {
		return []authz.FieldID{"made_up"}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fields.ResolveFields(trust.WithPrincipal(context.Background(), p), p, "purpose", []string{}, []string{}, nil); !errors.Is(err, errAgentDirectoryFacts) {
		t.Fatalf("unknown field err=%v", err)
	}
}

func TestTodo_AGENT2_005_SubjectAndFieldResultsAreTenantBoundAndOwned(t *testing.T) {
	p := directoryFactsPrincipal(t)
	ctx := trust.WithPrincipal(context.Background(), p)
	source, err := NewTenantAgentSubjectSource(subjectDirectoryFunc(func(context.Context, values.TenantId, string, string, []string, []string) ([]agentgate.Subject, error) {
		return []agentgate.Subject{{Ref: values.EntityRef{Tenant: "tenant-a", Kind: "worker", Id: "00000000-0000-0000-0000-000000000001"}, Organization: authz.OrgUnitRef{Tenant: "tenant-a", ID: "org-a"}}}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	subjects, err := source.ResolveSubjects(ctx, p, "self_service", []string{}, []string{})
	if err != nil || len(subjects) != 1 {
		t.Fatalf("subjects=%v err=%v", subjects, err)
	}
	fields, err := NewTenantAgentFieldSource(fieldPolicyFunc(func(_ context.Context, _ *trust.Principal, _ string, _ []string, _ []string, _ []agentgate.Subject) ([]authz.FieldID, error) {
		return []authz.FieldID{authz.FieldWorkEmail, authz.FieldWorkerNumber}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	got, err := fields.ResolveFields(ctx, p, "self_service", []string{}, []string{}, subjects)
	if err != nil || !reflect.DeepEqual(got, []authz.FieldID{authz.FieldWorkEmail, authz.FieldWorkerNumber}) {
		t.Fatalf("fields=%v err=%v", got, err)
	}
}
