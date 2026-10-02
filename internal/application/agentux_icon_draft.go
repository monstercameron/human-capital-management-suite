package application

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
)

// createPersonaDraftIcon uses the atomic identity writer when the store is
// supplied directly; the served adapter also delegates to that writer.
func createPersonaDraftIcon(ctx context.Context, store PersonaDraftStore, version agentpersonastore.PersonaVersion, owner, steward agentpersonastore.PersonaOwner, actor string, at time.Time) error {
	if writer, ok := store.(interface {
		CreateDraftWithIcon(context.Context, agentpersonastore.PersonaVersion, agentpersonastore.PersonaOwner, agentpersonastore.PersonaOwner, string, time.Time) error
	}); ok {
		return writer.CreateDraftWithIcon(ctx, version, owner, steward, actor, at)
	}
	return store.CreateDraft(ctx, version, owner, steward, actor, at)
}
