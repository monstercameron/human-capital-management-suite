package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/agentcontrols"
)

type AgentAnnouncementActorSource interface {
	AgentAnnouncementActor(context.Context) (AgentAnnouncementActor, error)
}

type AgentAnnouncementNameSource interface {
	AgentAnnouncementNames(context.Context, AgentAnnouncementActor, agentstore.Announcement) (agentName, conversationName, ownerName string, err error)
	AgentAnnouncementChoices(context.Context, AgentAnnouncementActor) ([]productui.AgentAnnouncementAgent, error)
}

type AgentAnnouncementPreviewer interface {
	PreviewAgentAnnouncement(context.Context, AgentAnnouncementActor, AgentAnnouncementDraft) (AgentAnnouncementRunResult, bool, error)
}

type AgentAnnouncementControlSurface struct {
	Service    *AgentAnnouncementService
	Runner     *AgentAnnouncementRunner
	Actors     AgentAnnouncementActorSource
	Names      AgentAnnouncementNameSource
	Preview    AgentAnnouncementPreviewer
	Now        func() time.Time
	previewsMu sync.Mutex
	previews   map[string]announcementSavedPreview
}

func (s *AgentAnnouncementControlSurface) actor(ctx context.Context) (AgentAnnouncementActor, error) {
	if s == nil || s.Service == nil || s.Actors == nil || s.Now == nil || ctx == nil {
		return AgentAnnouncementActor{}, agentcontrols.ErrUnavailable
	}
	actor, err := s.Actors.AgentAnnouncementActor(ctx)
	if err != nil {
		return AgentAnnouncementActor{}, agentcontrols.ErrUnauthenticated
	}
	if actor.TenantUUID.String() == "00000000-0000-0000-0000-000000000000" || strings.TrimSpace(actor.SubjectID) == "" {
		return AgentAnnouncementActor{}, agentcontrols.ErrUnauthenticated
	}
	return actor, nil
}

func (s *AgentAnnouncementControlSurface) ListAnnouncements(ctx context.Context) (agentcontrols.AnnouncementReply, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return agentcontrols.AnnouncementReply{}, err
	}
	if s.Names == nil {
		return agentcontrols.AnnouncementReply{}, agentcontrols.ErrUnavailable
	}
	records, err := s.Service.List(ctx, actor)
	if err != nil {
		return agentcontrols.AnnouncementReply{}, announcementTransportError(err)
	}
	choices, err := s.Names.AgentAnnouncementChoices(ctx, actor)
	if err != nil {
		return agentcontrols.AnnouncementReply{}, agentcontrols.ErrUnavailable
	}
	snapshot := productui.AgentAnnouncementsSnapshot{Available: true, CanCreate: len(choices) > 0, Agents: choices}
	for _, record := range records {
		agentName, conversationName, ownerName, nameErr := s.Names.AgentAnnouncementNames(ctx, actor, record)
		if nameErr != nil || strings.TrimSpace(agentName) == "" || strings.TrimSpace(conversationName) == "" {
			// One announcement whose agent or conversation can no longer be
			// named (the agent was removed from that conversation) must not
			// take the whole list away from its owner: it is left out.
			continue
		}
		row := productui.AgentAnnouncementRow{ID: record.ID, AgentName: agentName, ConversationName: conversationName, Instruction: record.Instruction, State: record.State, Revision: record.Revision, ResultCode: record.LastResult, Reason: record.LastReason, LastOccurrence: record.LastOccurrence, OwnerName: ownerName}
		row.Editor = productui.AgentAnnouncementEditorValue{InstallationID: record.InstallationID, PersonaID: record.PersonaID, ConversationID: record.ConversationID, Instruction: record.Instruction, Cadence: record.Cadence, Time: record.LocalTime.Format("15:04"), Zone: record.Zone, MonthDay: int(record.MonthDay), Documents: record.Documents}
		for _, day := range record.Weekdays {
			row.Editor.Weekdays = append(row.Editor.Weekdays, int(day))
		}
		if record.LastAttemptedAt != nil {
			row.LastRunAt = record.LastAttemptedAt.UTC().Format(time.RFC3339)
		}
		if record.NextRunAt != nil {
			row.NextRunAt = record.NextRunAt.UTC().Format(time.RFC3339)
		}
		if record.LastMessageID != "" {
			row.MessageHref = "/workspace/app/chat?" + url.Values{"conversation": {record.ConversationID}, "message": {record.LastMessageID}}.Encode()
		}
		if s.Runner != nil {
			if history, ok := s.Runner.Delivery.(interface {
				AnnouncementAttempts(context.Context, uuid.UUID, string) ([]agentstore.AnnouncementOccurrence, error)
			}); ok {
				attempts, err := history.AnnouncementAttempts(ctx, actor.TenantUUID, record.ID)
				if err != nil {
					return agentcontrols.AnnouncementReply{}, announcementTransportError(err)
				}
				for _, attempt := range attempts {
					entry := productui.AgentAnnouncementAttempt{At: attempt.AttemptedAt.UTC().Format(time.RFC3339), ResultCode: attempt.Result, Reason: attempt.Reason}
					if attempt.MessageID != "" {
						entry.MessageHref = "/workspace/app/chat?" + url.Values{"conversation": {record.ConversationID}, "message": {attempt.MessageID}}.Encode()
					}
					row.Attempts = append(row.Attempts, entry)
				}
			}
		}
		snapshot.Rows = append(snapshot.Rows, row)
	}
	return agentcontrols.AnnouncementReply{Snapshot: snapshot}, nil
}

