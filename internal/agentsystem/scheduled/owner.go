package scheduled

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/cycle"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

var (
	ErrRevision        = errors.New("agent schedule: stale revision")
	ErrAuthority       = errors.New("agent schedule: current authority refused action")
	ErrInactive        = errors.New("agent schedule: inactive schedule")
	ErrInvalidSchedule = errors.New("agent schedule: invalid owner contract")
	ErrOverlapQueued   = errors.New("agent schedule: predecessor still active")
	ErrOverlapSkipped  = errors.New("agent schedule: overlapping occurrence skipped")
	ErrOverlapRefused  = errors.New("agent schedule: overlapping occurrence refused")
)

const (
	StateDraft   = "DRAFT"
	StateActive  = "ACTIVE"
	StatePaused  = "PAUSED"
	StateRetired = "RETIRED"
)

type Action string

const (
	ActionDraft   Action = "DRAFT"
	ActionPreview Action = "PREVIEW"
	ActionPublish Action = "PUBLISH"
	ActionPause   Action = "PAUSE"
	ActionResume  Action = "RESUME"
	ActionSkip    Action = "SKIP"
	ActionDryRun  Action = "DRY_RUN"
	ActionRetire  Action = "RETIRE"
)

// Actor is supplied by the authenticated application boundary.
type Actor struct{ TenantID, UserID string }

// Schedule retains a pinned Scheduling publication and independently revisioned
// owner controls. Cursor advances only with the durable occurrence outbox.
type Schedule struct {
	Trigger                                                       schedule.PublishedTrigger
	Revision                                                      uint64
	State, OwnerID, InstallationID, AgentPrincipalID, LegalEntity string
	Zone                                                          values.ZoneRef
	Calendar                                                      cycle.TenantCalendar
	Misfire                                                       schedule.MisfireConfig
	RunTimeout                                                    time.Duration
	Context                                                       agentrun.ContextScope
	DST                                                           string
	SkippedKeys                                                   []string
	Cursor                                                        values.Instant
}
type Audit struct {
	TenantID, ScheduleID, ActorID string
	Revision                      uint64
	Action                        Action
	OccurrenceKey                 string
	At                            time.Time
}

// OwnerStore must save the state CAS and its immutable audit in one transaction.
type OwnerStore interface {
	Load(context.Context, string, string) (Schedule, bool, error)
	Save(context.Context, Schedule, uint64, Audit) error
}

// OwnerAuthority resolves current source-owned grants; target authorizations
// returned here must never come from the request payload.
type OwnerAuthority interface {
	Authorize(context.Context, Actor, Action, Schedule) ([]schedule.AuthorizedTarget, error)
	CheckCurrent(context.Context, Schedule) error
}
type Owner struct {
	store     OwnerStore
	authority OwnerAuthority
}

