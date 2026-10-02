package application

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentrun"
	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/scheduled"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/engines/schedule"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

type AgentUXAmbientScheduleBindings interface {
	ResolveAmbientAnnouncement(context.Context, AgentUXAmbientOffer) (scheduled.Schedule, error)
}

// AgentUXAmbientAnnouncementDelivery writes a normal agent message (with the
// source link) and, for PRIVATE only, a notification. Both operations use key
// as their durable idempotency identity; the recipient is never a model field.
type AgentUXAmbientAnnouncementDelivery interface {
	DeliverAmbientAnnouncement(context.Context, AgentUXAmbientOffer, string, time.Time) (string, error)
}

type AgentUXAmbientAnnouncements struct {
	Service    *AgentUXAmbientService
	Store      scheduled.OwnerStore
	Outbox     scheduled.Outbox
	Bindings   AgentUXAmbientScheduleBindings
	Delivery   AgentUXAmbientAnnouncementDelivery
	Admissions scheduled.Inbox
}

func agentUXAmbientScheduleID(o AgentUXAmbientOffer, at time.Time) string {
	return agentUXAmbientID(o.Tenant, o.ID, strconv.FormatUint(o.Revision, 10), at.UTC().Format(time.RFC3339))
}
func agentUXAmbientOfferDigest(o AgentUXAmbientOffer) string {
	raw, _ := json.Marshal(o)
	return personaRunBytesDigest(raw)
}

// One-shot message announcements use the existing Scheduling occurrence engine
// and durable outbox worker. UTC pins avoid civil-time fold replay; current
// authority and the exact source revision are rechecked before every delivery.
func (a AgentUXAmbientAnnouncements) SetAmbientMessageAnnouncement(ctx context.Context, o AgentUXAmbientOffer) error {
	if a.Store == nil || a.Bindings == nil || a.Service == nil || len(o.Time.Fire) == 0 || o.State != "SET" {
		return ErrAgentUXAmbientInvalid
	}
	for _, at := range o.Time.Fire {
		id := agentUXAmbientScheduleID(o, at)
		prior, found, err := a.Store.Load(ctx, o.Tenant, id)
		if err != nil {
			return err
		}
		if found {
			if prior.Context.Digest != agentUXAmbientOfferDigest(o) {
				return ErrAgentUXAmbientInvalid
			}
			continue
		}
		s, err := a.Bindings.ResolveAmbientAnnouncement(ctx, o)
		if err != nil {
			return err
		}
		if s.Trigger.Definition.TenantID != o.Tenant || s.OwnerID == "" || s.Trigger.Definition.AgentRun == nil || s.Persona == nil {
			return ErrAgentUXAmbientDenied
		}
		d := s.Trigger.Definition
		d.ID = id
		d.Owner = s.OwnerID
		d.Version = "1"
		utc := at.UTC()
		d.Source = schedule.TriggerSource{Kind: schedule.SourceCron, Cron: schedule.CronSource{Expression: fmt.Sprintf("%d %d %d %d *", utc.Minute(), utc.Hour(), utc.Day(), utc.Month())}}
		d.InputTemplateDigest = agentUXAmbientOfferDigest(o)
		d.Overlap = schedule.OverlapSkip
		d.OverlapPolicy = ""
		s.SourceKind = agentrun.SourceAnnouncement
		s.RequesterID = s.OwnerID
		s.Zone = values.ZoneRef{ID: "UTC", TzdbVersion: "2026a"}
		s.DST = "EARLIER"
		s.Misfire = schedule.MisfireConfig{Policy: schedule.MisfireCatchUpOnce, MaxCatchUp: 1}
		s.State = scheduled.StateActive
		s.Revision = 1
		s.Cursor = values.NewInstant(a.Service.Now().UTC())
		s.Context = agentrun.ContextScope{ID: o.ID, SnapshotID: strconv.FormatUint(o.Revision, 10), Digest: agentUXAmbientOfferDigest(o)}
		s.Trigger, err = schedule.Publish(schedule.NewRegistry(), d, []schedule.AuthorizedTarget{{AgentRun: d.AgentRun}})
		if err != nil {
			return err
		}
		if err = scheduled.ValidateSchedule(s); err != nil {
			return err
		}
		if err = a.Store.Save(ctx, s, 0, scheduled.Audit{TenantID: o.Tenant, ScheduleID: id, ActorID: s.OwnerID, Revision: 1, Action: scheduled.ActionPublish, At: a.Service.Now().UTC()}); err != nil {
			return err
		}
	}
	return nil
}

