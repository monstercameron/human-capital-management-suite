package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/aggregates"
	"github.com/monstercameron/human-capital-management-suite/internal/data/governance"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type personaPrincipalBindingFake struct {
	binding agentpersonastore.PersonaAgentPrincipalBinding
	err     error
}

func (f personaPrincipalBindingFake) ResolvePersonaAgentPrincipal(context.Context, values.TenantId, string, int64) (agentpersonastore.PersonaAgentPrincipalBinding, error) {
	return f.binding, f.err
}

type personaPrincipalAuthorityFake struct {
	principal governance.Principal
	err       error
	gotTenant values.TenantId
	gotID     uuid.UUID
}

type personaPrincipalBindingWriterFake struct {
	calls int
	gotID uuid.UUID
	err   error
}

func (f *personaPrincipalBindingWriterFake) RegisterPersonaAgentPrincipal(_ context.Context, _ values.TenantId, _ string, _ int64, id uuid.UUID, _ time.Time) error {
	f.calls++
	f.gotID = id
	return f.err
}

func (f *personaPrincipalAuthorityFake) CurrentPrincipal(_ context.Context, tenant values.TenantId, id uuid.UUID) (governance.Principal, error) {
	f.gotTenant, f.gotID = tenant, id
	return f.principal, f.err
}

func TestTodo_AGENT_015_PersonaRunResolvesOnlyProvisionedActiveServicePrincipal(t *testing.T) {
	principalID := uuid.New()
	bindings := personaPrincipalBindingFake{binding: agentpersonastore.PersonaAgentPrincipalBinding{
		TenantID: "tenant-a", PersonaID: "persona:benefits", PersonaVersion: 7, PrincipalID: principalID,
	}}
	authority := &personaPrincipalAuthorityFake{principal: governance.Principal{
		TenantID: uuid.New(), PrincipalID: principalID, Kind: "SERVICE", Lifecycle: "ACTIVE",
	}}
	resolver := PersonaRunAgentPrincipalResolver{Bindings: bindings, Principals: authority, Now: func() time.Time { return time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC) }}
	got, err := resolver.Resolve(context.Background(), values.TenantId("tenant-a"), "persona:benefits", 7)
	if err != nil {
		t.Fatal(err)
	}
	if got != principalID.String() || authority.gotTenant != "tenant-a" || authority.gotID != principalID {
		t.Fatalf("principal=%q authority tenant=%q id=%s", got, authority.gotTenant, authority.gotID)
	}
}

func TestTodo_AGENT_015_PersonaRunPrincipalFailsClosed(t *testing.T) {
	principalID := uuid.New()
	active := governance.Principal{TenantID: uuid.New(), PrincipalID: principalID, Kind: "SERVICE", Lifecycle: "ACTIVE"}
	baseBinding := agentpersonastore.PersonaAgentPrincipalBinding{TenantID: "tenant-a", PersonaID: "persona:benefits", PersonaVersion: 7, PrincipalID: principalID}
	tests := []struct {
		name         string
		binding      agentpersonastore.PersonaAgentPrincipalBinding
		principal    governance.Principal
		bindingErr   error
		principalErr error
		wantErr      bool
	}{
		{name: "binding tenant mismatch", binding: func() agentpersonastore.PersonaAgentPrincipalBinding {
			b := baseBinding
			b.TenantID = "tenant-b"
			return b
		}(), principal: active, wantErr: true},
		{name: "missing principal id", binding: func() agentpersonastore.PersonaAgentPrincipalBinding {
			b := baseBinding
			b.PrincipalID = uuid.Nil
			return b
		}(), principal: active, wantErr: true},
		{name: "human principal", binding: baseBinding, principal: governance.Principal{TenantID: active.TenantID, PrincipalID: principalID, Kind: "USER", Lifecycle: "ACTIVE"}, wantErr: true},
		{name: "revoked principal", binding: baseBinding, principal: governance.Principal{TenantID: active.TenantID, PrincipalID: principalID, Kind: "SERVICE", Lifecycle: "REVOKED"}, wantErr: true},
		{name: "expired principal", binding: baseBinding, principal: func() governance.Principal {
			p := active
			expiry := time.Date(2026, 9, 30, 11, 0, 0, 0, time.UTC)
			p.ExpiresAt = &expiry
			return p
		}(), wantErr: true},
		{name: "binding authority failure", binding: baseBinding, bindingErr: errors.New("unavailable"), wantErr: true},
		{name: "trust authority failure", binding: baseBinding, principalErr: errors.New("unavailable"), wantErr: true},
		{name: "invalid exact version", binding: baseBinding, principal: active, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resolver := PersonaRunAgentPrincipalResolver{
				Bindings:   personaPrincipalBindingFake{binding: tc.binding, err: tc.bindingErr},
				Principals: &personaPrincipalAuthorityFake{principal: tc.principal, err: tc.principalErr},
				Now:        func() time.Time { return time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC) },
			}
			version := int64(7)
			if tc.name == "invalid exact version" {
				version = 0
			}
			got, err := resolver.Resolve(context.Background(), values.TenantId("tenant-a"), "persona:benefits", version)
			if (err != nil) != tc.wantErr || got != "" {
				t.Fatalf("Resolve() = (%q, %v), want fail closed", got, err)
			}
		})
	}
}

