package application

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentpersona"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/experience/roleaccess"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type personaAdminCommandCatalogFake struct{}

func (personaAdminCommandCatalogFake) Snapshot(context.Context, productui.PersonaAdminSnapshotRequest) (productui.PersonaAdminSnapshot, error) {
	return productui.PersonaAdminSnapshot{Available: true}, nil
}
func (personaAdminCommandCatalogFake) Preview(context.Context, productui.PersonaAdminPreviewRequest) (productui.PersonaAdminPreview, error) {
	return productui.PersonaAdminPreview{}, nil
}
func (personaAdminCommandCatalogFake) RequestReview(string) error   { return nil }
func (personaAdminCommandCatalogFake) PublishPersona(string) error  { return nil }
func (personaAdminCommandCatalogFake) RollbackPersona(string) error { return nil }
func (personaAdminCommandCatalogFake) SuspendPersona(string) error  { return nil }
func (personaAdminCommandCatalogFake) RetirePersona(string) error   { return nil }

type personaAdminCommandAuthFake struct {
	err    error
	actor  PersonaAdminCommandActor
	action PersonaAdminCommandAction
	id     string
	calls  int
}

func (f *personaAdminCommandAuthFake) AuthorizePersonaAdminCommand(_ context.Context, actor PersonaAdminCommandActor, action PersonaAdminCommandAction, id string) error {
	f.calls++
	f.actor, f.action, f.id = actor, action, id
	return f.err
}

type personaAdminCommandExecutorFake struct {
	err     error
	actor   PersonaAdminCommandActor
	command PersonaAdminCommand
	calls   int
}

func (f *personaAdminCommandExecutorFake) ExecutePersonaAdminCommand(_ context.Context, actor PersonaAdminCommandActor, command PersonaAdminCommand) error {
	f.calls++
	f.actor, f.command = actor, command
	return f.err
}

func TestTodo_AGENTP_006_CommandTransportBindsAuthenticatedActorAndLifecycleAction(t *testing.T) {
	ctx, principal := personaAdminCommandContext(t)
	authorizer := &personaAdminCommandAuthFake{}
	executor := &personaAdminCommandExecutorFake{}
	factory, err := NewPersonaAdminCommandFactory(personaAdminCommandCatalogFake{}, authorizer, executor)
	if err != nil {
		t.Fatal(err)
	}
	client := factory.ClientForRequest(ctx)
	if client == nil {
		t.Fatal("trusted request did not receive persona admin client")
	}
	if err := client.RequestReview("persona-a"); err != nil {
		t.Fatal(err)
	}
	if authorizer.calls != 1 || executor.calls != 1 || authorizer.action != PersonaAdminRequestReview || executor.command.PersonaID != "persona-a" {
		t.Fatalf("authorization=%+v execution=%+v", authorizer, executor)
	}
	if executor.actor.Principal != principal || executor.actor.Tenant != principal.Tenant() || executor.actor.Subject != principal.Subject() {
		t.Fatalf("executor actor = %+v, want verified principal", executor.actor)
	}
}

func TestTodo_AGENTP_018_CommandTransportMapsVersionOverridesWithoutCallerVersion(t *testing.T) {
	ctx, _ := personaAdminCommandContext(t)
	authorizer := &personaAdminCommandAuthFake{}
	executor := &personaAdminCommandExecutorFake{}
	factory, err := NewPersonaAdminCommandFactory(personaAdminCommandCatalogFake{}, authorizer, executor)
	if err != nil {
		t.Fatal(err)
	}
	request := productui.PersonaAdminCommandRequest{Action: "CREATE_VERSION", PersonaID: "persona-a", StarterID: "hcmnext.persona_template.policy_helper", StarterVersion: 1, Handle: "policy-helper-v2", DisplayName: "Policy Helper v2", Purpose: "Answer approved policy questions", AllowedChannels: []string{"PRIVATE"}, BusinessOwnerID: "user:owner", TechnicalStewardID: "user:steward"}
	if err := factory.ExecutePersonaAdminCommand(ctx, request); err != nil {
		t.Fatal(err)
	}
	edit := executor.command.StarterVersionEdit
	if executor.command.Action != PersonaAdminCreateVersion || edit == nil || edit.Version != 0 || edit.Handle != request.Handle || len(edit.ChannelClasses) != 1 || edit.ChannelClasses[0] != agentpersona.ChannelPrivate {
		t.Fatalf("mapped version command = %+v", executor.command)
	}
	request.AllowedChannels = []string{"EXTERNAL"}
	if err := factory.ExecutePersonaAdminCommand(ctx, request); !errors.Is(err, ErrPersonaAdminCommandUnavailable) {
		t.Fatalf("unsupported channel error = %v", err)
	}
}

