package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/monstercameron/human-capital-management-suite/internal/agentdocref"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsecurity"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
)

var (
	ErrAgentAnnouncementInvalid     = errors.New("application: invalid agent announcement")
	ErrAgentAnnouncementDenied      = errors.New("application: agent announcement access denied")
	ErrAgentAnnouncementNotPublic   = errors.New("application: announcement documents are not public to the conversation")
	ErrAgentAnnouncementUnavailable = errors.New("application: agent announcement service unavailable")
)

// AgentAnnouncementActor is derived from verified request context. TenantUUID
// is the RLS identity; TenantID and SubjectID are the business identities.
type AgentAnnouncementActor struct {
	TenantID   string
	TenantUUID uuid.UUID
	SubjectID  string
}

type AgentAnnouncementSchedule struct {
	Cadence  string
	Weekdays []time.Weekday
	MonthDay int
	At       time.Time
	Zone     string
}

type AgentAnnouncementDraft struct {
	ID, InstallationID, PersonaID, ConversationID, Instruction string
	Documents                                                  []agentdocref.Reference
	Schedule                                                   AgentAnnouncementSchedule
	ExpectedRevision                                           uint64
}

// AgentAnnouncementCron translates the bounded owner choices into the cron
// source consumed by the existing scheduling engine. It does no date math.
func AgentAnnouncementCron(schedule AgentAnnouncementSchedule) (string, error) {
	if schedule.At.IsZero() || strings.TrimSpace(schedule.Zone) == "" {
		return "", ErrAgentAnnouncementInvalid
	}
	if _, err := time.LoadLocation(schedule.Zone); err != nil {
		return "", errors.Join(ErrAgentAnnouncementInvalid, err)
	}
	minute, hour := schedule.At.Minute(), schedule.At.Hour()
	switch schedule.Cadence {
	case "DAILY":
		return fmt.Sprintf("%d %d * * *", minute, hour), nil
	case "WEEKLY":
		if len(schedule.Weekdays) == 0 || len(schedule.Weekdays) > 7 {
			return "", ErrAgentAnnouncementInvalid
		}
		days := make([]int, 0, len(schedule.Weekdays))
		for _, day := range schedule.Weekdays {
			value := int(day)
			if value < 0 || value > 6 || slices.Contains(days, value) {
				return "", ErrAgentAnnouncementInvalid
			}
			days = append(days, value)
		}
		sort.Ints(days)
		values := make([]string, 0, len(days))
		for _, day := range days {
			values = append(values, strconv.Itoa(day))
		}
		return fmt.Sprintf("%d %d * * %s", minute, hour, strings.Join(values, ",")), nil
	case "MONTHLY":
		if schedule.MonthDay < 1 || schedule.MonthDay > 31 {
			return "", ErrAgentAnnouncementInvalid
		}
		return fmt.Sprintf("%d %d %d * *", minute, hour, schedule.MonthDay), nil
	case "NOW", "ONCE":
		return "", nil
	default:
		return "", ErrAgentAnnouncementInvalid
	}
}

func validateAgentAnnouncementDraft(draft AgentAnnouncementDraft) error {
	if strings.TrimSpace(draft.ID) == "" || strings.TrimSpace(draft.InstallationID) == "" || strings.TrimSpace(draft.PersonaID) == "" ||
		strings.TrimSpace(draft.ConversationID) == "" || strings.TrimSpace(draft.Instruction) == "" || !utf8.ValidString(draft.Instruction) || len([]rune(draft.Instruction)) > 1000 || len(draft.Documents) == 0 && draft.PersonaID != AgentUXDemoBirthdayPersonaID || draft.PersonaID == AgentUXDemoBirthdayPersonaID && len(draft.Documents) != 0 {
		return ErrAgentAnnouncementInvalid
	}
	if err := agentdocref.Validate(draft.Documents, agentdocref.MaxRequestReferences); err != nil {
		return errors.Join(ErrAgentAnnouncementInvalid, err)
	}
	_, err := AgentAnnouncementCron(draft.Schedule)
	if draft.Schedule.Cadence != "WEEKLY" && len(draft.Schedule.Weekdays) != 0 || draft.Schedule.Cadence != "MONTHLY" && draft.Schedule.MonthDay != 0 {
		return ErrAgentAnnouncementInvalid
	}
	return err
}

