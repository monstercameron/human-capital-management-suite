package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/governance"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type candidatePrincipalBindings struct {
	binding agentpersonastore.PersonaAgentPrincipalBinding
	calls   []values.TenantId
}

func (s *candidatePrincipalBindings) ResolvePersonaAgentPrincipal(_ context.Context, tenant values.TenantId, _ string, _ int64) (agentpersonastore.PersonaAgentPrincipalBinding, error) {
	s.calls = append(s.calls, tenant)
	return s.binding, nil
}

type candidatePrincipalAuthority struct {
	rows  map[values.TenantId]governance.Principal
	calls []values.TenantId
}

func (s *candidatePrincipalAuthority) CurrentPrincipal(_ context.Context, tenant values.TenantId, _ uuid.UUID) (governance.Principal, error) {
	s.calls = append(s.calls, tenant)
	row, ok := s.rows[tenant]
	if !ok {
		return governance.Principal{}, errors.New("missing current principal")
	}
	return row, nil
}

func candidatePrincipalFixture(t *testing.T) (*PersonaCandidatePrincipalResolver, context.Context, *candidatePrincipalBindings, *candidatePrincipalAuthority) {
	t.Helper()
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	productionID, syntheticID, principalID := uuid.New(), uuid.New(), uuid.New()
	bindings := &candidatePrincipalBindings{binding: agentpersonastore.PersonaAgentPrincipalBinding{TenantID: "production", PersonaID: "policy-helper", PersonaVersion: 3, PrincipalID: principalID, ProvisionedAt: now.Add(-time.Hour)}}
	authority := &candidatePrincipalAuthority{rows: map[values.TenantId]governance.Principal{
		"production": {TenantID: productionID, PrincipalID: principalID, Kind: "SERVICE", Lifecycle: "ACTIVE"},
		"synthetic":  {TenantID: syntheticID, PrincipalID: principalID, Kind: "SERVICE", Lifecycle: "ACTIVE"},
	}}
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "synthetic", Subject: "invoker", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "fixture-session", CredentialDigest: "fixture-only-test-credential", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), Purposes: []string{"persona-mention"}})
	if err != nil {
		t.Fatal(err)
	}
	resolver := &PersonaCandidatePrincipalResolver{
		Target: agenteval.PersonaEvaluationTarget{TenantID: "production", SyntheticTenantID: "synthetic", PersonaID: "policy-helper", PersonaVersion: 3, InvokerID: "invoker", ProfileDigest: "sha256:" + strings.Repeat("a", 64), ModelDigest: "sha256:" + strings.Repeat("b", 64)},
		Scope:  &liveEvaluationTestScope{}, Bindings: bindings, Principals: authority,
		TenantUUID: func(tenant values.TenantId) uuid.UUID {
			if tenant == "production" {
				return productionID
			}
			if tenant == "synthetic" {
				return syntheticID
			}
			return uuid.Nil
		},
		Now: func() time.Time { return now },
	}
	return resolver, trust.WithPrincipal(context.Background(), principal), bindings, authority
}

func TestTodo_AGENTP_021_CandidatePrincipalUsesCurrentTenantIdentities(t *testing.T) {
	r, ctx, bindings, authority := candidatePrincipalFixture(t)
	id, err := r.Resolve(ctx, "synthetic", "policy-helper", 3)
	if err != nil || id != bindings.binding.PrincipalID.String() {
		t.Fatalf("candidate identity: %q %v", id, err)
	}
	if len(bindings.calls) != 1 || bindings.calls[0] != "production" || len(authority.calls) != 2 || authority.calls[0] != "production" || authority.calls[1] != "synthetic" {
		t.Fatalf("identity owners: binding=%v principals=%v", bindings.calls, authority.calls)
	}
	// Current trust is re-read; an earlier successful resolution cannot keep a
	// revoked synthetic identity alive for a subsequent admission.
	row := authority.rows["synthetic"]
	row.Lifecycle = "REVOKED"
	authority.rows["synthetic"] = row
	if id, err := r.Resolve(ctx, "synthetic", "policy-helper", 3); id != "" || !errors.Is(err, agenteval.ErrPersonaEvaluation) {
		t.Fatalf("revoked identity reused: %q %v", id, err)
	}
}

