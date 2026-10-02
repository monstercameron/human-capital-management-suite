package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type catalogVersions []PersonaCatalogVersion

func (r catalogVersions) ListPersonaCatalogVersions(context.Context, values.TenantId) ([]PersonaCatalogVersion, error) {
	return r, nil
}

type catalogInstalls []PersonaCatalogInstallation

func (r catalogInstalls) ListPersonaCatalogInstallations(context.Context, values.TenantId) ([]PersonaCatalogInstallation, error) {
	return r, nil
}

type catalogTargets struct {
	users, conversations []productui.PersonaAdminTarget
}

func (r catalogTargets) ListPersonaCatalogTargets(context.Context, trust.Principal, values.TenantId) ([]productui.PersonaAdminTarget, []productui.PersonaAdminTarget, error) {
	return r.users, r.conversations, nil
}

type catalogGrants struct {
	allowed bool
	calls   int
}

func (r *catalogGrants) ResolvePersonaSkillGrant(context.Context, trust.Principal, values.TenantId, string, string, agentskills.SkillPin) (PersonaCatalogGrant, error) {
	r.calls++
	if !r.allowed {
		return PersonaCatalogGrant{Reason: "denied"}, nil
	}
	return PersonaCatalogGrant{Allowed: true, Tier: agentskills.TierRead.String(), DataClasses: []string{"WORKFORCE"}}, nil
}

type catalogSkills struct{}

func (catalogSkills) ResolvePin(pin agentskills.SkillPin) (agentskills.SkillRecord, error) {
	if pin.ID != "skill.read" || pin.Version != 1 || pin.Digest != "skill-digest" {
		return agentskills.SkillRecord{}, errors.New("unknown pin")
	}
	return agentskills.SkillRecord{Definition: agentskills.SkillDefinition{ID: pin.ID, Version: pin.Version, Description: "Reads approved workforce records.", SideEffectTier: agentskills.TierRead, DataClassesRead: []string{"WORKFORCE"}}, Digest: pin.Digest, Status: agentskills.StatusActive}, nil
}

type catalogAuth struct {
	err   error
	calls int
}

type catalogStarterSource struct {
	catalog productui.PersonaAdminStarterCatalog
	err     error
	calls   int
}

func (s *catalogStarterSource) PersonaAdminStarterCatalog(context.Context, productui.PersonaAdminSnapshotRequest) (productui.PersonaAdminStarterCatalog, error) {
	s.calls++
	return s.catalog, s.err
}

func (a *catalogAuth) AuthorizePersonaCatalog(context.Context, *trust.Principal, values.TenantId) error {
	a.calls++
	return a.err
}

