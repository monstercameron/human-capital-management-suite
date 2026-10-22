package application

import (
	"context"
	"time"

	"github.com/google/uuid"
	chatcore "github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentpersonastore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/agentstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/data/pgxadapter"
	"github.com/monstercameron/human-capital-management-suite/internal/intent/app/pgstore"
	"github.com/monstercameron/human-capital-management-suite/internal/kernel/values"
)

// agentUXAmbientDeps are the stores and ports the served assembly puts the
// ambient agents together from. A dependency that is not there leaves the part
// that needs it out: no agent database means a reminder cannot be set, no
// planner means no message is read.
type agentUXAmbientDeps struct {
	Chat       *chatstore.Store
	Extensions *ChatExtensions
	Service    chatcore.ConversationService
	Agents     *agentstore.Store
	Personas   *agentpersonastore.Store
	Core       dbport.Beginner
	TenantUUID func(values.TenantId) uuid.UUID
	Now        func() time.Time
	// Planner runs the screen model through the governed gateway. The served
	// composition leaves it nil until a screen model is qualified, which is why
	// no message is read on a workspace that has none.
	Planner AgentUXAmbientRunPlanner
}

// BindAgentUXAmbient composes the ambient agents' whole served surface: the
// read and the controls, "Set reminder" on the announcement scheduler, the
// consumer that turns real messages into cards when a screen model is
// configured, and their share of the tenant loop (AGENTUX-066, -067, -068). It
// runs after BindAgentUX and replaces what that bound.
func (s *agentServedAssembly) BindAgentUXAmbient(d agentUXAmbientDeps) error {
	if s == nil || d.Chat == nil || d.Now == nil {
		return nil
	}
	// Adding a task to the channel's list is the person's own change to that
	// list: it passes the same check as editing it by hand.
	var todoAuthority func(context.Context, string, string, string) error
	if d.Extensions != nil {
		todoAuthority = func(ctx context.Context, tenant, conversation, actor string) error {
			return d.Extensions.channelWriteActor(ctx, chatcore.Principal{TenantID: tenant, SubjectID: actor}, tenant, conversation, channelTodoStatusAction("ADD"))
		}
	}
	composition := agentUXAmbientComposition{Chat: d.Chat, TodoAuthority: todoAuthority, Now: d.Now, Planner: d.Planner, EnsureDM: agentUXAmbientDMEnsurer(d.Chat, d.Service)}
	if d.Agents != nil && d.Personas != nil && d.Core != nil && d.TenantUUID != nil {
		store := agentUXAmbientTriggerStore{agents: d.Agents, tenantUUID: d.TenantUUID}
		principals := AgentAnnouncementServicePrincipal{Agents: d.Agents, Personas: d.Personas, Principals: GovernancePersonaPrincipalAuthority{DB: d.Core, TenantUUID: d.TenantUUID}, TenantUUID: d.TenantUUID, Now: d.Now}
		composition.Schedules = store
		composition.Bindings = agentUXAmbientBindings{Personas: d.Personas, Agents: d.Agents, Principals: principals, LegalEntity: agentUXAmbientLegalEntity(d.Core, d.TenantUUID, d.Now), TenantUUID: d.TenantUUID, Now: d.Now}
		composition.Admissions = agentUXAmbientAdmissions{Agents: d.Agents, Store: store, Principals: principals, TenantUUID: d.TenantUUID, Now: d.Now}
	}
	served := composeAgentUXAmbient(composition)
	if served.Surface != nil {
		s.agentUX.Ambient = served.Surface
		s.agentUX.AmbientWorker = served.Worker
	}
	return nil
}

// BindAgentUXAmbientServed is the served assembly's one call: the stores the
// composition root opened, handed over as they are. A pool that was not opened
// is left out rather than passed on as a typed nil.
func (s *agentServedAssembly) BindAgentUXAmbientServed(chat *chatstore.Store, extensions *ChatExtensions, service chatcore.ConversationService, agents *agentstore.Store, personas *agentpersonastore.Store, pool *pgxadapter.Pool, now func() time.Time) error {
	deps := agentUXAmbientDeps{Chat: chat, Extensions: extensions, Service: service, Agents: agents, Personas: personas, TenantUUID: tenantKeyMapper[values.TenantId](pgstore.TenantID), Now: now}
	if pool != nil {
		deps.Core = pool
	}
	return s.BindAgentUXAmbient(deps)
}
