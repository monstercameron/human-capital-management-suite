package application

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agenteval"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentmanifest"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentdelegationstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgtest"
	"github.com/monstercameron/human-capital-management-suite/internal/data/tenancy"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type candidateGrantVersionReader struct {
	row agentpersonastore.PersonaVersion
}

func (r candidateGrantVersionReader) GetVersion(context.Context, values.TenantId, string, int64) (agentpersonastore.PersonaVersion, error) {
	return r.row, nil
}

type candidateGrantManifestReader struct {
	tenant   string
	manifest agentmanifest.Manifest
}

func (r candidateGrantManifestReader) TenantID() string { return r.tenant }
func (r candidateGrantManifestReader) ResolveAgentManifestContext(context.Context, agentpersona.AgentManifestRef) (agentmanifest.Manifest, error) {
	return r.manifest, nil
}

type candidateGrantConversationReader struct{ chat.ConversationService }

func (candidateGrantConversationReader) GetConversation(_ context.Context, r chat.GetConversationRequest) (chat.Conversation, error) {
	return chat.Conversation{TenantID: r.TenantID, ID: r.ConversationID, Kind: chat.PrivateChannel}, nil
}

type candidateGrantInvokerReader struct{ authority agentdelegation.UserAuthority }

func (r candidateGrantInvokerReader) ResolvePersonaChatInvokerAuthority(context.Context, PersonaChatAuthorityRequest) (agentdelegation.UserAuthority, error) {
	return r.authority, nil
}

func (r candidateGrantInvokerReader) ResolveInvokerAuthority(context.Context, string, values.TenantId, string, time.Time) (agentdelegation.UserAuthority, error) {
	return r.authority, nil
}

type candidateGrantAdmissionReader struct {
	invoker   TrustedInvoker
	admission agentinvoke.Admission
}

func (r candidateGrantAdmissionReader) ResolveTrustedInvoker(context.Context, string, string) (TrustedInvoker, error) {
	return r.invoker, nil
}

func (r candidateGrantAdmissionReader) ResolvePersonaAuthority(context.Context, agentinvoke.AdmissionRequest, TrustedInvoker) (agentinvoke.Admission, error) {
	return r.admission, nil
}

