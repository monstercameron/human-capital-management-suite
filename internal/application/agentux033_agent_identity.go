package application

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// personaChatIdentityActive reports whether subjectID is an active agent chat
// identity of the tenant, read from the agent identity registry and never from
// the human directory. A nil store means the caller has no registry: every
// member is then a person, which is the safe reading.
func personaChatIdentityActive(ctx context.Context, installations PersonaAudienceInstallationStore, tenantID, homeTenantID, subjectID string) (bool, error) {
	if installations == nil || tenantID != homeTenantID {
		return false, nil
	}
	store, err := installations.ForTenant(ctx, values.TenantId(tenantID))
	if err != nil || store == nil {
		return false, fmt.Errorf("%w: scope persona identities", errPersonaAudienceSourceUnavailable)
	}
	identity, err := store.LookupPersonaChatIdentity(ctx, subjectID)
	if errors.Is(err, agentpersonastore.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("%w: resolve persona identity: %v", errPersonaAudienceSourceUnavailable, err)
	}
	if identity.TenantID.String() != tenantID || identity.AgentID != subjectID || strings.TrimSpace(identity.PersonaID) == "" {
		return false, fmt.Errorf("%w: invalid persona identity", errPersonaAudienceSourceUnavailable)
	}
	return identity.Active, nil
}
