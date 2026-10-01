package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentgate"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/runstate"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type privateChatBuilderVersionReader struct {
	version agentpersonastore.PersonaVersion
	err     error
}

func (r privateChatBuilderVersionReader) GetVersion(context.Context, string, int64) (agentpersonastore.PersonaVersion, error) {
	return r.version, r.err
}

type privateChatBuilderVersionFactory struct {
	reader personaPrivateChatVersionReader
	tenant values.TenantId
	err    error
}

func (f privateChatBuilderVersionFactory) ForTenant(_ context.Context, tenant values.TenantId) (personaPrivateChatVersionReader, error) {
	if tenant != f.tenant {
		return nil, errors.New("wrong tenant")
	}
	return f.reader, f.err
}

type privateChatBuilderSkills struct {
	records map[agentskills.SkillKey]agentskills.SkillRecord
}

func (s privateChatBuilderSkills) ResolvePin(pin agentskills.SkillPin) (agentskills.SkillRecord, error) {
	record, ok := s.records[pin.Key()]
	if !ok || record.Digest != pin.Digest {
		return agentskills.SkillRecord{}, agentskills.ErrDigestMismatch
	}
	return record, nil
}

type privateChatBuilderUserResolver struct {
	user agentgate.UserContext
	err  error
}

func (r privateChatBuilderUserResolver) Resolve(context.Context, *trust.Principal, string) (agentgate.UserContext, error) {
	return r.user, r.err
}

func TestTodo_AGENTP_008_PrivateChatScopeBuilderUsesDurablePinAndVerifiedCurrentUser(t *testing.T) {
	at := time.Date(2026, 9, 30, 16, 0, 0, 0, time.UTC)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "alice", SubjectKind: trust.SubjectKindHuman,
		Roles: []string{"member"}, Purposes: []string{"persona-mention"}, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceHigh, SessionRef: "session-a", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "cred:alice"})
	if err != nil {
		t.Fatal(err)
	}
	pin := agentskills.SkillPin{ID: "persona.chat_reply", Version: 1, Digest: runSourceDigest('f')}
	profile := personaRunTestProfile(personaRunTestManifest(), runSourceDigest('a'))
	profile.SkillPins = []agentskills.SkillPin{pin}
	sealed, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	profileJSON, err := json.Marshal(sealed.Profile)
	if err != nil {
		t.Fatal(err)
	}
	version := agentpersonastore.PersonaVersion{TenantID: "tenant-a", PersonaID: "persona-a", Version: 2, ContentDigest: sealed.Digest, Profile: profileJSON}
	key := capability.Key{ID: agentgate.PrivateChatReplyCapability, Version: 1}
	capRecord := capability.Record{Definition: capability.Definition{ID: key.ID, Version: key.Version, EffectClass: capability.EffectPure, AgentEligible: true, AuthZScopeRef: agentgate.PrivateChatReplyScope}, Status: capability.StatusActive}
	skill := agentskills.SkillRecord{Definition: agentskills.SkillDefinition{ID: pin.ID, Version: pin.Version, SideEffectTier: agentskills.TierT0, RequiredPurposes: []string{"persona-mention"}}, Digest: pin.Digest, Status: agentskills.StatusActive,
		ResolvedOperations: []agentskills.ResolvedOperation{{Capability: capRecord, HasCapability: true}}}
	users := privateChatBuilderUserResolver{user: agentgate.UserContext{Principal: principal, Population: "members", Roles: []string{"member"}, OrganizationScopes: []string{"org-a"}}}
	builder, err := NewPersonaPrivateChatScopeRequestBuilder(privateChatBuilderVersionFactory{reader: privateChatBuilderVersionReader{version: version}, tenant: "tenant-a"},
		privateChatBuilderSkills{records: map[agentskills.SkillKey]agentskills.SkillRecord{pin.Key(): skill}}, users, func() time.Time { return at })
	if err != nil {
		t.Fatal(err)
	}
	record, run := privateChatBuilderRun(version.ContentDigest)
	ctx := trust.WithPrincipal(context.Background(), principal)
	got, err := builder.BuildPrivateChatScopeRequest(ctx, record, run)
	if err != nil {
		t.Fatalf("build scope request: %v", err)
	}
	if got.User.Principal != principal || got.User.Population != "members" || got.Skill != pin || got.Purpose != "persona-mention" || got.Tenant != "tenant-a" ||
		got.ConversationID != "room-a" || got.ThreadID != "thread-a" || got.InvokingPostID != "post-a" || !got.At.Equal(at) {
		t.Fatalf("scope request did not preserve durable bindings and current facts: %+v", got)
	}
}

