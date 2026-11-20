package chatstore

import (
	"context"
	"strconv"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Issue 3818: tell an applicant what a reviewer decided about their request.
// The notice is a moderation notice addressed to the applicant alone, in the
// applicant's own workspace; the decline reason is the one the reviewer gave
// them. The id is fixed by the request and its revision, so a replay is the
// same notice.

// NotifyDecision implements chatgate.DecisionNotifier.
func (r *GateRepository) NotifyDecision(ctx context.Context, n chatgate.DecisionNotice) error {
	if r == nil || r.Store == nil {
		return chatgate.ErrUnavailable
	}
	if n.Scope.Tenant == "" || n.Scope.Conversation == "" || n.SubmissionID == "" || n.Person == "" {
		return chatgate.ErrInvalid
	}
	outcome := chat.ModerationOutcomeGateDeclined
	if n.Admitted {
		outcome = chat.ModerationOutcomeGateAdmitted
	}
	reason := n.Reason
	if len([]rune(reason)) > 2000 {
		reason = string([]rune(reason)[:2000])
	}
	id := "gate-decision:" + n.SubmissionID + ":" + strconv.FormatUint(n.Revision, 10)
	return r.Store.RunTenantTx(ctx, n.Scope.Tenant, func(tx dbport.Tx) error {
		_, err := tx.Exec(ctx, `INSERT INTO chat_moderation_notice(tenant_id,id,conversation_id,post_id,home_tenant_id,subject_id,reason,outcome,at_time)
SELECT $1,$2,$3,'',$1,$4,$5,$6,now()
WHERE EXISTS (SELECT 1 FROM chat_conversation cv WHERE cv.tenant_id=$1 AND cv.id=$3 AND cv.lifecycle<>'ARCHIVED')
ON CONFLICT DO NOTHING`, n.Scope.Tenant, id, n.Scope.Conversation, n.Person, reason, outcome)
		return err
	})
}

var _ chatgate.DecisionNotifier = (*GateRepository)(nil)
