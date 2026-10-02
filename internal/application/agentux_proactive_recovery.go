package application

import (
	"context"
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentrunstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
)

// Recover the durable public receipt when a crash followed the chat commit but
// preceded the occurrence projection. Replay never asks the model to post again.
func (r *AgentAnnouncementRuntime) RecoverAnnouncementOccurrence(ctx context.Context, definition agentstore.Announcement, occurrence string) (string, bool, error) {
	repo, err := r.admissionRepository(definition.TenantKey)
	if err != nil {
		return "", false, err
	}
	id, err := agentrun.AdmissionRequestID(agentrun.SourceIdentity{TenantID: definition.TenantKey, Kind: agentrun.SourceAnnouncement, Key: occurrence})
	if err != nil {
		return "", false, err
	}
	admission, err := repo.GetByID(ctx, id)
	if errors.Is(err, agentrunstore.ErrAdmissionNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if admission.Decision != agentrun.DecisionAccepted || admission.Request.Context.ID != definition.ID || admission.Request.InstallationID != definition.InstallationID || admission.Request.Principal.RequesterID != definition.OwnerID || admission.Request.Persona == nil || admission.Request.Persona.ID != definition.PersonaID || admission.Request.Audience.ID != definition.ConversationID {
		return "", false, ErrAgentAnnouncementDenied
	}
	var messageID, body string
	err = r.Chat.RunTenantTx(ctx, definition.TenantKey, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT p.id,p.body FROM chat_idempotency i JOIN chat_post p ON p.tenant_id=i.tenant_id AND p.id=i.post_id WHERE i.tenant_id=$1 AND i.conversation_id=$2 AND i.client_key=$3 AND p.author_id=$4 AND p.author_home_tenant_id=$1 AND COALESCE(p.parent_id,'')=''`, definition.TenantKey, definition.ConversationID, id, definition.PersonaID).Scan(&messageID, &body)
	})
	if errors.Is(err, dbport.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if !strings.HasPrefix(body, chatui.AgentAnnouncementBodyPrefix) {
		return "", false, ErrAgentAnnouncementDenied
	}
	return messageID, true, nil
}
