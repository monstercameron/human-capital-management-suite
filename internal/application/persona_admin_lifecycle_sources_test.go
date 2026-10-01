package application

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/agenttemplate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type personaAdminScopesFake struct {
	scopes       []agentstore.PersonaSecurityScope
	err          error
	beforeRevoke func()
}

func (f *personaAdminScopesFake) RevokePersonaSecurityScope(_ context.Context, _ uuid.UUID, scope agentstore.PersonaSecurityScope, _ string, _ time.Time) (int64, error) {
	if f.beforeRevoke != nil {
		f.beforeRevoke()
	}
	if f.err != nil {
		return 0, f.err
	}
	f.scopes = append(f.scopes, scope)
	return 2, nil
}

func TestTodo_AGENTP_018_SecuritySuspendFencesAllVersionsBeforeLifecycle(t *testing.T) {
	ctx, principal := personaAdminCommandContext(t)
	actor := PersonaAdminCommandActor{Principal: principal, Tenant: principal.Tenant(), Subject: principal.Subject()}
	tenant := &personaAdminLifecycleTenantFake{versions: []agentpersonastore.PersonaVersion{{TenantID: principal.Tenant(), PersonaID: "persona-a", Version: 1}, {TenantID: principal.Tenant(), PersonaID: "persona-a", Version: 2}}, states: map[int64]agentpersonastore.LifecycleState{1: agentpersonastore.StatePublished, 2: agentpersonastore.StatePublished}}
	scopes := &personaAdminScopesFake{beforeRevoke: func() {
		if len(tenant.events) != 0 {
			t.Fatal("lifecycle changed before all versions were fenced")
		}
	}}
	transitions := &PersonaAdminReviewedTransitions{Store: personaAdminLifecycleStoreFake{tenant}, Authorizer: personaAdminLifecycleAuthorizerFake{}, Security: scopes, TenantUUID: func(values.TenantId) uuid.UUID { return uuid.MustParse("11111111-1111-1111-1111-111111111111") }, Now: func() time.Time { return time.Unix(300, 0) }, NewEventID: func() string { return uuid.NewString() }}
	if err := transitions.SuspendPersona(ctx, actor, "persona-a", "Incident response"); err != nil {
		t.Fatal(err)
	}
	if len(scopes.scopes) != 2 || scopes.scopes[0].Key != "persona-a:1" || scopes.scopes[1].Key != "persona-a:2" || tenant.states[1] != agentpersonastore.StateSuspended || tenant.states[2] != agentpersonastore.StateSuspended {
		t.Fatalf("suspension = %+v states %+v", scopes.scopes, tenant.states)
	}
	scopes.beforeRevoke = nil
	if err := transitions.SuspendPersona(ctx, actor, "persona-a", "Incident response"); err != nil {
		t.Fatal(err)
	}
	if len(tenant.events) != 2 {
		t.Fatalf("retry duplicated lifecycle events: %+v", tenant.events)
	}
}

func TestTodo_AGENTP_018_FaultFailedRevocationNeverChangesLifecycle(t *testing.T) {
	ctx, principal := personaAdminCommandContext(t)
	actor := PersonaAdminCommandActor{Principal: principal, Tenant: principal.Tenant(), Subject: principal.Subject()}
	tenant := &personaAdminLifecycleTenantFake{versions: []agentpersonastore.PersonaVersion{{TenantID: principal.Tenant(), PersonaID: "persona-a", Version: 1}}, states: map[int64]agentpersonastore.LifecycleState{1: agentpersonastore.StatePublished}}
	failure := errors.New("durable fence unavailable")
	transitions := &PersonaAdminReviewedTransitions{Store: personaAdminLifecycleStoreFake{tenant}, Authorizer: personaAdminLifecycleAuthorizerFake{}, Security: &personaAdminScopesFake{err: failure}, TenantUUID: func(values.TenantId) uuid.UUID { return uuid.New() }, Now: func() time.Time { return time.Unix(300, 0) }, NewEventID: func() string { return uuid.NewString() }}
	if err := transitions.RetirePersona(ctx, actor, "persona-a", "Retire"); !errors.Is(err, failure) {
		t.Fatalf("retirement error %v", err)
	}
	if len(tenant.events) != 0 || tenant.states[1] != agentpersonastore.StatePublished {
		t.Fatal("failed fence changed lifecycle")
	}
}