type agentAnnouncementRepository interface {
	Create(context.Context, agentstore.Announcement) error
	Save(context.Context, agentstore.Announcement, uint64) error
	Get(context.Context, uuid.UUID, string) (agentstore.Announcement, error)
	ListOwner(context.Context, uuid.UUID, string) ([]agentstore.Announcement, error)
	RecordOccurrence(context.Context, agentstore.AnnouncementOccurrence) (bool, error)
	GetOccurrence(context.Context, uuid.UUID, string, string) (agentstore.AnnouncementOccurrence, bool, error)
	WithAnnouncementFence(context.Context, uuid.UUID, string, func() error) error
	WithAnnouncementCommandFence(context.Context, uuid.UUID, string, string, string, func() error) error
	CheckCommand(context.Context, uuid.UUID, string, string, string, string) (bool, error)
	RecordCommand(context.Context, uuid.UUID, string, string, string, string) error
}

type AgentAnnouncementInstallationAuthority interface {
	ManageAgentInstallation(context.Context, AgentAnnouncementActor, string, string, string) (bool, error)
}

type AgentAnnouncementScheduleOwner interface {
	CreateAnnouncementSchedule(context.Context, AgentAnnouncementActor, agentstore.Announcement, string) (string, *time.Time, error)
	UpdateAnnouncementSchedule(context.Context, AgentAnnouncementActor, agentstore.Announcement, string) (*time.Time, error)
	PauseAnnouncementSchedule(context.Context, AgentAnnouncementActor, string, uint64) error
	ResumeAnnouncementSchedule(context.Context, AgentAnnouncementActor, string, uint64) (*time.Time, error)
	DeleteAnnouncementSchedule(context.Context, AgentAnnouncementActor, string, uint64) error
	NextAnnouncementRun(context.Context, AgentAnnouncementActor, agentstore.Announcement) (*time.Time, error)
}

type AgentAnnouncementService struct {
	Store     agentAnnouncementRepository
	Authority AgentAnnouncementInstallationAuthority
	Schedules AgentAnnouncementScheduleOwner
	Now       func() time.Time
	Shares    AgentAnnouncementDocumentShares
}

type AgentAnnouncementDocumentShares interface {
	SetAnnouncementDocumentShares(context.Context, AgentAnnouncementActor, agentstore.Announcement) error
}

func (s *AgentAnnouncementService) available() bool {
	return s != nil && s.Store != nil && s.Authority != nil && s.Schedules != nil && s.Now != nil
}

func (s *AgentAnnouncementService) Create(ctx context.Context, actor AgentAnnouncementActor, draft AgentAnnouncementDraft) (created agentstore.Announcement, createErr error) {
	if !s.available() || ctx == nil {
		return agentstore.Announcement{}, ErrAgentAnnouncementUnavailable
	}
	if actor.TenantUUID == uuid.Nil || strings.TrimSpace(actor.TenantID) == "" || strings.TrimSpace(actor.SubjectID) == "" || validateAgentAnnouncementDraft(draft) != nil || draft.ExpectedRevision != 0 {
		return agentstore.Announcement{}, ErrAgentAnnouncementInvalid
	}
	allowed, err := s.Authority.ManageAgentInstallation(ctx, actor, draft.InstallationID, draft.PersonaID, draft.ConversationID)
	if err != nil || !allowed {
		return agentstore.Announcement{}, ErrAgentAnnouncementDenied
	}
	if _, err := s.Store.Get(ctx, actor.TenantUUID, draft.ID); err == nil {
		return agentstore.Announcement{}, agentstore.ErrAnnouncementRevision
	} else if !errors.Is(err, agentstore.ErrAnnouncementNotFound) {
		return agentstore.Announcement{}, err
	}
	cron, _ := AgentAnnouncementCron(draft.Schedule)
	now := s.Now().UTC()
	record := agentstore.Announcement{
		TenantID: actor.TenantUUID, TenantKey: actor.TenantID, ID: draft.ID, InstallationID: draft.InstallationID, PersonaID: draft.PersonaID,
		ConversationID: draft.ConversationID, Instruction: strings.TrimSpace(draft.Instruction), Documents: slices.Clone(draft.Documents),
		Cadence: draft.Schedule.Cadence, MonthDay: int16(draft.Schedule.MonthDay), LocalTime: draft.Schedule.At, Zone: draft.Schedule.Zone,
		State: agentstore.AnnouncementActive, OwnerID: actor.SubjectID, SchedulerID: "announcement:" + draft.ID, Revision: 1, CreatedAt: now, UpdatedAt: now,
	}
	for _, day := range draft.Schedule.Weekdays {
		record.Weekdays = append(record.Weekdays, int16(day))
	}
	if s.Shares != nil {
		if err := s.Shares.SetAnnouncementDocumentShares(ctx, actor, record); err != nil {
			return agentstore.Announcement{}, err
		}
		defer func() {
			if createErr != nil {
				cleanup := record
				cleanup.State = agentstore.AnnouncementDeleted
				createErr = errors.Join(createErr, s.Shares.SetAnnouncementDocumentShares(context.WithoutCancel(ctx), actor, cleanup))
			}
		}()
	}
	record.SchedulerID, record.NextRunAt, err = s.Schedules.CreateAnnouncementSchedule(ctx, actor, record, cron)
	if err != nil {
		return agentstore.Announcement{}, err
	}
	if err := s.Store.Create(ctx, record); err != nil {
		return agentstore.Announcement{}, err
	}
	return record, nil
}

