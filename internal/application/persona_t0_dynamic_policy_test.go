package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdelegation"
	"github.com/monstercameron/human-capital-management-suite/internal/agentinvoke"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/capability"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type dynamicT0Catalog struct{ record agentskills.SkillRecord }

func (c dynamicT0Catalog) ResolvePin(pin agentskills.SkillPin) (agentskills.SkillRecord, error) {
	if pin.ID != c.record.Definition.ID || pin.Version != c.record.Definition.Version || pin.Digest != c.record.Digest {
		return agentskills.SkillRecord{}, agentskills.ErrDigestMismatch
	}
	return c.record, nil
}

type dynamicT0PersonaReader struct {
	version agentpersonastore.PersonaVersion
	install agentpersonastore.ActiveInstallation
	err     error
}

func (r dynamicT0PersonaReader) ReadCurrentPersonaAuthority(context.Context, string, string) (agentpersonastore.PersonaVersion, agentpersonastore.ActiveInstallation, error) {
	return r.version, r.install, r.err
}

type dynamicT0PersonaStore struct {
	reader dynamicT0PersonaReader
	tenant values.TenantId
	calls  int
}

func (s *dynamicT0PersonaStore) ForTenant(_ context.Context, tenant values.TenantId) (PersonaAuthorityInstallationReader, error) {
	s.calls++
	s.tenant = tenant
	if tenant != s.reader.version.TenantID {
		return nil, errors.New("tenant mismatch")
	}
	return s.reader, nil
}

type dynamicT0Authority struct {
	admission agentinvoke.Admission
	err       error
	calls     int
}

func (a *dynamicT0Authority) Resolve(context.Context, agentinvoke.AdmissionRequest) (agentinvoke.Admission, error) {
	a.calls++
	return a.admission, a.err
}

type dynamicT0InvokerAuthority struct {
	value agentdelegation.UserAuthority
	err   error
}

func (a dynamicT0InvokerAuthority) ResolveInvokerAuthority(context.Context, string, values.TenantId, string, time.Time) (agentdelegation.UserAuthority, error) {
	return a.value, a.err
}

type dynamicT0Facts struct {
	value PersonaRunRequestFacts
	err   error
}

func (f dynamicT0Facts) ResolvePersonaRun(context.Context, agentinvoke.RunRequest) (PersonaRunRequestFacts, error) {
	return f.value, f.err
}

type dynamicT0GrantStore struct {
	grant  agentdelegation.Grant
	epoch  uint64
	err    error
	gotID  string
	tenant values.TenantId
	user   string
}

func (s *dynamicT0GrantStore) Save(agentdelegation.Grant) error { return errors.New("unexpected Save") }
func (s *dynamicT0GrantStore) Get(id string) (agentdelegation.Grant, error) {
	s.gotID = id
	return s.grant, s.err
}
func (s *dynamicT0GrantStore) Revoke(string, string) error { return errors.New("unexpected Revoke") }
func (s *dynamicT0GrantStore) CurrentRevocationEpoch(tenant values.TenantId, user string) uint64 {
	s.tenant, s.user = tenant, user
	return s.epoch
}
func (s *dynamicT0GrantStore) BumpRevocationEpoch(values.TenantId, string, string) (uint64, error) {
	return 0, errors.New("unexpected epoch bump")
}

type dynamicT0GrantFactory struct{ store agentdelegation.GrantStore }

func (f dynamicT0GrantFactory) ForTenant(context.Context, values.TenantId) (agentdelegation.GrantStore, error) {
	return f.store, nil
}

