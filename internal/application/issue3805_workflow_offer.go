package application

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/chatstore"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
	"github.com/monstercameron/human-capital-management-suite/internal/humanwork/productui"
)

const (
	// AgentUXAmbientWorkflowAgent is the identity under which workflow offers
	// are granted, journaled and counted. It reads a message only where a
	// channel manager turned it on, like the other ambient agents, but it is a
	// fixed classifier over the administrator's catalog tags and is not an
	// installed persona: it needs no installation and calls no model.
	AgentUXAmbientWorkflowAgent = "workflow-offer"
	// AgentUXAmbientWorkflowKind is the offer kind of "start this workflow".
	AgentUXAmbientWorkflowKind = "WORKFLOW_START"

	// A person is offered at most this many workflows per conversation per day,
	// on top of the grant's own conversation and daily limits.
	agentUXAmbientWorkflowPersonDailyLimit = 3
	// "Not now" pauses offers of the same workflow to the same person.
	agentUXAmbientWorkflowNotNow = 24 * time.Hour
)

// AgentUXAmbientWorkflowCatalog lists the workflows a person may be offered:
// published, tagged by an administrator as offerable from Chat, and startable
// by that person right now. The service asks it again when a person accepts an
// offer, so a workflow that was withdrawn or whose access changed is not
// started from an old card.
type AgentUXAmbientWorkflowCatalog interface {
	OfferableWorkflows(ctx context.Context, tenant, person string) ([]AgentUXAmbientWorkflow, error)
}

// AgentUXAmbientWorkflowStartHref is where an accepted offer sends the person:
// the workflow's own start page, which shows what the workflow will ask and
// starts nothing until the person submits there. No message text travels in
// the link.
func AgentUXAmbientWorkflowStartHref(workflowID string) string {
	if workflowID == "" {
		return ""
	}
	return productui.Path(productui.PageWorkflowStart) + "/start/" + url.PathEscape(workflowID)
}

type agentUXAmbientWorkflowSource struct {
	author, body string
	revision     uint64
}

