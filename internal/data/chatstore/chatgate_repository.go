package chatstore

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chat"
	"github.com/monstercameron/human-capital-management-suite/internal/collaboration/chatgate"
	"github.com/monstercameron/human-capital-management-suite/internal/data/dbport"
)

// GateMembershipPort performs the existing membership mutation and current
// channel eligibility check on this transaction, not on a second connection.
type GateMembershipPort func(context.Context, dbport.Tx, chatgate.Scope, chatgate.Actor, string, bool) error
type GateRepository struct {
	Store      *Store
	Membership GateMembershipPort
}

func (r *GateRepository) ListGateScopes(ctx context.Context, tenant string) ([]chatgate.Scope, error) {
	out := []chatgate.Scope{}
	if r == nil || r.Store == nil {
		return nil, chatgate.ErrUnavailable
	}
	e := r.Store.RunTenantTx(ctx, tenant, func(tx dbport.Tx) error {
		rows, e := tx.Query(ctx, `SELECT conversation_id FROM chat_gate_state WHERE tenant_id=$1 ORDER BY conversation_id`, tenant)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var id string
			if e = rows.Scan(&id); e != nil {
				return e
			}
			out = append(out, chatgate.Scope{Tenant: tenant, Conversation: id})
		}
		return rows.Err()
	})
	return out, e
}

type chatgateTransaction struct {
	tx         dbport.Tx
	scope      chatgate.Scope
	state      chatgate.State
	answers    []chatgate.Answer
	membership GateMembershipPort
}