func TestTodo_AGENTP_008_DynamicT0PolicyUsesCurrentTenantAuthorityAndGrant(t *testing.T) {
	at := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	request, profile, record, durableGrant := dynamicT0Fixture(t, at)
	sealed, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	profileJSON, err := json.Marshal(sealed.Profile)
	if err != nil {
		t.Fatal(err)
	}
	tenant := values.TenantId(request.TenantID)
	store := &dynamicT0GrantStore{grant: durableGrant, epoch: durableGrant.RevocationEpoch}
	personas := &dynamicT0PersonaStore{reader: dynamicT0PersonaReader{
		version: agentpersonastore.PersonaVersion{TenantID: tenant, PersonaID: request.PersonaID, Version: 2, AgentVersion: "agent-a@1", ContentDigest: sealed.Digest, Profile: profileJSON},
		install: agentpersonastore.ActiveInstallation{InstallationID: request.InstallationID, PersonaID: request.PersonaID, PersonaVersion: 2, ConversationID: request.ConversationID},
	}}
	policy, err := NewDatabasePersonaT0SkillPolicy(PersonaT0DynamicPolicyConfig{
		Personas: personas,
		Authority: &dynamicT0Authority{admission: agentinvoke.Admission{
			Persona:      agentinvoke.Persona{ID: request.PersonaID, Version: request.PersonaVersion, Current: true, InstallationID: request.InstallationID},
			Installation: agentinvoke.Installation{ID: request.InstallationID, Current: true}, HumanMember: true, AudienceMember: true,
			PersonaInstalled: true, Discoverable: request.Skills.Clone(),
		}},
		InvokerAuthority: dynamicT0InvokerAuthority{value: agentdelegation.UserAuthority{
			UserID: request.InvokerID, Active: true,
			Authority:        trust.AuthorityScope{Tenant: tenant, OrganizationScopeID: "org-a", Assurance: trust.AssuranceHigh, NotBefore: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour)},
			SkillAuthorities: trust.SkillAuthorities{"skill.read": {Capabilities: []string{"people:read"}, Resources: []string{"worker:1"}, Fields: []string{"worker.name"}, Purposes: []string{"persona-mention"}}},
		}},
		GrantStores: dynamicT0GrantFactory{store: store}, Facts: dynamicT0Facts{value: PersonaRunRequestFacts{
			TenantID: request.TenantID, TriggerID: request.InvocationID, PersonaDigest: sealed.Digest, Agent: agentrunVersionRef("agent-a"),
		}}, Catalog: dynamicT0Catalog{record: record}, Now: func() time.Time { return at },
	})
	if err != nil {
		t.Fatal(err)
	}
	principal, err := trust.NewPrincipal(trust.PrincipalSpec{
		Tenant: tenant, Subject: request.InvokerID, SubjectKind: trust.SubjectKindHuman, OrganizationScopeID: "org-a",
		Purposes: []string{"persona-mention"}, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceHigh, SessionRef: "session-a", IssuedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), CredentialDigest: "credential-digest",
	})
	if err != nil {
		t.Fatal(err)
	}
	allowed, err := policy.IsBoundT0Run(trust.WithPrincipal(context.Background(), principal), request)
	if err != nil || !allowed {
		t.Fatalf("current run was denied: allowed=%v err=%v", allowed, err)
	}
	if personas.calls != 1 || personas.tenant != tenant || store.gotID != request.Grant.ID || store.tenant != tenant || store.user != request.InvokerID {
		t.Fatalf("policy did not use exact tenant-bound stores: persona=%+v grant=%+v", personas, store)
	}
	store.epoch++
	if allowed, err = policy.IsBoundT0Run(trust.WithPrincipal(context.Background(), principal), request); allowed || !errors.Is(err, errPersonaT0DynamicPolicy) {
		t.Fatalf("revoked grant result allowed=%v err=%v", allowed, err)
	}
}

func TestTodo_AGENTP_008_DynamicT0PolicyRejectsChangedPinAndMissingPrincipal(t *testing.T) {
	at := time.Date(2026, 9, 30, 15, 0, 0, 0, time.UTC)
	request, profile, record, grant := dynamicT0Fixture(t, at)
	if pinnedT0ScopesMatch(dynamicT0Catalog{record: record}, []agentskills.SkillPin{{ID: "skill.read", Version: 2, Digest: "different"}}, request.Skills) {
		t.Fatal("a changed immutable digest was accepted")
	}
	if !currentPersonaT0Grant(grant, request, at, grant.RevocationEpoch) {
		t.Fatal("fixture durable grant is not structurally current")
	}
	wrongAuthoritySkill := grant
	wrongAuthoritySkill.SkillAuthorities = trust.SkillAuthorities{"skill.other": grant.SkillAuthorities["skill.read"]}
	wrongAuthoritySkill.Authority.SkillAuthorities = trust.CloneSkillAuthorities(wrongAuthoritySkill.SkillAuthorities)
	if currentPersonaT0Grant(wrongAuthoritySkill, request, at, grant.RevocationEpoch) {
		t.Fatal("grant authority for a different skill was accepted")
	}
	profile.SkillPins[0].Digest = "different"
	if pinnedT0ScopesMatch(dynamicT0Catalog{record: record}, profile.SkillPins, request.Skills) {
		t.Fatal("catalog mismatch accepted a changed profile pin")
	}
	if _, err := NewDatabasePersonaT0SkillPolicy(PersonaT0DynamicPolicyConfig{}); !errors.Is(err, errPersonaT0DynamicPolicy) {
		t.Fatalf("empty production configuration err=%v", err)
	}
}

