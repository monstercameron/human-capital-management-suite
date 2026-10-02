package application

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

type agentUXAmbientCandidate struct {
	input               AgentUXAmbientModelInput
	installationVersion uint64
}

// Reservation and authorized source reads finish before the model is called.
// A crashed reserved read is not automatically retried: another source edit or
// an explicit new request is required. This prevents paid-call/agent loops.
func (s *AgentUXAmbientService) agentUXAmbientCandidate(ctx context.Context, tenant, conversation, postID, agent, zone string) (*agentUXAmbientCandidate, error) {
	var candidate *agentUXAmbientCandidate
	err := s.DB.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, tenant+":ambient:"+agent); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `SELECT 1 FROM chat_conversation WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, tenant, conversation); err != nil {
			return err
		}
		var deleted bool
		err := tx.QueryRow(ctx, `SELECT tombstoned FROM chat_post WHERE tenant_id=$1 AND conversation_id=$2 AND id=$3`, tenant, conversation, postID).Scan(&deleted)
		if errors.Is(err, dbport.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if deleted {
			return s.cancelSource(ctx, tx, tenant, conversation, postID, "")
		}
		var enabled bool
		var conversationLimit, dailyLimit int
		var enabledAt time.Time
		var version uint64
		err = tx.QueryRow(ctx, `SELECT g.enabled,g.conversation_limit,g.daily_limit,g.enabled_at,i.version FROM agentux_ambient_grant g JOIN chat_app_installation i ON i.tenant_id=g.tenant_id AND i.conversation_id=g.conversation_id AND i.app_id=g.agent_id JOIN chat_conversation c ON c.tenant_id=g.tenant_id AND c.id=g.conversation_id WHERE g.tenant_id=$1 AND g.conversation_id=$2 AND g.agent_id=$3 AND i.status='ACTIVE' AND i.version=g.installation_version AND 'chat.posts.read'=ANY(i.granted_scopes) AND c.kind<>'DIRECT' AND c.lifecycle='ACTIVE' FOR SHARE OF g,i,c`, tenant, conversation, agent).Scan(&enabled, &conversationLimit, &dailyLimit, &enabledAt, &version)
		if errors.Is(err, dbport.ErrNoRows) || err == nil && !enabled {
			return nil
		}
		if err != nil {
			return err
		}
		m := AgentUXAmbientMessage{Tenant: tenant, Conversation: conversation, ID: postID, Zone: zone}
		var created time.Time
		err = tx.QueryRow(ctx, `SELECT p.author_id,p.revision,p.created_at FROM chat_post p JOIN chat_membership m ON m.tenant_id=p.tenant_id AND m.conversation_id=p.conversation_id AND m.member_id=p.author_id AND m.home_tenant_id=p.author_home_tenant_id AND m.state='active' AND m.left_at IS NULL WHERE p.tenant_id=$1 AND p.conversation_id=$2 AND p.id=$3 AND p.author_home_tenant_id=$1 AND NOT p.tombstoned AND p.source_attribution IS NULL AND NOT EXISTS(SELECT 1 FROM chat_app_installation i WHERE i.tenant_id=p.tenant_id AND i.app_id=p.author_id) AND NOT EXISTS(SELECT 1 FROM agentux_ambient_optout o WHERE o.tenant_id=p.tenant_id AND o.conversation_id=p.conversation_id AND o.person_id=p.author_id AND o.opted_out) FOR SHARE OF p,m`, tenant, conversation, postID).Scan(&m.Author, &m.Revision, &created)
		if errors.Is(err, dbport.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if created.Before(enabledAt) {
			return nil
		}
		if err = s.invalidateAcceptedSource(ctx, tx, tenant, conversation, postID, agent, m.Revision); err != nil {
			return err
		}
		var already, count, dayCount, channelCount int
		now := s.Now().UTC()
		start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		err = tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE post_id=$4 AND revision=$5),count(*) FILTER(WHERE post_id=$4),count(*) FILTER(WHERE at>=$6 AND outcome<>'SCREENED'),count(*) FILTER(WHERE at>=$6 AND conversation_id=$2 AND outcome<>'SCREENED') FROM agentux_ambient_read WHERE tenant_id=$1 AND agent_id=$3 AND stage='READ'`, tenant, conversation, agent, postID, m.Revision, start).Scan(&already, &count, &dayCount, &channelCount)
		if err != nil {
			return err
		}
		if already > 0 || count >= 2 || dayCount >= dailyLimit || channelCount >= conversationLimit {
			return nil
		}
		var references []byte
		if err = tx.QueryRow(ctx, `SELECT body,parent_id,references_json FROM chat_post WHERE tenant_id=$1 AND conversation_id=$2 AND id=$3`, tenant, conversation, postID).Scan(&m.Body, &m.Parent, &references); err != nil {
			return err
		}
		if !AgentUXAmbientScreen(m.Body, agent) {
			if _, err = tx.Exec(ctx, `INSERT INTO agentux_ambient_read(tenant_id,conversation_id,agent_id,post_id,revision,at,outcome) VALUES($1,$2,$3,$4,$5,$6,'SCREENED')`, tenant, conversation, agent, postID, m.Revision, now); err != nil {
				return err
			}
			if m.Revision > 1 {
				return s.cancelSource(ctx, tx, tenant, conversation, postID, agent)
			}
			return nil
		}
		var refs []struct{ Kind, ID string }
		if json.Unmarshal(references, &refs) != nil {
			return ErrAgentUXAmbientInvalid
		}
		for _, ref := range refs {
			if ref.Kind == "PERSON_MENTION" || ref.Kind == "MEMBER_MENTION" || ref.Kind == "USER_MENTION" {
				m.Mentions = append(m.Mentions, ref.ID)
			}
		}
		var parent, readParentID string
		if m.Parent != "" {
			err = tx.QueryRow(ctx, `SELECT p.body FROM chat_post p JOIN chat_membership m ON m.tenant_id=p.tenant_id AND m.conversation_id=p.conversation_id AND m.member_id=p.author_id AND m.home_tenant_id=p.author_home_tenant_id AND m.state='active' AND m.left_at IS NULL WHERE p.tenant_id=$1 AND p.conversation_id=$2 AND p.id=$3 AND p.author_home_tenant_id=$1 AND NOT p.tombstoned AND p.source_attribution IS NULL AND NOT EXISTS(SELECT 1 FROM chat_app_installation i WHERE i.tenant_id=p.tenant_id AND i.app_id=p.author_id) AND NOT EXISTS(SELECT 1 FROM agentux_ambient_optout o WHERE o.tenant_id=p.tenant_id AND o.conversation_id=p.conversation_id AND o.person_id=p.author_id AND o.opted_out) FOR SHARE OF p,m`, tenant, conversation, m.Parent).Scan(&parent)
			if err != nil && !errors.Is(err, dbport.ErrNoRows) {
				return err
			}
			if err == nil {
				readParentID = m.Parent
			}
		}
		m.Parent = readParentID
		if _, err = tx.Exec(ctx, `INSERT INTO agentux_ambient_read(tenant_id,conversation_id,agent_id,post_id,revision,parent_id,at,outcome) VALUES($1,$2,$3,$4,$5,$6,$7,'RESERVED')`, tenant, conversation, agent, postID, m.Revision, readParentID, now); err != nil {
			return err
		}
		candidate = &agentUXAmbientCandidate{input: AgentUXAmbientModelInput{SourceKind: "CHAT_MESSAGE", Message: m, ThreadParent: parent, Untrusted: true}, installationVersion: version}
		return nil
	})
	return candidate, err
}