func TestTodo_AGENTP_021_CandidatePrincipalRejectsForeignAndStaleOwners(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*PersonaCandidatePrincipalResolver, *candidatePrincipalBindings, *candidatePrincipalAuthority)
	}{
		{"binding tenant", func(_ *PersonaCandidatePrincipalResolver, b *candidatePrincipalBindings, _ *candidatePrincipalAuthority) {
			b.binding.TenantID = "foreign"
		}},
		{"binding version", func(_ *PersonaCandidatePrincipalResolver, b *candidatePrincipalBindings, _ *candidatePrincipalAuthority) {
			b.binding.PersonaVersion = 2
		}},
		{"binding persona", func(_ *PersonaCandidatePrincipalResolver, b *candidatePrincipalBindings, _ *candidatePrincipalAuthority) {
			b.binding.PersonaID = "other"
		}},
		{"missing principal", func(_ *PersonaCandidatePrincipalResolver, b *candidatePrincipalBindings, _ *candidatePrincipalAuthority) {
			b.binding.PrincipalID = uuid.Nil
		}},
		{"future binding", func(r *PersonaCandidatePrincipalResolver, b *candidatePrincipalBindings, _ *candidatePrincipalAuthority) {
			b.binding.ProvisionedAt = r.Now().Add(time.Hour)
		}},
		{"foreign trust tenant", func(_ *PersonaCandidatePrincipalResolver, _ *candidatePrincipalBindings, a *candidatePrincipalAuthority) {
			p := a.rows["synthetic"]
			p.TenantID = uuid.New()
			a.rows["synthetic"] = p
		}},
		{"human identity", func(_ *PersonaCandidatePrincipalResolver, _ *candidatePrincipalBindings, a *candidatePrincipalAuthority) {
			p := a.rows["synthetic"]
			p.Kind = "USER"
			a.rows["synthetic"] = p
		}},
		{"foreign identity", func(_ *PersonaCandidatePrincipalResolver, _ *candidatePrincipalBindings, a *candidatePrincipalAuthority) {
			p := a.rows["synthetic"]
			p.PrincipalID = uuid.New()
			a.rows["synthetic"] = p
		}},
		{"expired identity", func(r *PersonaCandidatePrincipalResolver, _ *candidatePrincipalBindings, a *candidatePrincipalAuthority) {
			p := a.rows["synthetic"]
			expiry := r.Now()
			p.ExpiresAt = &expiry
			a.rows["synthetic"] = p
		}},
		{"missing synthetic identity", func(_ *PersonaCandidatePrincipalResolver, _ *candidatePrincipalBindings, a *candidatePrincipalAuthority) {
			delete(a.rows, "synthetic")
		}},
		{"revoked production identity", func(_ *PersonaCandidatePrincipalResolver, _ *candidatePrincipalBindings, a *candidatePrincipalAuthority) {
			p := a.rows["production"]
			p.Lifecycle = "REVOKED"
			a.rows["production"] = p
		}},
		{"same canonical tenant", func(r *PersonaCandidatePrincipalResolver, _ *candidatePrincipalBindings, _ *candidatePrincipalAuthority) {
			id := uuid.New()
			r.TenantUUID = func(values.TenantId) uuid.UUID { return id }
		}},
		{"missing tenant mapping", func(r *PersonaCandidatePrincipalResolver, _ *candidatePrincipalBindings, _ *candidatePrincipalAuthority) {
			r.TenantUUID = func(values.TenantId) uuid.UUID { return uuid.Nil }
		}},
		{"expired invoker", func(r *PersonaCandidatePrincipalResolver, _ *candidatePrincipalBindings, _ *candidatePrincipalAuthority) {
			old := r.Now()
			r.Now = func() time.Time { return old.Add(2 * time.Hour) }
		}},
		{"revoked evaluation scope", func(r *PersonaCandidatePrincipalResolver, _ *candidatePrincipalBindings, _ *candidatePrincipalAuthority) {
			r.Scope = &liveEvaluationTestScope{denied: true}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, ctx, b, a := candidatePrincipalFixture(t)
			tc.change(r, b, a)
			id, err := r.Resolve(ctx, "synthetic", "policy-helper", 3)
			if id != "" || !errors.Is(err, agenteval.ErrPersonaEvaluation) {
				t.Fatalf("unverified identity: %q %v", id, err)
			}
		})
	}
}

func TestTodo_AGENTP_021_CandidatePrincipalRejectsOrdinaryInvocation(t *testing.T) {
	for _, request := range []struct {
		tenant  values.TenantId
		persona string
		version int64
	}{{"production", "policy-helper", 3}, {"foreign", "policy-helper", 3}, {"synthetic", "other", 3}, {"synthetic", "policy-helper", 4}} {
		r, ctx, b, a := candidatePrincipalFixture(t)
		if id, err := r.Resolve(ctx, request.tenant, request.persona, request.version); id != "" || !errors.Is(err, agenteval.ErrPersonaEvaluation) {
			t.Fatalf("ordinary invocation: %q %v", id, err)
		}
		if len(b.calls) != 0 || len(a.calls) != 0 {
			t.Fatal("ordinary invocation reached identity stores")
		}
	}
	r, _, b, a := candidatePrincipalFixture(t)
	if _, err := r.Resolve(context.Background(), "synthetic", "policy-helper", 3); !errors.Is(err, agenteval.ErrPersonaEvaluation) {
		t.Fatal("unverified invoker accepted")
	}
	if len(b.calls) != 0 || len(a.calls) != 0 {
		t.Fatal("unverified invoker reached identity stores")
	}
	var absent *PersonaCandidatePrincipalResolver
	if _, err := absent.Resolve(context.Background(), "synthetic", "policy-helper", 3); !errors.Is(err, agenteval.ErrPersonaEvaluation) {
		t.Fatal("absent resolver accepted")
	}
}