func (s *AgentAnnouncementControlSurface) PreviewAnnouncement(ctx context.Context, input agentcontrols.AnnouncementDraft) (agentcontrols.AnnouncementReply, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return agentcontrols.AnnouncementReply{}, err
	}
	if s.Preview == nil || !validAnnouncementKey(input.IdempotencyKey) {
		return agentcontrols.AnnouncementReply{}, agentcontrols.ErrInvalid
	}
	definitionID := input.ID
	if definitionID == "" {
		definitionID = announcementID(actor, input.IdempotencyKey)
	}
	input.ID = announcementID(actor, "preview:"+input.IdempotencyKey+":"+uuid.NewString())
	draft, err := transportAnnouncementDraft(actor, input, s.Now())
	if err != nil {
		return agentcontrols.AnnouncementReply{}, err
	}
	result, public, err := s.Preview.PreviewAgentAnnouncement(ctx, actor, draft)
	if err != nil {
		return agentcontrols.AnnouncementReply{}, announcementTransportError(err)
	}
	status := "This message can be posted publicly."
	if !public {
		status = "This message will not be posted because not everyone can read its sources."
	}
	sources := make([]agentcontrols.AnnouncementPreviewSource, 0, len(result.Sources))
	for _, source := range result.Sources {
		sources = append(sources, agentcontrols.AnnouncementPreviewSource{Title: source.Title, Href: "/workspace/app/docs?" + url.Values{"document": {source.DocumentID}}.Encode()})
	}
	// The execution uses an ephemeral draft; its cached result belongs only to
	// the definition being edited or to this new form's stable creation key.
	draft.ID = definitionID
	digest := s.savePreview(actor, draft, result, public)
	return agentcontrols.AnnouncementReply{Preview: &agentcontrols.AnnouncementPreview{Text: result.Text, Sources: sources, Public: public, PublicStatus: status, Digest: digest}}, nil
}

func (s *AgentAnnouncementControlSurface) CreateAnnouncement(ctx context.Context, input agentcontrols.AnnouncementDraft) (agentcontrols.AnnouncementReply, error) {
	ctx = withAnnouncementPreviewDigest(ctx, input.PreviewDigest)
	actor, err := s.actor(ctx)
	if err != nil {
		return agentcontrols.AnnouncementReply{}, err
	}
	if !validAnnouncementKey(input.IdempotencyKey) {
		return agentcontrols.AnnouncementReply{}, agentcontrols.ErrInvalid
	}
	input.ID = announcementID(actor, input.IdempotencyKey)
	draft, err := transportAnnouncementDraft(actor, input, s.Now())
	if err != nil {
		return agentcontrols.AnnouncementReply{}, err
	}
	ctx = s.withSavedPreview(ctx, actor, draft, input.PreviewDigest)
	var record agentstore.Announcement
	createErr := s.applyCommand(ctx, actor, input.ID, input.IdempotencyKey, "CREATE", input, func() error {
		var err error
		record, err = s.Service.Create(ctx, actor, draft)
		return err
	})
	if createErr != nil {
		return agentcontrols.AnnouncementReply{}, announcementTransportError(createErr)
	}
	if draft.Schedule.Cadence == "NOW" || draft.Schedule.Cadence == "ONCE" {
		if s.Runner == nil {
			return agentcontrols.AnnouncementReply{}, agentcontrols.ErrUnavailable
		}
		if record.ID == "" {
			record, err = s.Service.Owned(ctx, actor, input.ID, 1)
			if err != nil {
				return agentcontrols.AnnouncementReply{}, announcementTransportError(err)
			}
		}
		if _, err := s.Service.Owned(ctx, actor, record.ID, record.Revision); err != nil {
			return agentcontrols.AnnouncementReply{}, announcementTransportError(err)
		}
		occurrence := announcementManualOccurrence(record.ID, input.IdempotencyKey)
		if _, err := s.Runner.RunOccurrence(ctx, actor.TenantUUID, record.ID, occurrence, false); err != nil {
			return s.projectAnnouncementRun(ctx, actor, record.ID, occurrence, err)
		}
	}
	return s.ListAnnouncements(ctx)
}