func TestTodo_AGENTP_006_CommandTransportCarriesExplicitReviewDecision(t *testing.T) {
	ctx, principal := personaAdminCommandContext(t)
	authorizer := &personaAdminCommandAuthFake{}
	executor := &personaAdminCommandExecutorFake{}
	factory, err := NewPersonaAdminCommandFactory(personaAdminCommandCatalogFake{}, authorizer, executor)
	if err != nil {
		t.Fatal(err)
	}
	reviewer, ok := factory.ClientForRequest(ctx).(productui.PersonaAdminReviewClient)
	if !ok {
		t.Fatal("authenticated command client has no explicit review surface")
	}
	if err := reviewer.ReviewPersona("persona-a", "APPROVE"); err != nil {
		t.Fatal(err)
	}
	if executor.command.Action != PersonaAdminReview || executor.command.Decision != "APPROVE" || executor.actor.Principal != principal || authorizer.calls != 1 {
		t.Fatalf("explicit review lost its trusted actor or decision: %+v, %+v", executor, authorizer)
	}
	if err := reviewer.ReviewPersona("persona-a", "UNREVIEWED"); err == nil || executor.calls != 1 {
		t.Fatalf("unsupported review decision reached executor: %v, %+v", err, executor)
	}
}

func TestTodo_AGENTP_018_CommandTransportReturnsStableOpaqueOutcomeCodes(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		code string
	}{
		{name: "conflict", err: agentpersonastore.ErrConflict, code: personaAdminCodeConflict},
		{name: "invalid", err: ErrPersonaDraftInvalid, code: personaAdminCodeInvalid},
		{name: "backend failure", err: errors.New("database password leaked"), code: personaAdminCodeUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, _ := personaAdminCommandContext(t)
			executor := &personaAdminCommandExecutorFake{err: tc.err}
			factory, err := NewPersonaAdminCommandFactory(personaAdminCommandCatalogFake{}, &personaAdminCommandAuthFake{}, executor)
			if err != nil {
				t.Fatal(err)
			}
			err = factory.Execute(ctx, PersonaAdminCommand{Action: PersonaAdminPublish, PersonaID: "persona-a"})
			if personaAdminCommandCode(err) != tc.code || strings.Contains(err.Error(), "password") {
				t.Fatalf("command error = %v, code %q; want opaque %q", err, personaAdminCommandCode(err), tc.code)
			}
		})
	}
}

func TestTodo_AGENTP_018_CommandTransportRejectsMissingTrustAndAuthority(t *testing.T) {
	if _, err := NewPersonaAdminCommandFactory(personaAdminCommandCatalogFake{}, nil, &personaAdminCommandExecutorFake{}); !errors.Is(err, ErrPersonaAdminCommandUnavailable) {
		t.Fatalf("missing authorizer error = %v", err)
	}
	authorizer := &personaAdminCommandAuthFake{}
	executor := &personaAdminCommandExecutorFake{}
	factory, err := NewPersonaAdminCommandFactory(personaAdminCommandCatalogFake{}, authorizer, executor)
	if err != nil {
		t.Fatal(err)
	}
	if got := factory.ClientForRequest(context.Background()); got != nil {
		t.Fatal("untrusted context received command client")
	}
	if err := factory.Execute(context.Background(), PersonaAdminCommand{Action: PersonaAdminPublish, PersonaID: "persona-a"}); !errors.Is(err, ErrPersonaAdminCommandUnavailable) {
		t.Fatalf("detached command error = %v", err)
	}
	if authorizer.calls != 0 || executor.calls != 0 {
		t.Fatalf("untrusted request reached authorities: auth=%d execute=%d", authorizer.calls, executor.calls)
	}
}

func TestTodo_AGENTP_018_CommandSurfaceRequiresTrustedLifecycleAuthorities(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config PersonaAdminCommandSurfaceConfig
	}{
		{name: "no configuration"},
		{name: "missing trusted ports", config: PersonaAdminCommandSurfaceConfig{Catalog: personaAdminCommandCatalogFake{}, Roles: &personaCatalogRoleStore{}, Store: &agentpersonastore.Store{}, Now: func() time.Time { return time.Unix(1, 0) }, NewEventID: func() string { return "event" }}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			factory, err := NewPersonaAdminCommandSurface(tc.config)
			if factory != nil || !errors.Is(err, ErrPersonaAdminCommandUnavailable) {
				t.Fatalf("surface = %v, error = %v; want closed construction", factory, err)
			}
		})
	}
}

