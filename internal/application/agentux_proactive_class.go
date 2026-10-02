package application

import (
	"context"
	"slices"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	trustdlp "github.com/monstercameron/human-capital-management-suite/internal/trust/dlp"
)

func (r *AgentAnnouncementRuntime) documentClass(ctx context.Context, record agentstore.Announcement, documents []AgentAnnouncementResolvedDocument, route PersonaRunModelRoute) (trustdlp.DataClass, error) {
	if r.DocumentAuthority.Store == nil || len(documents) == 0 {
		return "", ErrAgentAnnouncementDenied
	}
	var class trustdlp.DataClass
	for _, document := range documents {
		id, err := r.DocumentAuthority.versionID(ctx, record.TenantKey, document.DocumentID, document.Version)
		if err != nil {
			return "", err
		}
		var classification string
		err = r.DocumentAuthority.Store.RunTenantTx(ctx, record.TenantKey, func(tx dbport.Tx) error {
			return tx.QueryRow(ctx, `SELECT classification FROM document_version WHERE tenant_id=$1 AND document_id=$2 AND id=$3`, record.TenantKey, document.DocumentID, id).Scan(&classification)
		})
		actual := trustdlp.DataClass(strings.ToUpper(strings.TrimSpace(classification)))
		if err != nil || !actual.Valid() || !slices.Contains(route.Route.Task.DataClasses, string(actual)) || !slices.Contains(route.Egress.AllowedClasses, actual) || !personaOpenAISourceClassCovers(route.ProfileClass, actual) {
			return "", ErrAgentAnnouncementDenied
		}
		class, err = announcementCombinedSourceClass(class, actual)
		if err != nil {
			return "", err
		}
	}
	return class, nil
}

func announcementCombinedSourceClass(current, actual trustdlp.DataClass) (trustdlp.DataClass, error) {
	if actual.Valid() && (current == "" || personaOpenAISourceClassCovers(actual, current)) {
		return actual, nil
	}
	if personaOpenAISourceClassCovers(current, actual) {
		return current, nil
	}
	return "", ErrAgentAnnouncementDenied
}
