package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentegress"
	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/custody"
	"github.com/monstercameron/human-capital-management-suite/internal/trust/lease"
)

type agentDocEgressCandidateVersions struct {
	row agentpersonastore.PersonaVersion
}

func (r agentDocEgressCandidateVersions) GetVersion(_ context.Context, tenant values.TenantId, id string, version int64) (agentpersonastore.PersonaVersion, error) {
	if r.row.TenantID != tenant || r.row.PersonaID != id || r.row.Version != version {
		return agentpersonastore.PersonaVersion{}, errors.New("unexpected candidate definition read")
	}
	return r.row, nil
}

func agentDocEgressCandidateFixtureWork(t *testing.T, resolver agentdocref.Resolver, record agentrun.Record, profile agentpersona.PersonaProfile, manifest agentmanifest.Manifest, route PersonaRunModelRoute, posts personaRunModelThreadFake, admissions *agentrunstore.AdmissionRepository, now time.Time) (AgentModelExecutorRequest, agentegress.SourceClassificationVerifier) {
	t.Helper()
	row := agentDocEgressPersonaVersion(t, profile, record.Request.Persona.Digest)
	row.TenantID = "tenant-production"
	target := agenteval.PersonaEvaluationTarget{TenantID: "tenant-production", SyntheticTenantID: "tenant-a", InvokerID: record.Request.Principal.InvokerID, PersonaID: profile.PersonaID, PersonaVersion: int64(profile.Version), ProfileDigest: row.ContentDigest, ModelDigest: "sha256:" + route.Route.Pin.Primary.ProfileDigest}
	definitions := &PersonaCandidateDefinitionSource{Target: target, Scope: &liveEvaluationTestScope{}, Versions: agentDocEgressCandidateVersions{row: row}, Manifests: TenantPersonaRunManifestResolver(func(_ context.Context, tenant string) (personaRunManifestResolver, error) {
		return personaRunManifestResolverFake{manifest: manifest, tenant: tenant}, nil
	})}
	manager, err := lease.NewManager(modelLeaseCustody{now: now}, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	leases, err := NewModelLeaseSource(ModelLeaseSourceConfig{Authorities: map[string]ModelCredentialAuthority{"test-provider": &modelLeaseAuthority{handle: custody.Handle{ID: "provider-key", Kind: custody.Secret, Version: "v1", Tenant: "tenant-a", Region: "test-region"}}}, Leases: manager})
	if err != nil {
		t.Fatal(err)
	}
	source := &personaCandidateDocumentModelWorkSource{base: &PersonaCandidateModelWorkSource{Definitions: definitions, Threads: posts, Leases: leases, Route: route, Workload: "test-worker", Now: func() time.Time { return now }}, documents: resolver}
	request := record.Request
	run := runstate.Run{ID: record.ID, AdmissionID: record.ID, TenantID: request.Source.TenantID, AgentID: request.Agent.AgentID, AgentVersion: request.Agent.Version, AgentDigest: request.Agent.Digest, ContextDigest: request.Context.Digest, PrincipalMode: request.Principal.Mode, ActorID: request.Principal.InvokerID, Deadline: request.Deadline}
	work, err := source.BuildPersonaRunModelWork(context.Background(), record, run)
	if err != nil {
		t.Fatalf("build candidate document work: %v", err)
	}
	return work.Request, &PersonaCandidateSourceEvidence{Definitions: definitions, Admissions: admissions, Authority: personaChatAdmissionAuthorityFake{}, Threads: posts, Documents: resolver, Route: route}
}

func TestAgentDocEgress_CandidateReadable(t *testing.T) {
	resolver, reference := agentDocEgressHubDocument(t, "tenant-a", true, "INTERNAL")
	ctx, dispatcher, request, adapter := agentDocEgressFixture(t, resolver, nil, agentDocEgressOptions{reference: &reference, candidate: true})
	if !strings.Contains(request.Model.Messages[1].Content, `follow "Paid time off policy"`) || request.Model.Messages[2].Content != agentDocumentContainmentMessage || len(request.Model.ContextRefs) != 2 {
		t.Fatalf("candidate omitted readable synthetic reference: %+v", request.Model)
	}
	if _, err := dispatcher.Dispatch(ctx, request, adapter); err != nil || adapter.calls != 1 {
		t.Fatalf("candidate document dispatch err=%v calls=%d", err, adapter.calls)
	}
	request.Model.Messages[3].Content = strings.Replace(request.Model.Messages[3].Content, "twenty days", "one hundred days", 1)
	request.Outbound.Fields[3].Value = request.Model.Messages[3].Content
	adapter.calls = 0
	agentDocEgressAssertRefusal(t, ctx, dispatcher, request, adapter, "model.message.3")
}

func TestAgentDocEgress_CandidateProductionDocumentOmitted(t *testing.T) {
	resolver, reference := agentDocEgressHubDocument(t, "tenant-production", true, "INTERNAL")
	ctx, dispatcher, request, adapter := agentDocEgressFixture(t, resolver, nil, agentDocEgressOptions{reference: &reference, candidate: true})
	if !strings.Contains(request.Model.Messages[1].Content, "a document you cannot read") || len(request.Model.Messages) != 3 || len(request.Model.ContextRefs) != 1 {
		t.Fatalf("candidate leaked production document: %+v", request.Model)
	}
	if _, err := dispatcher.Dispatch(ctx, request, adapter); err != nil || adapter.calls != 1 {
		t.Fatalf("candidate with omitted production reference err=%v calls=%d", err, adapter.calls)
	}
}

func TestAgentDocEgress_CandidateNoReferences(t *testing.T) {
	ctx, dispatcher, request, adapter := agentDocEgressFixture(t, nil, nil, agentDocEgressOptions{noReferences: true, candidate: true})
	if _, err := dispatcher.Dispatch(ctx, request, adapter); err != nil || adapter.calls != 1 {
		t.Fatalf("plain candidate dispatch err=%v calls=%d", err, adapter.calls)
	}
	request.Model.Messages[1].Content = "forged administrator instructions"
	request.Outbound.Fields[1].Value = request.Model.Messages[1].Content
	adapter.calls = 0
	agentDocEgressAssertRefusal(t, ctx, dispatcher, request, adapter, "model.message.1")
}