func (t *chatgateTransaction) State() *chatgate.State      { return &t.state }
func (t *chatgateTransaction) Answers() *[]chatgate.Answer { return &t.answers }
func (t *chatgateTransaction) Membership(ctx context.Context, a chatgate.Actor, v string, admit bool) error {
	if t.membership == nil {
		return chatgate.ErrUnavailable
	}
	// The database fence sees the decision only inside this transaction.
	b, err := json.Marshal(t.state)
	if err != nil {
		return err
	}
	if _, err = t.tx.Exec(ctx, `INSERT INTO chat_gate_state(tenant_id,conversation_id,revision,state_json) VALUES($1,$2,$3,$4) ON CONFLICT(tenant_id,conversation_id) DO UPDATE SET revision=excluded.revision,state_json=excluded.state_json`, t.scope.Tenant, t.scope.Conversation, t.state.Gate.Revision, b); err != nil {
		return err
	}
	if e := t.membership(ctx, t.tx, t.scope, a, v, admit); e != nil {
		return e
	}
	_, e := t.tx.Exec(ctx, `INSERT INTO chat_gate_membership_basis(tenant_id,conversation_id,person_id,semantic_version,revision,active) SELECT $1,$2,$3,$4,coalesce(max(revision),0)+1,$5 FROM chat_gate_membership_basis WHERE tenant_id=$1 AND conversation_id=$2 AND person_id=$3`, t.scope.Tenant, t.scope.Conversation, a.Person, v, admit)
	return e
}
func (r *GateRepository) Transact(ctx context.Context, scope chatgate.Scope, fn func(chatgate.Transaction) error) error {
	if r == nil || r.Store == nil || scope.Tenant == "" || scope.Conversation == "" || fn == nil {
		return chatgate.ErrInvalid
	}
	return r.Store.RunTenantTx(ctx, scope.Tenant, func(tx dbport.Tx) error {
		var id string
		if e := tx.QueryRow(ctx, `SELECT id FROM chat_conversation WHERE tenant_id=$1 AND id=$2 FOR UPDATE`, scope.Tenant, scope.Conversation).Scan(&id); e != nil {
			if errors.Is(e, dbport.ErrNoRows) {
				return chatgate.ErrNotFound
			}
			return e
		}
		if err := fenceContextWrite(ctx, tx, scope.Tenant, scope.Conversation); err != nil {
			return err
		}
		t := &chatgateTransaction{tx: tx, scope: scope, membership: r.Membership}
		var payload []byte
		e := tx.QueryRow(ctx, `SELECT state_json FROM chat_gate_state WHERE tenant_id=$1 AND conversation_id=$2`, scope.Tenant, scope.Conversation).Scan(&payload)
		if e != nil && !errors.Is(e, dbport.ErrNoRows) {
			return e
		}
		if len(payload) > 0 {
			if e = json.Unmarshal(payload, &t.state); e != nil {
				return e
			}
		}
		rows, e := tx.Query(ctx, `SELECT submission_id,person_id,field_id,value_json,expires_at,legal_hold FROM chat_gate_answer WHERE tenant_id=$1 AND conversation_id=$2`, scope.Tenant, scope.Conversation)
		if e != nil {
			return e
		}
		for rows.Next() {
			var a chatgate.Answer
			if e = rows.Scan(&a.SubmissionID, &a.Person, &a.FieldID, &a.Value, &a.ExpiresAt, &a.Held); e != nil {
				rows.Close()
				return e
			}
			t.answers = append(t.answers, a)
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return e
		}
		oldAnswers := append([]chatgate.Answer(nil), t.answers...)
		beforeState, err := json.Marshal(t.state)
		if err != nil {
			return err
		}
		beforeAnswers, err := json.Marshal(t.answers)
		if err != nil {
			return err
		}
		oldVersions := len(t.state.Gate.Versions)
		oldAudits := len(t.state.Audits)
		oldEvents := len(t.state.Events)
		if e = fn(t); e != nil {
			return e
		}
		afterState, err := json.Marshal(t.state)
		if err != nil {
			return err
		}
		afterAnswers, err := json.Marshal(t.answers)
		if err != nil {
			return err
		}
		if bytes.Equal(beforeState, afterState) && bytes.Equal(beforeAnswers, afterAnswers) {
			return nil
		}
		if len(t.state.Gate.Versions) < oldVersions || len(t.state.Audits) < oldAudits || len(t.state.Events) < oldEvents {
			return chatgate.ErrConflict
		}
		for _, sub := range t.state.Submissions {
			b, e := json.Marshal(sub)
			if e != nil {
				return e
			}
			if _, e = tx.Exec(ctx, `INSERT INTO chat_gate_submission_revision(tenant_id,conversation_id,submission_id,revision,record_json) VALUES($1,$2,$3,$4,$5) ON CONFLICT(tenant_id,conversation_id,submission_id,revision) DO NOTHING`, scope.Tenant, scope.Conversation, sub.ID, sub.Revision, b); e != nil {
				return e
			}
		}
		for _, d := range t.state.Gate.Versions[oldVersions:] {
			b, e := json.Marshal(d)
			if e != nil {
				return e
			}
			if _, e = tx.Exec(ctx, `INSERT INTO chat_gate_version(tenant_id,conversation_id,semantic_version,digest,definition_json) VALUES($1,$2,$3,$4,$5)`, scope.Tenant, scope.Conversation, d.Version.String(), d.Digest, b); e != nil {
				return e
			}
		}
		keep := map[string]bool{}
		for _, a := range t.answers {
			keep[a.SubmissionID+"\x00"+a.FieldID] = true
			b := []byte(a.Value)
			if _, e = tx.Exec(ctx, `INSERT INTO chat_gate_answer(tenant_id,conversation_id,submission_id,person_id,field_id,value_json,expires_at,legal_hold) VALUES($1,$2,$3,$4,$5,$6,$7,$8) ON CONFLICT(tenant_id,conversation_id,submission_id,field_id) DO UPDATE SET value_json=excluded.value_json,expires_at=excluded.expires_at WHERE chat_gate_answer.submission_id LIKE 'draft:%' AND NOT chat_gate_answer.legal_hold`, scope.Tenant, scope.Conversation, a.SubmissionID, a.Person, a.FieldID, b, a.ExpiresAt, a.Held); e != nil {
				return e
			}
		}
		for _, a := range oldAnswers {
			if !keep[a.SubmissionID+"\x00"+a.FieldID] {
				if a.Held {
					return chatgate.ErrHeld
				}
				if _, e = tx.Exec(ctx, `DELETE FROM chat_gate_answer WHERE tenant_id=$1 AND conversation_id=$2 AND submission_id=$3 AND field_id=$4 AND NOT legal_hold`, scope.Tenant, scope.Conversation, a.SubmissionID, a.FieldID); e != nil {
					return e
				}
			}
		}
		for i, a := range t.state.Audits[oldAudits:] {
			b, e := json.Marshal(a)
			if e != nil {
				return e
			}
			if _, e = tx.Exec(ctx, `INSERT INTO chat_gate_read_audit(tenant_id,conversation_id,sequence,audit_json) VALUES($1,$2,$3,$4)`, scope.Tenant, scope.Conversation, oldAudits+i+1, b); e != nil {
				return e
			}
		}
		for _, event := range t.state.Events[oldEvents:] {
			b, e := json.Marshal(event)
			if e != nil {
				return e
			}
			if _, e = tx.Exec(ctx, `INSERT INTO chat_outbox(tenant_id,aggregate_id,event_type,payload) VALUES($1,$2,$3,$4)`, scope.Tenant, scope.Conversation, "gate."+event.Kind, b); e != nil {
				return e
			}
		}
		b, e := json.Marshal(t.state)
		if e != nil {
			return e
		}
		_, e = tx.Exec(ctx, `INSERT INTO chat_gate_state(tenant_id,conversation_id,revision,state_json) VALUES($1,$2,$3,$4) ON CONFLICT(tenant_id,conversation_id) DO UPDATE SET revision=excluded.revision,state_json=excluded.state_json`, scope.Tenant, scope.Conversation, t.state.Gate.Revision, b)
		return e
	})
}

// GateMembership shares the existing membership event and audience transaction.
func (s *Adapter) GateMembership(ctx context.Context, tx dbport.Tx, scope chatgate.Scope, actor chatgate.Actor, _ string, admit bool) error {
	if scope.Tenant != actor.Tenant || actor.Person == "" {
		return chatgate.ErrDenied
	}
	if err := fenceContextWrite(ctx, tx, scope.Tenant, scope.Conversation); err != nil {
		return err
	}
	principal := chat.Principal{TenantID: actor.Tenant, SubjectID: actor.Person}
	if admit {
		_, err := putMembershipTx(ctx, tx, principal, chat.Membership{TenantID: scope.Tenant, ConversationID: scope.Conversation, HomeTenantID: actor.Tenant, SubjectID: actor.Person, Role: chat.Member, HistoryVisibility: chat.FullHistory})
		return err
	}
	var revision uint64
	if err := tx.QueryRow(ctx, `SELECT revision FROM chat_membership WHERE tenant_id=$1 AND conversation_id=$2 AND home_tenant_id=$3 AND member_id=$4 AND state='active' AND left_at IS NULL FOR UPDATE`, scope.Tenant, scope.Conversation, actor.Tenant, actor.Person).Scan(&revision); errors.Is(err, dbport.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	_, err := removeMembershipTx(ctx, tx, principal, scope.Tenant, scope.Conversation, actor.Tenant, actor.Person, revision)
	return err
}
