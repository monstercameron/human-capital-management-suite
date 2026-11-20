package chatstore

import (
	"context"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Issue 3817: tell the people who decide a join request that one is waiting.
// The notice is a moderation notice, the private kind every person already
// reads on the Moderation page and is counted for in the sidebar. It names the
// channel and the level; it holds no applicant, no answer and no words.
//
// Who is told, always limited to this workspace and to people who may know the
// channel exists:
//   - new and overdue: the channel's active managers;
//   - escalated: those managers, and the workspace administrators who can read
//     the channel (it is public, or they are in it). An administrator outside a
//     private channel is not told it exists.
//
// The applicant is never told about their own request. The notice id is fixed by
// the request and the level, so a replay or a repeated sweep is the same notice.

func gateReviewOutcome(level string) (string, bool) {
	switch level {
	case chatgate.ReviewNoticeNew:
		return chat.ModerationOutcomeGateReview, true
	case chatgate.ReviewNoticeOverdue:
		return chat.ModerationOutcomeGateOverdue, true
	case chatgate.ReviewNoticeEscalated:
		return chat.ModerationOutcomeGateEscalated, true
	}
	return "", false
}

// NotifyReview implements chatgate.ReviewNotifier.
func (r *GateRepository) NotifyReview(ctx context.Context, n chatgate.ReviewNotice) error {
	if r == nil || r.Store == nil {
		return chatgate.ErrUnavailable
	}
	outcome, ok := gateReviewOutcome(n.Level)
	if !ok || n.Scope.Tenant == "" || n.Scope.Conversation == "" || n.SubmissionID == "" {
		return chatgate.ErrInvalid
	}
	id := "gate-review:" + n.Level + ":" + n.SubmissionID
	return r.Store.RunTenantTx(ctx, n.Scope.Tenant, func(tx dbport.Tx) error {
		t, c := n.Scope.Tenant, n.Scope.Conversation
		_, err := tx.Exec(ctx, `INSERT INTO chat_moderation_notice(tenant_id,id,conversation_id,post_id,home_tenant_id,subject_id,reason,outcome,at_time)
SELECT $1,$2,$3,'',$1,r.who,'',$4,now()
FROM (
 SELECT m.member_id AS who FROM chat_membership m
 WHERE m.tenant_id=$1 AND m.conversation_id=$3 AND m.home_tenant_id=$1 AND m.role='MANAGER' AND m.state='active'
 UNION
 SELECT w.subject_id FROM chat_moderation_role w
 WHERE $6 AND w.tenant_id=$1 AND w.role='WORKSPACE_ADMIN' AND EXISTS (
  SELECT 1 FROM chat_conversation cv WHERE cv.tenant_id=$1 AND cv.id=$3 AND (cv.kind='PUBLIC_CHANNEL' OR EXISTS (
   SELECT 1 FROM chat_membership x WHERE x.tenant_id=$1 AND x.conversation_id=$3 AND x.home_tenant_id=$1 AND x.member_id=w.subject_id AND x.state='active')))
) r
WHERE r.who<>$5 AND EXISTS (SELECT 1 FROM chat_conversation cv WHERE cv.tenant_id=$1 AND cv.id=$3 AND cv.lifecycle<>'ARCHIVED')
ON CONFLICT DO NOTHING`, t, id, c, outcome, n.Person, n.Level == chatgate.ReviewNoticeEscalated)
		return err
	})
}

var _ chatgate.ReviewNotifier = (*GateRepository)(nil)
