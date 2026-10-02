package application

import (
	"context"
	"encoding/json"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"time"
)

const announcementPreviewLifetime = 10 * time.Minute
const announcementPreviewLimit = 256

type announcementSavedPreview struct {
	Result  AgentAnnouncementRunResult
	Binding string
	Until   time.Time
}
type announcementSavedPreviewKey struct{}

func announcementDraftFromRecord(r agentstore.Announcement) AgentAnnouncementDraft {
	return AgentAnnouncementDraft{ID: r.ID, InstallationID: r.InstallationID, PersonaID: r.PersonaID, ConversationID: r.ConversationID, Instruction: r.Instruction, Documents: r.Documents, Schedule: AgentAnnouncementSchedule{Zone: r.Zone}, ExpectedRevision: r.Revision}
}

func announcementPreviewBinding(actor AgentAnnouncementActor, draft AgentAnnouncementDraft) string {
	raw, _ := json.Marshal([]any{actor, draft.ID, draft.InstallationID, draft.PersonaID, draft.ConversationID, draft.Instruction, draft.Documents, draft.Schedule.Zone, draft.ExpectedRevision})
	return personaRunBytesDigest(raw)
}

// Only opaque, gateway-sealed server output enters this bounded cache. A restart
// or expiry asks for a new preview; a client never supplies announcement text.
func (s *AgentAnnouncementControlSurface) savePreview(actor AgentAnnouncementActor, draft AgentAnnouncementDraft, result AgentAnnouncementRunResult, public bool) string {
	if !public || result.Output.Digest() == "" {
		return ""
	}
	binding := announcementPreviewBinding(actor, draft)
	digest := personaRunBytesDigest([]byte(binding + "\x00" + result.Output.Digest()))
	s.previewsMu.Lock()
	defer s.previewsMu.Unlock()
	if s.previews == nil {
		s.previews = map[string]announcementSavedPreview{}
	}
	for key, preview := range s.previews {
		if !preview.Until.After(s.Now()) {
			delete(s.previews, key)
		}
	}
	if len(s.previews) >= announcementPreviewLimit {
		var oldest string
		for key, preview := range s.previews {
			if oldest == "" || preview.Until.Before(s.previews[oldest].Until) {
				oldest = key
			}
		}
		delete(s.previews, oldest)
	}
	s.previews[digest] = announcementSavedPreview{Result: result, Binding: binding, Until: s.Now().Add(announcementPreviewLifetime)}
	return digest
}

func (s *AgentAnnouncementControlSurface) withSavedPreview(ctx context.Context, actor AgentAnnouncementActor, draft AgentAnnouncementDraft, digest string) context.Context {
	if digest == "" {
		return ctx
	}
	s.previewsMu.Lock()
	preview, ok := s.previews[digest]
	s.previewsMu.Unlock()
	if !ok || !preview.Until.After(s.Now()) || preview.Binding != announcementPreviewBinding(actor, draft) {
		return ctx
	}
	return context.WithValue(ctx, announcementSavedPreviewKey{}, preview.Result)
}
