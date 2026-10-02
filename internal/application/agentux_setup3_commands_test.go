package application

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type agentUXSetup3InstallAuthorizer struct{ allowed bool }

func (a agentUXSetup3InstallAuthorizer) AuthorizePersonaAdminCommand(_ context.Context, _ PersonaAdminCommandActor, action PersonaAdminCommandAction, _ string) error {
	if a.allowed && (action == PersonaAdminInstall || action == PersonaAdminUninstall) {
		return nil
	}
	return errors.New("not granted")
}

type agentUXSetup3CommandExecutor struct{}

func (agentUXSetup3CommandExecutor) ExecutePersonaAdminCommand(context.Context, PersonaAdminCommandActor, PersonaAdminCommand) error {
	return nil
}

func TestAgentUXSetup3_F5AllowedCommandsRequireInstallPermission(t *testing.T) {
	ctx, _ := personaAdminCommandContext(t)
	for _, tc := range []struct {
		name    string
		allowed bool
		want    []string
	}{
		{name: "administrator with install permission", allowed: true, want: []string{"INSTALL", "UNINSTALL"}},
		{name: "administrator without install permission", allowed: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			factory, err := NewPersonaAdminCommandFactory(personaAdminCommandCatalogFake{}, agentUXSetup3InstallAuthorizer{allowed: tc.allowed}, agentUXSetup3CommandExecutor{})
			if err != nil {
				t.Fatal(err)
			}
			client := factory.ClientForRequest(ctx)
			if client == nil {
				t.Fatal("authenticated administrator did not receive a command client")
			}
			snapshot, err := client.Snapshot(ctx, productui.PersonaAdminSnapshotRequest{})
			if err != nil || !snapshot.CommandPermissionsAvailable || !slices.Equal(snapshot.AllowedCommands, tc.want) {
				t.Fatalf("allowed commands=%v want=%v err=%v", snapshot.AllowedCommands, tc.want, err)
			}
		})
	}
}

type agentUXSetup3LifecycleTenant struct {
	PersonaAdminLifecycleTenant
	active  map[string]bool
	retired string
}

func (t *agentUXSetup3LifecycleTenant) RetireActiveInstallation(_ context.Context, persona, conversation, actor, _ string) (agentpersonastore.PersonaInstallation, bool, error) {
	key := persona + "/" + conversation
	if actor == "" || !t.active[key] {
		return agentpersonastore.PersonaInstallation{}, false, errors.New("active installation not found")
	}
	t.active[key] = false
	t.retired = key
	return agentpersonastore.PersonaInstallation{PersonaID: persona, ConversationID: conversation, State: agentpersonastore.InstallationRetired}, true, nil
}

type agentUXSetup3LifecycleStore struct{ tenant *agentUXSetup3LifecycleTenant }

func (s agentUXSetup3LifecycleStore) ForTenant(context.Context, values.TenantId) (PersonaAdminLifecycleTenant, error) {
	return s.tenant, nil
}

type agentUXSetup3InstallationAuthorizer struct{}

func (agentUXSetup3InstallationAuthorizer) AuthorizePersonaInstallation(_ context.Context, actor PersonaAdminCommandActor, installation agentpersonastore.PersonaInstallation) (agentpersonastore.PersonaInstallation, error) {
	if actor.Subject == "" || installation.PersonaID == "" || installation.ConversationID == "" {
		return agentpersonastore.PersonaInstallation{}, errors.New("invalid placement")
	}
	return installation, nil
}

func TestAgentUXSetup3_F5UninstallRetiresExactInstallation(t *testing.T) {
	ctx, principal := personaAdminCommandContext(t)
	tenant := &agentUXSetup3LifecycleTenant{active: map[string]bool{"policy-helper/general": true, "policy-helper/direct-policy": true}}
	authorizer := agentUXSetup3InstallAuthorizer{allowed: true}
	executor := NewPersonaAdminLifecycleExecutor(agentUXSetup3LifecycleStore{tenant: tenant}, authorizer, nil, nil, nil, nil, agentUXSetup3InstallationAuthorizer{}, nil, nil, nil)
	actor := PersonaAdminCommandActor{Principal: principal, Tenant: principal.Tenant(), Subject: principal.Subject()}
	err := executor.ExecutePersonaAdminCommand(ctx, actor, PersonaAdminCommand{Action: PersonaAdminUninstall, PersonaID: "policy-helper", Installation: agentpersonastore.PersonaInstallation{PersonaID: "policy-helper", ConversationID: "general"}})
	if err != nil || tenant.retired != "policy-helper/general" || tenant.active["policy-helper/general"] || !tenant.active["policy-helper/direct-policy"] {
		t.Fatalf("retired=%q active=%v err=%v", tenant.retired, tenant.active, err)
	}
}