func (s *AgentAnnouncementControlSurface) UpdateAnnouncement(ctx context.Context, input agentcontrols.AnnouncementDraft) (agentcontrols.AnnouncementReply, error) {
	actor, err := s.actor(ctx)
	if err != nil {
		return agentcontrols.AnnouncementReply{}, err
	}
	if !validAnnouncementKey(input.IdempotencyKey) {
		return agentcontrols.AnnouncementReply{}, agentcontrols.ErrInvalid
	}
	draft, err := transportAnnouncementDraft(actor, input, s.Now())
	if err != nil {
		return agentcontrols.AnnouncementReply{}, err
	}
	if err := s.applyCommand(ctx, actor, input.ID, input.IdempotencyKey, "UPDATE", input, func() error { _, err := s.Service.Update(ctx, actor, draft); return err }); err != nil {
		return agentcontrols.AnnouncementReply{}, announcementTransportError(err)
	}
	return s.ListAnnouncements(ctx)
}

func (s *AgentAnnouncementControlSurface) ControlAnnouncement(ctx context.Context, command agentcontrols.AnnouncementCommand) (agentcontrols.AnnouncementReply, error) {
	ctx = withAnnouncementPreviewDigest(ctx, command.PreviewDigest)
	actor, err := s.actor(ctx)
	if err != nil {
		return agentcontrols.AnnouncementReply{}, err
	}
	if !validAnnouncementKey(command.IdempotencyKey) || strings.TrimSpace(command.ID) == "" || command.ExpectedRevision == 0 {
		return agentcontrols.AnnouncementReply{}, agentcontrols.ErrInvalid
	}
	switch strings.ToUpper(command.Action) {
	case "PAUSE":
		err = s.applyCommand(ctx, actor, command.ID, command.IdempotencyKey, "PAUSE", command, func() error { _, err := s.Service.Pause(ctx, actor, command.ID, command.ExpectedRevision); return err })
	case "RESUME":
		err = s.applyCommand(ctx, actor, command.ID, command.IdempotencyKey, "RESUME", command, func() error { _, err := s.Service.Resume(ctx, actor, command.ID, command.ExpectedRevision); return err })
	case "DELETE":
		err = s.applyCommand(ctx, actor, command.ID, command.IdempotencyKey, "DELETE", command, func() error { _, err := s.Service.Delete(ctx, actor, command.ID, command.ExpectedRevision); return err })
	case "POST_NOW":
		if s.Runner == nil {
			return agentcontrols.AnnouncementReply{}, agentcontrols.ErrUnavailable
		}
		record, ownErr := s.Service.Owned(ctx, actor, command.ID, command.ExpectedRevision)
		if ownErr != nil {
			return agentcontrols.AnnouncementReply{}, announcementTransportError(ownErr)
		}
		ctx = s.withSavedPreview(ctx, actor, announcementDraftFromRecord(record), command.PreviewDigest)
		if command.RetryOccurrence != "" {
			prior, found, err := s.Service.Store.GetOccurrence(ctx, actor.TenantUUID, record.ID, command.RetryOccurrence)
			if err != nil || !found || prior.Result != agentstore.AnnouncementFailed {
				return agentcontrols.AnnouncementReply{}, agentcontrols.ErrInvalid
			}
			record.LastResult, record.LastOccurrence = prior.Result, prior.OccurrenceID
		}
		recovered, recoverErr := s.Runner.retryCommittedAnnouncement(ctx, record, announcementManualOccurrence(command.ID, command.IdempotencyKey))
		if recoverErr != nil {
			return agentcontrols.AnnouncementReply{}, announcementTransportError(recoverErr)
		}
		if recovered {
			return s.ListAnnouncements(ctx)
		}
		_, err = s.Runner.RunOccurrence(ctx, actor.TenantUUID, command.ID, announcementManualOccurrence(command.ID, command.IdempotencyKey), false)
	default:
		return agentcontrols.AnnouncementReply{}, agentcontrols.ErrInvalid
	}
	if err != nil && strings.ToUpper(command.Action) == "POST_NOW" {
		return s.projectAnnouncementRun(ctx, actor, command.ID, announcementManualOccurrence(command.ID, command.IdempotencyKey), err)
	}
	if err != nil {
		return agentcontrols.AnnouncementReply{}, announcementTransportError(err)
	}
	return s.ListAnnouncements(ctx)
}

