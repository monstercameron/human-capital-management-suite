package application

import (
	"context"
	"testing"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

func TestTodo_AGENTP_018_DraftCommandsRemainAvailableWithoutReviewCredential(t *testing.T) {
	profiles := PersonaProfileBuilderSourceFunc(func(context.Context, values.TenantId) (PersonaProfileBuilder, error) {
		return nil, nil
	})
	manifests := PersonaStarterManifestResolverSourceFunc(func(context.Context, values.TenantId) (PersonaStarterManifestResolver, error) {
		return nil, nil
	})
	instructions := PersonaStarterInstructionsResolverSourceFunc(func(context.Context, values.TenantId) (PersonaStarterInstructionsResolver, error) {
		return nil, nil
	})
	factory, err := NewPersonaAdminCommandSurface(PersonaAdminCommandSurfaceConfig{
		Catalog: personaAdminCommandCatalogFake{}, Roles: &personaCatalogRoleStore{},
		Store: &agentpersonastore.Store{}, Profiles: profiles, Manifests: manifests,
		Instructions: instructions, Now: func() time.Time { return time.Unix(1, 0) },
		NewEventID: func() string { return "event" },
	})
	if err != nil || factory == nil {
		t.Fatalf("draft surface unavailable: factory=%v err=%v", factory, err)
	}
	executor, ok := factory.executor.(*PersonaAdminLifecycleExecutor)
	if !ok || executor.Drafts == nil || executor.StarterDrafts == nil || executor.Reviews != nil || executor.Evidence != nil {
		t.Fatalf("unexpected draft/review/publication authorities: %#v", executor)
	}
}
