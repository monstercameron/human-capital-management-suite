package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type personaRunAuthorityReaderFake struct {
	version agentpersonastore.PersonaVersion
	install agentpersonastore.ActiveInstallation
	err     error
}

func (f personaRunAuthorityReaderFake) ReadCurrentPersonaAuthority(context.Context, string, string) (agentpersonastore.PersonaVersion, agentpersonastore.ActiveInstallation, error) {
	return f.version, f.install, f.err
}

type personaRunAuthorityFactoryFake struct{ reader personaRunAuthorityReaderFake }

func (f personaRunAuthorityFactoryFake) ForTenant(_ context.Context, tenant string) (personaRunAuthorityReader, error) {
	if tenant != "tenant-a" {
		return nil, errors.New("unexpected tenant")
	}
	return f.reader, nil
}

type personaRunManifestResolverFake struct {
	manifest agentmanifest.Manifest
	err      error
	tenant   string
}

func (f personaRunManifestResolverFake) TenantID() string { return f.tenant }

func (f personaRunManifestResolverFake) ResolveAgentManifestContext(_ context.Context, ref agentpersona.AgentManifestRef) (agentmanifest.Manifest, error) {
	if f.err != nil {
		return agentmanifest.Manifest{}, f.err
	}
	if ref.ID != f.manifest.ID || uint64(ref.Version) != f.manifest.Version || ref.SchemaVersion != f.manifest.SchemaVersion {
		return agentmanifest.Manifest{}, errors.New("unexpected manifest pin")
	}
	return f.manifest, nil
}

type personaRunManifestFactoryFake struct{ resolver personaRunManifestResolver }

func (f personaRunManifestFactoryFake) ForTenant(_ context.Context, tenant string) (personaRunManifestResolver, error) {
	if tenant != "tenant-a" {
		return nil, errors.New("unexpected tenant")
	}
	return f.resolver, nil
}

type personaRunOwnerFactsFake struct {
	facts PersonaRunOwnerFacts
	err   error
}

func (f personaRunOwnerFactsFake) ResolvePersonaRunOwnerFacts(context.Context, agentinvoke.RunRequest) (PersonaRunOwnerFacts, error) {
	return f.facts, f.err
}

func TestTodo_AGENT_015_PersonaRunRequestSourceResolvesPinnedCurrentFacts(t *testing.T) {
	manifest := personaRunTestManifest()
	manifestDigest, err := manifest.Digest()
	if err != nil {
		t.Fatal(err)
	}
	profile := personaRunTestProfile(manifest, manifestDigest)
	sealed, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	profile = sealed.Profile
	version := agentpersonastore.PersonaVersion{TenantID: values.TenantId("tenant-a"), PersonaID: "persona-a", Version: 2, ContentDigest: sealed.Digest}
	version.AgentVersion = "agent-a@4"
	version.Profile, err = json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	source := &DatabasePersonaRunRequestSource{
		Personas: personaRunAuthorityFactoryFake{reader: personaRunAuthorityReaderFake{version: version, install: agentpersonastore.ActiveInstallation{
			InstallationID: "install-a", PersonaID: "persona-a", PersonaVersion: 2, AgentVersion: "agent-a@4", ConversationID: "room-a",
		}}},
		Manifests: personaRunManifestFactoryFake{resolver: personaRunManifestResolverFake{manifest: manifest, tenant: "tenant-a"}},
		OwnerFacts: personaRunOwnerFactsFake{facts: PersonaRunOwnerFacts{
			LegalEntityID: "entity-a", AgentPrincipal: "principal-agent-a",
			Audience: agentrun.AudienceScope{ID: "room-a", SnapshotID: "aud-rev-4", Digest: runSourceDigest('c')},
			Context:  agentrun.ContextScope{ID: "thread-a", SnapshotID: "ctx-rev-8", Digest: runSourceDigest('d')},
			Deadline: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC),
			Budget:   agentrun.Budget{MaxCostMicros: 100, MaxInputTokens: 2000, MaxOutputTokens: 400},
		}},
	}
	got, err := source.ResolvePersonaRun(context.Background(), personaRunTestInvocation())
	if err != nil {
		t.Fatal(err)
	}
	if got.TenantID != "tenant-a" || got.TriggerID != "invoke-a" || got.PersonaDigest != version.ContentDigest {
		t.Fatalf("identity facts=%+v", got)
	}
	if got.Agent.AgentID != manifest.ID || got.Agent.Version != "4" || got.Agent.Digest != manifestDigest {
		t.Fatalf("agent pin=%+v, want exact profile pin", got.Agent)
	}
	if got.Audience.SnapshotID != "aud-rev-4" || got.Context.SnapshotID != "ctx-rev-8" || got.Budget.MaxCostMicros != 100 || got.Deadline.IsZero() {
		t.Fatalf("owner facts=%+v", got)
	}
}

