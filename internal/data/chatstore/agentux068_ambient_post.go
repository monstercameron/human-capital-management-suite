package chatstore

import (
	"context"
	"errors"
	"strings"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// PostAmbientAgentMessage writes the message a Reminder agent posts in a public
// channel when a reminder someone set comes due (AGENTUX-068). The agent is not
// a member of the channel; it is installed in it with the read scope its
// "Reads every message here" switch needs, and that installation is the
// authority for this one write. The post takes the same server-owned path as an
// agent's other public message (the trusted author, the workspace's content
// guard, the conversation's audience revision) and is idempotent under key, so
// a retried delivery posts once. A conversation that is not a public channel, or
// one the agent is not installed in, is refused.
func (s *Store) PostAmbientAgentMessage(ctx context.Context, tenantID, conversationID, agentID, body, key string) (Post, error) {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(conversationID) == "" || strings.TrimSpace(agentID) == "" || strings.TrimSpace(body) == "" || strings.TrimSpace(key) == "" {
		return Post{}, chat.ErrInvalidArgument
	}
	var revision int64
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT c.audience_revision FROM chat_conversation c WHERE c.tenant_id=$1 AND c.id=$2 AND c.kind='PUBLIC_CHANNEL' AND c.lifecycle='ACTIVE' AND EXISTS(SELECT 1 FROM chat_app_installation i WHERE i.tenant_id=c.tenant_id AND i.conversation_id=c.id AND i.app_id=$3 AND i.status='ACTIVE' AND 'chat.posts.read'=ANY(i.granted_scopes))`, tenantID, conversationID, agentID).Scan(&revision)
	})
	if errors.Is(err, dbport.ErrNoRows) {
		return Post{}, ErrNotMember
	}
	if err != nil {
		return Post{}, err
	}
	return s.sendPostRaw(ctx, SendRequest{TenantID: tenantID, ConversationID: conversationID, HomeTenantID: tenantID, AuthorID: agentID, ClientKey: key, Body: body, TrustedAuthor: true, ExpectedAudienceRevision: uint64(revision)})
}
