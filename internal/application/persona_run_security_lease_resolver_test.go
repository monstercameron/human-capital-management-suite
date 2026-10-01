package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type personaRunSecurityLeaseRepositoryFake struct {
	lease    agentstore.PersonaSecurityLease
	err      error
	tenantID uuid.UUID
	request  string
	at       time.Time
	calls    int
}

func (f *personaRunSecurityLeaseRepositoryFake) ResolveActivePersonaSecurityLease(_ context.Context, tenant uuid.UUID, admission string, at time.Time) (agentstore.PersonaSecurityLease, error) {
	f.calls++
	f.tenantID, f.request, f.at = tenant, admission, at
	return f.lease, f.err
}

func TestDatabasePersonaRunSecurityLeaseResolver_BindsDurableAdmissionAndRun(t *testing.T) {
	at := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	tenantID := uuid.New()
	repository := &personaRunSecurityLeaseRepositoryFake{lease: personaRunSecurityLeaseFixture(tenantID, at)}
	resolver, err := NewDatabasePersonaRunSecurityLeaseResolver(PersonaRunSecurityLeaseResolverConfig{
		Leases: repository, TenantUUID: func(values.TenantId) uuid.UUID { return tenantID }, Now: func() time.Time { return at },
	})
	if err != nil {
		t.Fatal(err)
	}
	admission, run := personaRunSecurityEvidenceFixture()
	got, err := resolver.ResolvePersonaRunSecurityLease(context.Background(), admission, run)
	if err != nil || got != agentsecurity.KillSwitchLeaseID(repository.lease.LeaseID) {
		t.Fatalf("resolved lease=%q err=%v", got, err)
	}
	if repository.calls != 1 || repository.tenantID != tenantID || repository.request != admission.ID || !repository.at.Equal(at) {
		t.Fatalf("durable lookup tenant=%s admission=%q at=%s calls=%d", repository.tenantID, repository.request, repository.at, repository.calls)
	}

	for _, tc := range []struct {
		name   string
		lookup bool
		change func(*agentrun.Record, *runstate.Run)
	}{
		{name: "foreign tenant", change: func(_ *agentrun.Record, r *runstate.Run) { r.TenantID = "tenant-foreign" }},
		{name: "foreign run", change: func(_ *agentrun.Record, r *runstate.Run) { r.ID = "run-foreign" }},
		{name: "foreign admission", change: func(_ *agentrun.Record, r *runstate.Run) { r.AdmissionID = "admission-foreign" }},
		{name: "foreign persona version", lookup: true, change: func(a *agentrun.Record, _ *runstate.Run) { a.Request.Persona.Version = "v2" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, r := personaRunSecurityEvidenceFixture()
			tc.change(&a, &r)
			before := repository.calls
			if _, err := resolver.ResolvePersonaRunSecurityLease(context.Background(), a, r); !errors.Is(err, errPersonaRunSecurityLeaseResolver) {
				t.Fatalf("mismatched durable evidence err=%v", err)
			}
			wantCalls := before
			if tc.lookup {
				wantCalls++
			}
			if repository.calls != wantCalls {
				t.Fatal("invalid admission/run evidence reached durable lease lookup")
			}
		})
	}

	wrongLease := repository.lease
	wrongLease.InvocationID = ""
	repository.lease = wrongLease
	if _, err := resolver.ResolvePersonaRunSecurityLease(context.Background(), admission, run); !errors.Is(err, errPersonaRunSecurityLeaseResolver) {
		t.Fatalf("lease with missing invocation binding err=%v", err)
	}
}