func (a AgentUXAmbientAnnouncements) CancelAmbientMessageAnnouncement(ctx context.Context, o AgentUXAmbientOffer) error {
	if a.Store == nil || a.Service == nil {
		return ErrAgentUXAmbientInvalid
	}
	for _, at := range o.Time.Fire {
		id := agentUXAmbientScheduleID(o, at)
		s, found, err := a.Store.Load(ctx, o.Tenant, id)
		if err != nil {
			return err
		}
		if !found || s.State == scheduled.StateRetired {
			continue
		}
		expected := s.Revision
		s.Revision++
		s.State = scheduled.StateRetired
		if err = a.Store.Save(ctx, s, expected, scheduled.Audit{TenantID: o.Tenant, ScheduleID: id, ActorID: s.OwnerID, Revision: s.Revision, Action: scheduled.ActionRetire, At: a.Service.Now().UTC()}); err != nil {
			return err
		}
	}
	return nil
}

func (a AgentUXAmbientAnnouncements) current(ctx context.Context, s scheduled.Schedule) (AgentUXAmbientOffer, error) {
	var o AgentUXAmbientOffer
	if a.Service == nil {
		return o, ErrAgentUXAmbientInvalid
	}
	err := a.Service.DB.RunTenantTx(ctx, s.Trigger.Definition.TenantID, func(tx dbport.Tx) error {
		var raw []byte
		if err := tx.QueryRow(ctx, `SELECT o.data FROM agentux_ambient_offer o JOIN chat_post p ON p.tenant_id=o.tenant_id AND p.id=o.source_id AND p.conversation_id=o.conversation_id JOIN chat_app_installation i ON i.tenant_id=o.tenant_id AND i.conversation_id=o.conversation_id AND i.app_id=o.agent_id WHERE o.tenant_id=$1 AND o.id=$2 AND o.state='SET' AND NOT p.tombstoned AND p.revision=o.source_revision AND i.status='ACTIVE' AND i.version=(o.data->>'InstallationVersion')::bigint AND 'chat.posts.read'=ANY(i.granted_scopes) FOR SHARE OF o,p,i`, s.Trigger.Definition.TenantID, s.Context.ID).Scan(&raw); err != nil {
			return ErrAgentUXAmbientDenied
		}
		if err := json.Unmarshal(raw, &o); err != nil {
			return err
		}
		if s.Context.Digest != agentUXAmbientOfferDigest(o) || s.Context.SnapshotID != strconv.FormatUint(o.Revision, 10) {
			return ErrAgentUXAmbientDenied
		}
		if o.Scope == "PRIVATE" {
			if err := agentUXAmbientMember(ctx, tx, o.Tenant, o.Conversation, o.Person, false); err != nil {
				return err
			}
		}
		return nil
	})
	return o, err
}

type agentUXAmbientScheduleAuthority struct{ a AgentUXAmbientAnnouncements }

func (x agentUXAmbientScheduleAuthority) CheckCurrent(ctx context.Context, s scheduled.Schedule) error {
	_, err := x.a.current(ctx, s)
	return err
}
func (x agentUXAmbientScheduleAuthority) Authorize(ctx context.Context, actor scheduled.Actor, _ scheduled.Action, s scheduled.Schedule) ([]schedule.AuthorizedTarget, error) {
	if actor.TenantID != s.Trigger.Definition.TenantID || actor.UserID != s.OwnerID {
		return nil, ErrAgentUXAmbientDenied
	}
	if err := x.CheckCurrent(ctx, s); err != nil {
		return nil, err
	}
	return []schedule.AuthorizedTarget{{AgentRun: s.Trigger.Definition.AgentRun}}, nil
}

type agentUXAmbientScheduleContext struct{}

func (agentUXAmbientScheduleContext) BuildScheduleContext(_ context.Context, s scheduled.Schedule, _ schedule.Occurrence) (agentrun.ContextScope, error) {
	return s.Context, nil
}

type agentUXAmbientAnnouncementInbox struct{ a AgentUXAmbientAnnouncements }

func (x agentUXAmbientAnnouncementInbox) Admit(ctx context.Context, r agentrun.Request) (agentrun.Record, bool, error) {
	delivery, found, err := x.a.Outbox.GetDelivery(ctx, r.Source.TenantID, r.Source.Ref)
	if err != nil || !found {
		return agentrun.Record{}, false, ErrAgentUXAmbientDenied
	}
	s, found, err := x.a.Store.Load(ctx, r.Source.TenantID, delivery.ScheduleID)
	if err != nil || !found {
		return agentrun.Record{}, false, ErrAgentUXAmbientDenied
	}
	o, err := x.a.current(ctx, s)
	if err != nil {
		return agentrun.Record{}, false, err
	}
	if x.a.Delivery == nil || x.a.Admissions == nil || r.Context != s.Context || r.Audience.ID != s.Trigger.Definition.AgentRun.Destination.AudienceID {
		return agentrun.Record{}, false, ErrAgentUXAmbientDenied
	}
	var at time.Time
	for _, fire := range o.Time.Fire {
		if agentUXAmbientScheduleID(o, fire) == s.Trigger.Definition.ID {
			at = fire
		}
	}
	if at.IsZero() || at.After(x.a.Service.Now()) || !delivery.Firing.Occurrence.At.Time().Equal(at) {
		return agentrun.Record{}, false, ErrAgentUXAmbientDenied
	}
	record, created, err := x.a.Admissions.Admit(ctx, r)
	if err != nil || record.Decision != agentrun.DecisionAccepted {
		return record, created, err
	}
	if _, err = x.a.Delivery.DeliverAmbientAnnouncement(ctx, o, r.Source.Key, at); err != nil {
		return agentrun.Record{}, false, err
	}
	return record, created, nil
}

