package application

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

type AgentUXAmbientDatabase interface {
	RunTenantTx(context.Context, string, func(dbport.Tx) error) error
}

type AgentUXAmbientModelInput struct {
	SourceKind   string
	Message      AgentUXAmbientMessage
	ThreadParent string
	Untrusted    bool
}

type AgentUXAmbientModel interface {
	ExtractAmbient(context.Context, string, AgentUXAmbientModelInput) (AgentUXAmbientProposal, error)
}

// All side-effect ports require durable offer identity as the idempotency key.
// The announcement adapter uses the existing scheduler and normal agent writer.
type AgentUXAmbientEffects interface {
	AddAmbientChannelTask(context.Context, string, string, string, string, string, string) error
	SetAmbientMessageAnnouncement(context.Context, AgentUXAmbientOffer) error
	CancelAmbientMessageAnnouncement(context.Context, AgentUXAmbientOffer) error
}

type AgentUXAmbientOffer struct {
	ID, Tenant, Conversation, Source, Agent, Kind, Scope, Person, Reason, Title, State string
	SourceRevision, Revision                                                           uint64
	InstallationVersion                                                                uint64
	Proposal                                                                           AgentUXAmbientProposal
	Time                                                                               AgentUXAmbientTime
	Zone                                                                               string
	Locale                                                                             string
}

type AgentUXAmbientGrant struct {
	Agent                            string
	Enabled, AutomaticPublic, Paused bool
	ConversationLimit, DailyLimit    int
}

type AgentUXAmbientService struct {
	DB      AgentUXAmbientDatabase
	Model   AgentUXAmbientModel
	Effects AgentUXAmbientEffects
	Now     func() time.Time
	// Names comes from the existing people directory, never the model.
	MemberNames  func(context.Context, string, string) (map[string]string, error)
	AuthorLocale func(context.Context, string, string) (string, error)
}

func agentUXAmbientMember(ctx context.Context, tx dbport.Tx, tenant, conversation, person string, manage bool) error {
	var manager bool
	var kind string
	err := tx.QueryRow(ctx, `SELECT (c.owner_id=$3 OR m.role='manager'),c.kind FROM chat_membership m JOIN chat_conversation c ON c.tenant_id=m.tenant_id AND c.id=m.conversation_id WHERE m.tenant_id=$1 AND m.conversation_id=$2 AND m.member_id=$3 AND m.home_tenant_id=$1 AND m.state='active' AND m.left_at IS NULL AND c.lifecycle='ACTIVE' FOR SHARE OF m,c`, tenant, conversation, person).Scan(&manager, &kind)
	if err != nil || kind == "DIRECT" || manage && !manager {
		return ErrAgentUXAmbientDenied
	}
	return nil
}

func (s *AgentUXAmbientService) available() bool { return s != nil && s.DB != nil && s.Now != nil }