func NewOwner(store OwnerStore, authority OwnerAuthority) (*Owner, error) {
	if store == nil || authority == nil {
		return nil, ErrInvalidSchedule
	}
	return &Owner{store, authority}, nil
}
func ValidateSchedule(s Schedule) error {
	if err := s.Trigger.Verify(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidSchedule, err)
	}
	if s.Trigger.Definition.TargetKind != schedule.TargetAgentRun || s.Trigger.Definition.AgentRun == nil || s.Trigger.Definition.Source.Kind == schedule.SourceEvent || !required(s.OwnerID) || s.OwnerID != s.Trigger.Definition.Owner || !required(s.InstallationID) || !required(s.AgentPrincipalID) || !required(s.LegalEntity) || s.RunTimeout <= 0 || s.RunTimeout > 24*time.Hour {
		return ErrInvalidSchedule
	}
	if !required(s.Context.ID) || !required(s.Context.SnapshotID) || !validAgentDigest(s.Context.Digest) {
		return ErrInvalidSchedule
	}
	if err := s.Zone.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidSchedule, err)
	}
	if s.Trigger.Definition.Source.Kind == schedule.SourceCalendar {
		ref := s.Trigger.Definition.Source.Calendar.CalendarRef
		if ref.Ref == "" {
			ref = s.Trigger.Definition.Source.Calendar.TenantCalendarRef
		}
		if s.Calendar.Validate() != nil || s.Calendar.Calendar != ref || s.Calendar.Zone != s.Zone {
			return ErrInvalidSchedule
		}
	}
	if err := s.Misfire.Validate(); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidSchedule, err)
	}
	if s.DST != "REJECT" && s.DST != "EARLIER" && s.DST != "LATER" && s.DST != "BOTH" {
		return ErrInvalidSchedule
	}
	switch s.State {
	case StateDraft, StateActive, StatePaused, StateRetired:
	default:
		return ErrInvalidSchedule
	}
	return nil
}
func (o *Owner) authorize(ctx context.Context, actor Actor, action Action, s Schedule) ([]schedule.AuthorizedTarget, error) {
	if o == nil || !required(actor.TenantID) || !required(actor.UserID) || actor.TenantID != s.Trigger.Definition.TenantID {
		return nil, ErrAuthority
	}
	targets, err := o.authority.Authorize(ctx, actor, action, s)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAuthority, err)
	}
	return targets, nil
}
func (o *Owner) Draft(ctx context.Context, actor Actor, s Schedule, expected uint64, at time.Time) (Schedule, error) {
	s.State = StateDraft
	if at.IsZero() || ValidateSchedule(s) != nil {
		return Schedule{}, ErrInvalidSchedule
	}
	if actor.UserID != s.OwnerID {
		return Schedule{}, ErrAuthority
	}
	targets, err := o.authorize(ctx, actor, ActionDraft, s)
	if err != nil {
		return Schedule{}, err
	}
	current, found, err := o.store.Load(ctx, actor.TenantID, s.Trigger.Definition.ID)
	if err != nil {
		return Schedule{}, err
	}
	if (found && (current.Revision != expected || current.OwnerID != s.OwnerID || current.State == StateRetired)) || (!found && expected != 0) {
		return Schedule{}, ErrRevision
	}
	// Publication revision must advance when changing a reviewed target/source.
	if found && current.Trigger.Digest != s.Trigger.Digest {
		old, oldErr := strconv.ParseUint(current.Trigger.Definition.Version, 10, 64)
		next, nextErr := strconv.ParseUint(s.Trigger.Definition.Version, 10, 64)
		if oldErr != nil || nextErr != nil || next <= old {
			return Schedule{}, ErrRevision
		}
	}
	published, err := schedule.Publish(schedule.NewRegistry(), s.Trigger.Definition, targets)
	if err != nil {
		return Schedule{}, err
	}
	s.Trigger = published
	s.Revision = expected + 1
	if err := o.store.Save(ctx, s, expected, Audit{actor.TenantID, s.Trigger.Definition.ID, actor.UserID, s.Revision, ActionDraft, "", at.UTC()}); err != nil {
		return Schedule{}, err
	}
	return s, nil
}
func (o *Owner) Preview(ctx context.Context, actor Actor, s Schedule, window schedule.OccurrenceWindow) (schedule.OccurrenceResult, error) {
	if err := ValidateSchedule(s); err != nil {
		return schedule.OccurrenceResult{}, err
	}
	if _, err := o.authorize(ctx, actor, ActionPreview, s); err != nil {
		return schedule.OccurrenceResult{}, err
	}
	result, err := schedule.CalculateOccurrences(s.Trigger, schedule.OccurrenceRequest{Window: window, Zone: s.Zone, Calendar: s.Calendar, Misfire: s.Misfire})
	if err != nil {
		return result, err
	}
	return ApplyDST(s, result)
}