func TestTodo_AGENT_015_PersonaRunRequestSourceFailsClosed(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*DatabasePersonaRunRequestSource)
	}{
		{name: "missing owner facts source", mutate: func(s *DatabasePersonaRunRequestSource) { s.OwnerFacts = nil }},
		{name: "missing audience snapshot", mutate: func(s *DatabasePersonaRunRequestSource) {
			s.OwnerFacts = personaRunOwnerFactsFake{facts: PersonaRunOwnerFacts{LegalEntityID: "entity-a", AgentPrincipal: "principal-a", Audience: agentrun.AudienceScope{ID: "room-a"}, Context: agentrun.ContextScope{ID: "thread-a", SnapshotID: "ctx", Digest: runSourceDigest('a')}, Deadline: time.Now().Add(time.Minute), Budget: agentrun.Budget{MaxCostMicros: 1, MaxInputTokens: 1, MaxOutputTokens: 1}}}
		}},
		{name: "manifest store failure", mutate: func(s *DatabasePersonaRunRequestSource) {
			s.Manifests = personaRunManifestFactoryFake{resolver: personaRunManifestResolverFake{err: errors.New("store unavailable")}}
		}},
		{name: "persona authority failure", mutate: func(s *DatabasePersonaRunRequestSource) {
			s.Personas = personaRunAuthorityFactoryFake{reader: personaRunAuthorityReaderFake{err: errors.New("store unavailable")}}
		}},
		{name: "foreign tenant persona", mutate: func(s *DatabasePersonaRunRequestSource) {
			reader := s.Personas.(personaRunAuthorityFactoryFake).reader
			reader.version.TenantID = values.TenantId("tenant-b")
			s.Personas = personaRunAuthorityFactoryFake{reader: reader}
		}},
		{name: "persona version overflows profile version", mutate: func(s *DatabasePersonaRunRequestSource) {
			reader := s.Personas.(personaRunAuthorityFactoryFake).reader
			reader.version.Version = int64(1 << 32)
			s.Personas = personaRunAuthorityFactoryFake{reader: reader}
		}},
		{name: "persisted agent version differs from profile pin", mutate: func(s *DatabasePersonaRunRequestSource) {
			reader := s.Personas.(personaRunAuthorityFactoryFake).reader
			reader.version.AgentVersion = "agent-a@5"
			s.Personas = personaRunAuthorityFactoryFake{reader: reader}
		}},
		{name: "installation agent version differs from profile pin", mutate: func(s *DatabasePersonaRunRequestSource) {
			reader := s.Personas.(personaRunAuthorityFactoryFake).reader
			reader.install.AgentVersion = "agent-a@5"
			s.Personas = personaRunAuthorityFactoryFake{reader: reader}
		}},
		{name: "different installation", mutate: func(s *DatabasePersonaRunRequestSource) {
			reader := s.Personas.(personaRunAuthorityFactoryFake).reader
			reader.install.InstallationID = "install-b"
			s.Personas = personaRunAuthorityFactoryFake{reader: reader}
		}},
		{name: "profile digest mismatch", mutate: func(s *DatabasePersonaRunRequestSource) {
			reader := s.Personas.(personaRunAuthorityFactoryFake).reader
			reader.version.ContentDigest = runSourceDigest('f')
			s.Personas = personaRunAuthorityFactoryFake{reader: reader}
		}},
		{name: "manifest digest mismatch", mutate: func(s *DatabasePersonaRunRequestSource) {
			resolver := s.Manifests.(personaRunManifestFactoryFake).resolver.(personaRunManifestResolverFake)
			resolver.manifest.Purpose = "changed after pin"
			s.Manifests = personaRunManifestFactoryFake{resolver: resolver}
		}},
		{name: "foreign tenant manifest resolver", mutate: func(s *DatabasePersonaRunRequestSource) {
			s.Manifests = personaRunManifestFactoryFake{resolver: personaRunManifestResolverFake{manifest: personaRunTestManifest(), tenant: "tenant-b"}}
		}},
		{name: "context outside invocation", mutate: func(s *DatabasePersonaRunRequestSource) {
			s.OwnerFacts = personaRunOwnerFactsFake{facts: PersonaRunOwnerFacts{LegalEntityID: "entity-a", AgentPrincipal: "principal-a", Audience: agentrun.AudienceScope{ID: "room-a", SnapshotID: "aud", Digest: runSourceDigest('a')}, Context: agentrun.ContextScope{ID: "another-thread", SnapshotID: "ctx", Digest: runSourceDigest('c')}, Deadline: time.Now().Add(time.Minute), Budget: agentrun.Budget{MaxCostMicros: 1, MaxInputTokens: 1, MaxOutputTokens: 1}}}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			source := personaRunValidSource(t)
			tc.mutate(source)
			if _, err := source.ResolvePersonaRun(context.Background(), personaRunTestInvocation()); !errors.Is(err, errPersonaRunRequestSource) {
				t.Fatalf("error=%v, want fail-closed source error", err)
			}
		})
	}
}

