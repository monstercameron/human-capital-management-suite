package application

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agentskills"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type personaDraftStoreFake struct {
	version agentpersonastore.PersonaVersion
	owner   agentpersonastore.PersonaOwner
	steward agentpersonastore.PersonaOwner
	actor   string
	at      time.Time
	calls   int
	err     error
}

func (f *personaDraftStoreFake) CreateDraft(_ context.Context, version agentpersonastore.PersonaVersion, owner, steward agentpersonastore.PersonaOwner, actor string, at time.Time) error {
	f.calls++
	f.version, f.owner, f.steward, f.actor, f.at = version, owner, steward, actor, at
	return f.err
}

type personaCreateAuthorizerFake struct {
	allowedTenant values.TenantId
	err           error
	request       PersonaCreateAuthorization
	calls         int
}

func (f *personaCreateAuthorizerFake) AuthorizePersonaCreate(_ context.Context, req PersonaCreateAuthorization) error {
	f.calls++
	f.request = req
	if f.err != nil {
		return f.err
	}
	if req.Tenant != f.allowedTenant {
		return errors.New("tenant is not authorized")
	}
	return nil
}

type personaProfileBuilderFake struct {
	err error
}

func (f personaProfileBuilderFake) Build(profile agentpersona.PersonaProfile) (agentpersona.PersonaVersion, error) {
	if f.err != nil {
		return agentpersona.PersonaVersion{}, f.err
	}
	return agentpersona.Seal(profile)
}

type personaDraftClockFake struct{ now time.Time }

func (f personaDraftClockFake) Now() time.Time { return f.now }

func TestPersonaAdminDraftService_CreateDraft(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	profile := agentpersona.PersonaProfile{
		Manifest:  agentpersona.AgentManifestRef{ID: "people-helper", Version: 2, Digest: "manifest-digest", SchemaVersion: 1},
		PersonaID: "people-helper", Version: 1, Handle: "people-helper", DisplayName: "People Helper",
		AvatarRef: "avatar:people-helper",
		Purpose:   "Answer employee policy questions", Audience: agentpersona.Audience{Roles: []string{"employee"}, Populations: []string{"staff"}, OrganizationScopes: []string{"org-a"}},
		SkillPins:   []agentskills.SkillPin{{ID: "hcm.people.read", Version: 1, Digest: "skill-digest"}},
		TierCeiling: agentskills.TierRead, ConversationKinds: []agentpersona.ConversationKind{agentpersona.ConversationChannel},
		ChannelClasses: []agentpersona.ChannelClass{agentpersona.ChannelPrivate}, Instructions: "Use only approved policy material.",
		Owner: "user:owner", Steward: "user:steward", EvalSuiteRef: "persona-eval-v1",
		EvalLimits:      agentpersona.EvaluationLimits{MaxCost: 1, MaxSteps: 2, MaxLatencyMS: 500},
		DataClassesRead: []string{"people.policy"},
	}
	sealed, err := agentpersona.Seal(profile)
	if err != nil {
		t.Fatal(err)
	}
	store := &personaDraftStoreFake{}
	authorizer := &personaCreateAuthorizerFake{allowedTenant: "tenant-a"}
	service := &PersonaAdminDraftService{Store: store, Authorizer: authorizer, Profiles: personaProfileBuilderFake{}, Clock: personaDraftClockFake{now}}
	principal := personaDraftPrincipal(t, "tenant-a", "user:creator", now)
	ctx := trust.WithPrincipal(context.Background(), principal)
	receipt, err := service.CreateDraft(ctx, PersonaDraftRequest{Version: sealed, BusinessOwnerID: "user:owner", TechnicalStewardID: "user:steward"})
	if err != nil {
		t.Fatal(err)
	}
	if receipt.PersonaID != profile.PersonaID || receipt.Version != profile.Version || receipt.Digest != sealed.Digest || receipt.Lifecycle != string(agentpersonastore.StateDraft) {
		t.Fatalf("receipt = %+v", receipt)
	}
	if store.calls != 1 || store.version.TenantID != principal.Tenant() || store.version.Profile == nil || store.actor != principal.Subject() || !store.at.Equal(now) {
		t.Fatalf("store call = %+v, calls %d", store, store.calls)
	}
	if store.owner.Role != agentpersonastore.BusinessOwner || store.owner.PrincipalID != profile.Owner || store.steward.Role != agentpersonastore.TechnicalSteward || store.steward.PrincipalID != profile.Steward {
		t.Fatalf("explicit owner assignments = %+v / %+v", store.owner, store.steward)
	}
	if authorizer.calls != 1 || authorizer.request.Principal != principal || authorizer.request.Tenant != principal.Tenant() || authorizer.request.PersonaID != profile.PersonaID {
		t.Fatalf("authorization request = %+v, calls %d", authorizer.request, authorizer.calls)
	}
}