// ApplyDST applies the owner's explicit fold choice to occurrences already
// resolved by Scheduling. It performs no independent timezone calculation.
func ApplyDST(s Schedule, result schedule.OccurrenceResult) (schedule.OccurrenceResult, error) {
	out := make([]schedule.Occurrence, 0, len(result.Occurrences))
	for _, occ := range result.Occurrences {
		choice := occ.ScheduledAt.Disambiguation()
		if choice == values.DisambiguationEarlier || choice == values.DisambiguationLater {
			if s.DST == "REJECT" {
				return schedule.OccurrenceResult{}, schedule.ErrOccurrenceDST
			}
			if s.DST == "EARLIER" && choice == values.DisambiguationLater || s.DST == "LATER" && choice == values.DisambiguationEarlier {
				continue
			}
		}
		out = append(out, occ)
	}
	result.Occurrences = out
	identity := []string{"agent-schedule-preview/v1", result.Digest, s.DST}
	for _, occ := range out {
		identity = append(identity, occ.Key)
	}
	data, _ := json.Marshal(identity)
	sum := sha256.Sum256(data)
	result.Digest = "sha256:" + hex.EncodeToString(sum[:])
	return result, nil
}
func (o *Owner) Control(ctx context.Context, actor Actor, id string, expected uint64, action Action, key string, at time.Time) (Schedule, error) {
	if o == nil || at.IsZero() {
		return Schedule{}, ErrInvalidSchedule
	}
	s, found, err := o.store.Load(ctx, actor.TenantID, id)
	if err != nil {
		return Schedule{}, err
	}
	if !found || s.Revision != expected {
		return Schedule{}, ErrRevision
	}
	if err := ValidateSchedule(s); err != nil {
		return Schedule{}, err
	}
	targets, err := o.authorize(ctx, actor, action, s)
	if err != nil {
		return Schedule{}, err
	}
	switch action {
	case ActionPublish:
		if s.State != StateDraft {
			return Schedule{}, ErrInactive
		}
		if actor.UserID == s.OwnerID {
			return Schedule{}, ErrAuthority
		}
		if _, err := schedule.Publish(schedule.NewRegistry(), s.Trigger.Definition, targets); err != nil {
			return Schedule{}, err
		}
		s.State = StateActive
		s.Cursor = values.NewInstant(at)
	case ActionPause:
		if s.State != StateActive {
			return Schedule{}, ErrInactive
		}
		s.State = StatePaused
	case ActionResume:
		if s.State != StatePaused {
			return Schedule{}, ErrInactive
		}
		if err := o.authority.CheckCurrent(ctx, s); err != nil {
			return Schedule{}, fmt.Errorf("%w: %v", ErrAuthority, err)
		}
		s.State = StateActive
	case ActionSkip:
		if s.State != StateActive || !required(key) || len(key) > 512 {
			return Schedule{}, ErrInvalidSchedule
		}
		if !slices.Contains(s.SkippedKeys, key) {
			s.SkippedKeys = append(s.SkippedKeys, key)
		}
	case ActionDryRun:
		if s.State != StateActive {
			return Schedule{}, ErrInactive
		}
		if err := o.authority.CheckCurrent(ctx, s); err != nil {
			return Schedule{}, fmt.Errorf("%w: %v", ErrAuthority, err)
		}
	case ActionRetire:
		if s.State == StateRetired {
			return Schedule{}, ErrInactive
		}
		s.State = StateRetired
	default:
		return Schedule{}, ErrInvalidSchedule
	}
	s.Revision++
	if err := o.store.Save(ctx, s, expected, Audit{actor.TenantID, id, actor.UserID, s.Revision, action, key, at.UTC()}); err != nil {
		return Schedule{}, err
	}
	return s, nil
}
func (o *Owner) CheckCurrent(ctx context.Context, firing Firing) error {
	ref := firing.Occurrence.Trigger
	s, found, err := o.store.Load(ctx, ref.TenantID, ref.ID)
	if err != nil {
		return err
	}
	if !found || s.State != StateActive || s.Trigger.Ref() != ref || s.Trigger.Definition.AgentRun == nil || *s.Trigger.Definition.AgentRun != firing.Target || slices.Contains(s.SkippedKeys, firing.Occurrence.Key) {
		return ErrInactive
	}
	if err := ValidateSchedule(s); err != nil {
		return err
	}
	return o.authority.CheckCurrent(ctx, s)
}
