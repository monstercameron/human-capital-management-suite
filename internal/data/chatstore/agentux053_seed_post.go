package chatstore

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// SeedAgentMessage writes one dated message an agent authored into a public
// channel. It exists for one caller, the operator-invoked demo preparation,
// which lays down a short history of what agents did in the channel (an
// announcement, a birthday greeting, answers) with the dates they happened on;
// no request path calls it. The write takes the same server-owned path as an
// agent's public message, so the workspace's content guard and the channel's
// audience revision apply, and it is idempotent under key: a second run returns
// the message the first one wrote.
func (s *Store) SeedAgentMessage(ctx context.Context, tenantID, conversationID, agentID, body, key, parentID string, at time.Time) (Post, error) {
	if strings.TrimSpace(tenantID) == "" || strings.TrimSpace(conversationID) == "" || strings.TrimSpace(agentID) == "" || strings.TrimSpace(body) == "" || strings.TrimSpace(key) == "" || at.IsZero() {
		return Post{}, chat.ErrInvalidArgument
	}
	var revision int64
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		return tx.QueryRow(ctx, `SELECT audience_revision FROM chat_conversation WHERE tenant_id=$1 AND id=$2 AND kind='PUBLIC_CHANNEL' AND lifecycle='ACTIVE'`, tenantID, conversationID).Scan(&revision)
	})
	if errors.Is(err, dbport.ErrNoRows) {
		return Post{}, ErrNotMember
	}
	if err != nil {
		return Post{}, err
	}
	return s.sendPostRaw(ctx, SendRequest{TenantID: tenantID, ConversationID: conversationID, HomeTenantID: tenantID, AuthorID: agentID, ClientKey: key, Body: body, ParentID: parentID, TrustedAuthor: true, ExpectedAudienceRevision: uint64(revision), CreatedAt: at.UTC()})
}
