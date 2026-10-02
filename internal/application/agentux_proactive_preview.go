package application

import (
	"context"
	"errors"
	"slices"

	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
)

// AgentAnnouncementDraftPreview uses the same resolution, model, seal and
// audience checks as a occurrence, without saving a definition or delivering.
type AgentAnnouncementDraftPreview struct {
	Authority AgentAnnouncementInstallationAuthority
	Runner    *AgentAnnouncementRunner
	Reader    AgentAnnouncementDocumentAuthority
}

func (p AgentAnnouncementDraftPreview) PreviewAgentAnnouncement(ctx context.Context, actor AgentAnnouncementActor, draft AgentAnnouncementDraft) (AgentAnnouncementRunResult, bool, error) {
	if ctx == nil || p.Authority == nil || p.Reader == nil || p.Runner == nil || p.Runner.Documents == nil || p.Runner.Model == nil || p.Runner.Public == nil || p.Runner.Now == nil {
		return AgentAnnouncementRunResult{}, false, ErrAgentAnnouncementUnavailable
	}
	if validateAgentAnnouncementDraft(draft) != nil {
		return AgentAnnouncementRunResult{}, false, ErrAgentAnnouncementInvalid
	}
	allowed, err := p.Authority.ManageAgentInstallation(ctx, actor, draft.InstallationID, draft.PersonaID, draft.ConversationID)
	if err != nil || !allowed {
		return AgentAnnouncementRunResult{}, false, ErrAgentAnnouncementDenied
	}
	record := agentstore.Announcement{TenantID: actor.TenantUUID, TenantKey: actor.TenantID, ID: draft.ID, InstallationID: draft.InstallationID, PersonaID: draft.PersonaID, ConversationID: draft.ConversationID, OwnerID: actor.SubjectID, Instruction: draft.Instruction, Documents: slices.Clone(draft.Documents), Zone: draft.Schedule.Zone, State: agentstore.AnnouncementActive, Revision: 1}
	if draft.ExpectedRevision > 0 {
		record.Revision = draft.ExpectedRevision
	}
	runner := *p.Runner
	ctx = context.WithValue(ctx, announcementPreviewKey{}, true)
	runner.Documents = announcementPreviewDocuments{service: p.Runner.Documents, reader: p.Reader, actor: actor}
	result, err := runner.runRecord(ctx, record, "preview:"+draft.ID, true)
	if errors.Is(err, ErrAgentAnnouncementNotPublic) {
		return result, false, nil
	}
	return result, err == nil, err
}

type announcementPreviewDocuments struct {
	service AgentAnnouncementDocumentResolver
	reader  AgentAnnouncementDocumentAuthority
	actor   AgentAnnouncementActor
}

func (p announcementPreviewDocuments) ResolveAnnouncementDocuments(ctx context.Context, tenant, installation string, refs []agentdocref.Reference) ([]AgentAnnouncementResolvedDocument, error) {
	documents, err := p.service.ResolveAnnouncementDocuments(ctx, tenant, installation, refs)
	if err != nil {
		return nil, err
	}
	for _, doc := range documents {
		if p.reader.AuthorizeDocumentRead(ctx, tenant, doc.DocumentID, doc.Version, doc.Digest, doc.SectionAnchor, p.actor.TenantID, p.actor.SubjectID) != nil {
			return nil, ErrAgentAnnouncementDenied
		}
	}
	return documents, nil
}

var _ AgentAnnouncementPreviewer = AgentAnnouncementDraftPreview{}