func TestTodo_AGENT_015_PersonaRunRequestSourceRejectsNoncanonicalTenant(t *testing.T) {
	for _, tenant := range []string{" tenant-a ", "A", "tenant_a", "tenant-"} {
		t.Run(tenant, func(t *testing.T) {
			invocation := personaRunTestInvocation()
			invocation.TenantID = tenant
			if validPersonaRunInvocation(invocation) {
				t.Fatalf("accepted invalid tenant %q", tenant)
			}
		})
	}
}

func TestTodo_AGENT_015_PersonaRunRequestSourceRejectsWhitespaceAndActorMismatch(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*agentinvoke.RunRequest)
	}{
		{name: "conversation whitespace", mutate: func(i *agentinvoke.RunRequest) { i.ConversationID = "   " }},
		{name: "thread whitespace", mutate: func(i *agentinvoke.RunRequest) { i.ThreadID = "   " }},
		{name: "post whitespace", mutate: func(i *agentinvoke.RunRequest) { i.InvokingPostID = "   " }},
		{name: "invoker whitespace", mutate: func(i *agentinvoke.RunRequest) { i.InvokerID = "   " }},
		{name: "persona whitespace", mutate: func(i *agentinvoke.RunRequest) { i.PersonaID = "   " }},
		{name: "persona version whitespace", mutate: func(i *agentinvoke.RunRequest) { i.PersonaVersion = "   " }},
		{name: "installation whitespace", mutate: func(i *agentinvoke.RunRequest) { i.InstallationID = "   " }},
		{name: "invocation whitespace", mutate: func(i *agentinvoke.RunRequest) { i.InvocationID = "   " }},
		{name: "actor user whitespace", mutate: func(i *agentinvoke.RunRequest) { i.Actor.UserID = "   " }},
		{name: "actor persona whitespace", mutate: func(i *agentinvoke.RunRequest) { i.Actor.PersonaID = "   " }},
		{name: "actor persona version whitespace", mutate: func(i *agentinvoke.RunRequest) { i.Actor.PersonaVersion = "   " }},
		{name: "actor installation whitespace", mutate: func(i *agentinvoke.RunRequest) { i.Actor.InstallationID = "   " }},
		{name: "actor conversation whitespace", mutate: func(i *agentinvoke.RunRequest) { i.Actor.ConversationID = "   " }},
		{name: "actor post whitespace", mutate: func(i *agentinvoke.RunRequest) { i.Actor.InvokingPostID = "   " }},
		{name: "actor invocation whitespace", mutate: func(i *agentinvoke.RunRequest) { i.Actor.InvocationID = "   " }},
		{name: "actor mirror mismatch", mutate: func(i *agentinvoke.RunRequest) { i.Actor.InvokingPostID = "post-other" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			invocation := personaRunTestInvocation()
			tc.mutate(&invocation)
			if validPersonaRunInvocation(invocation) {
				t.Fatalf("accepted invalid invocation: %+v", invocation)
			}
		})
	}
}