// agentUXAmbientWorkflowSourceNow reads the source message for a workflow offer
// under the room's current consent: the grant is on, the conversation is a live
// non-direct one, the author is an active human member who has not asked agents
// to leave their messages alone, and the message was posted after consent. It
// returns nil when any of that is not so.
func agentUXAmbientWorkflowSourceNow(ctx context.Context, tx dbport.Tx, tenant, conversation, postID string) (*agentUXAmbientWorkflowSource, bool, error) {
	var deleted bool
	if err := tx.QueryRow(ctx, `SELECT tombstoned FROM chat_post WHERE tenant_id=$1 AND conversation_id=$2 AND id=$3`, tenant, conversation, postID).Scan(&deleted); err != nil {
		if errors.Is(err, dbport.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, err
	}
	if deleted {
		return nil, true, nil
	}
	var source agentUXAmbientWorkflowSource
	err := tx.QueryRow(ctx, `SELECT p.author_id,p.revision,p.body FROM chat_post p JOIN agentux_ambient_grant g ON g.tenant_id=p.tenant_id AND g.conversation_id=p.conversation_id AND g.agent_id=$4 AND g.enabled JOIN chat_conversation c ON c.tenant_id=p.tenant_id AND c.id=p.conversation_id JOIN chat_membership m ON m.tenant_id=p.tenant_id AND m.conversation_id=p.conversation_id AND m.member_id=p.author_id AND m.home_tenant_id=p.author_home_tenant_id AND m.state='active' AND m.left_at IS NULL WHERE p.tenant_id=$1 AND p.conversation_id=$2 AND p.id=$3 AND p.author_home_tenant_id=$1 AND NOT p.tombstoned AND p.source_attribution IS NULL AND p.created_at>=g.enabled_at AND c.kind<>'DIRECT' AND c.lifecycle='ACTIVE' AND NOT EXISTS(SELECT 1 FROM chat_app_installation i WHERE i.tenant_id=p.tenant_id AND i.app_id=p.author_id) AND NOT EXISTS(SELECT 1 FROM agentux_ambient_optout o WHERE o.tenant_id=p.tenant_id AND o.conversation_id=p.conversation_id AND o.person_id=p.author_id AND o.opted_out) FOR SHARE OF p,g,c,m`, tenant, conversation, postID, AgentUXAmbientWorkflowAgent).Scan(&source.author, &source.revision, &source.body)
	if errors.Is(err, dbport.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return &source, false, nil
}

// ProcessWorkflowOffer reads one message for a request to start a tagged
// workflow and, when it finds exactly one, leaves a private offer for the
// message's author. It starts nothing and drafts nothing: the offer names the
// workflow from the catalog, never the message, and only the author can see or
// answer it. Replays and edits are safe: a message revision is read once, an
// edit that no longer asks for the workflow cancels its offer, and a deleted
// message cancels it. Nothing runs until an administrator has tagged a workflow
// and a channel manager has turned the offers on.
func (s *AgentUXAmbientService) ProcessWorkflowOffer(ctx context.Context, tenant, conversation, postID string) error {
	if !s.available() || s.Workflows == nil {
		return ErrAgentUXAmbientInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var source *agentUXAmbientWorkflowSource
	err := s.DB.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		var deleted bool
		var err error
		if source, deleted, err = agentUXAmbientWorkflowSourceNow(ctx, tx, tenant, conversation, postID); err != nil {
			return err
		}
		if deleted {
			return s.cancelSource(ctx, tx, tenant, conversation, postID, "")
		}
		return nil
	})
	if err != nil || source == nil {
		return err
	}
	// A catalog that cannot be read is no reason to retry the whole message
	// queue or to withdraw an offer: this message simply gets no offer now.
	entries, catalogErr := s.Workflows.OfferableWorkflows(ctx, tenant, source.author)
	if catalogErr != nil {
		return nil
	}
	entry, matched := AgentUXAmbientClassifyWorkflow(source.body, entries)
	return s.DB.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, tenant+":ambient:"+AgentUXAmbientWorkflowAgent); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT 1 FROM chat_conversation WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, conversation); err != nil {
			return err
		}
		current, deleted, err := agentUXAmbientWorkflowSourceNow(ctx, tx, tenant, conversation, postID)
		if err != nil {
			return err
		}
		if deleted {
			return s.cancelSource(ctx, tx, tenant, conversation, postID, "")
		}
		// An edit or a withdrawn consent since the read: the newer event decides.
		if current == nil || current.revision != source.revision || current.author != source.author {
			return nil
		}
		now := s.Now().UTC()
		journal := func(outcome string) error {
			_, err := tx.Exec(ctx, `INSERT INTO agentux_ambient_read(tenant_id,conversation_id,agent_id,post_id,revision,stage,at,outcome) VALUES($1,$2,$3,$4,$5,'READ',$6,$7) ON CONFLICT DO NOTHING`, tenant, conversation, AgentUXAmbientWorkflowAgent, postID, source.revision, now, outcome)
			return err
		}
		if !matched {
			if err = journal("SCREENED"); err != nil {
				return err
			}
			if source.revision > 1 {
				return s.cancelSource(ctx, tx, tenant, conversation, postID, AgentUXAmbientWorkflowAgent)
			}
			return nil
		}
		var muted, open bool
		if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM agentux_ambient_workflow_mute WHERE tenant_id=$1 AND person_id=$2 AND workflow_id=$3 AND (muted_until IS NULL OR muted_until>$4)),EXISTS(SELECT 1 FROM agentux_ambient_offer WHERE tenant_id=$1 AND conversation_id=$5 AND agent_id=$6 AND person_id=$2 AND state='OFFERED' AND source_id<>$7 AND data->>'WorkflowID'=$3)`, tenant, source.author, entry.ID, now, conversation, AgentUXAmbientWorkflowAgent, postID).Scan(&muted, &open); err != nil {
			return err
		}
		if muted || open {
			return journal("SCREENED")
		}
		var conversationLimit, dailyLimit int
		if err = tx.QueryRow(ctx, `SELECT conversation_limit,daily_limit FROM agentux_ambient_grant WHERE tenant_id=$1 AND conversation_id=$2 AND agent_id=$3`, tenant, conversation, AgentUXAmbientWorkflowAgent).Scan(&conversationLimit, &dailyLimit); err != nil {
			return err
		}
		var dayCount, channelCount, personCount int
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		if err = tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE r.at>=$4),count(*) FILTER(WHERE r.at>=$4 AND r.conversation_id=$2),count(*) FILTER(WHERE r.at>=$4 AND r.conversation_id=$2 AND p.author_id=$5) FROM agentux_ambient_read r LEFT JOIN chat_post p ON p.tenant_id=r.tenant_id AND p.conversation_id=r.conversation_id AND p.id=r.post_id WHERE r.tenant_id=$1 AND r.agent_id=$3 AND r.stage='READ' AND r.outcome<>'SCREENED'`, tenant, conversation, AgentUXAmbientWorkflowAgent, start, source.author).Scan(&dayCount, &channelCount, &personCount); err != nil {
			return err
		}
		if dayCount >= dailyLimit || channelCount >= conversationLimit || personCount >= agentUXAmbientWorkflowPersonDailyLimit {
			return nil
		}
		id := agentUXAmbientID(tenant, postID, AgentUXAmbientWorkflowAgent)
		var oldData []byte
		revision := uint64(1)
		err = tx.QueryRow(ctx, `SELECT data FROM agentux_ambient_offer WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, id).Scan(&oldData)
		if err == nil {
			var old AgentUXAmbientOffer
			if json.Unmarshal(oldData, &old) != nil {
				return ErrAgentUXAmbientInvalid
			}
			if old.State == "DISMISSED" || old.State == "CANCELLED" {
				return journal("SCREENED")
			}
			revision = old.Revision + 1
		} else if !errors.Is(err, dbport.ErrNoRows) {
			return err
		}
		count, err := tx.Exec(ctx, `INSERT INTO agentux_ambient_read(tenant_id,conversation_id,agent_id,post_id,revision,stage,at,outcome) VALUES($1,$2,$3,$4,$5,'READ',$6,'RESERVED') ON CONFLICT DO NOTHING`, tenant, conversation, AgentUXAmbientWorkflowAgent, postID, source.revision, now)
		if err != nil {
			return err
		}
		if count != 1 {
			return nil
		}
		if _, err = tx.Exec(ctx, `INSERT INTO agentux_ambient_read(tenant_id,conversation_id,agent_id,post_id,revision,stage,at,outcome) VALUES($1,$2,$3,$4,$5,'RESULT',$6,'PROPOSED') ON CONFLICT DO NOTHING`, tenant, conversation, AgentUXAmbientWorkflowAgent, postID, source.revision, now); err != nil {
			return err
		}
		return agentUXAmbientSave(ctx, tx, AgentUXAmbientOffer{ID: id, Tenant: tenant, Conversation: conversation, Source: postID, SourceRevision: source.revision, Revision: revision, Agent: AgentUXAmbientWorkflowAgent, Kind: AgentUXAmbientWorkflowKind, Scope: "PRIVATE", Person: source.author, Reason: "workflow_match", Title: entry.Name, State: "OFFERED", WorkflowID: entry.ID})
	})
}

// controlWorkflowOffer answers a person's button on a workflow offer. Opening
// it re-reads everything the offer depended on (the message and its revision,
// the person's access to it, the room's consent, and the catalog entry and the
// person's right to start it) and, if all still hold, records that the person
// went to the workflow's start page. It never starts, drafts or submits
// anything: a submission is the person's own, on the workflow's page, under the
// workflow's own authority. "Not now" pauses the same offer for a day and
// "Never" stops it for good, per person and per workflow.
func (s *AgentUXAmbientService) controlWorkflowOffer(ctx context.Context, tx dbport.Tx, tenant, conversation, actor string, cmd AgentUXAmbientCommand, offer *AgentUXAmbientOffer) error {
	if offer.Scope != "PRIVATE" || offer.Person != actor || offer.Agent != AgentUXAmbientWorkflowAgent || offer.WorkflowID == "" {
		return ErrAgentUXAmbientDenied
	}
	switch cmd.Action {
	case "DISMISS", "NEVER":
		if offer.State == "DISMISSED" {
			return ErrAgentUXAmbientDenied
		}
		switch {
		case cmd.Action == "NEVER":
			if _, err := tx.Exec(ctx, `INSERT INTO agentux_ambient_workflow_mute(tenant_id,person_id,workflow_id,muted_until) VALUES($1,$2,$3,NULL) ON CONFLICT(tenant_id,person_id,workflow_id) DO UPDATE SET muted_until=NULL`, tenant, actor, offer.WorkflowID); err != nil {
				return err
			}
		case offer.State == "OFFERED" || offer.State == "ACCEPTED" || offer.State == "UNAVAILABLE":
			// A card whose message was edited or deleted shows as withdrawn; closing
			// it is not an answer to the offer, so it does not pause the workflow.
			var postRevision uint64
			var postGone bool
			if err := tx.QueryRow(ctx, `SELECT revision,tombstoned FROM chat_post WHERE tenant_id=$1 AND conversation_id=$2 AND id=$3`, tenant, conversation, offer.Source).Scan(&postRevision, &postGone); err != nil && !errors.Is(err, dbport.ErrNoRows) {
				return err
			} else if err != nil || postGone || postRevision != offer.SourceRevision {
				break
			}
			until := s.Now().UTC().Add(agentUXAmbientWorkflowNotNow)
			if _, err := tx.Exec(ctx, `INSERT INTO agentux_ambient_workflow_mute(tenant_id,person_id,workflow_id,muted_until) VALUES($1,$2,$3,$4) ON CONFLICT(tenant_id,person_id,workflow_id) DO UPDATE SET muted_until=CASE WHEN agentux_ambient_workflow_mute.muted_until IS NULL THEN NULL ELSE GREATEST(agentux_ambient_workflow_mute.muted_until,EXCLUDED.muted_until) END`, tenant, actor, offer.WorkflowID, until); err != nil {
				return err
			}
		}
		offer.State = "DISMISSED"
	case "OPEN":
		if offer.State == "ACCEPTED" {
			return nil
		}
		if offer.State != "OFFERED" || s.Workflows == nil {
			return ErrAgentUXAmbientDenied
		}
		current, deleted, err := agentUXAmbientWorkflowSourceNow(ctx, tx, tenant, conversation, offer.Source)
		if err != nil {
			return err
		}
		if deleted || current == nil || current.revision != offer.SourceRevision || current.author != actor {
			return ErrAgentUXAmbientDenied
		}
		if err = chatstore.AmbientChannelTaskSourceReadable(ctx, tx, tenant, tenant, conversation, actor, offer.Source); err != nil {
			return ErrAgentUXAmbientDenied
		}
		entries, err := s.Workflows.OfferableWorkflows(ctx, tenant, actor)
		if err != nil {
			return ErrAgentUXAmbientInvalid
		}
		found := false
		for _, entry := range entries {
			if entry.ID == offer.WorkflowID {
				offer.Title, found = entry.Name, true
				break
			}
		}
		offer.State = "ACCEPTED"
		if !found {
			offer.State, offer.Reason = "UNAVAILABLE", "workflow_unavailable"
		}
	default:
		return ErrAgentUXAmbientInvalid
	}
	offer.Revision++
	return agentUXAmbientSave(ctx, tx, *offer)
}

// agentUXAmbientWorkflowGrant is the workflow offers' row in a conversation's
// list of agents that read every message. It has no installation behind it, so
// the list the installations produce does not carry it.
func (s *AgentUXAmbientService) agentUXAmbientWorkflowGrant(ctx context.Context, tx dbport.Tx, tenant, conversation string, since time.Time) (AgentUXAmbientGrant, error) {
	g := AgentUXAmbientGrant{Agent: AgentUXAmbientWorkflowAgent, ConversationLimit: 25, DailyLimit: 100}
	err := tx.QueryRow(ctx, `SELECT g.enabled,g.conversation_limit,g.daily_limit,g.enabled AND ((SELECT count(*) FROM agentux_ambient_read r WHERE r.tenant_id=g.tenant_id AND r.agent_id=g.agent_id AND r.at>=$4 AND r.stage='READ' AND r.outcome<>'SCREENED')>=g.daily_limit OR (SELECT count(*) FROM agentux_ambient_read r WHERE r.tenant_id=g.tenant_id AND r.agent_id=g.agent_id AND r.conversation_id=g.conversation_id AND r.at>=$4 AND r.stage='READ' AND r.outcome<>'SCREENED')>=g.conversation_limit) FROM agentux_ambient_grant g WHERE g.tenant_id=$1 AND g.conversation_id=$2 AND g.agent_id=$3`, tenant, conversation, AgentUXAmbientWorkflowAgent, since).Scan(&g.Enabled, &g.ConversationLimit, &g.DailyLimit, &g.Paused)
	if err != nil && !errors.Is(err, dbport.ErrNoRows) {
		return g, err
	}
	return g, nil
}
