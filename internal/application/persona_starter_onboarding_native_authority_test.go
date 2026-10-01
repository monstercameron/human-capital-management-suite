package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/authz"
)

type nativeOnboardingDirectory struct {
	roles         []string
	organizations []string
	subjects      []agentgate.Subject
	fields        []authz.FieldID
	err           error
	calls         int
}

func (d *nativeOnboardingDirectory) CurrentRoles(context.Context, values.TenantId, string) ([]string, error) {
	d.calls++
	return d.roles, d.err
}
func (d *nativeOnboardingDirectory) CurrentOrganizationScopes(context.Context, values.TenantId, string, []string) ([]string, error) {
	return d.organizations, d.err
}
func (d *nativeOnboardingDirectory) CurrentSubjects(context.Context, values.TenantId, string, string, []string, []string) ([]agentgate.Subject, error) {
	return d.subjects, d.err
}
func (d *nativeOnboardingDirectory) CurrentFields(context.Context, *trust.Principal, string, []string, []string, []agentgate.Subject) ([]authz.FieldID, error) {
	return d.fields, d.err
}

func TestTodo_AGENTP_021_Onboarding_NativeCurrentTargetAuthority(t *testing.T) {
	port, source, _, ctx, principal := nativeOnboardingFixture(t)
	worker := source.snapshot.Request.Plan.Worker
	authority := port.authority.(*CurrentPersonaOnboardingAuthority)
	directory := authority.directory.(*nativeOnboardingDirectory)
	if _, err := port.Checklist(ctx, worker); err != nil {
		t.Fatal(err)
	}
	calls := source.calls
	directory.roles = nil
	if _, err := port.Checklist(ctx, worker); !errors.Is(err, ErrPersonaOnboardingFacts) || source.calls != calls {
		t.Fatalf("revoked current role read owner facts: %v calls=%d", err, source.calls)
	}
	directory.roles = []string{string(authz.RoleManager)}
	directory.subjects = nil
	if _, err := port.Tasks(ctx, worker); !errors.Is(err, ErrPersonaOnboardingFacts) || source.calls != calls {
		t.Fatalf("same-tenant unauthorized target read: %v calls=%d", err, source.calls)
	}
	directory.subjects = []agentgate.Subject{{Ref: worker, Organization: authz.OrgUnitRef{Tenant: worker.Tenant, ID: "people"}}}
	directory.fields = nil
	if _, err := port.DraftWelcome(ctx, worker); !errors.Is(err, ErrPersonaOnboardingFacts) || source.calls != calls {
		t.Fatalf("withheld core fields read: %v calls=%d", err, source.calls)
	}
	directory.fields = []authz.FieldID{authz.FieldWorkerNumber}
	directory.subjects = append(directory.subjects, directory.subjects[0])
	if err := authority.AuthorizePersonaOnboarding(ctx, principal, worker); !errors.Is(err, ErrPersonaOnboardingFacts) {
		t.Fatalf("ambiguous target grant: %v", err)
	}
	directory.subjects = directory.subjects[:1]
	authority.now = func() time.Time { return principal.ExpiresAt() }
	if err := authority.AuthorizePersonaOnboarding(ctx, principal, worker); !errors.Is(err, ErrPersonaOnboardingFacts) {
		t.Fatalf("expired caller: %v", err)
	}
	authority.now = func() time.Time { return principal.IssuedAt().Add(-time.Second) }
	if err := authority.AuthorizePersonaOnboarding(ctx, principal, worker); !errors.Is(err, ErrPersonaOnboardingFacts) {
		t.Fatalf("future caller: %v", err)
	}
	if _, err := NewCurrentPersonaOnboardingAuthority(nil, time.Now); !errors.Is(err, ErrPersonaOnboardingFacts) {
		t.Fatalf("missing directory: %v", err)
	}
	if _, err := NewNativePersonaOnboardingPort(source, &nativeOnboardingChannels{}, time.Now, nil); !errors.Is(err, ErrPersonaOnboardingFacts) {
		t.Fatalf("missing current target authority: %v", err)
	}
}