func (s *AgentAnnouncementService) List(ctx context.Context, actor AgentAnnouncementActor) ([]agentstore.Announcement, error) {
	if !s.available() || ctx == nil {
		return nil, ErrAgentAnnouncementUnavailable
	}
	if actor.TenantUUID == uuid.Nil || strings.TrimSpace(actor.SubjectID) == "" {
		return nil, ErrAgentAnnouncementDenied
	}
	records, err := s.Store.ListOwner(ctx, actor.TenantUUID, actor.SubjectID)
	if err != nil {
		return nil, err
	}
	for i := range records {
		if records[i].State != agentstore.AnnouncementActive || records[i].Cadence == "NOW" || records[i].Cadence == "ONCE" {
			records[i].NextRunAt = nil
			continue
		}
		records[i].NextRunAt, err = s.Schedules.NextAnnouncementRun(ctx, actor, records[i])
		if err != nil {
			return nil, err
		}
	}
	return records, nil
}

func (s *AgentAnnouncementService) Update(ctx context.Context, actor AgentAnnouncementActor, draft AgentAnnouncementDraft) (updated agentstore.Announcement, updateErr error) {
	if !s.available() || ctx == nil {
		return agentstore.Announcement{}, ErrAgentAnnouncementUnavailable
	}
	if actor.TenantUUID == uuid.Nil || validateAgentAnnouncementDraft(draft) != nil || draft.ExpectedRevision == 0 {
		return agentstore.Announcement{}, ErrAgentAnnouncementInvalid
	}
	record, err := s.Store.Get(ctx, actor.TenantUUID, draft.ID)
	if err != nil {
		return agentstore.Announcement{}, err
	}
	if record.OwnerID != actor.SubjectID || record.State == agentstore.AnnouncementDeleted {
		return agentstore.Announcement{}, ErrAgentAnnouncementDenied
	}
	if record.Revision != draft.ExpectedRevision {
		return agentstore.Announcement{}, agentstore.ErrAnnouncementRevision
	}
	allowed, authErr := s.Authority.ManageAgentInstallation(ctx, actor, draft.InstallationID, draft.PersonaID, draft.ConversationID)
	if authErr != nil || !allowed {
		return agentstore.Announcement{}, ErrAgentAnnouncementDenied
	}
	original := record
	original.Documents = slices.Clone(record.Documents)
	original.Weekdays = slices.Clone(record.Weekdays)
	cron, _ := AgentAnnouncementCron(draft.Schedule)
	record.InstallationID, record.PersonaID, record.ConversationID = draft.InstallationID, draft.PersonaID, draft.ConversationID
	record.Instruction, record.Documents = strings.TrimSpace(draft.Instruction), slices.Clone(draft.Documents)
	record.Cadence, record.MonthDay, record.LocalTime, record.Zone = draft.Schedule.Cadence, int16(draft.Schedule.MonthDay), draft.Schedule.At, draft.Schedule.Zone
	record.Weekdays = record.Weekdays[:0]
	for _, day := range draft.Schedule.Weekdays {
		record.Weekdays = append(record.Weekdays, int16(day))
	}
	record.Revision, record.UpdatedAt = draft.ExpectedRevision+1, s.Now().UTC()
	if s.Shares != nil {
		if err := s.Shares.SetAnnouncementDocumentShares(ctx, actor, record); err != nil {
			return agentstore.Announcement{}, err
		}
		defer func() {
			if updateErr != nil {
				updateErr = errors.Join(updateErr, s.Shares.SetAnnouncementDocumentShares(context.WithoutCancel(ctx), actor, original))
			}
		}()
	}
	record.NextRunAt, err = s.Schedules.UpdateAnnouncementSchedule(ctx, actor, record, cron)
	if err != nil {
		return agentstore.Announcement{}, err
	}
	if err := s.Store.Save(ctx, record, draft.ExpectedRevision); err != nil {
		return agentstore.Announcement{}, err
	}
	return record, nil
}

