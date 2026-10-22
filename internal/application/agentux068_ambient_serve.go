package application

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/agentsystem/scheduled"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/ambientagents"
)

// agentUXAmbientZone is a person's time zone as they keep it in Chat
// preferences (the quiet-hours zone, UTC until they choose one). Cards state
// their times in it, and a time a person types is understood in it. A zone that
// cannot be read or is not one the zone data knows is UTC, so a failed read
// never blanks the surface (AGENTUX-067, AGENTUX-068).
func agentUXAmbientZone(store *chatstore.Store) func(context.Context, string, string) (string, error) {
	state := chatstore.NewRecipientStateStore(store)
	return func(ctx context.Context, tenant, person string) (string, error) {
		hours, err := state.QuietHours(ctx, tenant, person)
		if err != nil {
			return "UTC", nil
		}
		zone := strings.TrimSpace(hours.Timezone)
		if zone == "" || zone == "Local" {
			return "UTC", nil
		}
		if _, err := time.LoadLocation(zone); err != nil {
			return "UTC", nil
		}
		return zone, nil
	}
}

// agentUXAmbientComposition is what the served assembly puts the ambient agents
// together from. The scheduler, bindings and admission put "Set reminder" on the
// announcement scheduler; without them a reminder cannot be set and says so. The
// planner is the screen model: without one nothing reads a message and nothing
// fails, so a workspace with no model configured is unchanged.
type agentUXAmbientComposition struct {
	Chat          *chatstore.Store
	TodoAuthority func(context.Context, string, string, string) error
	Now           func() time.Time
	Schedules     interface {
		scheduled.OwnerStore
		scheduled.Outbox
	}
	Bindings   AgentUXAmbientScheduleBindings
	Admissions scheduled.Inbox
	// EnsureDM creates a person's conversation with an agent while they set a
	// private reminder; nil leaves that to whoever created the conversation.
	EnsureDM func(ctx context.Context, tenant, person, agent string) error
	Planner  AgentUXAmbientRunPlanner
}

// agentUXAmbientServed is the composed ambient surface and the workload that
// keeps it running: the outbox consumer that offers cards from real messages
// and the scheduler tick that delivers the reminders people set.
type agentUXAmbientServed struct {
	Surface ambientagents.Surface
	Worker  *AgentUXAmbientWorker
}

func agentUXAmbientSourceLink(_ context.Context, _, conversation, post string) (string, error) {
	return "/workspace/app/chat?" + url.Values{"conversation": {conversation}, "message": {post}}.Encode(), nil
}

func composeAgentUXAmbient(c agentUXAmbientComposition) agentUXAmbientServed {
	if c.Chat == nil || c.Now == nil {
		return agentUXAmbientServed{}
	}
	zone := agentUXAmbientZone(c.Chat)
	service := &AgentUXAmbientService{DB: c.Chat, Now: c.Now}
	if c.Planner != nil {
		service.Model = AgentUXAmbientModelAdapter{Planner: c.Planner}
	}
	effects := AgentUXAmbientEffectsAdapter{Chat: c.Chat, TodoAuthority: c.TodoAuthority}
	var announcements AgentUXAmbientAnnouncements
	if c.Schedules != nil && c.Bindings != nil && c.Admissions != nil {
		announcements = AgentUXAmbientAnnouncements{Service: service, Store: c.Schedules, Outbox: c.Schedules, Bindings: c.Bindings, Admissions: c.Admissions, EnsureDM: c.EnsureDM}
		announcements.Delivery = agentUXAmbientChatDelivery(c.Chat, service, agentUXAmbientSourceLink)
		effects.Announcements = announcements
	}
	service.Effects = effects
	worker := &AgentUXAmbientWorker{Service: service, Announcements: announcements, Outbox: AgentUXAmbientOutbox{Service: service, AuthorZone: zone}}
	return agentUXAmbientServed{
		Surface: AgentUXAmbientSurface{Service: service, ViewerZone: zone, SourceLink: agentUXAmbientSourceLink},
		Worker:  worker,
	}
}

// AgentUXAmbientWorker is the ambient agents' share of the served tenant loop.
type AgentUXAmbientWorker struct {
	Service       *AgentUXAmbientService
	Announcements AgentUXAmbientAnnouncements
	Outbox        AgentUXAmbientOutbox
}

// TickTenant offers cards for the messages posted since the last tick, when a
// screen model is configured, and delivers the reminders that have come due.
// Neither step runs, or fails, for what is not composed: no model reads no
// message, and no scheduler leaves reminders unset.
func (w *AgentUXAmbientWorker) TickTenant(ctx context.Context, tenant string) error {
	if w == nil || w.Service == nil || !w.Service.available() {
		return nil
	}
	var failures []error
	if w.Service.Model != nil {
		failures = append(failures, w.Outbox.Drain(ctx, tenant))
	}
	if w.Announcements.Store != nil && w.Announcements.Outbox != nil && w.Announcements.Delivery != nil {
		ids, err := w.dueScheduleIDs(ctx, tenant)
		if err != nil {
			failures = append(failures, err)
		} else if len(ids) > 0 {
			failures = append(failures, w.Announcements.Tick(ctx, tenant, ids))
		}
	}
	return errors.Join(failures...)
}

// dueScheduleIDs lists the schedules of the reminders that are set and whose
// time is within the last two weeks or still ahead (a reminder that came due
// while the process was down is still delivered, once, when it is back). A
// reminder's schedule retires itself once it
// has fired, so an old one needs no more ticks.
func (w *AgentUXAmbientWorker) dueScheduleIDs(ctx context.Context, tenant string) ([]string, error) {
	var offers []AgentUXAmbientOffer
	if err := w.Service.DB.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT data FROM agentux_ambient_offer WHERE tenant_id=$1 AND kind='REMINDER' AND state='SET' ORDER BY updated_at DESC LIMIT 500`, tenant)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var raw []byte
			var offer AgentUXAmbientOffer
			if err := rows.Scan(&raw); err != nil {
				return err
			}
			if json.Unmarshal(raw, &offer) == nil {
				offers = append(offers, offer)
			}
		}
		return rows.Err()
	}); err != nil {
		return nil, err
	}
	horizon := w.Service.Now().Add(-14 * 24 * time.Hour)
	var ids []string
	for _, offer := range offers {
		for _, at := range offer.Time.Fire {
			if at.After(horizon) {
				ids = append(ids, agentUXAmbientScheduleID(offer, at))
			}
		}
	}
	return ids, nil
}