// SetGrant is never inferred from message text. It requires current channel
// administration and a currently active installation in that exact channel.
func (s *AgentUXAmbientService) SetGrant(ctx context.Context, tenant, conversation, actor string, g AgentUXAmbientGrant) error {
	if !s.available() {
		return ErrAgentUXAmbientInvalid
	}
	if g.Agent != "task-catcher" && g.Agent != "reminder" || g.AutomaticPublic && g.Agent != "task-catcher" {
		return ErrAgentUXAmbientInvalid
	}
	if g.ConversationLimit == 0 {
		g.ConversationLimit = 25
	}
	if g.DailyLimit == 0 {
		g.DailyLimit = 100
	}
	if g.ConversationLimit < 1 || g.ConversationLimit > 500 || g.DailyLimit < 1 || g.DailyLimit > 2000 {
		return ErrAgentUXAmbientInvalid
	}
	return s.DB.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if err := agentUXAmbientMember(ctx, tx, tenant, conversation, actor, true); err != nil {
			return err
		}
		var installationVersion uint64
		if err := tx.QueryRow(ctx, `SELECT version FROM chat_app_installation WHERE tenant_id=$1 AND conversation_id=$2 AND app_id=$3 AND status='ACTIVE' AND 'chat.posts.read'=ANY(granted_scopes) FOR SHARE`, tenant, conversation, g.Agent).Scan(&installationVersion); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return ErrAgentUXAmbientDenied
			}
			return err
		}
		if installationVersion == 0 {
			return ErrAgentUXAmbientDenied
		}
		_, err := tx.Exec(ctx, `INSERT INTO agentux_ambient_grant(tenant_id,conversation_id,agent_id,enabled,automatic_public,conversation_limit,daily_limit,updated_by,enabled_at,installation_version) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) ON CONFLICT(tenant_id,conversation_id,agent_id) DO UPDATE SET enabled=EXCLUDED.enabled,automatic_public=EXCLUDED.automatic_public,conversation_limit=EXCLUDED.conversation_limit,daily_limit=EXCLUDED.daily_limit,updated_by=EXCLUDED.updated_by,installation_version=EXCLUDED.installation_version,enabled_at=CASE WHEN (NOT agentux_ambient_grant.enabled AND EXCLUDED.enabled) OR agentux_ambient_grant.installation_version<>EXCLUDED.installation_version THEN EXCLUDED.enabled_at ELSE agentux_ambient_grant.enabled_at END`, tenant, conversation, g.Agent, g.Enabled, g.AutomaticPublic, g.ConversationLimit, g.DailyLimit, actor, s.Now().UTC(), installationVersion)
		if err != nil {
			return err
		}
		return chatstore.AppendAmbientGrantChanged(ctx, tx, tenant, conversation, g.Agent, actor)
	})
}