func (s *AgentAnnouncementService) Owned(ctx context.Context, actor AgentAnnouncementActor, id string, expected uint64) (agentstore.Announcement, error) {
	if !s.available() || ctx == nil || actor.TenantUUID == uuid.Nil {
		return agentstore.Announcement{}, ErrAgentAnnouncementUnavailable
	}
	record, err := s.Store.Get(ctx, actor.TenantUUID, id)
	if err != nil {
		return agentstore.Announcement{}, err
	}
	if record.OwnerID != actor.SubjectID || record.State == agentstore.AnnouncementDeleted {
		return agentstore.Announcement{}, ErrAgentAnnouncementDenied
	}
	if record.Revision != expected {
		return agentstore.Announcement{}, agentstore.ErrAnnouncementRevision
	}
	allowed, authErr := s.Authority.ManageAgentInstallation(ctx, actor, record.InstallationID, record.PersonaID, record.ConversationID)
	if authErr != nil || !allowed {
		return agentstore.Announcement{}, ErrAgentAnnouncementDenied
	}
	return record, nil
}

func (s *AgentAnnouncementService) setState(ctx context.Context, actor AgentAnnouncementActor, id, state string, expected uint64) (agentstore.Announcement, error) {
	if !s.available() || ctx == nil {
		return agentstore.Announcement{}, ErrAgentAnnouncementUnavailable
	}
	record, err := s.Store.Get(ctx, actor.TenantUUID, id)
	if err != nil {
		return agentstore.Announcement{}, err
	}
	if record.OwnerID != actor.SubjectID || record.State == agentstore.AnnouncementDeleted {
		return agentstore.Announcement{}, ErrAgentAnnouncementDenied
	}
	if record.Revision != expected {
		return agentstore.Announcement{}, agentstore.ErrAnnouncementRevision
	}
	allowed, authErr := s.Authority.ManageAgentInstallation(ctx, actor, record.InstallationID, record.PersonaID, record.ConversationID)
	if authErr != nil || !allowed {
		return agentstore.Announcement{}, ErrAgentAnnouncementDenied
	}
	projected := record
	projected.State, projected.Revision = state, expected+1
	ctx = context.WithValue(ctx, announcementDraftKey{}, projected)
	switch state {
	case agentstore.AnnouncementPaused:
		err = s.Schedules.PauseAnnouncementSchedule(ctx, actor, record.SchedulerID, expected)
	case agentstore.AnnouncementActive:
		record.NextRunAt, err = s.Schedules.ResumeAnnouncementSchedule(ctx, actor, record.SchedulerID, expected)
	case agentstore.AnnouncementDeleted:
		err = s.Schedules.DeleteAnnouncementSchedule(ctx, actor, record.SchedulerID, expected)
	default:
		return agentstore.Announcement{}, ErrAgentAnnouncementInvalid
	}
	if err != nil {
		return agentstore.Announcement{}, err
	}
	record.State, record.Revision, record.UpdatedAt = state, expected+1, s.Now().UTC()
	if state != agentstore.AnnouncementActive {
		record.NextRunAt = nil
	}
	if state == agentstore.AnnouncementDeleted && s.Shares != nil {
		if err := s.Shares.SetAnnouncementDocumentShares(ctx, actor, record); err != nil {
			return agentstore.Announcement{}, err
		}
	}
	if err := s.Store.Save(ctx, record, expected); err != nil {
		return agentstore.Announcement{}, err
	}
	return record, nil
}