func TestTodo_AGENTP_018(t *testing.T) {
	ctx, principal := catalogContext(t)
	starter, ok := agenttemplate.PersonaStarterFor("hcmnext.persona_template.policy_helper", 1)
	if !ok {
		t.Fatal("policy helper starter is missing")
	}
	pv, err := agentpersona.Seal(agentpersona.PersonaProfile{Manifest: agentpersona.AgentManifestRef{ID: "agent", Version: 1, Digest: "manifest", SchemaVersion: 1}, PersonaID: "persona-a", Version: 1, Handle: "persona", DisplayName: "Persona", AvatarRef: "avatar", Purpose: "Help safely", Audience: agentpersona.Audience{Roles: []string{"member"}, Populations: []string{"staff"}, OrganizationScopes: []string{"org-a"}}, SkillPins: []agentskills.SkillPin{{ID: "skill.read", Version: 1, Digest: "skill-digest"}}, TierCeiling: agentskills.TierRead, ConversationKinds: []agentpersona.ConversationKind{agentpersona.ConversationDirect}, ChannelClasses: []agentpersona.ChannelClass{agentpersona.ChannelPrivate, agentpersona.ChannelPublic}, Instructions: "Answer only with approved records.", Owner: "owner", Steward: "steward", EvalSuiteRef: starter.EvaluationSuite, EvalLimits: agentpersona.EvaluationLimits{MaxCost: 1, MaxSteps: 1, MaxLatencyMS: 1}})
	if err != nil {
		t.Fatal(err)
	}
	grants := &catalogGrants{allowed: true}
	auth := &catalogAuth{}
	install := PersonaCatalogInstallation{ID: "install", PersonaID: "persona-a", PersonaVersion: 1, ConversationID: "conv-a", Conversation: "Team", Kind: "CHANNEL", Audience: "staff", ReplyPlacement: "thread", Active: true, ChannelClass: string(agentpersona.ChannelPrivate), ConversationKind: string(agentpersona.ConversationDirect), MaxTier: agentskills.TierRead, AllowedDataClasses: []string{"WORKFORCE"}}
	svc := &PersonaAdminCatalogService{Versions: catalogVersions{{Profile: pv, Lifecycle: agentpersona.StatePublished, Owner: "owner", Steward: "steward"}}, Installations: catalogInstalls{install}, Targets: catalogTargets{users: []productui.PersonaAdminTarget{{ID: "user-a", Label: "User A"}, {ID: "owner", Label: "Olivia Owner"}, {ID: "steward", Label: "Sam Steward"}}, conversations: []productui.PersonaAdminTarget{{ID: "conv-a", Label: "Team"}}}, Skills: catalogSkills{}, Grants: grants, Authorizer: auth}
	snapshot, err := svc.Snapshot(ctx, productui.PersonaAdminSnapshotRequest{TenantID: "tenant-a", Principal: "user-a"})
	if err != nil {
		t.Fatal(err)
	}
	if !snapshot.Available || len(snapshot.Personas) != 1 || snapshot.Personas[0].Lifecycle != productui.PersonaPublished || len(snapshot.Personas[0].Installations) != 1 || len(snapshot.Personas[0].Skills) != 1 {
		t.Fatalf("incomplete catalog projection: %#v", snapshot)
	}
	persona := snapshot.Personas[0]
	if persona.StarterID != starter.ID || persona.StarterVersion != starter.Version || len(persona.ChannelClasses) != 2 || persona.ChannelClasses[0] != string(agentpersona.ChannelPrivate) || persona.ChannelClasses[1] != string(agentpersona.ChannelPublic) {
		t.Fatalf("starter/channel metadata projection = %+v", persona)
	}
	if persona.Instructions != "Answer only with approved records." || persona.OwnerName != "Olivia Owner" || persona.StewardName != "Sam Steward" || len(persona.AudienceRoles) != 1 || persona.AudienceRoles[0].ID != "member" || len(persona.Organizations) != 1 || persona.Organizations[0].ID != "org-a" || persona.Skills[0].Description != "Reads approved workforce records." {
		t.Fatalf("readable persona projection = %+v", persona)
	}
	if len(snapshot.Preview.EffectiveSkills) != 1 || snapshot.Preview.DerivedData[0] != "WORKFORCE" {
		t.Fatalf("preview did not derive authorized reach: %#v", snapshot.Preview)
	}
	readyStarter := &catalogStarterSource{catalog: productui.PersonaAdminStarterCatalog{Available: true, Starters: []productui.PersonaAdminStarter{{ID: starter.ID, Version: starter.Version, ManifestID: "agent.starter.policy_helper", SkillGrantIDs: []string{"grant-1"}, ChannelClasses: []string{"PRIVATE"}}}}}
	svc.Starters = readyStarter
	withStarters, err := svc.Snapshot(ctx, productui.PersonaAdminSnapshotRequest{TenantID: "tenant-a", Principal: "user-a"})
	if err != nil || !withStarters.StarterCatalogAvailable || len(withStarters.Starters) != 1 || readyStarter.calls != 1 {
		t.Fatalf("ready starter projection = %+v, calls = %d, err = %v", withStarters, readyStarter.calls, err)
	}
	readyStarter.err = errors.New("starter authority unavailable")
	withoutStarters, err := svc.Snapshot(ctx, productui.PersonaAdminSnapshotRequest{TenantID: "tenant-a", Principal: "user-a"})
	if err != nil || withoutStarters.StarterCatalogAvailable || len(withoutStarters.Starters) != 0 || len(withoutStarters.Personas) != 1 {
		t.Fatalf("starter failure should leave authorized catalog readable without ready starters: %+v, err = %v", withoutStarters, err)
	}
	svc.Starters = nil
	if _, err := svc.Preview(ctx, productui.PersonaAdminPreviewRequest{PersonaID: "persona-a", SubjectID: "other-user", ConversationID: "conv-a"}); !errors.Is(err, ErrPersonaCatalogDenied) {
		t.Fatalf("unauthorized target error = %v", err)
	}
	for _, tc := range []struct {
		name   string
		mutate func(*PersonaCatalogInstallation)
	}{
		{name: "absent", mutate: func(i *PersonaCatalogInstallation) { i.ConversationID = "elsewhere" }},
		{name: "inactive", mutate: func(i *PersonaCatalogInstallation) { i.Active = false }},
		{name: "wrong version", mutate: func(i *PersonaCatalogInstallation) { i.PersonaVersion = 2 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bad := install
			tc.mutate(&bad)
			svc.Installations = catalogInstalls{bad}
			if _, err := svc.Preview(ctx, productui.PersonaAdminPreviewRequest{PersonaID: "persona-a", SubjectID: "user-a", ConversationID: "conv-a"}); !errors.Is(err, ErrPersonaCatalogDenied) {
				t.Fatalf("Preview error=%v, want denied", err)
			}
		})
	}
	svc.Installations = catalogInstalls{install}
	current := PersonaCatalogVersion{Profile: pv, Lifecycle: agentpersona.StatePublished, Owner: "owner", Steward: "steward"}
	t.Run("duplicate current persona version is denied", func(t *testing.T) {
		svc.Versions = catalogVersions{current, current}
		if _, err := svc.Snapshot(ctx, productui.PersonaAdminSnapshotRequest{TenantID: "tenant-a", Principal: principal.Subject()}); !errors.Is(err, ErrPersonaCatalogDenied) {
			t.Fatalf("Snapshot error = %v, want ambiguous current version denied", err)
		}
		if _, err := svc.Preview(ctx, productui.PersonaAdminPreviewRequest{PersonaID: "persona-a", SubjectID: "user-a", ConversationID: "conv-a"}); !errors.Is(err, ErrPersonaCatalogDenied) {
			t.Fatalf("Preview error = %v, want ambiguous current version denied", err)
		}
	})
	t.Run("unpublished persona cannot be previewed", func(t *testing.T) {
		svc.Versions = catalogVersions{{Profile: pv, Lifecycle: agentpersona.StateSuspended, Owner: "owner", Steward: "steward"}}
		if _, err := svc.Preview(ctx, productui.PersonaAdminPreviewRequest{PersonaID: "persona-a", SubjectID: "user-a", ConversationID: "conv-a"}); !errors.Is(err, ErrPersonaCatalogDenied) {
			t.Fatalf("Preview error = %v, want unpublished lifecycle denied", err)
		}
	})
	svc.Versions = catalogVersions{current}
	t.Run("new authoring draft retains exact older placement", func(t *testing.T) {
		profile := clonePersonaVersionProfile(pv.Profile)
		profile.Version = 2
		draft, err := agentpersona.Seal(profile)
		if err != nil {
			t.Fatal(err)
		}
		svc.Versions = catalogVersions{{Profile: draft, Lifecycle: agentpersona.StateDraft, Owner: "owner", Steward: "steward"}}
		defer func() { svc.Versions = catalogVersions{current} }()
		snapshot, err := svc.Snapshot(ctx, productui.PersonaAdminSnapshotRequest{})
		if err != nil || len(snapshot.Personas) != 1 || snapshot.Personas[0].Version != "2" || len(snapshot.Personas[0].Installations) != 1 || snapshot.Personas[0].Installations[0].Version != "1" || len(snapshot.Preview.EffectiveSkills) != 0 {
			t.Fatalf("draft and exact published placement = %+v, %v", snapshot, err)
		}
	})
	grants.allowed = false
	metadata, err := svc.Snapshot(ctx, productui.PersonaAdminSnapshotRequest{TenantID: "tenant-a", Principal: principal.Subject()})
	if err != nil {
		t.Fatalf("catalog metadata should not depend on the admin's own skill grants: %v", err)
	}
	if len(metadata.Personas[0].Skills) != 1 || metadata.Personas[0].DerivedData[0] != "WORKFORCE" || len(metadata.Preview.EffectiveSkills) != 0 {
		t.Fatalf("catalog metadata and caller-specific preview were not separated: %#v", metadata)
	}
}

