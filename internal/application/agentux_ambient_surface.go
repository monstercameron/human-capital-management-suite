package application

import (
	"context"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/chatui"
	"github.com/monstercameron/human-capital-management-suite/internal/transport/ambientagents"
	"github.com/monstercameron/human-capital-management-suite/internal/trust"
)

type AgentUXAmbientSurface struct {
	Service    *AgentUXAmbientService
	ViewerZone func(context.Context, string, string) (string, error)
	SourceLink func(context.Context, string, string, string) (string, error)
}

func (s AgentUXAmbientSurface) actor(ctx context.Context) (string, string, error) {
	p, ok := trust.FromContext(ctx)
	if !ok || p == nil || p.SubjectKind() != trust.SubjectKindHuman || s.Service == nil || !s.Service.available() || !s.Service.Now().Before(p.ExpiresAt()) {
		return "", "", ErrAgentUXAmbientDenied
	}
	return p.Tenant().String(), p.Subject(), nil
}

func (s AgentUXAmbientSurface) Snapshot(ctx context.Context, conversation string) (ambientagents.Snapshot, error) {
	result := ambientagents.Snapshot{Cards: []chatui.AgentUXAmbientCard{}, Tasks: []ambientagents.Task{}, Grants: []ambientagents.Grant{}}
	tenant, person, err := s.actor(ctx)
	if err != nil {
		return result, err
	}
	if s.ViewerZone == nil || s.SourceLink == nil {
		return result, ErrAgentUXAmbientInvalid
	}
	zone, err := s.ViewerZone(ctx, tenant, person)
	if err != nil {
		return result, err
	}
	offers, err := s.Service.ListOffers(ctx, tenant, conversation, person)
	if err != nil {
		return result, err
	}
	var manages bool
	if err = s.Service.DB.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		manages = agentUXAmbientMember(ctx, tx, tenant, conversation, person, true) == nil
		return nil
	}); err != nil {
		return result, err
	}
	for _, o := range offers {
		href := ""
		if o.State != "CANCELLED" {
			href, err = s.SourceLink(ctx, tenant, conversation, o.Source)
			if err != nil {
				return result, err
			}
		}
		name := "Task Catcher"
		if o.Kind == "REMINDER" {
			name = "Reminder"
		}
		canManage := o.Scope == "PRIVATE" || o.Kind == "TASK" || manages
		result.Cards = append(result.Cards, chatui.AgentUXAmbientCard{ID: o.ID, AgentID: o.Agent, Conversation: conversation, Source: o.Source, AgentName: name, Kind: o.Kind, Scope: o.Scope, Person: o.Person, Reason: o.Reason, Title: o.Title, State: o.State, SourceHref: href, Zone: zone, Ask: o.Time.Ask, Lead: o.Time.Lead, Due: o.Time.At, Fire: o.Time.Fire, Revision: o.Revision, CanManage: canManage})
	}
	grants, optout, err := s.Service.Grants(ctx, tenant, conversation, person)
	if err != nil {
		return result, err
	}
	result.OptOut = optout
	for _, g := range grants {
		result.Grants = append(result.Grants, ambientagents.Grant{Agent: g.Agent, Enabled: g.Enabled, AutomaticPublic: g.AutomaticPublic, Paused: g.Paused})
	}
	tasks, err := s.Service.ListTasks(ctx, tenant, person)
	if err != nil {
		return result, err
	}
	for _, task := range tasks {
		due := ""
		if task.Due != nil {
			due = task.Due.UTC().Format(time.RFC3339)
		}
		result.Tasks = append(result.Tasks, ambientagents.Task{ID: task.ID, Title: task.Title, Conversation: task.Conversation, Source: task.Source, Completed: task.Completed, Due: due})
	}
	return result, nil
}

func (s AgentUXAmbientSurface) Control(ctx context.Context, cmd ambientagents.Command) (ambientagents.Snapshot, error) {
	tenant, person, err := s.actor(ctx)
	if err != nil {
		return ambientagents.Snapshot{}, err
	}
	zone := ""
	if s.ViewerZone != nil {
		zone, err = s.ViewerZone(ctx, tenant, person)
		if err != nil {
			return ambientagents.Snapshot{}, err
		}
	}
	_, err = s.Service.Control(ctx, tenant, cmd.Conversation, person, AgentUXAmbientCommand{ID: cmd.ID, Action: cmd.Action, Title: cmd.Title, Date: cmd.Date, Clock: cmd.Clock, Zone: zone, ExpectedRevision: cmd.ExpectedRevision})
	if err != nil {
		return ambientagents.Snapshot{}, err
	}
	return s.Snapshot(ctx, cmd.Conversation)
}

func (s AgentUXAmbientSurface) Grant(ctx context.Context, cmd ambientagents.GrantCommand) (ambientagents.Snapshot, error) {
	tenant, person, err := s.actor(ctx)
	if err != nil {
		return ambientagents.Snapshot{}, err
	}
	if err = s.Service.SetGrant(ctx, tenant, cmd.Conversation, person, AgentUXAmbientGrant{Agent: cmd.Agent, Enabled: cmd.Enabled, AutomaticPublic: cmd.AutomaticPublic}); err != nil {
		return ambientagents.Snapshot{}, err
	}
	return s.Snapshot(ctx, cmd.Conversation)
}

func (s AgentUXAmbientSurface) OptOut(ctx context.Context, cmd ambientagents.OptOutCommand) (ambientagents.Snapshot, error) {
	tenant, person, err := s.actor(ctx)
	if err != nil {
		return ambientagents.Snapshot{}, err
	}
	if err = s.Service.SetOptOut(ctx, tenant, cmd.Conversation, person, cmd.OptOut); err != nil {
		return ambientagents.Snapshot{}, err
	}
	return s.Snapshot(ctx, cmd.Conversation)
}

var _ ambientagents.Surface = AgentUXAmbientSurface{}