func (s *AgentAnnouncementService) Pause(ctx context.Context, actor AgentAnnouncementActor, id string, expected uint64) (agentstore.Announcement, error) {
	return s.setState(ctx, actor, id, agentstore.AnnouncementPaused, expected)
}
func (s *AgentAnnouncementService) Resume(ctx context.Context, actor AgentAnnouncementActor, id string, expected uint64) (agentstore.Announcement, error) {
	return s.setState(ctx, actor, id, agentstore.AnnouncementActive, expected)
}
func (s *AgentAnnouncementService) Delete(ctx context.Context, actor AgentAnnouncementActor, id string, expected uint64) (agentstore.Announcement, error) {
	return s.setState(ctx, actor, id, agentstore.AnnouncementDeleted, expected)
}

type AgentAnnouncementResolvedDocument struct {
	DocumentID, Version, Title, Content, Digest string
	SectionAnchor                               string
}

type AgentAnnouncementRunRequest struct {
	Source                                                                            *AgentAnnouncementSource
	TenantID, AnnouncementID, OccurrenceID, InstallationID, PersonaID, ConversationID string
	OwnerID, Instruction, Today, Zone                                                 string
	Documents                                                                         []AgentAnnouncementResolvedDocument
	Preview                                                                           bool
}

type AgentAnnouncementRunResult struct {
	Text             string
	CitedDocumentIDs []string
	MessageID        string
	Output           agentsecurity.FinalOutputPersistence
	Sources          []AgentAnnouncementResolvedDocument
}

type AgentAnnouncementDocumentResolver interface {
	ResolveAnnouncementDocuments(context.Context, string, string, []agentdocref.Reference) ([]AgentAnnouncementResolvedDocument, error)
}
type AgentAnnouncementModel interface {
	RunAnnouncement(context.Context, AgentAnnouncementRunRequest) (AgentAnnouncementRunResult, error)
}
type AgentAnnouncementPublicGate interface {
	AuthorizeAnnouncementDocuments(context.Context, string, string, []AgentAnnouncementResolvedDocument, []string) (bool, int, error)
}
type AgentAnnouncementDelivery interface {
	PostAnnouncement(context.Context, AgentAnnouncementRunRequest, AgentAnnouncementRunResult, string) (string, error)
}

type AgentAnnouncementRunner struct {
	Sources          AgentAnnouncementSourceProvider
	Store            agentAnnouncementRepository
	Documents        AgentAnnouncementDocumentResolver
	Model            AgentAnnouncementModel
	Public           AgentAnnouncementPublicGate
	Delivery         AgentAnnouncementDelivery
	Now              func() time.Time
	ConversationName func(context.Context, string, string) (string, error)
}

// RunOccurrence has exactly one durable result per occurrence. A replay reads
// that result before model or delivery work, so it cannot post twice.
func (r *AgentAnnouncementRunner) RunOccurrence(ctx context.Context, tenant uuid.UUID, id, occurrence string, preview bool) (AgentAnnouncementRunResult, error) {
	if r == nil || r.Store == nil || (r.Documents == nil && r.Sources == nil) || r.Model == nil || r.Public == nil || r.Delivery == nil || r.Now == nil || ctx == nil || tenant == uuid.Nil || strings.TrimSpace(occurrence) == "" {
		return AgentAnnouncementRunResult{}, ErrAgentAnnouncementUnavailable
	}
	var result AgentAnnouncementRunResult
	err := r.Store.WithAnnouncementFence(ctx, tenant, id, func() error {
		var runErr error
		result, runErr = r.runOccurrence(ctx, tenant, id, occurrence, preview)
		return runErr
	})
	return result, err
}

