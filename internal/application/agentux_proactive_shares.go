package application

import (
	"context"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/documenthubstore"
)

type AgentAnnouncementHubShares struct {
	Hub        *documenthubstore.Store
	Principals AgentAnnouncementServicePrincipalSource
}

func (s AgentAnnouncementHubShares) SetAnnouncementDocumentShares(ctx context.Context, actor AgentAnnouncementActor, record agentstore.Announcement) error {
	if s.Hub == nil || s.Principals == nil || actor.TenantUUID != record.TenantID || actor.SubjectID != record.OwnerID {
		return ErrAgentAnnouncementDenied
	}
	service, err := s.Principals.CurrentAnnouncementServicePrincipal(ctx, actor.TenantID, record.InstallationID)
	if err != nil {
		return err
	}
	var ids []string
	if record.State != agentstore.AnnouncementDeleted {
		for _, ref := range record.Documents {
			ids = append(ids, ref.DocumentID)
		}
	}
	return s.Hub.SetAnnouncementDocuments(ctx, actor.TenantID, actor.SubjectID, service.SubjectID, record.ID, ids, record.ConversationID)
}