func TestTodo_AGENTUX_003(t *testing.T) {
	ctx, principal := catalogContext(t)
	profile, err := agentpersona.Seal(agentpersona.PersonaProfile{
		Manifest:  agentpersona.AgentManifestRef{ID: "agent", Version: 1, Digest: "manifest", SchemaVersion: 1},
		PersonaID: "persona-readable", Version: 1, Handle: "readable", DisplayName: "Readable Persona", AvatarRef: "avatar",
		Purpose: "Help safely", Instructions: "Answer only with approved records.", Owner: "owner", Steward: "steward",
		Audience:          agentpersona.Audience{Roles: []string{"custom_people_partner"}, Populations: []string{"employees"}, OrganizationScopes: []string{"org-a"}},
		SkillPins:         []agentskills.SkillPin{{ID: "skill.read", Version: 1, Digest: "skill-digest"}},
		TierCeiling:       agentskills.TierRead,
		ConversationKinds: []agentpersona.ConversationKind{agentpersona.ConversationDirect},
		ChannelClasses:    []agentpersona.ChannelClass{agentpersona.ChannelPrivate},
		EvalSuiteRef:      "evaluation-suite",
		EvalLimits:        agentpersona.EvaluationLimits{MaxCost: 1, MaxSteps: 1, MaxLatencyMS: 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	room := chat.Conversation{ID: "room-a", TenantID: "tenant-a", Name: "People operations", Kind: chat.PrivateChannel}
	targets := &ChatDirectoryPersonaCatalogTargets{
		Chat:      personaCatalogChatFake{rooms: []chat.Conversation{room}, members: []chat.Membership{{ConversationID: room.ID, TenantID: room.TenantID, HomeTenantID: room.TenantID, SubjectID: principal.Subject()}}},
		Directory: personaCatalogDirectoryFake{},
		Roles:     personaCatalogRoleDirectoryFake{roles: []string{"hcm_admin"}},
		RoleNames: personaCatalogRoleStoreFake{snapshot: roleaccess.Snapshot{Roles: []roleaccess.Role{{ID: "custom_people_partner", Name: "Custom people partner", Active: true}}}},
	}
	svc := &PersonaAdminCatalogService{Versions: catalogVersions{{Profile: profile, Lifecycle: agentpersona.StatePublished, Owner: "owner", Steward: "steward"}}, Installations: catalogInstalls{}, Targets: targets, Skills: catalogSkills{}, Grants: &catalogGrants{allowed: true}, Authorizer: &catalogAuth{}}
	snapshot, err := svc.Snapshot(ctx, productui.PersonaAdminSnapshotRequest{})
	if err != nil || len(snapshot.Personas) != 1 || len(snapshot.SubjectOptions) != 1 || snapshot.SubjectOptions[0].Role != "hcm_admin" || len(snapshot.Conversations) != 1 || snapshot.Conversations[0].Kind != string(chat.PrivateChannel) {
		t.Fatalf("readable snapshot=%+v err=%v", snapshot, err)
	}
	persona := snapshot.Personas[0]
	if persona.Instructions != "Answer only with approved records." || persona.OwnerName != "Member owner" || persona.StewardName != "Member steward" || len(persona.AudienceRoles) != 1 || persona.AudienceRoles[0].Label != "Custom people partner" || len(persona.Organizations) != 1 || persona.Organizations[0].ID != "org-a" || len(persona.Skills) != 1 || persona.Skills[0].Description != "Reads approved workforce records." {
		t.Fatalf("readable persona=%+v", persona)
	}
}

func TestTodo_AGENTP_018_Security(t *testing.T) {
	ctx, _ := catalogContext(t)
	auth := &catalogAuth{}
	svc := &PersonaAdminCatalogService{Authorizer: auth}
	if _, err := svc.Snapshot(ctx, productui.PersonaAdminSnapshotRequest{TenantID: "tenant-b", Principal: "user-a"}); !errors.Is(err, ErrPersonaCatalogDenied) {
		t.Fatalf("cross-tenant request error = %v", err)
	}
	if auth.calls != 0 {
		t.Fatalf("authorizer called %d times for tenant mismatch", auth.calls)
	}
	deny := errors.New("permission missing")
	auth.err = deny
	svc = &PersonaAdminCatalogService{Versions: catalogVersions{}, Installations: catalogInstalls{}, Targets: catalogTargets{}, Skills: catalogSkills{}, Grants: &catalogGrants{}, Authorizer: auth}
	if _, err := svc.Snapshot(ctx, productui.PersonaAdminSnapshotRequest{}); !errors.Is(err, ErrPersonaCatalogDenied) {
		t.Fatalf("denied request error = %v", err)
	}
}

func catalogContext(t *testing.T) (context.Context, *trust.Principal) {
	t.Helper()
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: "tenant-a", Subject: "user-a", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceHigh, SessionRef: "session", IssuedAt: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour), CredentialDigest: "cred:sha256:abc"})
	if err != nil {
		t.Fatal(err)
	}
	return trust.WithPrincipal(context.Background(), p), p
}