func TestTodo_AGENTP_021_Integration_CandidateGrantRereadsPostgresExpiry(t *testing.T) {
	db := pgtest.New(t)
	const synthetic = values.TenantId("candidate-synthetic")
	tenantID := uuid.New()
	db.Exec(t, `INSERT INTO tenant(tenant_id,tenant_key,cell_id,display_name,status,effective_from) VALUES($1,$2,'cell-local','Candidate synthetic','ACTIVE','2026-01-01')`, tenantID, synthetic.String())
	conn := db.NewConn(t)
	ctx := context.Background()
	if _, err := conn.Exec(ctx, "SET ROLE "+tenancy.AppRole); err != nil {
		t.Fatal(err)
	}
	grants, err := agentdelegationstore.New(conn, func(tenant values.TenantId) uuid.UUID {
		if tenant == synthetic {
			return tenantID
		}
		return uuid.Nil
	})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 30, 12, 0, 0, 123456789, time.UTC)
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: synthetic, Subject: "alice", SubjectKind: trust.SubjectKindHuman,
		Purposes: []string{"persona-mention"}, OrganizationScopeID: "org-a", AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceSubstantial, SessionRef: "candidate-session", CredentialDigest: "candidate-credential",
		IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	ctx = trust.WithPrincipal(ctx, principal)
	caps := capability.NewRegistry()
	registry := agentskills.NewRegistry(caps)
	pin, err := bindPersonaChatReplySkill(caps, registry)
	if err != nil {
		t.Fatal(err)
	}
	manifest := personaRunTestManifest()
	manifestDigest, err := manifest.Digest()
	if err != nil {
		t.Fatal(err)
	}
	profile := personaRunTestProfile(manifest, manifestDigest)
	profile.SkillPins = []agentskills.SkillPin{pin}
	sealed, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(sealed.Profile)
	if err != nil {
		t.Fatal(err)
	}
	target := agenteval.PersonaEvaluationTarget{TenantID: "candidate-production", SyntheticTenantID: synthetic.String(), InvokerID: principal.Subject(),
		PersonaID: profile.PersonaID, PersonaVersion: int64(profile.Version), ProfileDigest: sealed.Digest}
	definitions := &PersonaCandidateDefinitionSource{Target: target, Scope: &liveEvaluationTestScope{},
		Versions: candidateGrantVersionReader{row: agentpersonastore.PersonaVersion{TenantID: values.TenantId(target.TenantID), PersonaID: profile.PersonaID, Version: int64(profile.Version), Profile: encoded, ContentDigest: sealed.Digest}},
		Manifests: TenantPersonaRunManifestResolver(func(_ context.Context, tenant string) (personaRunManifestResolver, error) {
			return candidateGrantManifestReader{tenant, manifest}, nil
		})}
	resource := personaChatAuthorityResource(synthetic.String(), "room", "thread", "post")
	scopes := trust.SkillAuthorities{pin.ID: {Capabilities: []string{"chat.current"}, Resources: []string{resource}, Purposes: []string{"persona-mention"}}}
	current := agentdelegation.UserAuthority{UserID: principal.Subject(), Active: true, SkillAuthorities: scopes,
		Authority: authorityScopeFromSkills(principal, "org-a", scopes, now)}
	owners := &PersonaCandidateCurrentOwners{Definitions: definitions, OwnerFacts: &PersonaRunOwnerFactsComposer{},
		Invokers: candidateGrantInvokerReader{current}, Grants: grants, Catalog: registry, Chat: candidateGrantConversationReader{},
		Placements: map[string]string{"room": "candidate-install"}, Now: func() time.Time { return now }}
	request := agentinvoke.GrantRequest{InvocationID: "candidate-invocation", UserID: principal.Subject(), TenantID: synthetic.String(),
		AgentVersion: "2", TargetAgentID: manifest.ID, PersonaID: profile.PersonaID, PersonaVersion: "2", InstallationID: "candidate-install",
		ConversationID: "room", ThreadID: "thread", InvokingPostID: "post", Purpose: "persona-mention", Mode: agentinvoke.OnBehalfOf,
		Skills: agentinvoke.SkillScopes{pin.ID: {"chat.current"}}, ExpiresAt: now.Add(15*time.Minute + 123456789*time.Nanosecond)}
	// The fixture's immutable persona version is used rather than a caller label.
	request.PersonaVersion, request.AgentVersion = strconv.FormatInt(target.PersonaVersion, 10), strconv.FormatInt(target.PersonaVersion, 10)
	grant, err := owners.CreateOnBehalfOfGrant(ctx, request)
	if err != nil {
		t.Fatalf("sub-microsecond expiry failed its durable reread: %v", err)
	}
	store, err := grants.ForTenant(ctx, synthetic)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := store.Get(grant.ID)
	if err != nil || !grant.ExpiresAt.Equal(stored.ExpiresAt) || !grant.ExpiresAt.Before(request.ExpiresAt) || request.ExpiresAt.Sub(grant.ExpiresAt) >= time.Microsecond {
		t.Fatalf("grant lost its exact durable expiry: returned=%s stored=%s requested=%s err=%v", grant.ExpiresAt, stored.ExpiresAt, request.ExpiresAt, err)
	}
	if !stored.NotBefore.Equal(stored.Authority.NotBefore) || stored.NotBefore.Before(now) || stored.NotBefore.Sub(now) >= time.Microsecond || !stored.ExpiresAt.Equal(stored.Authority.ExpiresAt) {
		t.Fatalf("durable authority window differs from the grant: grant=%s..%s authority=%s..%s", stored.NotBefore, stored.ExpiresAt, stored.Authority.NotBefore, stored.Authority.ExpiresAt)
	}
	if stored.TargetAgentID != manifest.ID || len(stored.SkillAuthorities[pin.ID].Fields) != 0 || stored.SkillAuthorities[pin.ID].Resources[0] != resource {
		t.Fatalf("expiry normalization changed the grant binding: %#v", stored)
	}
	checkEnvelope := func(t *testing.T, request agentinvoke.GrantRequest, grant agentdelegation.Grant) {
		t.Helper()
		admission := agentrun.Request{Source: agentrun.SourceIdentity{TenantID: synthetic.String(), Key: request.InvocationID},
			Principal: agentrun.PrincipalChain{InvokerID: principal.Subject(), DelegatedCredentialRef: grant.GrantID},
			Persona:   &agentrun.PersonaRef{Version: request.PersonaVersion}, Agent: agentrun.VersionRef{AgentID: manifest.ID},
			InstallationID: request.InstallationID, Purpose: request.Purpose}
		if !personaForegroundGrantEnvelope(grant, admission, now.Add(time.Microsecond), store.CurrentRevocationEpoch(synthetic, principal.Subject())) {
			t.Fatal("persisted grant cannot enter foreground admission")
		}
	}
	checkEnvelope(t, request, stored)
	t.Run("ordinary mention and exact retry", func(t *testing.T) {
		service, err := agentdelegation.NewService(agentdelegation.Config{Store: store, Authority: agentdelegation.ResolverFunc(unusedPersonaGrantResolver), Now: func() time.Time { return now }})
		if err != nil {
			t.Fatal(err)
		}
		skills := request.Skills.Clone()
		facts := candidateGrantAdmissionReader{invoker: TrustedInvoker{ID: principal.Subject(), TenantID: synthetic.String(), HumanMember: true, AudienceMember: true, Discoverable: skills},
			admission: agentinvoke.Admission{Persona: agentinvoke.Persona{ID: profile.PersonaID, Version: request.PersonaVersion, Current: true, PinnedSkills: skills},
				Installation: agentinvoke.Installation{ID: request.InstallationID, Current: true, SkillCeiling: skills}, Channel: agentinvoke.ChannelPolicy{SkillCeiling: skills},
				Discoverable: skills, HumanMember: true, AudienceMember: true, PersonaInstalled: true}}
		issuer := &PersonaAuthorityAdapter{Personas: facts, Identity: facts, Invokers: candidateGrantInvokerReader{current}, Grants: service, Store: store, Now: func() time.Time { return now }}
		request.InvocationID = "ordinary-invocation"
		issued, err := issuer.CreateOnBehalfOfGrant(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		saved, err := store.Get(issued.ID)
		if err != nil || !issued.ExpiresAt.Equal(saved.ExpiresAt) || saved.ExpiresAt.After(request.ExpiresAt) || saved.NotBefore.Before(now) {
			t.Fatalf("ordinary grant changed at persistence: %v", err)
		}
		checkEnvelope(t, request, saved)
		retried, err := issuer.CreateOnBehalfOfGrant(ctx, request)
		if err != nil || retried.ID != issued.ID || !retried.ExpiresAt.Equal(issued.ExpiresAt) {
			t.Fatalf("exact retry did not reuse durable grant: %v", err)
		}
		request.ExpiresAt = request.ExpiresAt.Add(-time.Second)
		if _, err := issuer.CreateOnBehalfOfGrant(ctx, request); err == nil {
			t.Fatal("changed expiry reused a durable grant")
		}
	})
}
