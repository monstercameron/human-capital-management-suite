package application

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

// ErrAgentAnnouncementAgentGone is joined to ErrAgentAnnouncementUnavailable
// when an announcement's agent is not installed in its conversation any more,
// so the list can show the record with a plain label and a Delete instead of
// leaving it out.
var ErrAgentAnnouncementAgentGone = errors.New("application: the announcement's agent is no longer in its conversation")

// announcementRetiredReason is stored as the last reason of a schedule that was
// paused because its installation was retired and nothing replaced it.
const announcementRetiredReason = "The agent is no longer in this conversation. The schedule was paused."

// AgentAnnouncementInstallationRetirement lets an authority say that the
// installation an announcement was made with no longer exists, which is what
// allows its owner to delete the record.
type AgentAnnouncementInstallationRetirement interface {
	AgentInstallationRetired(ctx context.Context, actor AgentAnnouncementActor, installation, persona, conversation string) (bool, error)
}

// AgentAnnouncementInstallationLookup names the installation of an agent that is
// active in a conversation now.
type AgentAnnouncementInstallationLookup interface {
	AgentAnnouncementCurrentInstallation(ctx context.Context, actor AgentAnnouncementActor, record agentstore.Announcement) (installationID string, found bool, err error)
}

func announcementInstallationRetired(ctx context.Context, authority AgentAnnouncementInstallationAuthority, actor AgentAnnouncementActor, record agentstore.Announcement) bool {
	retirement, ok := authority.(AgentAnnouncementInstallationRetirement)
	if !ok {
		return false
	}
	retired, err := retirement.AgentInstallationRetired(ctx, actor, record.InstallationID, record.PersonaID, record.ConversationID)
	return err == nil && retired
}

// AgentInstallationRetired reports whether the installation is gone from the
// conversation. Only the signed-in owner of the record asks, never a stranger.
func (a AgentAnnouncementOwnerAuthority) AgentInstallationRetired(ctx context.Context, actor AgentAnnouncementActor, installation, persona, conversation string) (bool, error) {
	current, err := a.AgentAnnouncementActor(ctx)
	if err != nil || current != actor || a.Personas == nil {
		return false, ErrAgentAnnouncementDenied
	}
	p, _ := trust.FromContext(ctx)
	scoped, err := a.Personas.Scoped(p.Tenant())
	if err != nil {
		return false, err
	}
	placements, err := scoped.ListActiveInstallations(ctx, conversation)
	if err != nil {
		return false, err
	}
	for _, placement := range placements {
		if placement.InstallationID == installation && placement.PersonaID == persona {
			return false, nil
		}
	}
	return true, nil
}

// AgentAnnouncementCurrentInstallation finds the active installation of the
// record's agent in the record's conversation.
func (s AgentAnnouncementCatalogNames) AgentAnnouncementCurrentInstallation(ctx context.Context, actor AgentAnnouncementActor, record agentstore.Announcement) (string, bool, error) {
	if s.Installations == nil || record.TenantID != actor.TenantUUID || record.OwnerID != actor.SubjectID {
		return "", false, ErrAgentAnnouncementDenied
	}
	placements, err := s.Installations.ListPersonaCatalogInstallations(ctx, values.TenantId(actor.TenantID))
	if err != nil {
		return "", false, err
	}
	for _, placement := range placements {
		if placement.Active && placement.PersonaID == record.PersonaID && placement.ConversationID == record.ConversationID && placement.ID != "" {
			return placement.ID, true, nil
		}
	}
	return "", false, nil
}

var _ AgentAnnouncementInstallationLookup = AgentAnnouncementCatalogNames{}
var _ AgentAnnouncementInstallationRetirement = AgentAnnouncementOwnerAuthority{}

// FollowInstallation keeps a scheduled announcement working after its agent was
// installed again: the schedule moves to the installation that is active now, or
// is paused with a reason when the agent is no longer in the conversation. A
// paused schedule is moved but stays paused; a one-time announcement and a
// deleted one have no schedule to keep.
func (s *AgentAnnouncementService) FollowInstallation(ctx context.Context, actor AgentAnnouncementActor, record agentstore.Announcement, current string, found bool) (agentstore.Announcement, error) {
	if !s.available() || ctx == nil {
		return record, ErrAgentAnnouncementUnavailable
	}
	paused := record.State == agentstore.AnnouncementPaused
	if record.OwnerID != actor.SubjectID || record.State != agentstore.AnnouncementActive && !paused || record.Cadence == "NOW" || record.Cadence == "ONCE" {
		return record, nil
	}
	if found && current == record.InstallationID || paused && !found {
		return record, nil
	}
	if found && strings.TrimSpace(current) != "" {
		draft := announcementDraftFromRecord(record)
		draft.InstallationID = current
		draft.Schedule.Cadence, draft.Schedule.MonthDay, draft.Schedule.At = record.Cadence, int(record.MonthDay), record.LocalTime
		for _, day := range record.Weekdays {
			draft.Schedule.Weekdays = append(draft.Schedule.Weekdays, time.Weekday(day))
		}
		moved, err := s.Update(ctx, actor, draft)
		if err != nil || !paused {
			return moved, err
		}
		// Saving a schedule starts it; a schedule the owner had paused stays paused.
		projected := moved
		projected.State, projected.Revision = agentstore.AnnouncementPaused, moved.Revision
		if err := s.Schedules.PauseAnnouncementSchedule(context.WithValue(ctx, announcementDraftKey{}, projected), actor, moved.SchedulerID, moved.Revision); err != nil {
			return moved, err
		}
		return moved, nil
	}
	projected := record
	projected.State, projected.Revision = agentstore.AnnouncementPaused, record.Revision+1
	pauseCtx := context.WithValue(ctx, announcementDraftKey{}, projected)
	if err := s.Schedules.PauseAnnouncementSchedule(pauseCtx, actor, record.SchedulerID, record.Revision); err != nil {
		return record, err
	}
	record.State, record.Revision, record.UpdatedAt = agentstore.AnnouncementPaused, record.Revision+1, s.Now().UTC()
	record.NextRunAt, record.LastReason = nil, announcementRetiredReason
	if err := s.Store.Save(ctx, record, projected.Revision-1); err != nil {
		return record, err
	}
	return record, nil
}