func TestTodo_AGENTP_006_CommandTransportDenialStopsBeforeLifecycle(t *testing.T) {
	ctx, _ := personaAdminCommandContext(t)
	authorizer := &personaAdminCommandAuthFake{err: errors.New("grant revoked")}
	executor := &personaAdminCommandExecutorFake{}
	factory, err := NewPersonaAdminCommandFactory(personaAdminCommandCatalogFake{}, authorizer, executor)
	if err != nil {
		t.Fatal(err)
	}
	if err := factory.Execute(ctx, PersonaAdminCommand{Action: PersonaAdminPublish, PersonaID: "persona-a"}); personaAdminCommandCode(err) != personaAdminCodeForbidden {
		t.Fatalf("denied publish code = %v, error %v", personaAdminCommandCode(err), err)
	}
	if executor.calls != 0 {
		t.Fatalf("denied request reached lifecycle executor %d times", executor.calls)
	}
}

func personaAdminCommandCode(err error) string {
	var coded interface{ PersonaAdminCommandCode() string }
	if errors.As(err, &coded) {
		return coded.PersonaAdminCommandCode()
	}
	return ""
}

func TestTodo_AGENTP_018_CommandTransportRejectsCallerIdentityAndInvalidAction(t *testing.T) {
	ctx, principal := personaAdminCommandContext(t)
	authorizer := &personaAdminCommandAuthFake{}
	executor := &personaAdminCommandExecutorFake{}
	factory, err := NewPersonaAdminCommandFactory(personaAdminCommandCatalogFake{}, authorizer, executor)
	if err != nil {
		t.Fatal(err)
	}
	if err := factory.Execute(ctx, PersonaAdminCommand{Action: "ADMIN_OVERRIDE", PersonaID: "persona-a"}); !errors.Is(err, ErrPersonaAdminCommandUnavailable) {
		t.Fatalf("unknown action error = %v", err)
	}
	if authorizer.calls != 0 || executor.calls != 0 {
		t.Fatalf("invalid action reached authorities: auth=%d execute=%d", authorizer.calls, executor.calls)
	}
	if principal.Subject() == "attacker" {
		t.Fatal("bad test principal")
	}
}

func TestTodo_AGENTP_018_CommandRoleAuthorizerRequiresExplicitCurrentAction(t *testing.T) {
	ctx, principal := personaCatalogRoleContext(t)
	ctx = trust.WithPrincipal(ctx, principal)
	actor := PersonaAdminCommandActor{Principal: principal, Tenant: principal.Tenant(), Subject: principal.Subject()}
	for _, tc := range []struct {
		name    string
		grant   roleaccess.PagePermission
		action  PersonaAdminCommandAction
		wantErr bool
	}{
		{name: "create explicit grant", grant: roleaccess.PagePermission{RoleID: "hcm_admin", PageID: string(productui.PagePersonaAdmin), View: true, Create: true}, action: PersonaAdminCreateDraft},
		{name: "update grant", grant: roleaccess.PagePermission{RoleID: "hcm_admin", PageID: string(productui.PagePersonaAdmin), View: true, Update: true}, action: PersonaAdminPublish},
		{name: "view does not grant publish", grant: roleaccess.PagePermission{RoleID: "hcm_admin", PageID: string(productui.PagePersonaAdmin), View: true}, action: PersonaAdminPublish, wantErr: true},
		{name: "reviewer view permits decision surface", grant: roleaccess.PagePermission{RoleID: "hcm_admin", PageID: string(productui.PagePersonaAdmin), View: true}, action: PersonaAdminReview},
		{name: "reviewer view cannot queue author draft", grant: roleaccess.PagePermission{RoleID: "hcm_admin", PageID: string(productui.PagePersonaAdmin), View: true}, action: PersonaAdminRequestReview, wantErr: true},
		{name: "no implicit admin grant", grant: roleaccess.PagePermission{}, action: PersonaAdminCreateDraft, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &personaCatalogRoleStore{snapshot: roleaccess.Snapshot{PagePermissions: []roleaccess.PagePermission{tc.grant}}}
			err := (PersonaAdminCommandRoleAuthorizer{Roles: store}).AuthorizePersonaAdminCommand(ctx, actor, tc.action, "persona-a")
			if (err != nil) != tc.wantErr {
				t.Fatalf("AuthorizePersonaAdminCommand error = %v, wantErr=%t", err, tc.wantErr)
			}
			if store.calls != 1 {
				t.Fatalf("role store calls = %d, want 1", store.calls)
			}
		})
	}
}

func personaAdminCommandContext(t *testing.T) (context.Context, *trust.Principal) {
	t.Helper()
	now := time.Unix(100, 0)
	p, err := trust.NewPrincipal(trust.PrincipalSpec{Tenant: values.TenantId("tenant-a"), Subject: "user-admin", SubjectKind: trust.SubjectKindHuman, AuthenticationMethod: trust.AuthenticationMethodBearerToken, Assurance: trust.AssuranceSubstantial, SessionRef: "session-1", IssuedAt: now, ExpiresAt: now.Add(time.Hour), CredentialDigest: "sha256:credential"})
	if err != nil {
		t.Fatal(err)
	}
	return trust.WithPrincipal(context.Background(), p), p
}
