package application

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// removedPlacements is the placement history of one agent: the conversations a
// person removed it from, newest first, each with the name of who removed it and
// the day. A conversation the viewer may not inspect is left out, as it is for a
// live placement, and a placement nobody removed (it was replaced or stopped by
// the server) has no one to name and is not listed.
func (s *PersonaAdminCatalogService) removedPlacements(ctx context.Context, tenant values.TenantId, personaID string, all []PersonaCatalogInstallation, users, conversations []productui.PersonaAdminTarget) []productui.PersonaAdminRemovedPlacement {
	var out []productui.PersonaAdminRemovedPlacement
	for _, in := range all {
		if in.PersonaID != personaID || in.State != "RETIRED" || strings.TrimSpace(in.RemovedBy) == "" || in.RemovedAt.IsZero() {
			continue
		}
		target, ok := personaCatalogTarget(conversations, in.ConversationID)
		if !ok {
			continue
		}
		conversation := target.Label
		if strings.TrimSpace(target.PlacementLabel) != "" {
			conversation = target.PlacementLabel
		}
		out = append(out, productui.PersonaAdminRemovedPlacement{
			ConversationID: in.ConversationID, Conversation: conversation, Kind: target.Kind,
			RemovedBy: s.removerName(ctx, tenant, in.RemovedBy, users), RemovedAt: in.RemovedAt.UTC(), Version: strconv.FormatUint(uint64(in.PersonaVersion), 10),
		})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].RemovedAt.After(out[j].RemovedAt) })
	return out
}

// removerName is the name people see for the person who removed a placement: the
// directory's label when the viewer may see it, otherwise empty, and the page
// then says when it was removed without naming anyone.
func (s *PersonaAdminCatalogService) removerName(ctx context.Context, tenant values.TenantId, subject string, users []productui.PersonaAdminTarget) string {
	if name := personaCatalogTargetName(users, subject); strings.TrimSpace(name) != "" {
		return name
	}
	if directory, ok := s.Targets.(personaCatalogReadableProjection); ok {
		if target, err := directory.ResolvePersonaCatalogTarget(ctx, tenant, subject); err == nil && target.ID == subject && strings.TrimSpace(target.Label) != "" {
			return target.Label
		}
	}
	return ""
}
