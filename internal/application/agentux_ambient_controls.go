package application

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

type AgentUXAmbientCommand struct {
	ID, Action, Title, Date, Clock, Zone string
	ExpectedRevision                     uint64
}

// ListOffers is the only card read seam. A private card is filtered in SQL,
// before its body can enter a response, and current membership is mandatory.
func (s *AgentUXAmbientService) ListOffers(ctx context.Context, tenant, conversation, person string) ([]AgentUXAmbientOffer, error) {
	if !s.available() {
		return nil, ErrAgentUXAmbientInvalid
	}
	result := []AgentUXAmbientOffer{}
	err := s.DB.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if err := agentUXAmbientMember(ctx, tx, tenant, conversation, person, false); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT CASE WHEN o.state='CANCELLED' THEN o.data-'Title'-'Proposal'-'Time' WHEN p.revision<>o.source_revision THEN (o.data-'Title'-'Proposal'-'Time')||jsonb_build_object('State','SOURCE_UNAVAILABLE','Reason','source_changed_unavailable') ELSE o.data END FROM agentux_ambient_offer o JOIN chat_post p ON p.tenant_id=o.tenant_id AND p.conversation_id=o.conversation_id AND p.id=o.source_id JOIN chat_membership m ON m.tenant_id=p.tenant_id AND m.conversation_id=p.conversation_id AND m.member_id=$3 AND m.home_tenant_id=$1 AND m.state='active' AND m.left_at IS NULL WHERE o.tenant_id=$1 AND o.conversation_id=$2 AND (o.scope='PUBLIC' OR o.person_id=$3) AND (NOT p.tombstoned OR o.state='CANCELLED') AND (m.history_visibility='FULL_HISTORY' OR p.created_at>=m.joined_at) AND o.state NOT IN ('DISMISSED','NOT_TASK') ORDER BY o.updated_at,o.id`, tenant, conversation, person)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var raw []byte
			var offer AgentUXAmbientOffer
			if err = rows.Scan(&raw); err != nil {
				return err
			}
			if err = json.Unmarshal(raw, &offer); err != nil {
				return err
			}
			result = append(result, offer)
		}
		return rows.Err()
	})
	return result, err
}

func (s *AgentUXAmbientService) Control(ctx context.Context, tenant, conversation, actor string, cmd AgentUXAmbientCommand) (AgentUXAmbientOffer, error) {
	var offer AgentUXAmbientOffer
	if !s.available() || cmd.ExpectedRevision == 0 {
		return offer, ErrAgentUXAmbientInvalid
	}
	err := s.DB.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if err := agentUXAmbientMember(ctx, tx, tenant, conversation, actor, false); err != nil {
			return err
		}
		var raw []byte
		err := tx.QueryRow(ctx, `SELECT data FROM agentux_ambient_offer WHERE tenant_id=$1 AND conversation_id=$2 AND id=$3 AND (scope='PUBLIC' OR person_id=$4) FOR UPDATE`, tenant, conversation, cmd.ID, actor).Scan(&raw)
		if errors.Is(err, dbport.ErrNoRows) {
			return ErrAgentUXAmbientDenied
		}
		if err != nil {
			return err
		}
		if err = json.Unmarshal(raw, &offer); err != nil {
			return err
		}
		if offer.Scope == "PUBLIC" && offer.Kind == "REMINDER" {
			if err = agentUXAmbientMember(ctx, tx, tenant, conversation, actor, true); err != nil {
				return err
			}
		}
		if offer.Revision != cmd.ExpectedRevision {
			return ErrAgentUXAmbientInvalid
		}
		if offer.State == "CANCELLED" && cmd.Action != "DISMISS" || offer.State == "DISMISSED" || offer.State == "NOT_TASK" {
			return ErrAgentUXAmbientDenied
		}
		var sourceRevision uint64
		var deleted bool
		if err = tx.QueryRow(ctx, `SELECT p.revision,p.tombstoned FROM chat_post p JOIN chat_membership m ON m.tenant_id=p.tenant_id AND m.conversation_id=p.conversation_id AND m.member_id=$4 AND m.home_tenant_id=$1 AND m.state='active' AND m.left_at IS NULL WHERE p.tenant_id=$1 AND p.conversation_id=$2 AND p.id=$3 AND (m.history_visibility='FULL_HISTORY' OR p.created_at>=m.joined_at) FOR SHARE OF p,m`, tenant, conversation, offer.Source, actor).Scan(&sourceRevision, &deleted); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return ErrAgentUXAmbientDenied
			}
			return err
		}
		terminal := cmd.Action == "DISMISS" || cmd.Action == "NOT_TASK" || cmd.Action == "CANCEL"
		if !terminal {
			var installed bool
			if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM chat_app_installation WHERE tenant_id=$1 AND conversation_id=$2 AND app_id=$3 AND status='ACTIVE' AND version=$4 AND 'chat.posts.read'=ANY(granted_scopes))`, tenant, conversation, offer.Agent, offer.InstallationVersion).Scan(&installed); err != nil {
				return err
			}
			if !installed {
				return ErrAgentUXAmbientDenied
			}
		}
		if (deleted || sourceRevision != offer.SourceRevision) && !terminal {
			return ErrAgentUXAmbientDenied
		}
		if !deleted {
			if err = chatstore.AmbientChannelTaskSourceReadable(ctx, tx, tenant, tenant, conversation, actor, offer.Source); err != nil {
				return ErrAgentUXAmbientDenied
			}
		}
		effectCtx := dbport.ContextWithTx(ctx, tx)
		switch cmd.Action {
		case "DISMISS", "NOT_TASK":
			if offer.State == "SET" {
				if s.Effects == nil {
					return ErrAgentUXAmbientInvalid
				}
				if err = s.Effects.CancelAmbientMessageAnnouncement(effectCtx, offer); err != nil {
					return err
				}
			}
			offer.State = "DISMISSED"
			if cmd.Action == "NOT_TASK" {
				offer.State = "NOT_TASK"
			}
		case "EDIT":
			if offer.Kind != "TASK" {
				return ErrAgentUXAmbientInvalid
			}
			if offer.State == "ADDED" || offer.State == "SET" || strings.TrimSpace(cmd.Title) == "" || len(cmd.Title) > 500 || agentUXAmbientUnsafe(cmd.Title) {
				return ErrAgentUXAmbientInvalid
			}
			offer.Title = strings.TrimSpace(cmd.Title)
			offer.State = "OFFERED"
			if cmd.Date != "" || cmd.Clock != "" {
				offer.Proposal.Date = cmd.Date
				offer.Proposal.Clock = cmd.Clock
				if offer.Proposal.Clock == "" && cmd.Date != "" {
					offer.Proposal.Clock = "17:00"
				}
				if cmd.Zone != "" {
					offer.Zone = cmd.Zone
				}
				offer.Time = AgentUXAmbientUnderstandTime(offer.Proposal, offer.Zone, s.Now())
			}
		case "ADD":
			if offer.Kind != "TASK" {
				return ErrAgentUXAmbientInvalid
			}
			if offer.State == "ADDED" {
				return nil
			}
			if offer.Time.Ask != "" {
				return ErrAgentUXAmbientInvalid
			}
			if offer.Scope == "PRIVATE" {
				var due any
				if !offer.Time.At.IsZero() {
					due = offer.Time.At
				}
				count, err := tx.Exec(ctx, `INSERT INTO agentux_ambient_task(tenant_id,id,person_id,conversation_id,source_id,title,due_at) VALUES($1,$2,$3,$4,$5,$6,$7) ON CONFLICT(tenant_id,id) DO UPDATE SET title=EXCLUDED.title,due_at=EXCLUDED.due_at WHERE agentux_ambient_task.person_id=EXCLUDED.person_id`, tenant, offer.ID, actor, conversation, offer.Source, offer.Title, due)
				if err != nil {
					return err
				}
				if count != 1 {
					return ErrAgentUXAmbientDenied
				}
			} else {
				if s.Effects == nil {
					return ErrAgentUXAmbientInvalid
				}
				if err = s.Effects.AddAmbientChannelTask(effectCtx, tenant, conversation, actor, offer.ID, offer.Title, offer.Source); err != nil {
					return err
				}
			}
			offer.State = "ADDED"
		case "SET", "CHANGE", "SNOOZE":
			if offer.Kind != "REMINDER" || s.Effects == nil {
				return ErrAgentUXAmbientInvalid
			}
			previous := offer
			if cmd.Date != "" || cmd.Clock != "" {
				proposal := offer.Proposal
				proposal.Date = cmd.Date
				proposal.Clock = cmd.Clock
				proposal.Explicit = true
				zone := cmd.Zone
				if zone == "" {
					zone = offer.Zone
				}
				offer.Time = AgentUXAmbientUnderstandTime(proposal, zone, s.Now())
				offer.Proposal = proposal
				offer.Zone = zone
			}
			if offer.Time.Ask != "" || len(offer.Time.Fire) == 0 {
				return ErrAgentUXAmbientInvalid
			}
			for _, at := range offer.Time.Fire {
				if !at.After(s.Now()) {
					return ErrAgentUXAmbientInvalid
				}
			}
			if offer.State == "SET" {
				if err = s.Effects.CancelAmbientMessageAnnouncement(effectCtx, previous); err != nil {
					return err
				}
			}
			offer.Revision++
			offer.State = "SET"
			if err = s.Effects.SetAmbientMessageAnnouncement(effectCtx, offer); err != nil {
				return err
			}
		case "CANCEL":
			if offer.Kind != "REMINDER" || s.Effects == nil {
				return ErrAgentUXAmbientInvalid
			}
			if err = s.Effects.CancelAmbientMessageAnnouncement(effectCtx, offer); err != nil {
				return err
			}
			offer.State = "CANCELLED"
		default:
			return ErrAgentUXAmbientInvalid
		}
		if cmd.Action != "SET" && cmd.Action != "CHANGE" && cmd.Action != "SNOOZE" {
			offer.Revision++
		}
		return agentUXAmbientSave(ctx, tx, offer)
	})
	return offer, err
}