func TestPersonaAdminDraftService_CreateDraftSecurity(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	sealed, err := agentpersona.Seal(personaDraftProfile())
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name          string
		tenant        string
		withPrincipal bool
		deny          bool
		wantDenied    bool
	}{
		{name: "missing principal", tenant: "tenant-a", wantDenied: true},
		{name: "authorizer denial", tenant: "tenant-a", withPrincipal: true, deny: true, wantDenied: true},
		{name: "cross tenant grant refused", tenant: "tenant-b", withPrincipal: true, wantDenied: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &personaDraftStoreFake{}
			authorizer := &personaCreateAuthorizerFake{allowedTenant: "tenant-a"}
			if tc.deny {
				authorizer.err = errors.New("missing persona-admin create grant")
			}
			service := &PersonaAdminDraftService{Store: store, Authorizer: authorizer, Profiles: personaProfileBuilderFake{}, Clock: personaDraftClockFake{now}}
			ctx := context.Background()
			if tc.withPrincipal {
				ctx = trust.WithPrincipal(ctx, personaDraftPrincipal(t, tc.tenant, "user:creator", now))
			}
			_, err := service.CreateDraft(ctx, PersonaDraftRequest{Version: sealed, BusinessOwnerID: "user:owner", TechnicalStewardID: "user:steward"})
			if tc.wantDenied && !errors.Is(err, ErrPersonaDraftDenied) {
				t.Fatalf("CreateDraft error = %v, want ErrPersonaDraftDenied", err)
			}
			if store.calls != 0 {
				t.Fatalf("denied request reached store %d times", store.calls)
			}
		})
	}
}

func TestPersonaAdminDraftService_CreateDraftDuplicate(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	sealed, err := agentpersona.Seal(personaDraftProfile())
	if err != nil {
		t.Fatal(err)
	}
	store := &personaDraftStoreFake{err: agentpersonastore.ErrConflict}
	service := &PersonaAdminDraftService{Store: store, Authorizer: &personaCreateAuthorizerFake{allowedTenant: "tenant-a"}, Profiles: personaProfileBuilderFake{}, Clock: personaDraftClockFake{now}}
	ctx := trust.WithPrincipal(context.Background(), personaDraftPrincipal(t, "tenant-a", "user:creator", now))
	_, err = service.CreateDraft(ctx, PersonaDraftRequest{Version: sealed, BusinessOwnerID: "user:owner", TechnicalStewardID: "user:steward"})
	if !errors.Is(err, agentpersonastore.ErrConflict) || store.calls != 1 {
		t.Fatalf("duplicate error = %v, store calls %d", err, store.calls)
	}
}

func personaDraftPrincipal(t *testing.T, tenant, subject string, now time.Time) *trust.Principal {
	t.Helper()
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId(tenant), Subject: subject,
		SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken,
		Assurance: trust.AssuranceSubstantial, SessionRef: subject + ":session", IssuedAt: now.Add(-time.Minute),
		ExpiresAt: now.Add(time.Hour), CredentialDigest: subject + ":credential"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func personaDraftProfile() agentpersona.PersonaProfile {
	return agentpersona.PersonaProfile{
		Manifest:  agentpersona.AgentManifestRef{ID: "people-helper", Version: 2, Digest: "manifest-digest", SchemaVersion: 1},
		PersonaID: "people-helper", Version: 1, Handle: "people-helper", DisplayName: "People Helper",
		AvatarRef: "avatar:people-helper",
		Purpose:   "Answer employee policy questions", Audience: agentpersona.Audience{Roles: []string{"employee"}, Populations: []string{"staff"}, OrganizationScopes: []string{"org-a"}},
		SkillPins:   []agentskills.SkillPin{{ID: "hcm.people.read", Version: 1, Digest: "skill-digest"}},
		TierCeiling: agentskills.TierRead, ConversationKinds: []agentpersona.ConversationKind{agentpersona.ConversationChannel},
		ChannelClasses: []agentpersona.ChannelClass{agentpersona.ChannelPrivate}, Instructions: "Use only approved policy material.",
		Owner: "user:owner", Steward: "user:steward", EvalSuiteRef: "persona-eval-v1",
		EvalLimits:      agentpersona.EvaluationLimits{MaxCost: 1, MaxSteps: 2, MaxLatencyMS: 500},
		DataClassesRead: []string{"people.policy"},
	}
}