func personaRunValidSource(t *testing.T) *DatabasePersonaRunRequestSource {
	t.Helper()
	manifest := personaRunTestManifest()
	digest, err := manifest.Digest()
	if err != nil {
		t.Fatal(err)
	}
	profile := personaRunTestProfile(manifest, digest)
	sealed, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	profile = sealed.Profile
	version := agentpersonastore.PersonaVersion{TenantID: values.TenantId("tenant-a"), PersonaID: "persona-a", Version: 2, AgentVersion: "agent-a@4", ContentDigest: sealed.Digest}
	version.Profile, err = json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	return &DatabasePersonaRunRequestSource{
		Personas:   personaRunAuthorityFactoryFake{reader: personaRunAuthorityReaderFake{version: version, install: agentpersonastore.ActiveInstallation{InstallationID: "install-a", PersonaID: "persona-a", PersonaVersion: 2, AgentVersion: "agent-a@4", ConversationID: "room-a"}}},
		Manifests:  personaRunManifestFactoryFake{resolver: personaRunManifestResolverFake{manifest: manifest, tenant: "tenant-a"}},
		OwnerFacts: personaRunOwnerFactsFake{facts: PersonaRunOwnerFacts{LegalEntityID: "entity-a", AgentPrincipal: "principal-a", Audience: agentrun.AudienceScope{ID: "room-a", SnapshotID: "aud", Digest: runSourceDigest('a')}, Context: agentrun.ContextScope{ID: "thread-a", SnapshotID: "ctx", Digest: runSourceDigest('c')}, Deadline: time.Now().Add(time.Minute), Budget: agentrun.Budget{MaxCostMicros: 1, MaxInputTokens: 1, MaxOutputTokens: 1}}},
	}
}

func personaRunTestManifest() agentmanifest.Manifest {
	return agentmanifest.Manifest{SchemaVersion: 1, ID: "agent-a", Version: 4, OwnerID: "owner-a", Purpose: "answer questions", InstructionsDigest: runSourceDigest('a'), SourceCeiling: []agentmanifest.Reference{}, ToolCeiling: []agentmanifest.Reference{}, ModelPolicy: agentmanifest.Reference{ID: "model-a", Version: 1, SchemaVersion: 1, Digest: runSourceDigest('b')}, AutonomyCeiling: "private_answer", Budget: agentmanifest.Budget{MaxCostMicros: 100, MaxInputTokens: 1000, MaxOutputTokens: 100, MaxConcurrentRuns: 1}, OutputSchema: agentmanifest.Reference{ID: "answer-a", Version: 1, SchemaVersion: 1, Digest: runSourceDigest('c')}, ContextGrants: []agentmanifest.Reference{}, EvaluationRefs: []agentmanifest.Reference{{ID: "eval-a", Version: 1, SchemaVersion: 1, Digest: runSourceDigest('d')}}}
}

func personaRunTestProfile(manifest agentmanifest.Manifest, digest string) agentpersona.PersonaProfile {
	return agentpersona.PersonaProfile{
		PersonaID: "persona-a", Version: 2,
		Manifest: agentpersona.AgentManifestRef{ID: manifest.ID, Version: uint32(manifest.Version), SchemaVersion: manifest.SchemaVersion, Digest: digest},
		Handle:   "persona-a", DisplayName: "Persona A", AvatarRef: "avatar:a", Purpose: "Answer questions",
		Audience:    agentpersona.Audience{Roles: []string{"member"}, Populations: []string{"staff"}, OrganizationScopes: []string{"org-a"}},
		SkillPins:   []agentskills.SkillPin{{ID: "skill.read", Version: 1, Digest: runSourceDigest('e')}},
		TierCeiling: agentskills.TierRead, ConversationKinds: []agentpersona.ConversationKind{agentpersona.ConversationChannel},
		ChannelClasses: []agentpersona.ChannelClass{agentpersona.ChannelPrivate}, Instructions: "Use permitted records.",
		Owner: "owner-a", Steward: "steward-a", EvalSuiteRef: "eval-suite-a",
		EvalLimits: agentpersona.EvaluationLimits{MaxCost: 1, MaxSteps: 1, MaxLatencyMS: 1},
	}
}

func personaRunTestInvocation() agentinvoke.RunRequest {
	return agentinvoke.RunRequest{InvocationID: "invoke-a", TenantID: "tenant-a", ConversationID: "room-a", ThreadID: "thread-a", InvokingPostID: "post-a", InvokerID: "user-a", PersonaID: "persona-a", PersonaVersion: "v2", InstallationID: "install-a", Mode: agentinvoke.OnBehalfOf, Actor: agentinvoke.ActorChain{UserID: "user-a", PersonaID: "persona-a", PersonaVersion: "v2", InstallationID: "install-a", ConversationID: "room-a", InvokingPostID: "post-a", InvocationID: "invoke-a"}}
}

func runSourceDigest(seed byte) string { return "sha256:" + strings.Repeat(string(seed), 64) }