type AgentUXAmbientTask struct {
	ID, Title, Conversation, Source string
	Due                             *time.Time
	Completed                       bool
}

func (s *AgentUXAmbientService) ListTasks(ctx context.Context, tenant, person string) ([]AgentUXAmbientTask, error) {
	if !s.available() {
		return nil, ErrAgentUXAmbientInvalid
	}
	result := []AgentUXAmbientTask{}
	err := s.DB.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, err := tx.Query(ctx, `SELECT t.id,t.title,t.conversation_id,t.source_id,t.due_at,t.completed FROM agentux_ambient_task t JOIN chat_membership m ON m.tenant_id=t.tenant_id AND m.conversation_id=t.conversation_id AND m.member_id=t.person_id AND m.home_tenant_id=t.tenant_id AND m.state='active' AND m.left_at IS NULL JOIN chat_post p ON p.tenant_id=t.tenant_id AND p.conversation_id=t.conversation_id AND p.id=t.source_id AND NOT p.tombstoned JOIN chat_conversation c ON c.tenant_id=t.tenant_id AND c.id=t.conversation_id AND c.lifecycle='ACTIVE' WHERE t.tenant_id=$1 AND t.person_id=$2 AND (m.history_visibility='FULL_HISTORY' OR p.created_at>=m.joined_at) ORDER BY t.due_at NULLS LAST,t.id`, tenant, person)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var task AgentUXAmbientTask
			if err = rows.Scan(&task.ID, &task.Title, &task.Conversation, &task.Source, &task.Due, &task.Completed); err != nil {
				return err
			}
			result = append(result, task)
		}
		return rows.Err()
	})
	return result, err
}