func (s *AgentUXAmbientService) SetOptOut(ctx context.Context, tenant, conversation, person string, optOut bool) error {
	if !s.available() {
		return ErrAgentUXAmbientInvalid
	}
	return s.DB.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM chat_conversation WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, conversation); err != nil {
			return err
		}
		if err := agentUXAmbientMember(ctx, tx, tenant, conversation, person, false); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO agentux_ambient_optout VALUES($1,$2,$3,$4) ON CONFLICT(tenant_id,conversation_id,person_id) DO UPDATE SET opted_out=EXCLUDED.opted_out`, tenant, conversation, person, optOut)
		return err
	})
}

// ProcessMessage runs on the independent outbox consumer, never in SendPost.
// Authorized reads and budget reservations commit before model work. A
// separate result transaction rechecks the source and current authority.
func (s *AgentUXAmbientService) ProcessMessage(ctx context.Context, tenant, conversation, postID, agent, zone string) error {
	if !s.available() || s.Model == nil {
		return ErrAgentUXAmbientInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	candidate, err := s.agentUXAmbientCandidate(ctx, tenant, conversation, postID, agent, zone)
	if err != nil || candidate == nil {
		return err
	}
	proposal, modelErr := s.Model.ExtractAmbient(ctx, agent, candidate.input)
	m := candidate.input.Message
	return s.DB.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, tenant+":ambient:"+agent); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT 1 FROM chat_conversation WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, conversation); err != nil {
			return err
		}
		var sourceRevision uint64
		var sourceDeleted bool
		if err := tx.QueryRow(ctx, `SELECT revision,tombstoned FROM chat_post WHERE tenant_id=$1 AND conversation_id=$2 AND id=$3 FOR SHARE`, tenant, conversation, postID).Scan(&sourceRevision, &sourceDeleted); err != nil {
			return err
		}
		var automatic bool
		currentErr := tx.QueryRow(ctx, `SELECT g.automatic_public FROM agentux_ambient_grant g JOIN chat_app_installation i ON i.tenant_id=g.tenant_id AND i.conversation_id=g.conversation_id AND i.app_id=g.agent_id JOIN chat_conversation c ON c.tenant_id=g.tenant_id AND c.id=g.conversation_id JOIN chat_post p ON p.tenant_id=g.tenant_id AND p.conversation_id=g.conversation_id AND p.id=$4 JOIN chat_membership m ON m.tenant_id=p.tenant_id AND m.conversation_id=p.conversation_id AND m.member_id=p.author_id AND m.home_tenant_id=$1 AND m.state='active' AND m.left_at IS NULL WHERE g.tenant_id=$1 AND g.conversation_id=$2 AND g.agent_id=$3 AND g.enabled AND i.status='ACTIVE' AND i.version=g.installation_version AND i.version=$5 AND 'chat.posts.read'=ANY(i.granted_scopes) AND c.kind<>'DIRECT' AND c.lifecycle='ACTIVE' AND p.created_at>=g.enabled_at AND p.author_home_tenant_id=$1 AND p.author_id=$6 AND p.source_attribution IS NULL AND NOT EXISTS(SELECT 1 FROM agentux_ambient_optout o WHERE o.tenant_id=p.tenant_id AND o.conversation_id=p.conversation_id AND o.person_id=p.author_id AND o.opted_out) FOR SHARE OF g,i,c,m`, tenant, conversation, agent, postID, candidate.installationVersion, m.Author).Scan(&automatic)
		if currentErr != nil && !errors.Is(currentErr, dbport.ErrNoRows) {
			return currentErr
		}
		outcome := "PROPOSED"
		if modelErr != nil {
			outcome = "FAILED"
		} else if proposal.Kind == "" {
			outcome = "NO_ACTION"
		}
		current := currentErr == nil && sourceRevision == m.Revision && !sourceDeleted
		if !current {
			outcome = "IGNORED_SOURCE_CHANGED"
		}
		now := s.Now().UTC()
		if _, err := tx.Exec(ctx, `INSERT INTO agentux_ambient_read(tenant_id,conversation_id,agent_id,post_id,revision,parent_id,stage,at,outcome) VALUES($1,$2,$3,$4,$5,$6,'RESULT',$7,$8) ON CONFLICT DO NOTHING`, tenant, conversation, agent, postID, m.Revision, m.Parent, now, outcome); err != nil {
			return err
		}
		if sourceDeleted {
			return s.cancelSource(ctx, tx, tenant, conversation, postID, "")
		}
		if !current || modelErr != nil {
			return nil
		}
		if proposal.Kind == "" {
			return s.cancelSource(ctx, tx, tenant, conversation, postID, agent)
		}

		if proposal.Kind != "TASK" && proposal.Kind != "REMINDER" || agent == "reminder" && proposal.Kind != "REMINDER" || agent == "task-catcher" && proposal.Kind != "TASK" || strings.TrimSpace(proposal.Title) == "" || len(proposal.Title) > 1000 || !strings.Contains(strings.ToLower(m.Body), strings.ToLower(proposal.Title)) || agentUXAmbientUnsafe(proposal.Title) {
			return nil
		}
		proposal.Explicit = agentUXAmbientContains(strings.ToLower(m.Body), "remind me", "remind us", "remind the channel", "erinnere mich", "erinnere uns", "ذكرني", "ذكّرني", "ذكرنا", "ذكّرنا")
		if !agentUXAmbientContains(strings.ToLower(m.Body), "meeting", "all-hands", "treffen", "اجتماع") {
			proposal.Event = "DEADLINE"
		}
		if proposal.Kind == "TASK" && proposal.Date != "" && proposal.Clock == "" {
			proposal.Clock = "17:00"
		}
		members, err := agentUXAmbientMembers(ctx, tx, tenant, conversation)
		if err != nil {
			return err
		}
		if s.MemberNames != nil {
			names, err := s.MemberNames(ctx, tenant, conversation)
			if err != nil {
				return err
			}
			for i := range members {
				members[i].Name = names[members[i].ID]
			}
		}
		audience := AgentUXAmbientAudienceFor(m, proposal, members)
		if audience.Scope == "PRIVATE" {
			if err = chatstore.AmbientChannelTaskSourceReadable(ctx, tx, tenant, tenant, conversation, audience.Person, postID); err != nil {
				return nil
			}
		}
		offer := AgentUXAmbientOffer{ID: agentUXAmbientID(tenant, postID, agent), Tenant: tenant, Conversation: conversation, Source: postID, SourceRevision: m.Revision, Revision: 1, Agent: agent, Kind: proposal.Kind, Scope: audience.Scope, Person: audience.Person, Reason: audience.Reason, Title: proposal.Title, State: "OFFERED", Proposal: proposal, Zone: zone}
		offer.InstallationVersion = candidate.installationVersion
		if s.AuthorLocale != nil {
			offer.Locale, err = s.AuthorLocale(ctx, tenant, m.Author)
			if err != nil {
				return err
			}
		}
		if proposal.Date != "" || proposal.Kind == "REMINDER" {
			offer.Time = AgentUXAmbientUnderstandTime(proposal, zone, now)
		}
		if proposal.Kind == "REMINDER" && offer.Time.Ask != "" {
			offer.State = "NEEDS_TIME"
		}
		var duplicate bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agentux_ambient_offer o WHERE tenant_id=$1 AND conversation_id=$2 AND agent_id=$3 AND lower(title)=lower($4) AND scope=$5 AND person_id=$6 AND source_id<>$7 AND (state IN ('OFFERED','SET','NEEDS_TIME') OR state='ADDED' AND (EXISTS(SELECT 1 FROM agentux_ambient_task t WHERE t.tenant_id=o.tenant_id AND t.id=o.id AND NOT t.completed) OR EXISTS(SELECT 1 FROM chat_channel_todo l CROSS JOIN LATERAL jsonb_array_elements(l.items_json) item WHERE l.tenant_id=o.tenant_id AND l.conversation_id=o.conversation_id AND item->>'id'=o.id AND NOT (item->>'completed')::boolean))))`, tenant, conversation, agent, offer.Title, offer.Scope, offer.Person, postID).Scan(&duplicate); err != nil {
			return err
		}
		if duplicate {
			return nil
		}
		var oldData []byte
		err = tx.QueryRow(ctx, `SELECT data FROM agentux_ambient_offer WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, offer.ID).Scan(&oldData)
		if err == nil {
			var old AgentUXAmbientOffer
			if json.Unmarshal(oldData, &old) != nil {
				return ErrAgentUXAmbientInvalid
			}
			if old.State == "DISMISSED" || old.State == "NOT_TASK" || old.State == "CANCELLED" {
				return nil
			}
			offer.Revision = old.Revision + 1
			if old.State == "SET" || old.State == "ADDED" || old.State == "SOURCE_CHANGED" {
				// An edit cannot silently consent to new text, ownership or time.
				if s.Effects == nil {
					return ErrAgentUXAmbientInvalid
				}
				if old.Kind == "REMINDER" && old.State == "SET" {
					if err = s.Effects.CancelAmbientMessageAnnouncement(dbport.ContextWithTx(ctx, tx), old); err != nil {
						return err
					}
				}
				offer.State = "SOURCE_CHANGED"
			}
		} else if !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		if automatic && offer.Scope == "PUBLIC" && offer.Kind == "TASK" && offer.State == "OFFERED" && offer.Time.Ask == "" {
			if s.Effects == nil {
				return ErrAgentUXAmbientInvalid
			}
			var owner string
			if err = tx.QueryRow(ctx, `SELECT updated_by FROM agentux_ambient_grant WHERE tenant_id=$1 AND conversation_id=$2 AND agent_id=$3`, tenant, conversation, agent).Scan(&owner); err != nil {
				return err
			}
			if err = agentUXAmbientMember(ctx, tx, tenant, conversation, owner, true); err != nil {
				return err
			}
			if err = s.Effects.AddAmbientChannelTask(dbport.ContextWithTx(ctx, tx), tenant, conversation, owner, offer.ID, offer.Title, postID); err != nil {
				return err
			}
			offer.State = "ADDED"
		}
		return agentUXAmbientSave(ctx, tx, offer)
	})
}

func agentUXAmbientID(parts ...string) string {
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "ambient-" + hex.EncodeToString(sum[:16])
}

func agentUXAmbientMembers(ctx context.Context, tx dbport.Tx, tenant, conversation string) ([]AgentUXAmbientMember, error) {
	rows, err := tx.Query(ctx, `SELECT member_id,home_tenant_id FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2 AND state='active' AND left_at IS NULL`, tenant, conversation)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []AgentUXAmbientMember
	for rows.Next() {
		var member AgentUXAmbientMember
		if err = rows.Scan(&member.ID, &member.HomeTenant); err != nil {
			return nil, err
		}
		result = append(result, member)
	}
	return result, rows.Err()
}

func agentUXAmbientSave(ctx context.Context, tx dbport.Tx, o AgentUXAmbientOffer) error {
	raw, err := json.Marshal(o)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO agentux_ambient_offer(tenant_id,id,conversation_id,source_id,source_revision,agent_id,kind,person_id,scope,state,reason,title,data,revision) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14) ON CONFLICT(tenant_id,id) DO UPDATE SET source_revision=EXCLUDED.source_revision,kind=EXCLUDED.kind,person_id=EXCLUDED.person_id,scope=EXCLUDED.scope,state=EXCLUDED.state,reason=EXCLUDED.reason,title=EXCLUDED.title,data=EXCLUDED.data,revision=EXCLUDED.revision,updated_at=now()`, o.Tenant, o.ID, o.Conversation, o.Source, o.SourceRevision, o.Agent, o.Kind, o.Person, o.Scope, o.State, o.Reason, o.Title, raw, o.Revision)
	if err != nil {
		return err
	}
	return chatstore.AppendAmbientOfferChanged(ctx, tx, o.Tenant, o.Conversation, o.Agent, o.ID, o.Revision)
}