func TestTodo_AGENTP_008_PrivateChatScopeBuilderRejectsAbsentOrMismatchedAuthority(t *testing.T) {
	at := time.Date(2026, 9, 30, 16, 0, 0, 0, time.UTC)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "alice", SubjectKind: trust.SubjectKindHuman,
		Roles: []string{"member"}, Purposes: []string{"persona-mention"}, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceHigh, SessionRef: "session-a", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "cred:alice"})
	if err != nil {
		t.Fatal(err)
	}
	profile := personaRunTestProfile(personaRunTestManifest(), runSourceDigest('a'))
	profile.SkillPins = []agentskills.SkillPin{{ID: "skill.people.read", Version: 1, Digest: runSourceDigest('d')}}
	sealed, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	profileJSON, _ := json.Marshal(sealed.Profile)
	version := agentpersonastore.PersonaVersion{TenantID: "tenant-a", PersonaID: "persona-a", Version: 2, ContentDigest: sealed.Digest, Profile: profileJSON}
	builder, err := NewPersonaPrivateChatScopeRequestBuilder(privateChatBuilderVersionFactory{reader: privateChatBuilderVersionReader{version: version}, tenant: "tenant-a"},
		privateChatBuilderSkills{records: map[agentskills.SkillKey]agentskills.SkillRecord{{ID: "skill.people.read", Version: 1}: {Definition: agentskills.SkillDefinition{ID: "skill.people.read", Version: 1}, Digest: runSourceDigest('d'), Status: agentskills.StatusActive}}},
		privateChatBuilderUserResolver{user: agentgate.UserContext{Principal: principal, Population: "members", Roles: []string{"member"}, OrganizationScopes: []string{"org-a"}}}, func() time.Time { return at })
	if err != nil {
		t.Fatal(err)
	}
	record, run := privateChatBuilderRun(version.ContentDigest)
	for _, tc := range []struct {
		name   string
		ctx    context.Context
		change func(*agentrun.Record)
	}{
		{name: "missing verified context", ctx: context.Background(), change: func(*agentrun.Record) {}},
		{name: "admission tuple changed", ctx: trust.WithPrincipal(context.Background(), principal), change: func(r *agentrun.Record) { r.Request.Context.ID = "other-thread" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := record
			tc.change(&candidate)
			if _, err := builder.BuildPrivateChatScopeRequest(tc.ctx, candidate, run); !errors.Is(err, errPersonaPrivateChatScopeBuilder) {
				t.Fatalf("invalid request error = %v", err)
			}
		})
	}
	if _, err := builder.BuildPrivateChatScopeRequest(trust.WithPrincipal(context.Background(), principal), record, run); !errors.Is(err, errPersonaPrivateChatScopeBuilder) || !strings.Contains(err.Error(), "private persona chat request") {
		t.Fatalf("missing pinned reply skill error = %v", err)
	}
}

func TestTodo_AGENTP_008_PrivateChatScopeBuilderRequiresCurrentDirectoryFacts(t *testing.T) {
	at := time.Date(2026, 9, 30, 16, 0, 0, 0, time.UTC)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "alice", SubjectKind: trust.SubjectKindHuman,
		Purposes: []string{"persona-mention"}, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceHigh, SessionRef: "session-a", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "cred:alice"})
	if err != nil {
		t.Fatal(err)
	}
	pin := agentskills.SkillPin{ID: "persona.chat_reply", Version: 1, Digest: runSourceDigest('f')}
	profile := personaRunTestProfile(personaRunTestManifest(), runSourceDigest('a'))
	profile.SkillPins = []agentskills.SkillPin{pin}
	sealed, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	profileJSON, _ := json.Marshal(sealed.Profile)
	version := agentpersonastore.PersonaVersion{TenantID: "tenant-a", PersonaID: "persona-a", Version: 2, ContentDigest: sealed.Digest, Profile: profileJSON}
	key := capability.Key{ID: agentgate.PrivateChatReplyCapability, Version: 1}
	skill := agentskills.SkillRecord{Definition: agentskills.SkillDefinition{ID: pin.ID, Version: pin.Version, SideEffectTier: agentskills.TierT0}, Digest: pin.Digest, Status: agentskills.StatusActive,
		ResolvedOperations: []agentskills.ResolvedOperation{{Capability: capability.Record{Definition: capability.Definition{ID: key.ID, Version: key.Version, EffectClass: capability.EffectPure, AgentEligible: true, AuthZScopeRef: agentgate.PrivateChatReplyScope}, Status: capability.StatusActive}, HasCapability: true}}}
	builder, err := NewPersonaPrivateChatScopeRequestBuilder(privateChatBuilderVersionFactory{reader: privateChatBuilderVersionReader{version: version}, tenant: "tenant-a"},
		privateChatBuilderSkills{records: map[agentskills.SkillKey]agentskills.SkillRecord{pin.Key(): skill}}, privateChatBuilderUserResolver{user: agentgate.UserContext{Principal: principal}}, func() time.Time { return at })
	if err != nil {
		t.Fatal(err)
	}
	record, run := privateChatBuilderRun(version.ContentDigest)
	if _, err := builder.BuildPrivateChatScopeRequest(trust.WithPrincipal(context.Background(), principal), record, run); !errors.Is(err, errPersonaPrivateChatScopeBuilder) {
		t.Fatalf("missing directory roles, population and organization scope error = %v", err)
	}
}

func privateChatBuilderRun(personaDigest string) (agentrun.Record, runstate.Run) {
	request := agentrun.Request{Source: agentrun.SourceIdentity{TenantID: "tenant-a", Kind: agentrun.SourcePersonaMention, Key: "invocation-a", Ref: "post-a"},
		Persona: &agentrun.PersonaRef{ID: "persona-a", Version: "v2", Digest: personaDigest}, Agent: agentrun.VersionRef{AgentID: "agent-a", Version: "4", Digest: runSourceDigest('b')},
		Principal: agentrun.PrincipalChain{Mode: agentrun.ModeOnBehalfOf, InvokerID: "alice"}, Purpose: "persona-mention",
		Audience: agentrun.AudienceScope{ID: "room-a"}, Context: agentrun.ContextScope{ID: "thread-a"}}
	id, _ := agentrun.AdmissionRequestID(request.Source)
	digest, _ := agentrun.AdmissionRequestDigest(request)
	record := agentrun.Record{ID: id, Request: request, RequestDigest: digest, Decision: agentrun.DecisionAccepted}
	run := runstate.Run{ID: record.ID, TenantID: "tenant-a", AdmissionID: record.ID, ActorID: "alice", RequestDigest: record.RequestDigest,
		AgentID: "agent-a", AgentVersion: "4", AgentDigest: runSourceDigest('b')}
	return record, run
}