func (s *AgentUXAmbientService) CompleteTask(ctx context.Context, tenant, person, id string, completed bool) error {
	if !s.available() {
		return ErrAgentUXAmbientInvalid
	}
	return s.DB.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		count, err := tx.Exec(ctx, `UPDATE agentux_ambient_task t SET completed=$4 WHERE tenant_id=$1 AND person_id=$2 AND id=$3 AND EXISTS(SELECT 1 FROM chat_membership m WHERE m.tenant_id=t.tenant_id AND m.conversation_id=t.conversation_id AND m.member_id=t.person_id AND m.home_tenant_id=t.tenant_id AND m.state='active' AND m.left_at IS NULL)`, tenant, person, id, completed)
		if err != nil {
			return err
		}
		if count != 1 {
			return ErrAgentUXAmbientDenied
		}
		return nil
	})
}

func (s *AgentUXAmbientService) Grants(ctx context.Context, tenant, conversation, person string) ([]AgentUXAmbientGrant, bool, error) {
	if !s.available() {
		return nil, false, ErrAgentUXAmbientInvalid
	}
	result := []AgentUXAmbientGrant{}
	var optout bool
	err := s.DB.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if err := agentUXAmbientMember(ctx, tx, tenant, conversation, person, false); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agentux_ambient_optout WHERE tenant_id=$1 AND conversation_id=$2 AND person_id=$3 AND opted_out)`, tenant, conversation, person).Scan(&optout); err != nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT i.app_id,COALESCE(g.enabled AND g.installation_version=i.version,false),COALESCE(g.automatic_public,false),COALESCE(g.conversation_limit,25),COALESCE(g.daily_limit,100),COALESCE(g.enabled AND g.installation_version=i.version,false) AND ((SELECT count(*) FROM agentux_ambient_read r WHERE r.tenant_id=i.tenant_id AND r.agent_id=i.app_id AND at>=$3 AND stage='READ' AND outcome<>'SCREENED')>=COALESCE(g.daily_limit,100) OR (SELECT count(*) FROM agentux_ambient_read r WHERE r.tenant_id=i.tenant_id AND r.agent_id=i.app_id AND r.conversation_id=i.conversation_id AND at>=$3 AND stage='READ' AND outcome<>'SCREENED')>=COALESCE(g.conversation_limit,25)) FROM chat_app_installation i LEFT JOIN agentux_ambient_grant g ON g.tenant_id=i.tenant_id AND g.conversation_id=i.conversation_id AND g.agent_id=i.app_id WHERE i.tenant_id=$1 AND i.conversation_id=$2 AND i.status='ACTIVE' AND 'chat.posts.read'=ANY(i.granted_scopes) AND i.app_id IN ('task-catcher','reminder') ORDER BY i.app_id`, tenant, conversation, s.Now().UTC().Truncate(24*time.Hour))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var g AgentUXAmbientGrant
			if err = rows.Scan(&g.Agent, &g.Enabled, &g.AutomaticPublic, &g.ConversationLimit, &g.DailyLimit, &g.Paused); err != nil {
				return err
			}
			result = append(result, g)
		}
		return rows.Err()
	})
	return result, optout, err
}