func (s *AgentUXAmbientService) cancelSource(ctx context.Context, tx dbport.Tx, tenant, conversation, source, agent string) error {
	rows, err := tx.Query(ctx, `SELECT data FROM agentux_ambient_offer WHERE tenant_id=$1 AND conversation_id=$2 AND source_id=$3 AND ($4='' OR agent_id=$4) AND state<>'CANCELLED' FOR UPDATE`, tenant, conversation, source, agent)
	if err != nil {
		return err
	}
	var offers []AgentUXAmbientOffer
	for rows.Next() {
		var raw []byte
		var o AgentUXAmbientOffer
		if err = rows.Scan(&raw); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal(raw, &o); err != nil {
			rows.Close()
			return err
		}
		offers = append(offers, o)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, o := range offers {
		if o.Kind == "TASK" && o.Scope == "PUBLIC" && (o.State == "ADDED" || o.State == "SOURCE_CHANGED") {
			store, ok := s.DB.(interface {
				CancelAmbientChannelTask(context.Context, string, string, string, string) error
			})
			if !ok {
				return ErrAgentUXAmbientInvalid
			}
			if err = store.CancelAmbientChannelTask(dbport.ContextWithTx(ctx, tx), tenant, conversation, o.ID, source); err != nil {
				return err
			}
		}
		if o.Kind == "REMINDER" && o.State == "SET" {
			if s.Effects == nil {
				return ErrAgentUXAmbientInvalid
			}
			if err = s.Effects.CancelAmbientMessageAnnouncement(dbport.ContextWithTx(ctx, tx), o); err != nil {
				return err
			}
		}
		o.State = "CANCELLED"
		o.Reason = "source_deleted"
		if agent != "" {
			o.Reason = "source_no_longer_action"
		}
		o.Revision++
		if err = agentUXAmbientSave(ctx, tx, o); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `DELETE FROM agentux_ambient_task WHERE tenant_id=$1 AND conversation_id=$2 AND source_id=$3 AND NOT completed AND ($4='' OR $4='task-catcher')`, tenant, conversation, source, agent)
	return err
}
