package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
)

func (s *AgentAnnouncementControlSurface) applyCommand(ctx context.Context, actor AgentAnnouncementActor, id, key, action string, payload any, fn func() error) error {
	data, err := json.Marshal([]any{action, payload})
	if err != nil {
		return ErrAgentAnnouncementInvalid
	}
	sum := sha256.Sum256(data)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	return s.Service.Store.WithAnnouncementCommandFence(ctx, actor.TenantUUID, id, actor.SubjectID, key, func() error {
		current, err := s.Service.Store.Get(ctx, actor.TenantUUID, id)
		if err != nil && !errors.Is(err, agentstore.ErrAnnouncementNotFound) {
			return err
		}
		if err == nil {
			if current.OwnerID != actor.SubjectID {
				return ErrAgentAnnouncementDenied
			}
			allowed, err := s.Service.Authority.ManageAgentInstallation(ctx, actor, current.InstallationID, current.PersonaID, current.ConversationID)
			if err != nil || !allowed {
				// The owner may still delete a record whose installation was retired.
				if action != "DELETE" || !announcementInstallationRetired(ctx, s.Service.Authority, actor, current) {
					return ErrAgentAnnouncementDenied
				}
			}
		}
		seen, err := s.Service.Store.CheckCommand(ctx, actor.TenantUUID, actor.SubjectID, key, id, digest)
		if err != nil || seen {
			return err
		}
		if err := fn(); err != nil {
			return err
		}
		return s.Service.Store.RecordCommand(ctx, actor.TenantUUID, actor.SubjectID, key, id, digest)
	})
}