func (r *AgentAnnouncementRunner) runOccurrence(ctx context.Context, tenant uuid.UUID, id, occurrence string, preview bool) (AgentAnnouncementRunResult, error) {
	if !preview {
		prior, found, err := r.Store.GetOccurrence(ctx, tenant, id, occurrence)
		if err != nil {
			return AgentAnnouncementRunResult{}, err
		}
		if found {
			switch prior.Result {
			case agentstore.AnnouncementPosted:
				return AgentAnnouncementRunResult{MessageID: prior.MessageID}, nil
			case agentstore.AnnouncementRefused:
				if prior.Reason == agentuxDemoNoBirthdayReason {
					return AgentAnnouncementRunResult{}, nil
				}
				return AgentAnnouncementRunResult{}, ErrAgentAnnouncementNotPublic
			default:
				return AgentAnnouncementRunResult{}, ErrAgentAnnouncementUnavailable
			}
		}
	}
	record, err := r.Store.Get(ctx, tenant, id)
	if err != nil {
		return AgentAnnouncementRunResult{}, err
	}
	if recovery, ok := r.Delivery.(interface {
		RecoverAnnouncementOccurrence(context.Context, agentstore.Announcement, string) (string, bool, error)
	}); ok && !preview {
		message, found, err := recovery.RecoverAnnouncementOccurrence(ctx, record, occurrence)
		if err != nil {
			return AgentAnnouncementRunResult{}, err
		}
		if found {
			_, err = r.Store.RecordOccurrence(ctx, agentstore.AnnouncementOccurrence{TenantID: tenant.String(), AnnouncementID: id, OccurrenceID: occurrence, Result: agentstore.AnnouncementPosted, MessageID: message, AttemptedAt: r.Now().UTC()})
			return AgentAnnouncementRunResult{MessageID: message}, err
		}
	}
	return r.runRecord(ctx, record, occurrence, preview)
}

