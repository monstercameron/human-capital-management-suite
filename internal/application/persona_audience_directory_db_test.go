package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

func personaDirectoryContext(t *testing.T, kind trust.SubjectKind) context.Context {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: values.TenantId("chat-tenant"), Subject: "invoker", SubjectKind: kind,
		AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial,
		SessionRef: "session", IssuedAt: time.Unix(1, 0), ExpiresAt: time.Unix(2, 0), CredentialDigest: "digest",
	})
	if err != nil {
		t.Fatal(err)
	}
	return trust.WithPrincipal(context.Background(), p)
}

func TestTodo_AGENTP_026_ResolvesExactHomeTenantFacts(t *testing.T) {
	directory := directoryReader(
		&directoryRowsFake{rows: [][]any{{"manager"}, {"member"}}},
		&directoryRowsFake{rows: [][]any{{"staff"}, {"pilot"}}},
	)
	got, err := NewPersonaAudienceDirectoryDB(directory, WithPersonaHomeOrganizationDirectory(personaHomeOrganizationFake{organization: "org-a"})).ResolvePersonaAudienceMember(personaDirectoryContext(t, trust.SubjectKindHuman), "home-tenant", "member-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.SubjectID != "member-1" || len(got.Roles) != 2 || len(got.Populations) != 2 || got.OrganizationScope != "org-a" {
		t.Fatalf("facts = %+v", got)
	}
}

func TestTodo_AGENTP_026_RejectsMissingAmbiguousAndMalformedFacts(t *testing.T) {
	tests := []struct {
		name string
		rows [][][]any
	}{
		{name: "missing populations", rows: [][][]any{{{"member"}}, {}}},
		{name: "empty role", rows: [][][]any{{{""}}, {{"staff"}}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rows := make([]*directoryRowsFake, len(tc.rows))
			for i, values := range tc.rows {
				rows[i] = &directoryRowsFake{rows: values}
			}
			_, err := NewPersonaAudienceDirectoryDB(directoryReader(toRows(rows)...), WithPersonaHomeOrganizationDirectory(personaHomeOrganizationFake{organization: "org-a"})).ResolvePersonaAudienceMember(personaDirectoryContext(t, trust.SubjectKindHuman), "home-tenant", "member-1")
			if !errors.Is(err, ErrPersonaAudienceDirectoryUnavailable) {
				t.Fatalf("error = %v, want unavailable", err)
			}
		})
	}
}

func TestTodo_AGENT2_029_UsesHomeOrganizationInsteadOfRoleVisibility(t *testing.T) {
	directory := directoryReader(
		&directoryRowsFake{rows: [][]any{{"member"}}},
		&directoryRowsFake{rows: [][]any{{"pilot"}}},
		&directoryRowsFake{rows: [][]any{{"role-org-a"}, {"role-org-b"}}},
	)
	got, err := NewPersonaAudienceDirectoryDB(directory, WithPersonaHomeOrganizationDirectory(personaHomeOrganizationFake{organization: "home-org"})).ResolvePersonaAudienceMember(personaDirectoryContext(t, trust.SubjectKindHuman), "home-tenant", "member-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.OrganizationScope != "home-org" {
		t.Fatalf("organization scope = %q, want authoritative home-org", got.OrganizationScope)
	}
}

func TestTodo_AGENT2_029_FailsClosedWhenHomeOrganizationUnavailable(t *testing.T) {
	directory := directoryReader(
		&directoryRowsFake{rows: [][]any{{"member"}}},
		&directoryRowsFake{rows: [][]any{{"pilot"}}},
	)
	_, err := NewPersonaAudienceDirectoryDB(directory, WithPersonaHomeOrganizationDirectory(personaHomeOrganizationFake{err: ErrAgentHomeOrganizationUnavailable})).ResolvePersonaAudienceMember(personaDirectoryContext(t, trust.SubjectKindHuman), "home-tenant", "member-1")
	if !errors.Is(err, ErrPersonaAudienceDirectoryUnavailable) {
		t.Fatalf("error = %v, want persona audience unavailable", err)
	}
}

type personaHomeOrganizationFake struct {
	organization string
	err          error
}

func (f personaHomeOrganizationFake) CurrentHomeOrganization(context.Context, values.TenantId, string) (string, error) {
	return f.organization, f.err
}

func toRows(rows []*directoryRowsFake) []dbport.Rows {
	result := make([]dbport.Rows, len(rows))
	for i, row := range rows {
		result[i] = row
	}
	return result
}

func TestTodo_AGENTP_026_RejectsNonHumanAndInvalidHomeTenant(t *testing.T) {
	directory := NewPersonaAudienceDirectoryDB(directoryReader())
	if _, err := directory.ResolvePersonaAudienceMember(personaDirectoryContext(t, trust.SubjectKindService), "home-tenant", "member-1"); !errors.Is(err, ErrPersonaAudienceDirectoryUnavailable) {
		t.Fatalf("service principal error = %v", err)
	}
	if _, err := directory.ResolvePersonaAudienceMember(personaDirectoryContext(t, trust.SubjectKindHuman), "foreign tenant", "member-1"); !errors.Is(err, ErrPersonaAudienceDirectoryUnavailable) {
		t.Fatalf("invalid tenant error = %v", err)
	}
	if _, err := directory.ResolvePersonaAudienceMember(context.Background(), "home-tenant", "member-1"); !errors.Is(err, ErrPersonaAudienceDirectoryUnavailable) {
		t.Fatalf("missing principal error = %v", err)
	}
}