func announcementManualOccurrence(id, key string) string { return "now:" + id + ":" + key }

func (s *AgentAnnouncementControlSurface) projectAnnouncementRun(ctx context.Context, actor AgentAnnouncementActor, id, occurrence string, runErr error) (agentcontrols.AnnouncementReply, error) {
	prior, found, err := s.Service.Store.GetOccurrence(ctx, actor.TenantUUID, id, occurrence)
	if err == nil && found && (prior.Result == agentstore.AnnouncementRefused || prior.Result == agentstore.AnnouncementFailed) {
		return s.ListAnnouncements(ctx)
	}
	return agentcontrols.AnnouncementReply{}, announcementTransportError(errors.Join(runErr, err))
}

func transportAnnouncementDraft(actor AgentAnnouncementActor, input agentcontrols.AnnouncementDraft, now time.Time) (AgentAnnouncementDraft, error) {
	if strings.TrimSpace(input.ID) == "" || strings.TrimSpace(input.InstallationID) == "" || strings.TrimSpace(input.PersonaID) == "" || strings.TrimSpace(input.ConversationID) == "" {
		return AgentAnnouncementDraft{}, agentcontrols.ErrInvalid
	}
	at := now
	if strings.ToUpper(input.Cadence) != "NOW" && strings.ToUpper(input.Cadence) != "ONCE" || input.Time != "" {
		clock, err := time.Parse("15:04", input.Time)
		if err != nil {
			return AgentAnnouncementDraft{}, agentcontrols.ErrInvalid
		}
		at = time.Date(2000, 1, 1, clock.Hour(), clock.Minute(), 0, 0, time.UTC)
	}
	weekdays := make([]time.Weekday, 0, len(input.Weekdays))
	for _, day := range input.Weekdays {
		weekdays = append(weekdays, time.Weekday(day))
	}
	draft := AgentAnnouncementDraft{ID: input.ID, InstallationID: input.InstallationID, PersonaID: input.PersonaID, ConversationID: input.ConversationID, Instruction: input.Instruction, Documents: input.Documents, ExpectedRevision: input.ExpectedRevision, Schedule: AgentAnnouncementSchedule{Cadence: strings.ToUpper(input.Cadence), Weekdays: weekdays, MonthDay: input.MonthDay, At: at, Zone: input.Zone}}
	if validateAgentAnnouncementDraft(draft) != nil || actor.TenantUUID.String() == "00000000-0000-0000-0000-000000000000" {
		return AgentAnnouncementDraft{}, agentcontrols.ErrInvalid
	}
	return draft, nil
}

func validAnnouncementKey(value string) bool {
	return len(value) >= 8 && len(value) <= 128 && strings.TrimSpace(value) == value
}

func announcementID(actor AgentAnnouncementActor, key string) string {
	sum := sha256.Sum256([]byte(actor.TenantID + "\x00" + actor.SubjectID + "\x00" + key))
	return "announcement-" + hex.EncodeToString(sum[:12])
}

func announcementTransportError(err error) error {
	switch {
	case errors.Is(err, ErrAgentAnnouncementDenied):
		return errors.Join(agentcontrols.ErrDenied, err)
	case errors.Is(err, ErrAgentAnnouncementInvalid):
		return errors.Join(agentcontrols.ErrInvalid, err)
	case errors.Is(err, agentstore.ErrAnnouncementRevision):
		return errors.Join(agentcontrols.ErrConflict, err)
	case errors.Is(err, ErrAgentAnnouncementNotPublic):
		return errors.Join(agentcontrols.ErrDenied, err)
	case err != nil:
		return errors.Join(agentcontrols.ErrUnavailable, err)
	default:
		return nil
	}
}

var _ agentcontrols.AnnouncementSurface = (*AgentAnnouncementControlSurface)(nil)