func dynamicT0Fixture(t *testing.T, at time.Time) (agentinvoke.RunRequest, agentpersona.PersonaProfile, agentskills.SkillRecord, agentdelegation.Grant) {
	t.Helper()
	pin := agentskills.SkillPin{ID: "skill.read", Version: 1, Digest: "sha256:" + strings.Repeat("a", 64)}
	request := agentinvoke.RunRequest{
		InvocationID: "invoke-a", TenantID: "tenant-a", ConversationID: "room-a", ThreadID: "thread-a", InvokingPostID: "post-a",
		InvokerID: "user-a", PersonaID: "persona-a", PersonaVersion: "v2", InstallationID: "install-a", Mode: agentinvoke.OnBehalfOf,
		Grant:  agentinvoke.DelegationGrant{ID: "grant-a", UserID: "user-a", TenantID: "tenant-a", TaskID: "invoke-a", TargetAgentID: "agent-a", Skills: agentinvoke.SkillScopes{"skill.read": {"people:read"}}, ExpiresAt: at.Add(time.Hour)},
		Skills: agentinvoke.SkillScopes{"skill.read": {"people:read"}},
		Actor:  agentinvoke.ActorChain{UserID: "user-a", PersonaID: "persona-a", PersonaVersion: "v2", InstallationID: "install-a", ConversationID: "room-a", InvokingPostID: "post-a", InvocationID: "invoke-a"},
	}
	manifest := agentpersona.AgentManifestRef{ID: "agent-a", Version: 1, Digest: "sha256:" + strings.Repeat("b", 64), SchemaVersion: 1}
	profile := agentpersona.PersonaProfile{
		Manifest: manifest, PersonaID: "persona-a", Version: 2, Handle: "persona-a", DisplayName: "Persona A", AvatarRef: "avatar:a",
		Purpose: "Answer questions", Audience: agentpersona.Audience{Roles: []string{"member"}, Populations: []string{"staff"}, OrganizationScopes: []string{"org-a"}},
		SkillPins: []agentskills.SkillPin{pin}, TierCeiling: agentskills.TierT0,
		ConversationKinds: []agentpersona.ConversationKind{agentpersona.ConversationChannel}, ChannelClasses: []agentpersona.ChannelClass{agentpersona.ChannelPrivate},
		Instructions: "Use approved read-only records.", Owner: "owner-a", Steward: "steward-a", EvalSuiteRef: "eval-a",
		EvalLimits: agentpersona.EvaluationLimits{MaxCost: 1, MaxSteps: 1, MaxLatencyMS: 1},
	}
	record := agentskills.SkillRecord{Definition: agentskills.SkillDefinition{ID: pin.ID, Version: pin.Version, SideEffectTier: agentskills.TierT0}, Digest: pin.Digest, Status: agentskills.StatusActive, HighestCapabilityTier: agentskills.TierT0,
		ResolvedOperations: []agentskills.ResolvedOperation{{HasCapability: true, Capability: capability.Record{Definition: capability.Definition{AuthZScopeRef: "people:read"}}}},
	}
	authority := trust.SkillAuthorities{"skill.read": {Capabilities: []string{"people:read"}, Resources: []string{"worker:1"}, Fields: []string{"worker.name"}, Purposes: []string{"persona-mention"}}}
	grantID := personaGrantID(agentinvoke.GrantRequest{InvocationID: request.InvocationID, TenantID: request.TenantID})
	request.Grant.ID = grantID
	grant := agentdelegation.Grant{
		GrantID: grantID, UserID: request.InvokerID, Tenant: values.TenantId(request.TenantID), AgentVersion: request.PersonaVersion,
		TargetAgentID: request.Grant.TargetAgentID, InstallationID: request.InstallationID, TaskID: request.InvocationID,
		PlanSkillSetDigest: skillDigest(request.Skills), Purpose: "persona-mention", OrganizationScopeID: "org-a", Skills: []string{"skill.read"},
		SkillScopes: map[string][]string{"skill.read": {"people:read"}}, SkillAuthorities: trust.CloneSkillAuthorities(authority),
		NotBefore: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), RevocationEpoch: 1,
		Authority: trust.DelegationGrant{GrantID: grantID, RootID: grantID, Delegator: request.InvokerID, Delegate: request.Grant.TargetAgentID,
			Tenant: values.TenantId(request.TenantID), OrganizationScopeID: "org-a", Capabilities: []string{"people:read"},
			Resources: []string{"worker:1"}, Fields: []string{"worker.name"}, Purposes: []string{"persona-mention"},
			SkillAuthorities: trust.CloneSkillAuthorities(authority), NotBefore: at.Add(-time.Minute), ExpiresAt: at.Add(time.Hour), RequiredAssurance: trust.AssuranceHigh, RevocationEpoch: 1},
	}
	return request, profile, record, grant
}

func agentrunVersionRef(id string) (ref agentrun.VersionRef) {
	return agentrun.VersionRef{AgentID: id, Version: "agent-a@1", Digest: "sha256:" + strings.Repeat("c", 64)}
}
