package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type personaRunPolicyReaderFake struct {
	policy     agentstore.PersonaRunPolicy
	err        error
	seenTenant uuid.UUID
	seenEntity string
	seenAt     time.Time
}

func (f *personaRunPolicyReaderFake) CurrentPersonaRunPolicy(_ context.Context, tenant uuid.UUID, entity string, at time.Time) (agentstore.PersonaRunPolicy, error) {
	f.seenTenant, f.seenEntity, f.seenAt = tenant, entity, at
	return f.policy, f.err
}

func TestPersonaRunEffectivePolicyResolver_UsesCurrentPolicyAndServerDeadline(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.FixedZone("test", -4*60*60))
	tenantID := uuid.New()
	reader := &personaRunPolicyReaderFake{policy: agentstore.PersonaRunPolicy{
		TenantID: tenantID, LegalEntityID: "entity-1", Revision: 7,
		EffectiveFrom: now.Add(-time.Hour), MaxCostMicros: 2500, MaxInputTokens: 12000,
		MaxOutputTokens: 3000, MaxRunDuration: 45 * time.Second,
	}}
	resolver, err := NewPersonaRunEffectivePolicyResolver(reader, func(values.TenantId) uuid.UUID { return tenantID }, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	got, err := resolver.Resolve(context.Background(), "tenant-1", "entity-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Budget.MaxCostMicros != 2500 || got.Budget.MaxInputTokens != 12000 || got.Budget.MaxOutputTokens != 3000 {
		t.Fatalf("budget = %+v, want persisted tenant policy ceilings", got.Budget)
	}
	if !got.Deadline.Equal(now.UTC().Add(45 * time.Second)) {
		t.Fatalf("deadline = %s, want server time plus policy duration", got.Deadline)
	}
	if got.PolicyDigest == "" || got.Revision != 7 || reader.seenTenant != tenantID || reader.seenEntity != "entity-1" || !reader.seenAt.Equal(now.UTC()) {
		t.Fatalf("resolved policy = %+v, reader scope/time = %s/%s/%s", got, reader.seenTenant, reader.seenEntity, reader.seenAt)
	}
}

func TestPersonaRunEffectivePolicyResolver_FailsClosed(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	tenantID := uuid.New()
	valid := agentstore.PersonaRunPolicy{TenantID: tenantID, LegalEntityID: "entity-1", Revision: 1,
		EffectiveFrom: now.Add(-time.Minute), MaxCostMicros: 1, MaxInputTokens: 1, MaxOutputTokens: 1, MaxRunDuration: time.Second}
	cases := []struct {
		name           string
		reader         *personaRunPolicyReaderFake
		mapper         func(values.TenantId) uuid.UUID
		ctx            context.Context
		tenant, entity string
	}{
		{name: "missing row", reader: &personaRunPolicyReaderFake{err: agentstore.ErrPersonaRunPolicyNotFound}, ctx: context.Background(), tenant: "tenant-1", entity: "entity-1"},
		{name: "invalid policy", reader: &personaRunPolicyReaderFake{policy: agentstore.PersonaRunPolicy{TenantID: tenantID, LegalEntityID: "entity-1", Revision: 1}}, ctx: context.Background(), tenant: "tenant-1", entity: "entity-1"},
		{name: "tenant mapping missing", reader: &personaRunPolicyReaderFake{policy: valid}, mapper: func(values.TenantId) uuid.UUID { return uuid.Nil }, ctx: context.Background(), tenant: "tenant-1", entity: "entity-1"},
		{name: "nil context", reader: &personaRunPolicyReaderFake{policy: valid}, ctx: nil, tenant: "tenant-1", entity: "entity-1"},
		{name: "missing tenant", reader: &personaRunPolicyReaderFake{policy: valid}, ctx: context.Background(), tenant: "", entity: "entity-1"},
		{name: "missing entity", reader: &personaRunPolicyReaderFake{policy: valid}, ctx: context.Background(), tenant: "tenant-1", entity: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mapper := tc.mapper
			if mapper == nil {
				mapper = func(values.TenantId) uuid.UUID { return tenantID }
			}
			current, err := NewPersonaRunEffectivePolicyResolver(tc.reader, mapper, func() time.Time { return now })
			if err != nil {
				t.Fatal(err)
			}
			if _, err := current.Resolve(tc.ctx, tc.tenant, tc.entity); !errors.Is(err, ErrPersonaRunEffectivePolicy) {
				t.Fatalf("Resolve() error = %v, want fail-closed policy error", err)
			}
		})
	}
	if _, err := NewPersonaRunEffectivePolicyResolver(nil, func(values.TenantId) uuid.UUID { return tenantID }, func() time.Time { return now }); !errors.Is(err, ErrPersonaRunEffectivePolicy) {
		t.Fatalf("nil reader constructor error = %v", err)
	}
	reader := &personaRunPolicyReaderFake{policy: valid}
	if _, err := NewPersonaRunEffectivePolicyResolver(reader, nil, func() time.Time { return now }); !errors.Is(err, ErrPersonaRunEffectivePolicy) {
		t.Fatalf("nil tenant mapper constructor error = %v", err)
	}
	if _, err := NewPersonaRunEffectivePolicyResolver(reader, func(values.TenantId) uuid.UUID { return tenantID }, nil); !errors.Is(err, ErrPersonaRunEffectivePolicy) {
		t.Fatalf("nil clock constructor error = %v", err)
	}
}