func (r *AgentAnnouncementRunner) runRecord(ctx context.Context, record agentstore.Announcement, occurrence string, preview bool) (result AgentAnnouncementRunResult, runErr error) {
	tenant, id := record.TenantID, record.ID
	if record.State != agentstore.AnnouncementActive && !(record.State == agentstore.AnnouncementPaused && strings.HasPrefix(occurrence, "now:")) {
		return AgentAnnouncementRunResult{}, ErrAgentAnnouncementDenied
	}
	tenantKey := record.TenantKey
	if tenantKey == "" {
		return AgentAnnouncementRunResult{}, ErrAgentAnnouncementDenied
	}
	ctx = context.WithValue(ctx, announcementDraftKey{}, record)
	var peopleSource *AgentAnnouncementSource
	var documents []AgentAnnouncementResolvedDocument
	var err error
	if record.PersonaID == AgentUXDemoBirthdayPersonaID {
		if r.Sources == nil {
			return result, ErrAgentAnnouncementUnavailable
		}
		source, sourceErr := r.Sources.ResolveAnnouncementSource(ctx, record, r.Now())
		if sourceErr != nil || source.Kind != AgentUXDemoBirthdaySourceKind {
			return result, errors.Join(ErrAgentAnnouncementDenied, sourceErr)
		}
		if len(source.People) == 0 {
			if !preview {
				_, err = r.Store.RecordOccurrence(ctx, agentstore.AnnouncementOccurrence{TenantID: tenant.String(), AnnouncementID: id, OccurrenceID: occurrence, Result: agentstore.AnnouncementRefused, Reason: agentuxDemoNoBirthdayReason, AttemptedAt: r.Now().UTC()})
			}
			return result, err
		}
		peopleSource = &source
		ctx = context.WithValue(ctx, agentuxDemoBirthdaySourceKey{}, source)
	} else {
		documents, err = r.agentuxDemoResolveDocumentSource(ctx, record)
	}
	if err != nil || len(documents) != len(record.Documents) {
		var refused AgentAnnouncementDocumentRefusal
		if errors.As(err, &refused) {
			if !preview {
				_, storeErr := r.Store.RecordOccurrence(ctx, agentstore.AnnouncementOccurrence{TenantID: tenant.String(), AnnouncementID: id, OccurrenceID: occurrence, Result: agentstore.AnnouncementRefused, Reason: r.refusalReason(ctx, tenantKey, record.ConversationID, refused.Unreadable), AttemptedAt: r.Now().UTC()})
				err = errors.Join(err, storeErr)
			}
			return AgentAnnouncementRunResult{}, err
		}
		reason := "The announcement documents could not be read with the agent's current access."
		if !preview {
			_, storeErr := r.Store.RecordOccurrence(ctx, agentstore.AnnouncementOccurrence{TenantID: tenant.String(), AnnouncementID: id, OccurrenceID: occurrence, Result: agentstore.AnnouncementFailed, Reason: reason, AttemptedAt: r.Now().UTC()})
			err = errors.Join(err, storeErr)
		}
		if preview && errors.Is(err, ErrAgentAnnouncementDenied) {
			return AgentAnnouncementRunResult{}, ErrAgentAnnouncementDenied
		}
		return AgentAnnouncementRunResult{}, errors.Join(ErrAgentAnnouncementUnavailable, err)
	}
	location, err := time.LoadLocation(record.Zone)
	if err != nil {
		return AgentAnnouncementRunResult{}, ErrAgentAnnouncementInvalid
	}
	request := AgentAnnouncementRunRequest{TenantID: tenantKey, AnnouncementID: id, OccurrenceID: occurrence, InstallationID: record.InstallationID, PersonaID: record.PersonaID, ConversationID: record.ConversationID, OwnerID: record.OwnerID, Instruction: record.Instruction, Today: r.Now().In(location).Format(time.DateOnly), Zone: record.Zone, Documents: documents, Preview: preview}
	request.Source = peopleSource
	if expected, _ := ctx.Value(announcementPreviewDigestKey{}).(string); expected != "" && !preview {
		saved, ok := ctx.Value(announcementSavedPreviewKey{}).(AgentAnnouncementRunResult)
		if !ok || !reflect.DeepEqual(saved.Sources, documents) {
			_, storeErr := r.Store.RecordOccurrence(ctx, agentstore.AnnouncementOccurrence{TenantID: tenant.String(), AnnouncementID: id, OccurrenceID: occurrence, Result: agentstore.AnnouncementRefused, Reason: announcementPreviewChangedReason, AttemptedAt: r.Now().UTC()})
			return result, errors.Join(ErrAgentAnnouncementNotPublic, storeErr)
		}
	}
	result, err = r.Model.RunAnnouncement(ctx, request)
	if err != nil {
		if !preview {
			_, storeErr := r.Store.RecordOccurrence(ctx, agentstore.AnnouncementOccurrence{TenantID: tenant.String(), AnnouncementID: id, OccurrenceID: occurrence, Result: agentstore.AnnouncementFailed, Reason: announcementFailureReason(err), AttemptedAt: r.Now().UTC()})
			err = errors.Join(err, storeErr)
		}
		return AgentAnnouncementRunResult{}, err
	}
	if finalizer, ok := r.Delivery.(interface {
		RefuseAnnouncementOutput(context.Context, agentsecurity.FinalOutputPersistence) error
	}); ok && !preview {
		sealedOutput := result.Output
		defer func() {
			if runErr != nil {
				runErr = errors.Join(runErr, finalizer.RefuseAnnouncementOutput(ctx, sealedOutput))
			}
		}()
	}
	if err := validateAnnouncementOutput(request, &result); err != nil {
		if !preview {
			_, storeErr := r.Store.RecordOccurrence(ctx, agentstore.AnnouncementOccurrence{TenantID: tenant.String(), AnnouncementID: id, OccurrenceID: occurrence, Result: agentstore.AnnouncementFailed, Reason: announcementFailureReason(err), AttemptedAt: r.Now().UTC()})
			err = errors.Join(err, storeErr)
		}
		return AgentAnnouncementRunResult{}, err
	}
	public, unreadable := false, 0
	if peopleSource != nil {
		current, sourceErr := r.Sources.ResolveAnnouncementSource(ctx, record, r.Now())
		err = sourceErr
		public = err == nil && reflect.DeepEqual(current, *peopleSource)
	} else {
		public, unreadable, err = r.Public.AuthorizeAnnouncementDocuments(ctx, tenantKey, record.ConversationID, documents, result.CitedDocumentIDs)
	}
	if err != nil || !public {
		reason := r.refusalReason(ctx, tenantKey, record.ConversationID, unreadable)
		if !preview {
			_, storeErr := r.Store.RecordOccurrence(ctx, agentstore.AnnouncementOccurrence{TenantID: tenant.String(), AnnouncementID: id, OccurrenceID: occurrence, Result: agentstore.AnnouncementRefused, Reason: reason, AttemptedAt: r.Now().UTC()})
			err = errors.Join(err, storeErr)
		}
		return result, errors.Join(ErrAgentAnnouncementNotPublic, err)
	}
	if preview {
		return result, nil
	}
	messageID, err := r.Delivery.PostAnnouncement(ctx, request, result, "announcement:"+occurrence)
	if err != nil {
		if errors.Is(err, ErrAgentAnnouncementNotPublic) {
			_, unreadable, _ := r.Public.AuthorizeAnnouncementDocuments(ctx, tenantKey, record.ConversationID, documents, result.CitedDocumentIDs)
			_, storeErr := r.Store.RecordOccurrence(ctx, agentstore.AnnouncementOccurrence{TenantID: tenant.String(), AnnouncementID: id, OccurrenceID: occurrence, Result: agentstore.AnnouncementRefused, Reason: r.refusalReason(ctx, tenantKey, record.ConversationID, unreadable), AttemptedAt: r.Now().UTC()})
			return result, errors.Join(err, storeErr)
		}
		// The stored reason is what the owner reads; the cause is for a
		// developer cell that has opted in to seeing it in the server log.
		if os.Getenv("HCMNEXT_AGENT_DEBUG_CAUSES") == "1" {
			slog.Warn("hcmnext.agent_announcement_post_failed", "announcement", id, "cause", err.Error())
		}
		_, storeErr := r.Store.RecordOccurrence(ctx, agentstore.AnnouncementOccurrence{TenantID: tenant.String(), AnnouncementID: id, OccurrenceID: occurrence, Result: agentstore.AnnouncementFailed, Reason: announcementFailureReason(err), AttemptedAt: r.Now().UTC()})
		return AgentAnnouncementRunResult{}, errors.Join(err, storeErr)
	}
	result.MessageID = messageID
	inserted, err := r.Store.RecordOccurrence(ctx, agentstore.AnnouncementOccurrence{TenantID: tenant.String(), AnnouncementID: id, OccurrenceID: occurrence, Result: agentstore.AnnouncementPosted, MessageID: messageID, AttemptedAt: r.Now().UTC()})
	if err != nil {
		return AgentAnnouncementRunResult{}, err
	}
	if !inserted {
		return result, nil
	}
	return result, nil
}