type personaAdminPlacementFake struct{ facts PersonaAdminPlacementFacts }

func (f personaAdminPlacementFake) ResolvePersonaAdminPlacement(context.Context, PersonaAdminCommandActor, string) (PersonaAdminPlacementFacts, error) {
	return f.facts, nil
}

type personaAdminVersionFake struct {
	version agentpersonastore.PersonaVersion
}

func (f personaAdminVersionFake) GetVersion(context.Context, values.TenantId, string, int64) (agentpersonastore.PersonaVersion, error) {
	return f.version, nil
}

func TestTodo_AGENTP_007_SecurityInstallationUsesRoomCeilingAndManager(t *testing.T) {
	ctx, principal := personaAdminCommandContext(t)
	actor := PersonaAdminCommandActor{Principal: principal, Tenant: principal.Tenant(), Subject: principal.Subject()}
	manifest := personaStarterManifest("Answer approved policy questions.")
	req := validPersonaStarterRequest(manifest)
	starter, _ := agenttemplate.PersonaStarterFor(req.StarterID, req.StarterVersion)
	profile, err := agentpersona.Seal(personaStarterProfile(starter, req, manifest, "Answer approved policy questions."))
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(profile.Profile)
	row := agentpersonastore.PersonaVersion{TenantID: actor.Tenant, PersonaID: req.PersonaID, Version: 1, Profile: encoded, ContentDigest: profile.Digest}
	facts := PersonaAdminPlacementFacts{Tenant: actor.Tenant, ConversationID: "room-a", Class: agentpersonastore.ConversationPrivate, ManagerID: actor.Subject, Revision: 1, Policy: agentpersonastore.ChannelPolicy{MaxTier: "T0", AllowedDataClasses: []string{}, AllowedChannelClasses: []agentpersonastore.ConversationClass{agentpersonastore.ConversationPrivate}, AlwaysPrivate: true}}
	request := agentpersonastore.PersonaInstallation{TenantID: actor.Tenant, PersonaID: row.PersonaID, PersonaVersion: 1, ConversationID: "room-a", InstallerID: actor.Subject}
	for _, tc := range []struct {
		name   string
		mutate func(*PersonaAdminPlacementFacts)
		denied bool
	}{
		{name: "governed placement"},
		{name: "member cannot install", mutate: func(f *PersonaAdminPlacementFacts) { f.ManagerID = "other-user" }, denied: true},
		{name: "external guest", mutate: func(f *PersonaAdminPlacementFacts) { f.ExternalMembers = true }, denied: true},
		{name: "cross company", mutate: func(f *PersonaAdminPlacementFacts) { f.CrossCompanyMembers = true }, denied: true},
		{name: "unapproved class", mutate: func(f *PersonaAdminPlacementFacts) { f.Policy.AllowedChannelClasses = nil }, denied: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			candidate := facts
			if tc.mutate != nil {
				tc.mutate(&candidate)
			}
			authorizer := GovernedPersonaAdminInstallation{Placement: personaAdminPlacementFake{candidate}, Versions: personaAdminVersionFake{row}, Profiles: &personaStarterProfileBuilderSpy{}}
			installation, err := authorizer.AuthorizePersonaInstallation(ctx, actor, request)
			if tc.denied {
				if err == nil {
					t.Fatalf("unsafe placement accepted: %+v", installation)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if installation.ChannelPolicy.MaxTier != "T0" || !installation.ChannelPolicy.AlwaysPrivate || installation.ConversationClass != agentpersonastore.ConversationPrivate {
				t.Fatalf("room policy not preserved: %+v", installation)
			}
		})
	}
}
