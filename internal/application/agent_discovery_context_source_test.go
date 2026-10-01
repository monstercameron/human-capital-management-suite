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

func productionDiscoveryPrincipal(t *testing.T, session string) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: "tenant-a", Subject: "user-a", SubjectKind: trust.SubjectKindHuman,
		Purposes: []string{"agent.self_service"}, SessionRef: session,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance:            trust.AssuranceSubstantial, CredentialDigest: "credential-a", IssuedAt: time.Unix(100, 0), ExpiresAt: time.Unix(200, 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTodo_AGENT2_005_ProductionContextReReadsTrustedFacts(t *testing.T) {
	p := productionDiscoveryPrincipal(t, "session-a")
	ctx := trust.WithPrincipal(context.Background(), p)
	var roleReads, fieldReads int
	source, err := NewAgentDiscoveryContextSource(
		roleDirectoryFunc(func(context.Context, values.TenantId, string) ([]string, error) {
			roleReads++
			return []string{"employee"}, nil
		}),
		populationDirectoryFunc(func(context.Context, values.TenantId, string) (string, error) { return "employees", nil }),
		organizationDirectoryFunc(func(_ context.Context, _ values.TenantId, _ string, roles []string) ([]string, error) {
			return append([]string(nil), roles...), nil
		}),
		subjectDirectoryFunc(func(_ context.Context, tenant values.TenantId, _ string, _ string, _, _ []string) ([]agentgate.Subject, error) {
			return []agentgate.Subject{{Ref: values.EntityRef{Tenant: tenant, Kind: "worker", Id: "00000000-0000-4000-8000-000000000001"}, Organization: authz.OrgUnitRef{Tenant: tenant, ID: "org-a"}}}, nil
		}),
		fieldPolicyFunc(func(_ context.Context, _ *trust.Principal, _ string, _, _ []string, _ []agentgate.Subject) ([]authz.FieldID, error) {
			fieldReads++
			return []authz.FieldID{authz.FieldWorkerNumber}, nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		user, subjects, fields, err := source.Resolve(ctx, p, "agent.self_service")
		if err != nil || user.Principal != p || len(subjects) != 1 || !reflect.DeepEqual(fields, []authz.FieldID{authz.FieldWorkerNumber}) {
			t.Fatalf("Resolve() = user=%+v subjects=%v fields=%v err=%v", user, subjects, fields, err)
		}
	}
	if roleReads != 2 || fieldReads != 2 {
		t.Fatalf("reads = roles %d fields %d, want two fresh reads", roleReads, fieldReads)
	}
}

func TestTodo_AGENT2_005_ProductionContextFailsClosedForIdentityAndSources(t *testing.T) {
	p := productionDiscoveryPrincipal(t, "session-a")
	otherSession := productionDiscoveryPrincipal(t, "session-b")
	valid := func() *AgentDiscoveryContextSource {
		source, err := NewAgentDiscoveryContextSource(
			roleDirectoryFunc(func(context.Context, values.TenantId, string) ([]string, error) { return []string{"employee"}, nil }),
			populationDirectoryFunc(func(context.Context, values.TenantId, string) (string, error) { return "employees", nil }),
			organizationDirectoryFunc(func(context.Context, values.TenantId, string, []string) ([]string, error) {
				return []string{"org-a"}, nil
			}),
			subjectDirectoryFunc(func(_ context.Context, tenant values.TenantId, _ string, _ string, _, _ []string) ([]agentgate.Subject, error) {
				return []agentgate.Subject{{Ref: values.EntityRef{Tenant: tenant, Kind: "worker", Id: "00000000-0000-4000-8000-000000000001"}, Organization: authz.OrgUnitRef{Tenant: tenant, ID: "org-a"}}}, nil
			}),
			fieldPolicyFunc(func(context.Context, *trust.Principal, string, []string, []string, []agentgate.Subject) ([]authz.FieldID, error) {
				return []authz.FieldID{authz.FieldWorkerNumber}, nil
			}),
		)
		if err != nil {
			t.Fatal(err)
		}
		return source
	}
	for _, tc := range []struct {
		name string
		ctx  context.Context
		p    *trust.Principal
	}{
		{"detached context", context.Background(), p},
		{"session mismatch", trust.WithPrincipal(context.Background(), otherSession), p},
		{"nil principal", trust.WithPrincipal(context.Background(), p), nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, _, err := valid().Resolve(tc.ctx, tc.p, "agent.self_service"); !errors.Is(err, errAgentDiscoveryContextSource) {
				t.Fatalf("Resolve() error = %v", err)
			}
		})
	}
	if _, err := NewAgentDiscoveryContextSource(nil, nil, nil, nil, nil); !errors.Is(err, errAgentDiscoveryContextSource) {
		t.Fatalf("nil sources error = %v", err)
	}
}

func TestTodo_AGENT2_005_ProductionContextPreservesDirectoryErrors(t *testing.T) {
	p := productionDiscoveryPrincipal(t, "session-a")
	marker := errors.New("directory temporarily unavailable")
	source, err := NewAgentDiscoveryContextSource(
		roleDirectoryFunc(func(context.Context, values.TenantId, string) ([]string, error) { return nil, marker }),
		populationDirectoryFunc(func(context.Context, values.TenantId, string) (string, error) { return "employees", nil }),
		organizationDirectoryFunc(func(context.Context, values.TenantId, string, []string) ([]string, error) {
			return []string{"org-a"}, nil
		}),
		subjectDirectoryFunc(func(_ context.Context, tenant values.TenantId, _ string, _ string, _, _ []string) ([]agentgate.Subject, error) {
			return []agentgate.Subject{{Ref: values.EntityRef{Tenant: tenant, Kind: "worker", Id: "00000000-0000-4000-8000-000000000001"}, Organization: authz.OrgUnitRef{Tenant: tenant, ID: "org-a"}}}, nil
		}),
		fieldPolicyFunc(func(context.Context, *trust.Principal, string, []string, []string, []agentgate.Subject) ([]authz.FieldID, error) {
			return []authz.FieldID{authz.FieldWorkerNumber}, nil
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	_, _, _, err = source.Resolve(trust.WithPrincipal(context.Background(), p), p, "agent.self_service")
	if !errors.Is(err, marker) || !errors.Is(err, errAgentDiscoveryContextSource) {
		t.Fatalf("Resolve() error = %v; want both source and directory errors", err)
	}
}