func (r *AgentAnnouncementRunner) refusalReason(ctx context.Context, tenant, conversation string, unreadable int) string {
	name := "the conversation"
	if r.ConversationName != nil {
		if label, err := r.ConversationName(ctx, tenant, conversation); err == nil && strings.TrimSpace(label) != "" {
			name = label
		}
	}
	return fmt.Sprintf("%d of the documents cannot be read by everyone in %s", unreadable, name)
}

func validateAnnouncementOutput(request AgentAnnouncementRunRequest, result *AgentAnnouncementRunResult) error {
	if request.Source != nil {
		return agentuxDemoValidateBirthdayOutput(request, result)
	}
	result.Sources = nil
	identity := result.Output.Identity()
	if result.Output.Digest() == "" || identity.TenantID != request.TenantID || identity.ConversationID != request.ConversationID || identity.InstallationID != request.InstallationID || identity.PersonaID != request.PersonaID || identity.InvokerID != request.OwnerID || identity.InvocationID != request.OccurrenceID {
		return ErrPersonaRunOutputRejected
	}
	delivery, err := personaChatReplyDeliveryResult(result.Output)
	if err != nil || len(delivery.Items) != 1 {
		return ErrPersonaRunOutputRejected
	}
	documents, cited, err := outputAnnouncementDocuments(result.Output)
	if err != nil || len(documents) == 0 {
		return ErrPersonaRunOutputRejected
	}
	for _, document := range documents {
		found := false
		for _, source := range request.Documents {
			found = found || source.DocumentID == document.DocumentID && source.Version == document.Version && source.Digest == document.Digest && source.SectionAnchor == document.SectionAnchor
		}
		if !found {
			return ErrPersonaRunOutputRejected
		}
		for _, source := range request.Documents {
			if source.DocumentID == document.DocumentID {
				result.Sources = append(result.Sources, source)
				break
			}
		}
	}
	for _, citation := range result.Output.Citations() {
		if !strings.HasPrefix(citation.SourceID, "document:") {
			return ErrPersonaRunOutputRejected
		}
	}
	result.Text, result.CitedDocumentIDs = delivery.Items[0].Text, cited
	return nil
}