func TestTodo_AGENT_015_PersonaPrincipalProvisionerRequiresCurrentServicePrincipal(t *testing.T) {
	principalID := uuid.New()
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	active := governance.Principal{TenantID: uuid.New(), PrincipalID: principalID, Kind: "SERVICE", Lifecycle: "ACTIVE"}
	tests := []struct {
		name      string
		principal governance.Principal
		wantCalls int
	}{
		{name: "active service", principal: active, wantCalls: 1},
		{name: "user", principal: governance.Principal{TenantID: active.TenantID, PrincipalID: principalID, Kind: "USER", Lifecycle: "ACTIVE"}},
		{name: "revoked service", principal: governance.Principal{TenantID: active.TenantID, PrincipalID: principalID, Kind: "SERVICE", Lifecycle: "REVOKED"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			writer := &personaPrincipalBindingWriterFake{}
			service := PersonaAgentPrincipalProvisioner{
				Bindings: writer, Principals: &personaPrincipalAuthorityFake{principal: tc.principal},
				Now: func() time.Time { return now },
			}
			err := service.Provision(context.Background(), values.TenantId("tenant-a"), "persona:benefits", 7, principalID)
			if (err == nil) != (tc.wantCalls == 1) || writer.calls != tc.wantCalls {
				t.Fatalf("Provision() error=%v writer calls=%d, want calls=%d", err, writer.calls, tc.wantCalls)
			}
			if tc.wantCalls == 1 && (writer.gotID != principalID) {
				t.Fatalf("provisioned principal=%s, want %s", writer.gotID, principalID)
			}
		})
	}
}

func TestTodo_AGENT_015_PersonaRunLegalEntityRequiresOneActiveCurrentEmployment(t *testing.T) {
	tenant, legalA, legalB := uuid.New(), uuid.New(), uuid.New()
	active := aggregates.Employment{Envelope: aggregates.Envelope{Tenant: tenant}, LegalEntityRef: legalA, EmploymentStatus: "ACTIVE"}
	tests := []struct {
		name        string
		employments []aggregates.Employment
		want        uuid.UUID
		wantErr     bool
	}{
		{name: "one current active employment", employments: []aggregates.Employment{active}, want: legalA},
		{name: "no current employment", wantErr: true},
		{name: "ambiguous current employments", employments: []aggregates.Employment{active, {Envelope: aggregates.Envelope{Tenant: tenant}, LegalEntityRef: legalB, EmploymentStatus: "ACTIVE"}}, wantErr: true},
		{name: "suspended employment", employments: []aggregates.Employment{{Envelope: aggregates.Envelope{Tenant: tenant}, LegalEntityRef: legalA, EmploymentStatus: "SUSPENDED"}}, wantErr: true},
		{name: "foreign tenant employment", employments: []aggregates.Employment{{Envelope: aggregates.Envelope{Tenant: uuid.New()}, LegalEntityRef: legalA, EmploymentStatus: "ACTIVE"}}, wantErr: true},
		{name: "missing legal entity", employments: []aggregates.Employment{{Envelope: aggregates.Envelope{Tenant: tenant}, EmploymentStatus: "ACTIVE"}}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := personaRunLegalEntityFromEmployment(tenant, tc.employments)
			if (err != nil) != tc.wantErr || got != tc.want {
				t.Fatalf("resolved legal entity = %s, %v; want %s, error=%v", got, err, tc.want, tc.wantErr)
			}
		})
	}
}
