package chatstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatrecords"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// Issue 3812: the binding of a conversation to a governed case. Every statement
// runs in the tenant transaction (row-level security backs the tenant
// predicate), and binding or lifting is written to the audit log in the same
// transaction, so a binding never changes without a record of who changed it.
// A binding is never deleted: lifting it stamps who and when.

// MaxCaseRefRunes bounds an opaque case reference.
const MaxCaseRefRunes = 120

var _ chat.CaseBindingPort = (*Store)(nil)

// ActiveCase returns the binding in force for a conversation of the tenant.
func (s *Store) ActiveCase(ctx context.Context, tenantID, conversationID string) (chat.CaseBinding, bool, error) {
	if tenantID == "" || conversationID == "" {
		return chat.CaseBinding{}, false, chat.ErrInvalidArgument
	}
	var out chat.CaseBinding
	var found bool
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var err error
		out, found, err = activeCaseIn(ctx, tx, tenantID, conversationID)
		return err
	})
	return out, found, err
}

func activeCaseIn(ctx context.Context, tx dbport.Tx, tenantID, conversationID string) (chat.CaseBinding, bool, error) {
	var b chat.CaseBinding
	err := tx.QueryRow(ctx, `SELECT conversation_id,case_ref,owner_id,restricted,bound_by,bound_at FROM chat_conversation_case WHERE tenant_id=$1 AND conversation_id=$2 AND unbound_at IS NULL`, tenantID, conversationID).
		Scan(&b.ConversationID, &b.CaseRef, &b.OwnerID, &b.Restricted, &b.BoundBy, &b.BoundAt)
	if errors.Is(err, dbport.ErrNoRows) {
		return chat.CaseBinding{}, false, nil
	}
	if err != nil {
		return chat.CaseBinding{}, false, err
	}
	b.TenantID, b.BoundAt = tenantID, b.BoundAt.UTC()
	return b, true, nil
}

// ConversationCaseBound reports, inside a transaction the caller already holds,
// whether a case is bound to the conversation. The join-request approval path
// writes membership in its own transaction and asks here.
func ConversationCaseBound(ctx context.Context, tx dbport.Tx, tenantID, conversationID string) (bool, error) {
	_, found, err := activeCaseIn(ctx, tx, tenantID, conversationID)
	return found, err
}

// BindCase binds the conversation to a case, replacing a binding already in
// force (which is lifted in the same transaction, so a change of owner or of
// the restricted flag leaves both rows and both audit events). The conversation
// must exist in the tenant, and the owner must be an active member of it.
func (s *Store) BindCase(ctx context.Context, tenantID, id string, b chat.CaseBinding) (chat.CaseBinding, error) {
	b.CaseRef = strings.TrimSpace(b.CaseRef)
	if tenantID == "" || id == "" || b.ConversationID == "" || b.CaseRef == "" || len([]rune(b.CaseRef)) > MaxCaseRefRunes || strings.TrimSpace(b.OwnerID) == "" || b.BoundBy == "" {
		return chat.CaseBinding{}, chat.ErrInvalidArgument
	}
	err := s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var kind string
		if err := tx.QueryRow(ctx, `SELECT kind FROM chat_conversation WHERE tenant_id=$1 AND id=$2`, tenantID, b.ConversationID).Scan(&kind); err != nil {
			if errors.Is(err, dbport.ErrNoRows) {
				return chat.ErrNotFound
			}
			return err
		}
		if kind == string(chat.PublicChannel) {
			// A public channel is open to the workspace by design; a case-bound
			// conversation is a private one.
			return chat.ErrInvalidArgument
		}
		var member int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$1 AND member_id=$3 AND state='active' AND left_at IS NULL`, tenantID, b.ConversationID, b.OwnerID).Scan(&member); err != nil {
			return err
		}
		if member == 0 {
			return chat.ErrInvalidArgument
		}
		if previous, found, err := activeCaseIn(ctx, tx, tenantID, b.ConversationID); err != nil {
			return err
		} else if found {
			if _, err := tx.Exec(ctx, `UPDATE chat_conversation_case SET unbound_at=now(), unbound_by=$3 WHERE tenant_id=$1 AND conversation_id=$2 AND unbound_at IS NULL`, tenantID, b.ConversationID, b.BoundBy); err != nil {
				return err
			}
			if err := caseAudit(ctx, tx, tenantID, b.BoundBy, b.ConversationID, "chat.case.unbind", "replaced: "+previous.CaseRef); err != nil {
				return err
			}
		}
		if err := tx.QueryRow(ctx, `INSERT INTO chat_conversation_case(tenant_id,id,conversation_id,case_ref,owner_id,restricted,bound_by) VALUES($1,$2,$3,$4,$5,$6,$7) RETURNING bound_at`,
			tenantID, id, b.ConversationID, b.CaseRef, b.OwnerID, b.Restricted, b.BoundBy).Scan(&b.BoundAt); err != nil {
			return err
		}
		return caseAudit(ctx, tx, tenantID, b.BoundBy, b.ConversationID, "chat.case.bind", fmt.Sprintf("%s; owner %s; restricted %t", b.CaseRef, b.OwnerID, b.Restricted))
	})
	if err != nil {
		return chat.CaseBinding{}, err
	}
	b.TenantID, b.BoundAt = tenantID, b.BoundAt.UTC()
	return b, nil
}

// UnbindCase lifts the binding in force. A conversation with none is
// chat.ErrNotFound.
func (s *Store) UnbindCase(ctx context.Context, tenantID, conversationID, actor string) error {
	if tenantID == "" || conversationID == "" || actor == "" {
		return chat.ErrInvalidArgument
	}
	return s.RunTenantTx(ctx, tenantID, func(tx dbport.Tx) error {
		var ref string
		err := tx.QueryRow(ctx, `UPDATE chat_conversation_case SET unbound_at=now(), unbound_by=$3 WHERE tenant_id=$1 AND conversation_id=$2 AND unbound_at IS NULL RETURNING case_ref`, tenantID, conversationID, actor).Scan(&ref)
		if errors.Is(err, dbport.ErrNoRows) {
			return chat.ErrNotFound
		}
		if err != nil {
			return err
		}
		return caseAudit(ctx, tx, tenantID, actor, conversationID, "chat.case.unbind", ref)
	})
}

// caseAudit appends the change to the workspace's audit log, serialised with
// the other audit writers of the tenant so the sequence has no gaps.
func caseAudit(ctx context.Context, tx dbport.Tx, tenantID, actor, conversationID, action, reason string) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,0))`, "chat-audit:"+tenantID); err != nil {
		return err
	}
	var sequence uint64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(max(sequence),0)+1 FROM chat_audit_event WHERE tenant_id=$1`, tenantID).Scan(&sequence); err != nil {
		return err
	}
	e := chatrecords.AuditEvent{TenantID: tenantID, EventID: fmt.Sprintf("case:%s:%d", conversationID, sequence), Sequence: sequence, ActorID: actor, Action: action, TargetType: "chat_case", TargetID: conversationID, Reason: reason, PolicyEvidence: "case:" + tenantID, At: time.Now().UTC()}
	_, err := tx.Exec(ctx, `INSERT INTO chat_audit_event(tenant_id,event_id,sequence,actor_id,action,target_type,target_id,prior_revision,reason,policy_evidence,at_time,digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, e.TenantID, e.EventID, e.Sequence, e.ActorID, e.Action, e.TargetType, e.TargetID, e.PriorRevision, e.Reason, e.PolicyEvidence, e.At, chatrecords.DigestEvent(e))
	return err
}