func TestDatabasePersonaRunSecurityLeaseResolver_DeniesRevokedAndOrphanedLeases(t *testing.T) {
	at := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "revoked scope", err: agentstore.ErrPersonaSecurityLeaseRevoked},
		{name: "expired lease", err: agentstore.ErrPersonaSecurityLeaseExpired},
		{name: "orphan recovery required", err: agentstore.ErrPersonaSecurityRecovery},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repository := &personaRunSecurityLeaseRepositoryFake{err: tc.err}
			resolver, err := NewDatabasePersonaRunSecurityLeaseResolver(PersonaRunSecurityLeaseResolverConfig{
				Leases: repository, TenantUUID: func(values.TenantId) uuid.UUID { return uuid.New() }, Now: func() time.Time { return at },
			})
			if err != nil {
				t.Fatal(err)
			}
			admission, run := personaRunSecurityEvidenceFixture()
			if _, err := resolver.ResolvePersonaRunSecurityLease(context.Background(), admission, run); !errors.Is(err, tc.err) {
				t.Fatalf("denied lease err=%v, want %v", err, tc.err)
			}
		})
	}
}

func TestDatabasePersonaRunSecurityLeaseResolver_RequiresTrustedDependencies(t *testing.T) {
	if _, err := NewDatabasePersonaRunSecurityLeaseResolver(PersonaRunSecurityLeaseResolverConfig{}); !errors.Is(err, errPersonaRunSecurityLeaseResolver) {
		t.Fatalf("empty config err=%v", err)
	}
}

func personaRunSecurityEvidenceFixture() (agentrun.Record, runstate.Run) {
	persona := &agentrun.PersonaRef{ID: "persona-a", Version: "v1", Digest: "persona-digest"}
	principal := agentrun.PrincipalChain{Mode: agentrun.ModeOnBehalfOf, InvokerID: "human-a", AgentPrincipalID: "persona-principal"}
	authority := agentrun.AuthoritySnapshot{
		Agent:          agentrun.VersionRef{AgentID: "agent-a", Version: "av1", Digest: "agent-digest"},
		InstallationID: "install-a", Principal: principal,
		Audience: agentrun.AudienceScope{ID: "conversation-a", SnapshotID: "audience-snapshot", Digest: "audience-digest"},
		Context:  agentrun.ContextScope{ID: "thread-a", SnapshotID: "context-snapshot"},
		GrantRef: "grant-a", PolicyDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	request := agentrun.Request{
		Source:  agentrun.SourceIdentity{TenantID: "tenant-a", Kind: agentrun.SourcePersonaMention, Key: "key-a", Ref: "post-a"},
		Persona: persona, Agent: authority.Agent, InstallationID: authority.InstallationID, Principal: principal,
		Audience: authority.Audience, Context: authority.Context, Deadline: time.Date(2026, 9, 30, 16, 0, 0, 0, time.UTC),
	}
	admission := agentrun.Record{ID: "admission-a", Request: request, RequestDigest: "request-digest", Decision: agentrun.DecisionAccepted, Authority: authority}
	run := runstate.Run{ID: admission.ID, AdmissionID: admission.ID, TenantID: request.Source.TenantID,
		PrincipalMode: principal.Mode, ActorID: principal.InvokerID, AgentID: request.Agent.AgentID,
		AgentVersion: request.Agent.Version, AgentDigest: request.Agent.Digest, RequestDigest: admission.RequestDigest,
		Deadline: request.Deadline}
	return admission, run
}

func personaRunSecurityLeaseFixture(tenantID uuid.UUID, at time.Time) agentstore.PersonaSecurityLease {
	return agentstore.PersonaSecurityLease{
		TenantID: tenantID.String(), LeaseID: "lease-a", AdmissionID: "admission-a", InvocationID: "invocation-a", RunID: "admission-a",
		IssuerID: "hcmnext_agent_app", AuthorityRef: "grant-a", PolicyDigest: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		PrincipalID: "human-a", PersonaID: "persona-a", PersonaVersion: "v1", InstallationID: "install-a",
		Mode: string(agentrun.ModeOnBehalfOf), AdmissionDecision: string(agentrun.DecisionAccepted),
		IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Minute), AdmissionDeadline: at.Add(time.Hour),
	}
}