// Tick is called by the existing announcement workload; it owns no goroutine,
// timer or browser event, and retains the shared scheduler's replay receipts.
func (a AgentUXAmbientAnnouncements) Tick(ctx context.Context, tenant string, ids []string) error {
	if a.Store == nil || a.Outbox == nil || a.Delivery == nil || a.Service == nil {
		return ErrAgentUXAmbientInvalid
	}
	owner, err := scheduled.NewOwner(a.Store, agentUXAmbientScheduleAuthority{a})
	if err != nil {
		return err
	}
	allowed := map[string]bool{}
	outbox := agentUXAmbientScheduleOutbox{Outbox: a.Outbox, ids: allowed}
	worker, err := scheduled.NewOutboxWorker(owner, outbox, agentUXAmbientScheduleContext{}, agentUXAmbientAnnouncementInbox{a})
	if err != nil {
		return err
	}
	for _, id := range ids {
		s, found, err := a.Store.Load(ctx, tenant, id)
		if err != nil {
			return err
		}
		if !found || s.State != scheduled.StateActive {
			continue
		}
		if _, err = a.current(ctx, s); err != nil {
			if errors.Is(err, ErrAgentUXAmbientDenied) {
				continue
			}
			return err
		}
		allowed[id] = true
		if !a.Service.Now().After(s.Cursor.Time()) {
			continue
		}
		if err = worker.Plan(ctx, tenant, id, a.Service.Now()); err != nil {
			return err
		}
	}
	_, err = worker.Replay(ctx, tenant, 100)
	if err != nil {
		return err
	}
	pending, err := a.Outbox.Pending(ctx, tenant, 1000)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if !allowed[id] {
			continue
		}
		s, found, err := a.Store.Load(ctx, tenant, id)
		if err != nil {
			return err
		}
		if !found || s.State != scheduled.StateActive {
			continue
		}
		waiting := false
		for _, delivery := range pending {
			if delivery.ScheduleID == id {
				waiting = true
			}
		}
		if waiting {
			continue
		}
		o, err := a.current(ctx, s)
		if err != nil {
			return err
		}
		due := false
		for _, at := range o.Time.Fire {
			if agentUXAmbientScheduleID(o, at) == id && !at.After(a.Service.Now()) {
				due = true
			}
		}
		if !due {
			continue
		}
		expected := s.Revision
		s.State = scheduled.StateRetired
		s.Revision++
		if err = a.Store.Save(ctx, s, expected, scheduled.Audit{TenantID: tenant, ScheduleID: id, ActorID: s.OwnerID, Revision: s.Revision, Action: scheduled.ActionRetire, At: a.Service.Now().UTC()}); err != nil {
			return err
		}
	}
	return nil
}

type AgentUXAmbientEffectsAdapter struct {
	Chat          *chatstore.Store
	Announcements AgentUXAmbientAnnouncements
	TodoAuthority func(context.Context, string, string, string) error
}

func (a AgentUXAmbientEffectsAdapter) AddAmbientChannelTask(ctx context.Context, tenant, conversation, actor, id, title, source string) error {
	if a.Chat == nil || a.TodoAuthority == nil {
		return ErrAgentUXAmbientInvalid
	}
	return a.Chat.AddAmbientChannelTask(ctx, tenant, conversation, actor, id, title, source, func(ctx context.Context) error { return a.TodoAuthority(ctx, tenant, conversation, actor) })
}
func (a AgentUXAmbientEffectsAdapter) SetAmbientMessageAnnouncement(ctx context.Context, o AgentUXAmbientOffer) error {
	return a.Announcements.SetAmbientMessageAnnouncement(ctx, o)
}
func (a AgentUXAmbientEffectsAdapter) CancelAmbientMessageAnnouncement(ctx context.Context, o AgentUXAmbientOffer) error {
	return a.Announcements.CancelAmbientMessageAnnouncement(ctx, o)
}
